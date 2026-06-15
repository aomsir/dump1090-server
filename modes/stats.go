// Package modes implements statistics collection.
//
// This is a direct translation from dump1090-mutability's stats.c/stats.h.
// Tracks demodulator performance, message counts, CPU usage, signal quality.
//
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// Licensed under GPL v2+
package modes

import (
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"time"
)


const (
	// STATS_HISTORY_SIZE is the number of 1-minute periods to keep
	STATS_HISTORY_SIZE = 15

	// RANGE_BUCKET_COUNT is the number of range histogram buckets
	RANGE_BUCKET_COUNT = 76
)

// Stats holds statistics for a time period
type Stats struct {
	Start uint64 // Unix milliseconds
	End   uint64 // Unix milliseconds

	// Mode S demodulator counts
	DemodPreambles            uint32
	DemodRejectedBad          uint32
	DemodRejectedUnknownICAO  uint32
	DemodAccepted             [MODES_MAX_BITERRORS + 1]uint32

	// Mode A/C counts
	DemodModeAC uint32

	// Sample processing
	SamplesProcessed uint64
	SamplesDropped   uint64

	// CPU time (milliseconds)
	DemodCPU      uint64
	ReaderCPU     uint64
	BackgroundCPU uint64

	// Noise power statistics
	NoisePowerSum   float64
	NoisePowerCount uint64

	// Signal power statistics
	SignalPowerSum   float64
	SignalPowerCount uint64
	PeakSignalPower  float64
	StrongSignalCount uint32 // Count of signals > -3dBFS

	// Remote message counts
	RemoteReceivedModeAC     uint32
	RemoteReceivedModes      uint32
	RemoteRejectedBad        uint32
	RemoteRejectedUnknownICAO uint32
	RemoteAccepted           [MODES_MAX_BITERRORS + 1]uint32

	// Total messages
	MessagesTotal uint32

	// CPR decoding statistics
	CPRSurface                uint32
	CPRAirborne               uint32
	CPRGlobalOK               uint32
	CPRGlobalBad              uint32
	CPRGlobalRangeChecks      uint32
	CPRGlobalSpeedChecks      uint32
	CPRGlobalSkipped          uint32
	CPRLocalOK                uint32
	CPRLocalAircraftRelative  uint32
	CPRLocalReceiverRelative  uint32
	CPRLocalSkipped           uint32
	CPRLocalRangeChecks       uint32
	CPRLocalSpeedChecks       uint32
	CPRFiltered               uint32

	// Aircraft tracking
	UniqueAircraft           uint32
	SingleMessageAircraft    uint32
	SuppressedAltitudeMessages uint32

	// Range histogram (76 buckets, 10NM each = 0-750NM)
	RangeHistogram [RANGE_BUCKET_COUNT]uint32
}

// StatsCollector manages statistics collection over multiple time windows
type StatsCollector struct {
	mu sync.RWMutex

	// Current 1-minute period
	current Stats

	// 15 x 1-minute circular buffer
	history      [STATS_HISTORY_SIZE]Stats
	latestIndex  int // Index of the most recent complete 1-minute period

	// Aggregated statistics
	last5Min  Stats // Aggregated from last 5 x 1min
	last15Min Stats // Aggregated from all 15 x 1min
	allTime   Stats // Since startup

	// Next update time
	nextUpdate uint64 // Unix milliseconds

	// clock returns the current time in Unix milliseconds. Defaults to
	// time.Now().UnixMilli() but can be overridden for testing.
	clock func() uint64
}

// NewStatsCollector creates a new statistics collector
func NewStatsCollector() *StatsCollector {
	now := uint64(time.Now().UnixMilli())
	sc := &StatsCollector{
		latestIndex: STATS_HISTORY_SIZE - 1, // Will wrap to 0 on first update
		nextUpdate:  now + 60000,            // First update in 60 seconds
		clock:       func() uint64 { return uint64(time.Now().UnixMilli()) },
	}
	sc.current.Start = now
	sc.current.End = now
	sc.allTime.Start = now
	return sc
}

