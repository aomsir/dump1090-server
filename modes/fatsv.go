// Package modes implements FATSV (FlightAware TSV) output format.
//
// fatsv.go: FlightAware Tab-Separated Values output
//
// This is a translation from dump1090-mutability's net_io.c FATSV functions.
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// Licensed under GPL v2+
package modes

import (
	"bytes"
	"fmt"
	"time"
)

const (
	// TSV_MAX_PACKET_SIZE is the maximum size of a FATSV packet
	TSV_MAX_PACKET_SIZE = 1500

	// TISB flags for tracking TIS-B sourced data
	TISB_IDENT            = 1 << 0
	TISB_SQUAWK           = 1 << 1
	TISB_ALTITUDE         = 1 << 2
	TISB_ALTITUDE_GNSS    = 1 << 3
	TISB_SPEED            = 1 << 4
	TISB_SPEED_IAS        = 1 << 5
	TISB_SPEED_TAS        = 1 << 6
	TISB_POSITION         = 1 << 7
	TISB_HEADING          = 1 << 8
	TISB_HEADING_MAGNETIC = 1 << 9
	TISB_AIRGROUND        = 1 << 10
	TISB_CATEGORY         = 1 << 11
)

// FATSVWriter manages FATSV output
type FATSVWriter struct {
	tracker *Tracker
}

// NewFATSVWriter creates a new FATSV writer
func NewFATSVWriter(tracker *Tracker) *FATSVWriter {
	return &FATSVWriter{
		tracker: tracker,
	}
}

// WriteFATSVEvent writes a FATSV event message for specific message types.
// This handles immediate output for specific interesting messages.
// Event gating (deduplication) and state updates are handled atomically by the
// tracker to avoid race conditions.
func (w *FATSVWriter) WriteFATSVEvent(mm *Message) []byte {
	if mm == nil {
		return nil
	}

	// DF20/21 Comm-B: check for BDS 1,0 or BDS 3,0
	if mm.MsgType == 20 || mm.MsgType == 21 {
		if len(mm.MB) >= 7 {
			// BDS 1,0 (datalink capabilities)
			if mm.MB[0] == 0x10 {
				var data [7]byte
				copy(data[:], mm.MB[:7])
				if w.tracker.CheckAndMarkFATSVEvent(mm.Addr, "bds10", data) {
					return w.writeFATSVEventMessage(mm, "datalink_caps", mm.MB[:7])
				}
			}
			// BDS 3,0 (ACAS RA)
			if mm.MB[0] == 0x30 {
				var data [7]byte
				copy(data[:], mm.MB[:7])
				if w.tracker.CheckAndMarkFATSVEvent(mm.Addr, "bds30", data) {
					return w.writeFATSVEventMessage(mm, "commb_acas_ra", mm.MB[:7])
				}
			}
		}
	}

	// DF17/18 Extended Squitter: check for specific ME types
	if mm.MsgType == 17 || mm.MsgType == 18 {
		if len(mm.ME) >= 7 {
			// ME type 28 subtype 2: ACAS RA broadcast
			if mm.METype == 28 && mm.MESub == 2 {
				var data [7]byte
				copy(data[:], mm.ME[:7])
				if w.tracker.CheckAndMarkFATSVEvent(mm.Addr, "es_acas_ra", data) {
					return w.writeFATSVEventMessage(mm, "es_acas_ra", mm.ME[:7])
				}
			}
			// ME type 31 subtype 0/1: operational status
			if mm.METype == 31 && (mm.MESub == 0 || mm.MESub == 1) {
				var data [7]byte
				copy(data[:], mm.ME[:7])
				if w.tracker.CheckAndMarkFATSVEvent(mm.Addr, "es_op_status", data) {
					return w.writeFATSVEventMessage(mm, "es_op_status", mm.ME[:7])
				}
			}
			// ME type 29 subtype 0/1: target state and status
			if mm.METype == 29 && (mm.MESub == 0 || mm.MESub == 1) {
				var data [7]byte
				copy(data[:], mm.ME[:7])
				if w.tracker.CheckAndMarkFATSVEvent(mm.Addr, "es_target", data) {
					return w.writeFATSVEventMessage(mm, "es_target", mm.ME[:7])
				}
			}
		}
	}

	return nil
}

