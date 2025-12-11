// Package modes implements statistics collection.
//
// This is a direct translation from dump1090-mutability's stats.c/stats.h.
// Tracks demodulator performance, message counts, CPU usage, signal quality.
//
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// Licensed under GPL v2+
package modes

import (
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
}

// NewStatsCollector creates a new statistics collector
func NewStatsCollector() *StatsCollector {
	now := uint64(time.Now().UnixMilli())
	sc := &StatsCollector{
		latestIndex: STATS_HISTORY_SIZE - 1, // Will wrap to 0 on first update
		nextUpdate:  now + 60000,             // First update in 60 seconds
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
func (sc *StatsCollector) AddCPRResult(surface bool, global bool, ok bool, reason string) {
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

// Update should be called periodically (e.g., every second) to check if
// a 1-minute period has completed and update aggregate statistics
func (sc *StatsCollector) Update() {
	now := uint64(time.Now().UnixMilli())

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

// GetLatest returns the most recent complete 1-minute period
func (sc *StatsCollector) GetLatest() Stats {
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

// GetAllTime returns cumulative stats since startup
func (sc *StatsCollector) GetAllTime() Stats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.allTime
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