// AddDemodResult records a demodulation result
func (sc *StatsCollector) AddDemodResult(preambleDetected bool, accepted bool, bitErrors int, isUnknownICAO bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if accepted {
		if bitErrors >= 0 && bitErrors <= MODES_MAX_BITERRORS {
			sc.current.DemodAccepted[bitErrors]++
		}
	} else {
		if isUnknownICAO {
			sc.current.DemodRejectedUnknownICAO++
		} else {
			sc.current.DemodRejectedBad++
		}
	}
}

// AddPreamble records a preamble detection
func (sc *StatsCollector) AddPreamble() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.current.DemodPreambles++
}

// AddModeACMessage records a Mode A/C message
func (sc *StatsCollector) AddModeACMessage() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.current.DemodModeAC++
}

// AddSamplesProcessed records processed samples
func (sc *StatsCollector) AddSamplesProcessed(count uint64) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.current.SamplesProcessed += count
}

// AddSamplesDropped records dropped samples
func (sc *StatsCollector) AddSamplesDropped(count uint64) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.current.SamplesDropped += count
}

// AddNoisePower records noise power measurement
func (sc *StatsCollector) AddNoisePower(power float64) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.current.NoisePowerSum += power
	sc.current.NoisePowerCount++
}

// AddSignalPower records signal power measurement
func (sc *StatsCollector) AddSignalPower(power float64) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.current.SignalPowerSum += power
	sc.current.SignalPowerCount++

	if power > sc.current.PeakSignalPower {
		sc.current.PeakSignalPower = power
	}

	// Count strong signals (> -3dBFS = power > 0.707)
	if power > 0.707 {
		sc.current.StrongSignalCount++
	}
}

// AddCPRResult records CPR decoding result
// For local CPR, relativeType indicates: "aircraft" or "receiver"
func (sc *StatsCollector) AddCPRResult(surface bool, global bool, ok bool, reason string, relativeType string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if surface {
		sc.current.CPRSurface++
	} else {
		sc.current.CPRAirborne++
	}

	if global {
		if ok {
			sc.current.CPRGlobalOK++
		} else {
			sc.current.CPRGlobalBad++
			switch reason {
			case "range":
				sc.current.CPRGlobalRangeChecks++
			case "speed":
				sc.current.CPRGlobalSpeedChecks++
			}
		}
	} else {
		if ok {
			sc.current.CPRLocalOK++
			// Track aircraft vs receiver relative
			switch relativeType {
			case "aircraft":
				sc.current.CPRLocalAircraftRelative++
			case "receiver":
				sc.current.CPRLocalReceiverRelative++
			}
		} else {
			switch reason {
			case "range":
				sc.current.CPRLocalRangeChecks++
			case "speed":
				sc.current.CPRLocalSpeedChecks++
			case "skipped":
				sc.current.CPRLocalSkipped++
			}
		}
	}
}

// AddMessage increments total message count
func (sc *StatsCollector) AddMessage() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.current.MessagesTotal++
}

// AddRemoteMessage records a message received from a remote network source.
// modeAC is true for Mode A/C messages. bitErrors is the number of corrected
// bit errors (0 for correct CRC). isUnknownICAO is true if the ICAO was not recognized.
func (sc *StatsCollector) AddRemoteMessage(modeAC bool, bitErrors int, isUnknownICAO bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if modeAC {
		sc.current.RemoteReceivedModeAC++
		return
	}

	sc.current.RemoteReceivedModes++

	if isUnknownICAO {
		sc.current.RemoteRejectedUnknownICAO++
	} else if bitErrors < 0 {
		sc.current.RemoteRejectedBad++
	} else if bitErrors >= 0 && bitErrors <= MODES_MAX_BITERRORS {
		sc.current.RemoteAccepted[bitErrors]++
	}
}

// AddUniqueAircraft increments the unique aircraft counter for the current window.
func (sc *StatsCollector) AddUniqueAircraft() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.current.UniqueAircraft++
}

// AddSingleMessageAircraft increments the single-message aircraft counter
// for the current window.
func (sc *StatsCollector) AddSingleMessageAircraft() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.current.SingleMessageAircraft++
}

