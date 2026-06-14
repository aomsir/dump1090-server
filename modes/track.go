// Package modes implements Mode S message decoding and aircraft tracking.
//
// track.go: aircraft state tracking
//
// This is a translation from dump1090-mutability's track.c.
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// and Copyright (C) 2012 Salvatore Sanfilippo <antirez@gmail.com>
// Licensed under GPL v2+ / BSD
package modes

import (
	"math"
	"sync"
	"time"
)

// Tracker manages the state of all tracked aircraft.
type Tracker struct {
	mu sync.RWMutex

	aircraft map[uint32]*Aircraft // Map from ICAO address to aircraft

	// Receiver location (for CPR decoding and range checks)
	receiverLat    float64
	receiverLon    float64
	receiverLatLon bool // True if receiver location is valid
	maxRange       float64 // Maximum range in meters (0 = no limit)

	// Statistics collector (optional)
	statsCollector *StatsCollector

	// Statistics (local counters, kept for GetStats)
	stats struct {
		uniqueAircraft           int64
		singleMessageAircraft    int64
		cprGlobalOK              int64
		cprGlobalBad             int64
		cprGlobalSkipped         int64
		cprGlobalRangeChecks     int64
		cprGlobalSpeedChecks     int64
		cprLocalOK               int64
		cprLocalAircraftRelative int64
		cprLocalReceiverRelative int64
		cprLocalSkipped          int64
		cprLocalRangeChecks      int64
		cprLocalSpeedChecks      int64
		cprAirborne              int64
		cprSurface               int64
	}
}

// NewTracker creates a new aircraft tracker.
func NewTracker() *Tracker {
	return &Tracker{
		aircraft: make(map[uint32]*Aircraft),
	}
}

// SetStatsCollector sets the statistics collector.
func (t *Tracker) SetStatsCollector(sc *StatsCollector) {
	t.statsCollector = sc
}

// SetReceiverLocation sets the receiver's location for CPR decoding and range checks.
func (t *Tracker) SetReceiverLocation(lat, lon float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.receiverLat = lat
	t.receiverLon = lon
	t.receiverLatLon = true
}

// SetMaxRange sets the maximum range in meters (0 = no limit).
func (t *Tracker) SetMaxRange(maxRange float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.maxRange = maxRange
}

// GetAircraft returns a copy of the aircraft with the given address, or nil if not found.
func (t *Tracker) GetAircraft(addr uint32) *Aircraft {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if a, ok := t.aircraft[addr]; ok {
		// Return a copy to avoid race conditions
		copy := *a
		return &copy
	}
	return nil
}

// GetAllAircraft returns a snapshot of all tracked aircraft.
func (t *Tracker) GetAllAircraft() []*Aircraft {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]*Aircraft, 0, len(t.aircraft))
	for _, a := range t.aircraft {
		copy := *a
		result = append(result, &copy)
	}
	return result
}