// writeFATSVEventMessage formats a FATSV event message
func (w *FATSVWriter) writeFATSVEventMessage(mm *Message, datafield string, data []byte) []byte {
	var buf bytes.Buffer

	// Clock (unix timestamp in seconds, with decimal)
	now := time.Now()
	buf.WriteString(fmt.Sprintf("clock\t%.1f", float64(now.UnixMilli())/1000.0))

	// Hex ID or other ID
	if mm.Addr&MODES_NON_ICAO_ADDRESS != 0 {
		buf.WriteString(fmt.Sprintf("\totherid\t%06X", mm.Addr&0xFFFFFF))
	} else {
		buf.WriteString(fmt.Sprintf("\thexid\t%06X", mm.Addr))
	}

	// Address type (if not standard ADSB ICAO)
	if mm.AddrType != ADDR_ADSB_ICAO {
		buf.WriteString(fmt.Sprintf("\taddrtype\t%s", addrTypeString(mm.AddrType)))
	}

	// Data field name and hex-encoded data
	buf.WriteString(fmt.Sprintf("\t%s\t", datafield))
	for _, b := range data {
		buf.WriteString(fmt.Sprintf("%02x", b))
	}

	buf.WriteString("\n")
	return buf.Bytes()
}

// WriteFATSV performs periodic FATSV output for all tracked aircraft
// Should be called approximately once per second
// Returns the formatted FATSV output for all aircraft with updates
func (w *FATSVWriter) WriteFATSV() []byte {
	now := uint64(time.Now().UnixMilli())
	var output bytes.Buffer

	aircraft := w.tracker.GetAllAircraft()

	for _, a := range aircraft {
		// Basic filter for bad decodes
		if a.Messages < 2 {
			continue
		}

		// Don't emit if it hasn't updated since last time
		if a.Seen < a.FATSVLastEmitted {
			continue
		}

		line, emittedState := w.formatAircraftFATSV(a, now)
		if line != nil {
			output.Write(line)
			// Persist all emitted fields on real tracker state (not just the copy)
			w.tracker.MarkFATSVPeriodicEmitted(a.Addr, emittedState, now)
		}
	}

	return output.Bytes()
}