// AddRangeHistogram updates the range histogram with a decoded position
// receiverLat/receiverLon: receiver position in degrees
// lat/lon: aircraft position in degrees
// maxRange: maximum range in meters (for normalization)
func (sc *StatsCollector) AddRangeHistogram(receiverLat, receiverLon, lat, lon, maxRange float64) {
	if maxRange <= 0 {
		return // Can't normalize without maxRange
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Calculate distance in meters using great circle formula (simplified from track.go)
	lat0Rad := receiverLat * 3.14159265358979323846 / 180.0
	lon0Rad := receiverLon * 3.14159265358979323846 / 180.0
	lat1Rad := lat * 3.14159265358979323846 / 180.0
	lon1Rad := lon * 3.14159265358979323846 / 180.0

	dlat := lat1Rad - lat0Rad
	if dlat < 0 {
		dlat = -dlat
	}
	dlon := lon1Rad - lon0Rad
	if dlon < 0 {
		dlon = -dlon
	}

	// Spherical law of cosines (faster than haversine for this use case)
	// Note: track.go has greatcircle function we could reuse, but avoiding cross-package dependency
	sinLat0 := math.Sin(lat0Rad)
	sinLat1 := math.Sin(lat1Rad)
	cosLat0 := math.Cos(lat0Rad)
	cosLat1 := math.Cos(lat1Rad)
	cosDlon := math.Cos(lon1Rad - lon0Rad)

	range_ := 6371e3 * math.Acos(sinLat0*sinLat1+cosLat0*cosLat1*cosDlon)

	// Normalize to bucket index (0 to RANGE_BUCKET_COUNT-1)
	bucket := int(math.Round(range_ / maxRange * float64(RANGE_BUCKET_COUNT)))

	// Clamp to valid range
	if bucket < 0 {
		bucket = 0
	} else if bucket >= RANGE_BUCKET_COUNT {
		bucket = RANGE_BUCKET_COUNT - 1
	}

	sc.current.RangeHistogram[bucket]++
}

// Update should be called periodically (e.g., every second) to check if
// a 1-minute period has completed and update aggregate statistics
func (sc *StatsCollector) Update() {
	now := sc.clock()

	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Update current period end time
	sc.current.End = now

	// Check if we need to complete the current 1-minute period
	if now < sc.nextUpdate {
		return
	}

	// Rotate the circular buffer
	sc.latestIndex = (sc.latestIndex + 1) % STATS_HISTORY_SIZE
	sc.history[sc.latestIndex] = sc.current

	// Add to all-time stats
	addStats(&sc.current, &sc.allTime)

	// Recalculate 5-minute stats (last 5 periods)
	sc.last5Min = Stats{}
	for i := 0; i < 5 && i < STATS_HISTORY_SIZE; i++ {
		idx := (sc.latestIndex - i + STATS_HISTORY_SIZE) % STATS_HISTORY_SIZE
		if sc.history[idx].Start > 0 { // Valid entry
			addStats(&sc.history[idx], &sc.last5Min)
		}
	}

	// Recalculate 15-minute stats (all periods)
	sc.last15Min = Stats{}
	for i := 0; i < STATS_HISTORY_SIZE; i++ {
		if sc.history[i].Start > 0 { // Valid entry
			addStats(&sc.history[i], &sc.last15Min)
		}
	}

	// Reset current stats for the new period
	sc.current = Stats{
		Start: now,
		End:   now,
	}

	// Schedule next update
	sc.nextUpdate = now + 60000
}

// GetLatest returns the current in-progress 1-minute window.
// This reflects counters added since the last rotation.
func (sc *StatsCollector) GetLatest() Stats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.current
}

// GetLast1Min returns the most recently completed (rotated) 1-minute window.
func (sc *StatsCollector) GetLast1Min() Stats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.history[sc.latestIndex]
}

// GetLast5Min returns aggregated stats for the last 5 minutes
func (sc *StatsCollector) GetLast5Min() Stats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.last5Min
}

// GetLast15Min returns aggregated stats for the last 15 minutes
func (sc *StatsCollector) GetLast15Min() Stats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.last15Min
}

// GetAllTime returns cumulative stats since startup, including the current
// in-progress window.
func (sc *StatsCollector) GetAllTime() Stats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	result := sc.allTime
	addStats(&sc.current, &result)
	return result
}