// UpdateFromMessage updates aircraft state from a decoded message.
// Returns the updated aircraft.
func (t *Tracker) UpdateFromMessage(mm *Message) *Aircraft {
	now := uint64(time.Now().UnixMilli())

	t.mu.Lock()
	defer t.mu.Unlock()

	// Find or create aircraft
	a, exists := t.aircraft[mm.Addr]
	if !exists {
		a = t.createAircraft(mm)
		t.aircraft[mm.Addr] = a
		t.stats.uniqueAircraft++
	}

	// Update signal level
	if mm.SignalLevel > 0 {
		a.SignalLevel[a.SignalNext] = mm.SignalLevel
		a.SignalNext = (a.SignalNext + 1) & 7
	}

	a.Seen = now
	a.Messages++

	// Update address type (only move towards "more direct" types)
	if mm.AddrType < a.AddrType {
		a.AddrType = mm.AddrType
	}

	// Update barometric altitude
	if mm.AltitudeValid && mm.AltitudeSource == ALTITUDE_BARO && acceptData(&a.AltitudeValid, mm.Source, now) {
		modeC := uint32((a.Altitude + 49) / 100)
		if modeC != a.AltitudeModeC {
			a.ModeCCount = 0
			a.ModeACFlags &= ^MODEAC_MSG_MODEC_HIT
		}
		a.Altitude = mm.Altitude
		a.AltitudeModeC = modeC
	}

	// Update squawk
	if mm.SquawkValid && acceptData(&a.SquawkValid, mm.Source, now) {
		if mm.Squawk != a.Squawk {
			a.ModeACount = 0
			a.ModeACFlags &= ^MODEAC_MSG_MODEA_HIT
		}
		a.Squawk = mm.Squawk
	}

	// Update GNSS altitude
	if mm.AltitudeValid && mm.AltitudeSource == ALTITUDE_GNSS && acceptData(&a.AltitudeGNSSValid, mm.Source, now) {
		a.AltitudeGNSS = mm.Altitude
	}

	// Update GNSS delta
	if mm.GNSSDeltaValid && acceptData(&a.GNSSDeltaValid, mm.Source, now) {
		a.GNSSDelta = mm.GNSSDelta
	}

	// Update true heading
	if mm.HeadingValid && mm.HeadingSource == HEADING_TRUE && acceptData(&a.HeadingValid, mm.Source, now) {
		a.Heading = mm.Heading
	}

	// Update magnetic heading
	if mm.HeadingValid && mm.HeadingSource == HEADING_MAGNETIC && acceptData(&a.HeadingMagneticValid, mm.Source, now) {
		a.HeadingMagnetic = mm.Heading
	}

	// Update ground speed
	if mm.SpeedValid && mm.SpeedSource == SPEED_GROUNDSPEED && acceptData(&a.SpeedValid, mm.Source, now) {
		a.Speed = mm.Speed
	}

	// Update IAS
	if mm.SpeedValid && mm.SpeedSource == SPEED_IAS && acceptData(&a.SpeedIASValid, mm.Source, now) {
		a.SpeedIAS = mm.Speed
	}

	// Update TAS
	if mm.SpeedValid && mm.SpeedSource == SPEED_TAS && acceptData(&a.SpeedTASValid, mm.Source, now) {
		a.SpeedTAS = mm.Speed
	}

	// Update vertical rate
	if mm.VertRateValid && acceptData(&a.VertRateValid, mm.Source, now) {
		a.VertRate = mm.VertRate
		a.VertRateSource = mm.VertRateSource
	}

	// Update category
	if mm.CategoryValid && acceptData(&a.CategoryValid, mm.Source, now) {
		a.Category = mm.Category
	}

	// Update air/ground state
	if mm.AirGround != AG_INVALID && acceptData(&a.AirGroundValid, mm.Source, now) {
		a.AirGround = mm.AirGround
	}

	// Update callsign
	if mm.CallsignValid && acceptData(&a.CallsignValid, mm.Source, now) {
		copy(a.Callsign[:], mm.Callsign[:])
	}

	// Update CPR even frame
	if mm.CPRValid && !mm.CPROdd && acceptData(&a.CPREvenValid, mm.Source, now) {
		a.CPREvenType = mm.CPRType
		a.CPREvenLat = mm.CPRLat
		a.CPREvenLon = mm.CPRLon
		a.CPREvenNUC = mm.CPRNUCP
	}

	// Update CPR odd frame
	if mm.CPRValid && mm.CPROdd && acceptData(&a.CPROddValid, mm.Source, now) {
		a.CPROddType = mm.CPRType
		a.CPROddLat = mm.CPRLat
		a.CPROddLon = mm.CPRLon
		a.CPROddNUC = mm.CPRNUCP
	}

	// Derive GNSS altitude from baro + delta if more recent
	if compareValidity(&a.AltitudeValid, &a.AltitudeGNSSValid, now) > 0 &&
		compareValidity(&a.GNSSDeltaValid, &a.AltitudeGNSSValid, now) > 0 {
		a.AltitudeGNSS = a.Altitude + a.GNSSDelta
		combineValidity(&a.AltitudeGNSSValid, &a.AltitudeValid, &a.GNSSDeltaValid)
	}

	// Update position if we have new CPR data
	if mm.CPRValid {
		t.updatePosition(a, mm, now)
	}

	return a
}