// formatAircraftFATSV formats a single aircraft's FATSV output.
// Returns the formatted line and the emitted field state. Callers must persist
// the emitted state on the real tracker (via MarkFATSVPeriodicEmitted) so that
// change detection works on subsequent calls.
func (w *FATSVWriter) formatAircraftFATSV(a *Aircraft, now uint64) ([]byte, FATSVEmittedState) {
	// Track emitted state locally; start with current emitted values so
	// unchanged fields carry forward when persisted on real tracker.
	emitted := FATSVEmittedState{
		Altitude:     a.FATSVEmittedAltitude,
		AltitudeGNSS: a.FATSVEmittedAltitudeGNSS,
		Heading:      a.FATSVEmittedHeading,
		HeadingMag:   a.FATSVEmittedHeadingMag,
		Speed:        a.FATSVEmittedSpeed,
		SpeedIAS:     a.FATSVEmittedSpeedIAS,
		SpeedTAS:     a.FATSVEmittedSpeedTAS,
		AirGround:    a.FATSVEmittedAirGround,
	}

	// Determine what data is valid and fresh
	altValid := dataValidEx(&a.AltitudeValid, now, 15000, SOURCE_MODE_S)
	altGNSSValid := dataValidEx(&a.AltitudeGNSSValid, now, 15000, SOURCE_MODE_S_CHECKED)
	airgroundValid := dataValidEx(&a.AirGroundValid, now, 15000, SOURCE_MODE_S_CHECKED)
	positionValid := dataValidEx(&a.PositionValid, now, 15000, SOURCE_MODE_S_CHECKED)
	headingValid := dataValidEx(&a.HeadingValid, now, 15000, SOURCE_MODE_S_CHECKED)
	headingMagValid := dataValidEx(&a.HeadingMagneticValid, now, 15000, SOURCE_MODE_S_CHECKED)
	speedValid := dataValidEx(&a.SpeedValid, now, 15000, SOURCE_MODE_S_CHECKED)
	speedIASValid := dataValidEx(&a.SpeedIASValid, now, 15000, SOURCE_MODE_S_CHECKED)
	speedTASValid := dataValidEx(&a.SpeedTASValid, now, 15000, SOURCE_MODE_S_CHECKED)
	categoryValid := dataValidEx(&a.CategoryValid, now, 15000, SOURCE_MODE_S_CHECKED)

	// If definitely on ground, suppress unreliable altitude info
	if airgroundValid && a.AirGround == AG_GROUND && a.AltitudeValid.Source < SOURCE_MODE_S_CHECKED {
		altValid = false
	}

	// Check if significant changes have occurred
	changed := false
	if altValid && abs(a.Altitude-a.FATSVEmittedAltitude) >= 50 {
		changed = true
	}
	if altGNSSValid && abs(a.AltitudeGNSS-a.FATSVEmittedAltitudeGNSS) >= 50 {
		changed = true
	}
	if headingValid && headingDifference(a.Heading, a.FATSVEmittedHeading) >= 2 {
		changed = true
	}
	if headingMagValid && headingDifference(a.HeadingMagnetic, a.FATSVEmittedHeadingMag) >= 2 {
		changed = true
	}
	if speedValid && unsignedDifference(a.Speed, a.FATSVEmittedSpeed) >= 25 {
		changed = true
	}
	if speedIASValid && unsignedDifference(a.SpeedIAS, a.FATSVEmittedSpeedIAS) >= 25 {
		changed = true
	}
	if speedTASValid && unsignedDifference(a.SpeedTAS, a.FATSVEmittedSpeedTAS) >= 25 {
		changed = true
	}

	// Determine minimum age before re-emitting
	var minAge uint64
	if airgroundValid && ((a.AirGround == AG_AIRBORNE && a.FATSVEmittedAirGround == AG_GROUND) ||
		(a.AirGround == AG_GROUND && a.FATSVEmittedAirGround == AG_AIRBORNE)) {
		// Air-ground transition, handle immediately
		minAge = 0
	} else if !positionValid {
		// Don't send Mode S very often
		minAge = 30000
	} else if (airgroundValid && a.AirGround == AG_GROUND) ||
		(altValid && a.Altitude < 500 && (!speedValid || a.Speed < 200)) ||
		(speedValid && a.Speed < 100 && (!altValid || a.Altitude < 1000)) {
		// Probably on ground, increase update rate
		minAge = 1000
	} else if !altValid || a.Altitude < 10000 {
		// Below 10000ft, emit up to every 5s when changing, 10s otherwise
		if changed {
			minAge = 5000
		} else {
			minAge = 10000
		}
	} else {
		// Above 10000ft, emit up to every 10s when changing, 30s otherwise
		if changed {
			minAge = 10000
		} else {
			minAge = 30000
		}
	}

	if (now - a.FATSVLastEmitted) < minAge {
		return nil, emitted
	}

	// Build FATSV output
	var buf bytes.Buffer
	useful := false
	var tisb int

	// Clock (unix timestamp in seconds)
	buf.WriteString(fmt.Sprintf("clock\t%d", a.Seen/1000))

	// Hex ID or other ID
	if a.Addr&MODES_NON_ICAO_ADDRESS != 0 {
		buf.WriteString(fmt.Sprintf("\totherid\t%06X", a.Addr&0xFFFFFF))
	} else {
		buf.WriteString(fmt.Sprintf("\thexid\t%06X", a.Addr))
	}

	// Address type (if not standard ADSB ICAO)
	if a.AddrType != ADDR_ADSB_ICAO {
		buf.WriteString(fmt.Sprintf("\taddrtype\t%s", addrTypeString(a.AddrType)))
	}

	// Callsign
	if dataValidEx(&a.CallsignValid, now, 15000, SOURCE_MODE_S_CHECKED) {
		callsign := callsignToString(a.Callsign)
		if callsign != "" && callsign != "        " && a.CallsignValid.Updated > a.FATSVLastEmitted {
			buf.WriteString(fmt.Sprintf("\tident\t%s", callsign))
			buf.WriteString(fmt.Sprintf("\tiSource\t%s", sourceString(a.CallsignValid.Source)))
			useful = true
			if a.CallsignValid.Source == SOURCE_TISB {
				tisb |= TISB_IDENT
			}
		}
	}

	// Squawk
	if dataValidEx(&a.SquawkValid, now, 15000, SOURCE_MODE_S) && a.SquawkValid.Updated > a.FATSVLastEmitted {
		buf.WriteString(fmt.Sprintf("\tsquawk\t%04x", a.Squawk))
		useful = true
		if a.SquawkValid.Source == SOURCE_TISB {
			tisb |= TISB_SQUAWK
		}
	}

	// Altitude (barometric)
	if altValid && a.AltitudeValid.Updated > a.FATSVLastEmitted {
		buf.WriteString(fmt.Sprintf("\talt\t%d", a.Altitude))
		emitted.Altitude = a.Altitude
		useful = true
		if a.AltitudeValid.Source == SOURCE_TISB {
			tisb |= TISB_ALTITUDE
		}
	}

	// Altitude (GNSS)
	if altGNSSValid && a.AltitudeGNSSValid.Updated > a.FATSVLastEmitted {
		buf.WriteString(fmt.Sprintf("\talt_gnss\t%d", a.AltitudeGNSS))
		emitted.AltitudeGNSS = a.AltitudeGNSS
		useful = true
		if a.AltitudeGNSSValid.Source == SOURCE_TISB {
			tisb |= TISB_ALTITUDE_GNSS
		}
	}

	// Ground speed
	if speedValid && a.SpeedValid.Updated > a.FATSVLastEmitted {
		buf.WriteString(fmt.Sprintf("\tspeed\t%d", a.Speed))
		emitted.Speed = a.Speed
		useful = true
		if a.SpeedValid.Source == SOURCE_TISB {
			tisb |= TISB_SPEED
		}
	}

	// IAS
	if speedIASValid && a.SpeedIASValid.Updated > a.FATSVLastEmitted {
		buf.WriteString(fmt.Sprintf("\tspeed_ias\t%d", a.SpeedIAS))
		emitted.SpeedIAS = a.SpeedIAS
		useful = true
		if a.SpeedIASValid.Source == SOURCE_TISB {
			tisb |= TISB_SPEED_IAS
		}
	}

	// TAS
	if speedTASValid && a.SpeedTASValid.Updated > a.FATSVLastEmitted {
		buf.WriteString(fmt.Sprintf("\tspeed_tas\t%d", a.SpeedTAS))
		emitted.SpeedTAS = a.SpeedTAS
		useful = true
		if a.SpeedTASValid.Source == SOURCE_TISB {
			tisb |= TISB_SPEED_TAS
		}
	}

	// Position
	if positionValid && a.PositionValid.Updated > a.FATSVLastEmitted {
		buf.WriteString(fmt.Sprintf("\tlat\t%.5f\tlon\t%.5f", a.Lat, a.Lon))
		useful = true
		if a.PositionValid.Source == SOURCE_TISB {
			tisb |= TISB_POSITION
		}
	}

	// True heading
	if headingValid && a.HeadingValid.Updated > a.FATSVLastEmitted {
		buf.WriteString(fmt.Sprintf("\theading\t%d", a.Heading))
		emitted.Heading = a.Heading
		useful = true
		if a.HeadingValid.Source == SOURCE_TISB {
			tisb |= TISB_HEADING
		}
	}

	// Magnetic heading
	if headingMagValid && a.HeadingMagneticValid.Updated > a.FATSVLastEmitted {
		buf.WriteString(fmt.Sprintf("\theading_magnetic\t%d", a.HeadingMagnetic))
		emitted.HeadingMag = a.HeadingMagnetic
		useful = true
		if a.HeadingMagneticValid.Source == SOURCE_TISB {
			tisb |= TISB_HEADING_MAGNETIC
		}
	}

	// Air/ground state
	if airgroundValid && (a.AirGround == AG_GROUND || a.AirGround == AG_AIRBORNE) &&
		a.AirGroundValid.Updated > a.FATSVLastEmitted {
		if a.AirGround == AG_GROUND {
			buf.WriteString("\tairGround\tG+")
		} else {
			buf.WriteString("\tairGround\tA+")
		}
		emitted.AirGround = a.AirGround
		useful = true
		if a.AirGroundValid.Source == SOURCE_TISB {
			tisb |= TISB_AIRGROUND
		}
	}

	// Category (only if interesting - not regular aircraft A0-AF)
	if categoryValid && (a.Category&0xF0) != 0xA0 && a.CategoryValid.Updated > a.FATSVLastEmitted {
		buf.WriteString(fmt.Sprintf("\tcategory\t%02X", a.Category))
		useful = true
		if a.CategoryValid.Source == SOURCE_TISB {
			tisb |= TISB_CATEGORY
		}
	}

	// If nothing useful, don't emit
	if !useful {
		return nil, emitted
	}

	// TIS-B flags
	if tisb != 0 {
		buf.WriteString(fmt.Sprintf("\ttisb\t%d", tisb))
	}

	buf.WriteString("\n")

	return buf.Bytes(), emitted
}

// Helper functions

// dataValidEx checks if data is valid with extended parameters
func dataValidEx(v *DataValidity, now uint64, ttl uint64, minSource DataSource) bool {
	if v.Source < minSource {
		return false
	}
	if v.Source == SOURCE_INVALID {
		return false
	}
	if now >= v.Updated+ttl {
		return false
	}
	return true
}

// abs returns absolute value of int
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// headingDifference returns the smaller difference between two headings (0-360)
func headingDifference(a, b uint32) uint32 {
	diff := int32(a) - int32(b)
	if diff < 0 {
		diff = -diff
	}
	if diff > 180 {
		diff = 360 - diff
	}
	return uint32(diff)
}

// unsignedDifference returns the absolute difference between two uint32 values
func unsignedDifference(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// sourceString returns string for data source (for FATSV iSource field)
func sourceString(s DataSource) string {
	switch s {
	case SOURCE_MODE_S, SOURCE_MODE_S_CHECKED:
		return "modes"
	case SOURCE_ADSB:
		return "adsb"
	case SOURCE_TISB:
		return "tisb"
	case SOURCE_MLAT:
		return "mlat"
	default:
		return "unknown"
	}
}