// GetCurrent returns the current in-progress statistics
func (sc *StatsCollector) GetCurrent() Stats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.current
}

// addStats adds src to dst (accumulates counters, updates time range)
func addStats(src, dst *Stats) {
	// Update time range
	if dst.Start == 0 || src.Start < dst.Start {
		dst.Start = src.Start
	}
	if src.End > dst.End {
		dst.End = src.End
	}

	// Accumulate counters
	dst.DemodPreambles += src.DemodPreambles
	dst.DemodRejectedBad += src.DemodRejectedBad
	dst.DemodRejectedUnknownICAO += src.DemodRejectedUnknownICAO
	for i := range src.DemodAccepted {
		dst.DemodAccepted[i] += src.DemodAccepted[i]
	}

	dst.DemodModeAC += src.DemodModeAC
	dst.SamplesProcessed += src.SamplesProcessed
	dst.SamplesDropped += src.SamplesDropped

	dst.DemodCPU += src.DemodCPU
	dst.ReaderCPU += src.ReaderCPU
	dst.BackgroundCPU += src.BackgroundCPU

	dst.NoisePowerSum += src.NoisePowerSum
	dst.NoisePowerCount += src.NoisePowerCount

	dst.SignalPowerSum += src.SignalPowerSum
	dst.SignalPowerCount += src.SignalPowerCount
	if src.PeakSignalPower > dst.PeakSignalPower {
		dst.PeakSignalPower = src.PeakSignalPower
	}
	dst.StrongSignalCount += src.StrongSignalCount

	dst.RemoteReceivedModeAC += src.RemoteReceivedModeAC
	dst.RemoteReceivedModes += src.RemoteReceivedModes
	dst.RemoteRejectedBad += src.RemoteRejectedBad
	dst.RemoteRejectedUnknownICAO += src.RemoteRejectedUnknownICAO
	for i := range src.RemoteAccepted {
		dst.RemoteAccepted[i] += src.RemoteAccepted[i]
	}

	dst.MessagesTotal += src.MessagesTotal

	dst.CPRSurface += src.CPRSurface
	dst.CPRAirborne += src.CPRAirborne
	dst.CPRGlobalOK += src.CPRGlobalOK
	dst.CPRGlobalBad += src.CPRGlobalBad
	dst.CPRGlobalRangeChecks += src.CPRGlobalRangeChecks
	dst.CPRGlobalSpeedChecks += src.CPRGlobalSpeedChecks
	dst.CPRGlobalSkipped += src.CPRGlobalSkipped
	dst.CPRLocalOK += src.CPRLocalOK
	dst.CPRLocalAircraftRelative += src.CPRLocalAircraftRelative
	dst.CPRLocalReceiverRelative += src.CPRLocalReceiverRelative
	dst.CPRLocalSkipped += src.CPRLocalSkipped
	dst.CPRLocalRangeChecks += src.CPRLocalRangeChecks
	dst.CPRLocalSpeedChecks += src.CPRLocalSpeedChecks
	dst.CPRFiltered += src.CPRFiltered

	dst.UniqueAircraft += src.UniqueAircraft
	dst.SingleMessageAircraft += src.SingleMessageAircraft
	dst.SuppressedAltitudeMessages += src.SuppressedAltitudeMessages

	for i := range src.RangeHistogram {
		dst.RangeHistogram[i] += src.RangeHistogram[i]
	}
}