// UpdateFromModeAC handles Mode A/C messages and attempts to match them
// with existing Mode S aircraft. Mode A/C messages have synthetic ICAO
// addresses and should not create new aircraft tracks.
// Returns nil (Mode A/C messages don't create independent tracks).
func (t *Tracker) UpdateFromModeAC(mm *Message) *Aircraft {
	now := uint64(time.Now().UnixMilli())

	t.mu.Lock()
	defer t.mu.Unlock()

	// Find or create the synthetic Mode A/C aircraft
	modeACAircraft, exists := t.aircraft[mm.Addr]
	if !exists {
		modeACAircraft = t.createAircraft(mm)
		modeACAircraft.ModeACFlags |= MODEAC_MSG_FLAG // Mark as synthetic
		t.aircraft[mm.Addr] = modeACAircraft
	}

	// Update signal level
	if mm.SignalLevel > 0 {
		modeACAircraft.SignalLevel[modeACAircraft.SignalNext] = mm.SignalLevel
		modeACAircraft.SignalNext = (modeACAircraft.SignalNext + 1) & 7
	}

	modeACAircraft.Seen = now
	modeACAircraft.Messages++

	// Update squawk
	if mm.SquawkValid && acceptData(&modeACAircraft.SquawkValid, mm.Source, now) {
		modeACAircraft.Squawk = mm.Squawk
	}

	// Update altitude
	if mm.AltitudeValid && mm.AltitudeSource == ALTITUDE_BARO && acceptData(&modeACAircraft.AltitudeValid, mm.Source, now) {
		modeACAircraft.Altitude = mm.Altitude
		modeACAircraft.AltitudeModeC = uint32((mm.Altitude + 49) / 100)
	}

	// Mode A/C decoded from RF always has at least MODE_S level data validity,
	// even though the Message Source field may be zero (DecodeModeAMessage does
	// not set it). Upstream dump1090-mutability does not gate Mode A/C matching
	// on DataValidity; promote the source so that dataValid() returns true for
	// squawk and altitude fields.
	if mm.SquawkValid && modeACAircraft.SquawkValid.Source == SOURCE_INVALID {
		modeACAircraft.SquawkValid.Source = SOURCE_MODE_S
	}
	if mm.AltitudeValid && modeACAircraft.AltitudeValid.Source == SOURCE_INVALID {
		modeACAircraft.AltitudeValid.Source = SOURCE_MODE_S
	}

	// Flag Mode A only (no altitude) if applicable
	if mm.SquawkValid && !mm.AltitudeValid {
		modeACAircraft.ModeACFlags |= MODEAC_MSG_MODEA_ONLY
	}

	// Clear hit flags for this matching attempt
	modeACAircraft.ModeACFlags &= ^(MODEAC_MSG_MODEA_HIT | MODEAC_MSG_MODEC_HIT | MODEAC_MSG_MODES_HIT)

	// Try to match with existing Mode S aircraft
	t.matchModeACWithModeS(modeACAircraft)

	// Handle MODEAC_MSG_MODEC_OLD flag (C version track.c:640-657)
	// If this Mode C used to match but doesn't anymore (aircraft changed altitude or left range),
	// it might be a new aircraft. Clear the OLD flag and reset message count.
	if modeACAircraft.ModeACFlags&(MODEAC_MSG_MODEC_HIT|MODEAC_MSG_MODEC_OLD) == MODEAC_MSG_MODEC_OLD {
		modeACAircraft.ModeACFlags &= ^MODEAC_MSG_MODEC_OLD
		modeACAircraft.Messages = 1
	}

	return nil // Don't return Mode A/C synthetic aircraft
}

// matchModeACWithModeS attempts to match a Mode A/C aircraft with Mode S aircraft
// based on squawk code (Mode A) and altitude (Mode C).
func (t *Tracker) matchModeACWithModeS(modeACAircraft *Aircraft) {
	// Iterate through all aircraft to find Mode S matches
	for _, modeS := range t.aircraft {
		// Skip Mode A/C synthetic records
		if modeS.ModeACFlags&MODEAC_MSG_FLAG != 0 {
			continue
		}

		// Mode A matching: check squawk codes
		if dataValid(&modeACAircraft.SquawkValid) && dataValid(&modeS.SquawkValid) {
			if modeACAircraft.Squawk == modeS.Squawk {
				// Found Mode A match
				modeS.ModeACount = modeACAircraft.Messages
				modeS.ModeACFlags |= MODEAC_MSG_MODEA_HIT
				modeACAircraft.ModeACFlags |= MODEAC_MSG_MODEA_HIT

				// If Mode S has both Mode A and Mode C hits, mark as matched
				if modeS.ModeACount > 0 &&
					(modeS.ModeCCount > 1 || (modeACAircraft.ModeACFlags&MODEAC_MSG_MODEA_ONLY) != 0) {
					modeACAircraft.ModeACFlags |= MODEAC_MSG_MODES_HIT
				}
			}
		}

		// Mode C matching: check altitude (within ±100ft)
		if dataValid(&modeACAircraft.AltitudeValid) && dataValid(&modeS.AltitudeValid) {
			// Match if within ±1 Mode C unit (100ft)
			if modeACAircraft.AltitudeModeC == modeS.AltitudeModeC ||
				modeACAircraft.AltitudeModeC == modeS.AltitudeModeC+1 ||
				modeACAircraft.AltitudeModeC+1 == modeS.AltitudeModeC {
				// Found Mode C match
				modeS.ModeCCount = modeACAircraft.Messages
				modeS.ModeACFlags |= MODEAC_MSG_MODEC_HIT
				modeACAircraft.ModeACFlags |= MODEAC_MSG_MODEC_HIT

				// If Mode S has both Mode A and Mode C hits, mark as matched
				if modeS.ModeACount > 0 && modeS.ModeCCount > 1 {
					modeACAircraft.ModeACFlags |= (MODEAC_MSG_MODES_HIT | MODEAC_MSG_MODEC_OLD)
				}
			}
		}
	}
}

// PeriodicUpdate performs periodic maintenance (cleanup, Mode A/C rematching).
// Should be called approximately once per second.
func (t *Tracker) PeriodicUpdate() {
	now := uint64(time.Now().UnixMilli())

	t.mu.Lock()
	defer t.mu.Unlock()

	// Remove stale aircraft
	for addr, a := range t.aircraft {
		age := now - a.Seen

		// Remove if too old, or if only one message and aged out
		if age > TRACK_AIRCRAFT_TTL || (a.Messages == 1 && age > TRACK_AIRCRAFT_ONEHIT_TTL) {
			if a.Messages == 1 {
				t.stats.singleMessageAircraft++
			}
			delete(t.aircraft, addr)
			continue
		}

		// Expire individual fields
		t.expireField(&a.CallsignValid, now)
		t.expireField(&a.AltitudeValid, now)
		t.expireField(&a.AltitudeGNSSValid, now)
		t.expireField(&a.GNSSDeltaValid, now)
		t.expireField(&a.SpeedValid, now)
		t.expireField(&a.SpeedIASValid, now)
		t.expireField(&a.SpeedTASValid, now)
		t.expireField(&a.HeadingValid, now)
		t.expireField(&a.HeadingMagneticValid, now)
		t.expireField(&a.VertRateValid, now)
		t.expireField(&a.SquawkValid, now)
		t.expireField(&a.CategoryValid, now)
		t.expireField(&a.AirGroundValid, now)
		t.expireField(&a.CPROddValid, now)
		t.expireField(&a.CPREvenValid, now)
		t.expireField(&a.PositionValid, now)
	}

	// Rematch Mode A/C records with Mode S aircraft.
	// This matches upstream dump1090-mutability track.c behavior where
	// PeriodicUpdate re-evaluates Mode A/C to Mode S correlations.
	// Clear hit flags on ALL aircraft before rematching so stale flags
	// from previous matches that no longer apply are removed.
	hitMask := ^(MODEAC_MSG_MODEA_HIT | MODEAC_MSG_MODEC_HIT | MODEAC_MSG_MODES_HIT)
	for _, a := range t.aircraft {
		a.ModeACFlags &= hitMask
	}
	for _, a := range t.aircraft {
		if a.ModeACFlags&MODEAC_MSG_FLAG != 0 {
			t.matchModeACWithModeS(a)
		}
	}
}