// DisplayStats prints statistics to stdout matching C version stats.c:display_stats()
func DisplayStats(st *Stats, w io.Writer, netOnly bool, nfixCRC int, maxRange float64) {
	if w == nil {
		w = os.Stdout
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w)

	// Time range header
	startTime := time.UnixMilli(int64(st.Start))
	endTime := time.UnixMilli(int64(st.End))
	fmt.Fprintf(w, "Statistics: %s - %s\n",
		startTime.Format("Mon Jan 02 15:04:05 2006 MST"),
		endTime.Format("Mon Jan 02 15:04:05 2006 MST"))

	// Local receiver statistics
	if !netOnly {
		fmt.Fprintln(w, "Local receiver:")
		fmt.Fprintf(w, "  %12d samples processed\n", st.SamplesProcessed)
		fmt.Fprintf(w, "  %12d samples dropped\n", st.SamplesDropped)
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %12d Mode A/C messages received\n", st.DemodModeAC)
		fmt.Fprintf(w, "  %12d Mode-S message preambles received\n", st.DemodPreambles)
		fmt.Fprintf(w, "    %12d with bad message format or invalid CRC\n", st.DemodRejectedBad)
		fmt.Fprintf(w, "    %12d with unrecognized ICAO address\n", st.DemodRejectedUnknownICAO)
		fmt.Fprintf(w, "    %12d accepted with correct CRC\n", st.DemodAccepted[0])
		for j := 1; j <= nfixCRC && j <= MODES_MAX_BITERRORS; j++ {
			fmt.Fprintf(w, "    %12d accepted with %d-bit error repaired\n", st.DemodAccepted[j], j)
		}
		fmt.Fprintln(w)

		// Noise power
		if st.NoisePowerSum > 0 && st.NoisePowerCount > 0 {
			noisePower := 10 * math.Log10(st.NoisePowerSum/float64(st.NoisePowerCount))
			fmt.Fprintf(w, "  %5.1f dBFS noise power\n", noisePower)
		} else {
			fmt.Fprintln(w, "  ----- dBFS noise power")
		}

		// Signal power
		if st.SignalPowerSum > 0 && st.SignalPowerCount > 0 {
			signalPower := 10 * math.Log10(st.SignalPowerSum/float64(st.SignalPowerCount))
			fmt.Fprintf(w, "  %5.1f dBFS mean signal power\n", signalPower)
		} else {
			fmt.Fprintln(w, "  ----- dBFS mean signal power")
		}

		// Peak signal power
		if st.PeakSignalPower > 0 {
			peakPower := 10 * math.Log10(st.PeakSignalPower)
			fmt.Fprintf(w, "  %5.1f dBFS peak signal power\n", peakPower)
		} else {
			fmt.Fprintln(w, "  ----- dBFS peak signal power")
		}

		fmt.Fprintf(w, "  %5d messages with signal power above -3dBFS\n", st.StrongSignalCount)
		fmt.Fprintln(w)
	}

	// Remote messages (network input)
	fmt.Fprintln(w, "Messages from network clients:")
	fmt.Fprintf(w, "  %8d Mode A/C messages received\n", st.RemoteReceivedModeAC)
	fmt.Fprintf(w, "  %8d Mode S messages received\n", st.RemoteReceivedModes)
	fmt.Fprintf(w, "    %8d with bad message format or invalid CRC\n", st.RemoteRejectedBad)
	fmt.Fprintf(w, "    %8d with unrecognized ICAO address\n", st.RemoteRejectedUnknownICAO)
	fmt.Fprintf(w, "    %8d accepted with correct CRC\n", st.RemoteAccepted[0])
	for j := 1; j <= nfixCRC && j <= MODES_MAX_BITERRORS; j++ {
		fmt.Fprintf(w, "    %8d accepted with %d-bit error repaired\n", st.RemoteAccepted[j], j)
	}
	fmt.Fprintln(w)

	// Decoder statistics
	fmt.Fprintln(w, "Decoder:")
	fmt.Fprintf(w, "  %8d total usable messages\n", st.MessagesTotal)
	fmt.Fprintln(w)

	// CPR decoding statistics
	fmt.Fprintf(w, "  %8d surface position messages received\n", st.CPRSurface)
	fmt.Fprintf(w, "  %8d airborne position messages received\n", st.CPRAirborne)
	fmt.Fprintf(w, "  %8d global CPR attempts with valid positions\n", st.CPRGlobalOK)
	fmt.Fprintf(w, "  %8d global CPR attempts with bad data\n", st.CPRGlobalBad)
	fmt.Fprintf(w, "    %8d global CPR attempts that failed the range check\n", st.CPRGlobalRangeChecks)
	fmt.Fprintf(w, "    %8d global CPR attempts that failed the speed check\n", st.CPRGlobalSpeedChecks)
	fmt.Fprintf(w, "  %8d global CPR attempts with insufficient data\n", st.CPRGlobalSkipped)
	fmt.Fprintf(w, "  %8d local CPR attempts with valid positions\n", st.CPRLocalOK)
	fmt.Fprintf(w, "    %8d aircraft-relative positions\n", st.CPRLocalAircraftRelative)
	fmt.Fprintf(w, "    %8d receiver-relative positions\n", st.CPRLocalReceiverRelative)
	fmt.Fprintf(w, "  %8d local CPR attempts that did not produce useful positions\n", st.CPRLocalSkipped)
	fmt.Fprintf(w, "    %8d local CPR attempts that failed the range check\n", st.CPRLocalRangeChecks)
	fmt.Fprintf(w, "    %8d local CPR attempts that failed the speed check\n", st.CPRLocalSpeedChecks)
	fmt.Fprintf(w, "  %8d CPR messages that look like transponder failures filtered\n", st.CPRFiltered)
	fmt.Fprintln(w)

	fmt.Fprintf(w, "  %8d unique aircraft tracks\n", st.UniqueAircraft)
	fmt.Fprintf(w, "  %8d aircraft tracks where only one message was seen\n", st.SingleMessageAircraft)
	fmt.Fprintln(w)

	// CPU load
	durationMs := st.End - st.Start
	if durationMs > 0 {
		totalCPU := st.DemodCPU + st.ReaderCPU + st.BackgroundCPU
		cpuPercent := 100.0 * float64(totalCPU) / float64(durationMs)
		fmt.Fprintf(w, "CPU load: %5.1f%%\n", cpuPercent)
		fmt.Fprintf(w, "  %5d ms for demodulation\n", st.DemodCPU)
		fmt.Fprintf(w, "  %5d ms for reading from USB\n", st.ReaderCPU)
		fmt.Fprintf(w, "  %5d ms for network input and background tasks\n", st.BackgroundCPU)
	}
	fmt.Fprintln(w)
}