// createAircraft creates a new aircraft record.
func (t *Tracker) createAircraft(mm *Message) *Aircraft {
	a := &Aircraft{
		Addr:     mm.Addr,
		AddrType: mm.AddrType,
	}

	// Initialize signal levels to small non-zero value
	for i := range a.SignalLevel {
		a.SignalLevel[i] = 1e-5
	}

	// Initialize FATSV emitted message headers (matching C version track.c:76-77)
	a.FATSVEmittedBDS30[0] = 0x30
	a.FATSVEmittedESACASRA[0] = 0xE2

	// Store first message
	a.FirstMessage = *mm

	return a
}

// acceptData checks if new data should be accepted based on source priority and freshness.
// If accepted, updates the validity timestamps and returns true.
func acceptData(v *DataValidity, source DataSource, now uint64) bool {
	// Reject if we have better data that hasn't gone stale yet
	if source < v.Source && now < v.Stale {
		return false
	}

	// Accept and update validity
	v.Source = source
	v.Updated = now
	v.Stale = now + 60000  // 60 seconds
	v.Expires = now + 70000 // 70 seconds
	return true
}

// combineValidity combines validity from two sources.
func combineValidity(to *DataValidity, from1, from2 *DataValidity) {
	if from1.Source == SOURCE_INVALID {
		*to = *from2
		return
	}

	if from2.Source == SOURCE_INVALID {
		*to = *from1
		return
	}

	// Worst of the two sources
	if from1.Source < from2.Source {
		to.Source = from1.Source
	} else {
		to.Source = from2.Source
	}

	// Later of the two update times
	if from1.Updated > from2.Updated {
		to.Updated = from1.Updated
	} else {
		to.Updated = from2.Updated
	}

	// Earlier of the two stale times
	if from1.Stale < from2.Stale {
		to.Stale = from1.Stale
	} else {
		to.Stale = from2.Stale
	}

	// Earlier of the two expiry times
	if from1.Expires < from2.Expires {
		to.Expires = from1.Expires
	} else {
		to.Expires = from2.Expires
	}
}

// compareValidity compares two validity records.
// Returns 1 if lhs is better, -1 if rhs is better, 0 if equal.
func compareValidity(lhs, rhs *DataValidity, now uint64) int {
	if now < lhs.Stale && lhs.Source > rhs.Source {
		return 1
	} else if now < rhs.Stale && lhs.Source < rhs.Source {
		return -1
	} else if lhs.Updated > rhs.Updated {
		return 1
	} else if lhs.Updated < rhs.Updated {
		return -1
	}
	return 0
}

// expireField marks a field as invalid if it has expired.
func (t *Tracker) expireField(v *DataValidity, now uint64) {
	if v.Source != SOURCE_INVALID && now >= v.Expires {
		v.Source = SOURCE_INVALID
	}
}

// dataValid checks if data is valid.
func dataValid(v *DataValidity) bool {
	return v.Source != SOURCE_INVALID
}

// dataAge returns the age of the data in milliseconds.
func dataAge(v *DataValidity, now uint64) uint64 {
	if v.Source == SOURCE_INVALID {
		return ^uint64(0) // Maximum value
	}
	if v.Updated >= now {
		return 0
	}
	return now - v.Updated
}

// greatcircle calculates the great circle distance between two points in meters.
// Uses the haversine formula for accuracy at small distances.
func greatcircle(lat0, lon0, lat1, lon1 float64) float64 {
	// Convert to radians
	lat0Rad := lat0 * math.Pi / 180.0
	lon0Rad := lon0 * math.Pi / 180.0
	lat1Rad := lat1 * math.Pi / 180.0
	lon1Rad := lon1 * math.Pi / 180.0

	dlat := math.Abs(lat1Rad - lat0Rad)
	dlon := math.Abs(lon1Rad - lon0Rad)

	// Use haversine for small distances for better numerical stability
	if dlat < 0.001 && dlon < 0.001 {
		a := math.Sin(dlat/2)*math.Sin(dlat/2) +
			math.Cos(lat0Rad)*math.Cos(lat1Rad)*
			math.Sin(dlon/2)*math.Sin(dlon/2)
		return 6371e3 * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))
	}

	// Spherical law of cosines
	return 6371e3 * math.Acos(math.Sin(lat0Rad)*math.Sin(lat1Rad)+
		math.Cos(lat0Rad)*math.Cos(lat1Rad)*math.Cos(dlon))
}

// speedCheck validates that an aircraft could have travelled from its last known position
// to the new position in the elapsed time.
func (t *Tracker) speedCheck(a *Aircraft, lat, lon float64, now uint64, surface bool) bool {
	if !dataValid(&a.PositionValid) {
		return true // No reference, assume OK
	}

	elapsed := dataAge(&a.PositionValid, now)

	// Estimate speed
	var speed uint32
	if dataValid(&a.SpeedValid) {
		speed = a.Speed
	} else if dataValid(&a.SpeedIASValid) {
		speed = a.SpeedIAS * 4 / 3
	} else if dataValid(&a.SpeedTASValid) {
		speed = a.SpeedTAS * 4 / 3
	} else if surface {
		speed = 100
	} else {
		speed = 600
	}

	// Add margin: current speed * 4/3
	speed = speed * 4 / 3

	// Constrain speed to reasonable values
	if surface {
		if speed < 20 {
			speed = 20
		}
		if speed > 150 {
			speed = 150
		}
	} else {
		if speed < 200 {
			speed = 200
		}
	}

	// Calculate maximum range:
	// Base distance (100m surface, 500m airborne) + distance at speed for elapsed time + 1 second
	var baseDistance float64
	if surface {
		baseDistance = 0.1e3
	} else {
		baseDistance = 0.5e3
	}
	maxRange := baseDistance + ((float64(elapsed)+1000.0)/1000.0)*(float64(speed)*1852.0/3600.0)

	// Calculate actual distance
	distance := greatcircle(a.Lat, a.Lon, lat, lon)

	return distance <= maxRange
}

// timeBetween returns the absolute difference between two timestamps.
func timeBetween(t1, t2 uint64) uint64 {
	if t1 >= t2 {
		return t1 - t2
	}
	return t2 - t1
}