// DisplayRangeHistogram prints the range histogram matching C version
func DisplayRangeHistogram(st *Stats, w io.Writer, maxRange float64) {
	if w == nil {
		w = os.Stdout
	}

	fmt.Fprintln(w, "Range histogram:")
	fmt.Fprintln(w)

	// Find peak value
	var peak uint32
	for i := 0; i < RANGE_BUCKET_COUNT; i++ {
		if st.RangeHistogram[i] > peak {
			peak = st.RangeHistogram[i]
		}
	}

	if peak == 0 {
		fmt.Fprintln(w, "  (no position data)")
		return
	}

	// Unicode bar characters (matching C version)
	pixels := []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	npixels := len(pixels)

	// Calculate heights (20 rows, npixels levels per row)
	heights := make([]int, RANGE_BUCKET_COUNT)
	for i := 0; i < RANGE_BUCKET_COUNT; i++ {
		heights[i] = int(float64(st.RangeHistogram[i]) * 20.0 * float64(npixels) / float64(peak))
		if st.RangeHistogram[i] > 0 && heights[i] == 0 {
			heights[i] = 1
		}
	}

	// Print histogram rows (top to bottom)
	for j := 0; j < 20; j++ {
		for i := 0; i < RANGE_BUCKET_COUNT; i++ {
			pheight := heights[i] - ((19 - j) * npixels)
			if pheight <= 0 {
				fmt.Fprint(w, " ")
			} else if pheight >= npixels {
				fmt.Fprint(w, pixels[npixels-1])
			} else {
				fmt.Fprint(w, pixels[pheight])
			}
		}
		fmt.Fprintln(w)
	}

	// Print x-axis line
	for i := 0; i < RANGE_BUCKET_COUNT/4; i++ {
		fmt.Fprint(w, "----")
	}
	fmt.Fprintln(w)

	// Print tick marks
	for i := 0; i < RANGE_BUCKET_COUNT/4; i++ {
		fmt.Fprint(w, " '  ")
	}
	fmt.Fprintln(w)

	// Print distance labels
	for i := 0; i < RANGE_BUCKET_COUNT/4; i++ {
		midpoint := int(math.Round((float64(i*4) + 1.5) * maxRange / float64(RANGE_BUCKET_COUNT) / 1000))
		fmt.Fprintf(w, "%03d ", midpoint)

	}
	fmt.Fprintln(w, "km")
}