// updatePosition decodes CPR position and updates aircraft location.
func (t *Tracker) updatePosition(a *Aircraft, mm *Message, now uint64) {
	var maxElapsed uint64
	surface := (mm.CPRType == CPR_SURFACE)

	if surface {
		t.stats.cprSurface++
		// Surface: 25 seconds if >25kt or unknown, 50 seconds otherwise
		if mm.SpeedValid && mm.Speed <= 25 {
			maxElapsed = 50000
		} else {
			maxElapsed = 25000
		}
	} else {
		t.stats.cprAirborne++
		// Airborne: 10 seconds
		maxElapsed = 10000
	}

	var locationResult int = -1 // Must be -1 to match C version; 0 means success
	var newLat, newLon float64
	var newNUC uint32

	// Try global CPR if we have both even and odd frames
	if dataValid(&a.CPROddValid) && dataValid(&a.CPREvenValid) &&
		a.CPROddValid.Source == a.CPREvenValid.Source &&
		a.CPROddType == a.CPREvenType &&
		timeBetween(a.CPROddValid.Updated, a.CPREvenValid.Updated) <= maxElapsed {

		locationResult, newLat, newLon, newNUC = t.doGlobalCPR(a, mm, now, surface)

		if locationResult == -2 {
			// Bad data, discard both frames
			// (stats already recorded in doGlobalCPR for range/speed check)
			t.stats.cprGlobalBad++
			a.CPROddValid.Source = SOURCE_INVALID
			a.CPREvenValid.Source = SOURCE_INVALID
			a.PositionValid.Source = SOURCE_INVALID
			return
		} else if locationResult == -1 {
			// Can't decode yet, try local
			t.stats.cprGlobalSkipped++
			if t.statsCollector != nil {
				t.statsCollector.AddCPRResult(surface, true, false, "skipped", "")
			}
		} else {
			t.stats.cprGlobalOK++
			if t.statsCollector != nil {
				t.statsCollector.AddCPRResult(surface, true, true, "", "")
			}
			combineValidity(&a.PositionValid, &a.CPREvenValid, &a.CPROddValid)
		}
	}

	// Try local CPR if global failed
	if locationResult == -1 {
		var aircraftRelative bool
		locationResult, newLat, newLon, newNUC, aircraftRelative = t.doLocalCPR(a, mm, now, surface)

		if locationResult < 0 {
			t.stats.cprLocalSkipped++
			if t.statsCollector != nil {
				t.statsCollector.AddCPRResult(surface, false, false, "skipped", "")
			}
		} else {
			t.stats.cprLocalOK++
			// Track whether we used aircraft or receiver position
			relativeType := "receiver"
			if aircraftRelative {
				t.stats.cprLocalAircraftRelative++
				relativeType = "aircraft"
			} else {
				t.stats.cprLocalReceiverRelative++
			}
			if t.statsCollector != nil {
				t.statsCollector.AddCPRResult(surface, false, true, "", relativeType)
			}
			mm.CPRRelative = true

			if mm.CPROdd {
				a.PositionValid = a.CPROddValid
			} else {
				a.PositionValid = a.CPREvenValid
			}
		}
	}

	// Update aircraft state if successful
	if locationResult == 0 {
		mm.CPRDecoded = true
		mm.DecodedLat = newLat
		mm.DecodedLon = newLon

		a.Lat = newLat
		a.Lon = newLon
		a.PosNUC = newNUC

		// Update range histogram if receiver location is known
		if t.statsCollector != nil && t.receiverLatLon && t.maxRange > 0 {
			t.statsCollector.AddRangeHistogram(t.receiverLat, t.receiverLon, newLat, newLon, t.maxRange)
		}
	}
}

// doGlobalCPR attempts global CPR decoding.
// Returns: (result, lat, lon, nuc) where result is 0=success, -1=can't decode, -2=bad data.
func (t *Tracker) doGlobalCPR(a *Aircraft, mm *Message, now uint64, surface bool) (int, float64, float64, uint32) {
	fflag := mm.CPROdd

	// NUC is the worst of the two positions
	nuc := a.CPREvenNUC
	if a.CPROddNUC < nuc {
		nuc = a.CPROddNUC
	}

	var lat, lon float64
	var result int

	if surface {
		// Surface global CPR needs a reference location
		var reflat, reflon float64

		if dataValid(&a.PositionValid) && dataAge(&a.PositionValid, now) < 50000 {
			reflat = a.Lat
			reflon = a.Lon
			if a.PosNUC < nuc {
				nuc = a.PosNUC
			}
		} else if t.receiverLatLon {
			reflat = t.receiverLat
			reflon = t.receiverLon
		} else {
			return -1, 0, 0, 0 // No reference
		}

		lat, lon, result = DecodeCPRSurface(reflat, reflon,
			int(a.CPREvenLat), int(a.CPREvenLon),
			int(a.CPROddLat), int(a.CPROddLon),
			fflag)
	} else {
		// Airborne global CPR
		lat, lon, result = DecodeCPRAirborne(
			int(a.CPREvenLat), int(a.CPREvenLon),
			int(a.CPROddLat), int(a.CPROddLon),
			fflag)
	}

	if result < 0 {
		return result, 0, 0, 0
	}

	// Check maximum range
	if t.maxRange > 0 && t.receiverLatLon {
		distance := greatcircle(t.receiverLat, t.receiverLon, lat, lon)
		if distance > t.maxRange {
			t.stats.cprGlobalRangeChecks++
			if t.statsCollector != nil {
				t.statsCollector.AddCPRResult(surface, true, false, "range", "")
			}
			return -2, 0, 0, 0
		}
	}

	// Skip speed check for MLAT
	if mm.Source == SOURCE_MLAT {
		return result, lat, lon, nuc
	}

	// Check speed limit
	if dataValid(&a.PositionValid) && a.PosNUC >= nuc && !t.speedCheck(a, lat, lon, now, surface) {
		t.stats.cprGlobalSpeedChecks++
		if t.statsCollector != nil {
			t.statsCollector.AddCPRResult(surface, true, false, "speed", "")
		}
		return -2, 0, 0, 0
	}

	return result, lat, lon, nuc
}

// doLocalCPR attempts local (relative) CPR decoding.
// Returns: (result, lat, lon, nuc, aircraftRelative)
// where result is 0=success, -1=can't decode, and aircraftRelative indicates reference type.
func (t *Tracker) doLocalCPR(a *Aircraft, mm *Message, now uint64, surface bool) (int, float64, float64, uint32, bool) {
	fflag := mm.CPROdd
	nuc := mm.CPRNUCP

	var reflat, reflon float64
	var rangeLimit float64
	var aircraftRelative bool

	if dataValid(&a.PositionValid) && dataAge(&a.PositionValid, now) < 50000 {
		reflat = a.Lat
		reflon = a.Lon
		if a.PosNUC < nuc {
			nuc = a.PosNUC
		}
		rangeLimit = 50e3
		aircraftRelative = true // Using aircraft's own position
	} else if !surface && t.receiverLatLon {
		reflat = t.receiverLat
		reflon = t.receiverLon

		// Cell size is at least 360NM, giving nominal max range of 180NM
		if t.maxRange == 0 {
			return -1, 0, 0, 0, false
		} else if t.maxRange <= 1852*180 {
			rangeLimit = t.maxRange
		} else if t.maxRange < 1852*360 {
			rangeLimit = (1852 * 360) - t.maxRange
		} else {
			return -1, 0, 0, 0, false
		}
		aircraftRelative = false // Using receiver position
	} else {
		// DEBUG: Log why we have no reference
		// fmt.Printf("DEBUG doLocalCPR %06X: no reference! PositionValid=%v age=%d surface=%v receiverLatLon=%v\n",
		//     mm.Addr, dataValid(&a.PositionValid), dataAge(&a.PositionValid, now), surface, t.receiverLatLon)
		return -1, 0, 0, 0, false // No reference
	}

	lat, lon, result := DecodeCPRRelative(reflat, reflon,
		int(mm.CPRLat), int(mm.CPRLon),
		fflag, surface)

	if result < 0 {
		return result, 0, 0, 0, false
	}

	// Check range limit
	if rangeLimit > 0 {
		distance := greatcircle(reflat, reflon, lat, lon)
		if distance > rangeLimit {
			t.stats.cprLocalRangeChecks++
			if t.statsCollector != nil {
				t.statsCollector.AddCPRResult(surface, false, false, "range", "")
			}
			return -1, 0, 0, 0, false
		}
	}

	// Check speed limit
	if dataValid(&a.PositionValid) && a.PosNUC >= nuc && !t.speedCheck(a, lat, lon, now, surface) {
		t.stats.cprLocalSpeedChecks++
		if t.statsCollector != nil {
			t.statsCollector.AddCPRResult(surface, false, false, "speed", "")
		}
		return -1, 0, 0, 0, false
	}

	return 0, lat, lon, nuc, aircraftRelative
}
