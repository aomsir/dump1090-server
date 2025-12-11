// Package modes implements Mode S message decoding and demodulation.
//
// demod.go: 2.4MHz Mode S demodulator
//
// This is a translation from dump1090-mutability's demod_2400.c.
// Original Copyright (c) 2014,2015 Oliver Jowett <oliver@mutability.co.uk>
// Licensed under GPL v2+
package modes

import (
	"time"
)

// Demodulator handles Mode S signal demodulation at 2.4MHz sample rate.
//
// When sampling at 2.4MHz we have exactly 6 samples per 5 symbols.
// Each symbol is 500ns wide, each sample is 416.7ns wide.
//
// We maintain a phase offset that is expressed in units of 1/5 of a sample
// i.e. 1/6 of a symbol, 83.333ns. Each symbol we process advances the phase
// offset by 6 i.e. 6/5 of a sample, 500ns.
type Demodulator struct {
	// icaoFilter for validating addresses
	icaoFilter *ICAOFilter

	// Statistics collector (optional)
	statsCollector *StatsCollector

	// Statistics (local counters for GetStats)
	stats struct {
		preambles           int64
		rejectedBad         int64
		rejectedUnknownICAO int64
		accepted            [MODES_MAX_BITERRORS + 1]int64
		modeAC              int64
		signalPowerSum      float64
		signalPowerCount    int64
		noisePowerSum       float64
		noisePowerCount     int64
		peakSignalPower     float64
		strongSignalCount   int64
	}

	// Callback for decoded messages
	messageHandler func(*Message)
}

// NewDemodulator creates a new Mode S demodulator.
func NewDemodulator() *Demodulator {
	return &Demodulator{
		icaoFilter: NewICAOFilter(),
	}
}

// SetStatsCollector sets the statistics collector.
func (d *Demodulator) SetStatsCollector(sc *StatsCollector) {
	d.statsCollector = sc
}

// SetMessageHandler sets the callback for decoded messages.
func (d *Demodulator) SetMessageHandler(handler func(*Message)) {
	d.messageHandler = handler
}

// AddKnownICAO adds an ICAO address to the known filter.
func (d *Demodulator) AddKnownICAO(addr uint32) {
	if d.icaoFilter != nil {
		d.icaoFilter.Add(addr)
	}
}

// GetICAOFilter returns the ICAO filter for periodic expiry
func (d *Demodulator) GetICAOFilter() *ICAOFilter {
	return d.icaoFilter
}

// Phase correlation functions for Manchester decoding.
// These correlate a 1-0 pair of symbols (i.e. manchester encoded 1 bit)
// starting at the given sample, and assuming that the symbol starts at
// a fixed 0-5 phase offset. They return a correlation value:
// >0 = 1 bit, <0 = 0 bit
//
// NB: The correlation functions sum to zero, so we do not need to adjust
// for DC offset in the input signal.

func slicePhase0(m []uint16) int {
	return 5*int(m[0]) - 3*int(m[1]) - 2*int(m[2])
}

func slicePhase1(m []uint16) int {
	return 4*int(m[0]) - int(m[1]) - 3*int(m[2])
}

func slicePhase2(m []uint16) int {
	return 3*int(m[0]) + int(m[1]) - 4*int(m[2])
}

func slicePhase3(m []uint16) int {
	return 2*int(m[0]) + 3*int(m[1]) - 5*int(m[2])
}

func slicePhase4(m []uint16) int {
	return int(m[0]) + 5*int(m[1]) - 5*int(m[2]) - int(m[3])
}

// Demodulate2400 demodulates Mode S messages from magnitude samples at 2.4MHz.
// It processes the magnitude buffer and calls messageHandler for each decoded message.
func (d *Demodulator) Demodulate2400(mag *MagBuf) {
	if mag == nil || len(mag.Data) == 0 {
		return
	}

	m := mag.Data
	mlen := int(mag.Length)

	var msg1, msg2 [MODES_LONG_MSG_BYTES]byte
	msg := msg1[:]

	var sumScaledSignalPower uint64

	// Need at least enough samples for preamble + shortest message
	// Preamble: 16 samples (8us), Short message: ~57 samples
	if mlen < 19+MODES_SHORT_MSG_BITS*12/5 {
		return
	}

	for j := 0; j < mlen-19-MODES_LONG_MSG_BITS*12/5; j++ {
		preamble := m[j:]

		// Quick check: we must have a rising edge 0->1 and a falling edge 12->13
		if !(preamble[0] < preamble[1] && preamble[12] > preamble[13]) {
			continue
		}

		var high int
		var baseSignal, baseNoise uint32

		// Look for peaks at expected preamble positions
		// Phase 3: peaks at 1,3,9,11-12
		if preamble[1] > preamble[2] &&
			preamble[2] < preamble[3] && preamble[3] > preamble[4] &&
			preamble[8] < preamble[9] && preamble[9] > preamble[10] &&
			preamble[10] < preamble[11] {
			high = (int(preamble[1]) + int(preamble[3]) + int(preamble[9]) + int(preamble[11]) + int(preamble[12])) / 4
			baseSignal = uint32(preamble[1]) + uint32(preamble[3]) + uint32(preamble[9])
			baseNoise = uint32(preamble[5]) + uint32(preamble[6]) + uint32(preamble[7])
		} else if preamble[1] > preamble[2] &&
			preamble[2] < preamble[3] && preamble[3] > preamble[4] &&
			preamble[8] < preamble[9] && preamble[9] > preamble[10] &&
			preamble[11] < preamble[12] {
			// Phase 4: peaks at 1,3,9,12
			high = (int(preamble[1]) + int(preamble[3]) + int(preamble[9]) + int(preamble[12])) / 4
			baseSignal = uint32(preamble[1]) + uint32(preamble[3]) + uint32(preamble[9]) + uint32(preamble[12])
			baseNoise = uint32(preamble[5]) + uint32(preamble[6]) + uint32(preamble[7]) + uint32(preamble[8])
		} else if preamble[1] > preamble[2] &&
			preamble[2] < preamble[3] && preamble[4] > preamble[5] &&
			preamble[8] < preamble[9] && preamble[10] > preamble[11] &&
			preamble[11] < preamble[12] {
			// Phase 5: peaks at 1,3-4,9-10,12
			high = (int(preamble[1]) + int(preamble[3]) + int(preamble[4]) + int(preamble[9]) + int(preamble[10]) + int(preamble[12])) / 4
			baseSignal = uint32(preamble[1]) + uint32(preamble[12])
			baseNoise = uint32(preamble[6]) + uint32(preamble[7])
		} else if preamble[1] > preamble[2] &&
			preamble[3] < preamble[4] && preamble[4] > preamble[5] &&
			preamble[9] < preamble[10] && preamble[10] > preamble[11] &&
			preamble[11] < preamble[12] {
			// Phase 6: peaks at 1,4,10,12
			high = (int(preamble[1]) + int(preamble[4]) + int(preamble[10]) + int(preamble[12])) / 4
			baseSignal = uint32(preamble[1]) + uint32(preamble[4]) + uint32(preamble[10]) + uint32(preamble[12])
			baseNoise = uint32(preamble[5]) + uint32(preamble[6]) + uint32(preamble[7]) + uint32(preamble[8])
		} else if preamble[2] > preamble[3] &&
			preamble[3] < preamble[4] && preamble[4] > preamble[5] &&
			preamble[9] < preamble[10] && preamble[10] > preamble[11] &&
			preamble[11] < preamble[12] {
			// Phase 7: peaks at 1-2,4,10,12
			high = (int(preamble[1]) + int(preamble[2]) + int(preamble[4]) + int(preamble[10]) + int(preamble[12])) / 4
			baseSignal = uint32(preamble[4]) + uint32(preamble[10]) + uint32(preamble[12])
			baseNoise = uint32(preamble[6]) + uint32(preamble[7]) + uint32(preamble[8])
		} else {
			// No suitable peaks
			continue
		}

		// Check for enough signal (~3.5dB SNR)
		if baseSignal*2 < 3*baseNoise {
			continue
		}

		// Check that "quiet" bits are actually quiet
		if int(preamble[5]) >= high ||
			int(preamble[6]) >= high ||
			int(preamble[7]) >= high ||
			int(preamble[8]) >= high ||
			int(preamble[14]) >= high ||
			int(preamble[15]) >= high ||
			int(preamble[16]) >= high ||
			int(preamble[17]) >= high ||
			int(preamble[18]) >= high {
			continue
		}

		// Try all phases
		d.stats.preambles++
		if d.statsCollector != nil {
			d.statsCollector.AddPreamble()
		}
		var bestMsg []byte
		bestScore := -2
		bestPhase := -1

		for tryPhase := 4; tryPhase <= 8; tryPhase++ {
			pPtr := m[j+19+tryPhase/5:]
			phase := tryPhase % 5

			byteLen := MODES_LONG_MSG_BYTES
			for i := 0; i < byteLen; i++ {
				var theByte byte

				switch phase {
				case 0:
					theByte = d.decodeBytePhase0(pPtr)
					phase = 1
					pPtr = pPtr[19:]
				case 1:
					theByte = d.decodeBytePhase1(pPtr)
					phase = 2
					pPtr = pPtr[19:]
				case 2:
					theByte = d.decodeBytePhase2(pPtr)
					phase = 3
					pPtr = pPtr[19:]
				case 3:
					theByte = d.decodeBytePhase3(pPtr)
					phase = 4
					pPtr = pPtr[19:]
				case 4:
					theByte = d.decodeBytePhase4(pPtr)
					phase = 0
					pPtr = pPtr[20:]
				}

				msg[i] = theByte

				// Determine message length from DF type
				if i == 0 {
					switch msg[0] >> 3 {
					case 0, 4, 5, 11:
						byteLen = MODES_SHORT_MSG_BYTES
					case 16, 17, 18, 20, 21, 24:
						// Keep long message
					default:
						byteLen = 1 // Unknown DF, give up
					}
				}
			}

			// Score the message
			score := d.scoreModesMessage(msg, byteLen*8)
			if score > bestScore {
				bestMsg = msg
				bestScore = score
				bestPhase = tryPhase

				// Swap buffers
				if &msg[0] == &msg1[0] {
					msg = msg2[:]
				} else {
					msg = msg1[:]
				}
			}
		}

		// Do we have a candidate?
		if bestScore < 0 {
			if bestScore == -1 {
				d.stats.rejectedUnknownICAO++
				if d.statsCollector != nil {
					d.statsCollector.AddDemodResult(false, false, 0, true)
				}
			} else {
				d.stats.rejectedBad++
				if d.statsCollector != nil {
					d.statsCollector.AddDemodResult(false, false, 0, false)
				}
			}
			continue
		}

		msgLen := ModesMessageLenByType(int(bestMsg[0] >> 3))

		// Decode the message
		mm, result := DecodeModesMessage(bestMsg[:msgLen/8])
		if result < 0 {
			if result == -1 {
				d.stats.rejectedUnknownICAO++
				if d.statsCollector != nil {
					d.statsCollector.AddDemodResult(false, false, 0, true)
				}
			} else {
				d.stats.rejectedBad++
				if d.statsCollector != nil {
					d.statsCollector.AddDemodResult(false, false, 0, false)
				}
			}
			continue
		}

		d.stats.accepted[mm.Corrected]++
		if d.statsCollector != nil {
			d.statsCollector.AddDemodResult(false, true, mm.Corrected, false)
		}

		// Set timestamp
		mm.Timestamp = mag.SampleTimestamp + uint64(j*5+bestPhase)
		mm.SysTimestamp = mag.SysTimestamp.Add(time.Duration(j*5+bestPhase) * 83333) // ~83.333ns per unit

		// Measure signal power
		signalLen := msgLen * 12 / 5
		var scaledSignalPower uint64
		for k := 0; k < signalLen; k++ {
			v := uint64(m[j+19+k])
			scaledSignalPower += v * v
		}

		signalPower := float64(scaledSignalPower) / 65535.0 / 65535.0
		mm.SignalLevel = signalPower / float64(signalLen)
		d.stats.signalPowerSum += signalPower
		d.stats.signalPowerCount += int64(signalLen)
		sumScaledSignalPower += scaledSignalPower

		if mm.SignalLevel > d.stats.peakSignalPower {
			d.stats.peakSignalPower = mm.SignalLevel
		}
		if mm.SignalLevel > 0.50119 { // -3dBFS
			d.stats.strongSignalCount++
		}

		// Record in stats collector
		if d.statsCollector != nil {
			d.statsCollector.AddSignalPower(mm.SignalLevel)
		}

		// Add address to known filter (matching C version logic)
		// Only add if no CRC errors and specific DF types (DF17/18 or DF11 with IID=0)
		if mm.Corrected == 0 && (mm.MsgType == 17 || mm.MsgType == 18 || (mm.MsgType == 11 && mm.IID == 0)) {
			d.AddKnownICAO(mm.Addr)
		}

		// Skip over the message
		j += msgLen * 12 / 5

		// Pass to handler
		if d.messageHandler != nil {
			d.messageHandler(mm)
		}
	}

	// Update noise power
	sumSignalPower := float64(sumScaledSignalPower) / 65535.0 / 65535.0
	noisePower := mag.TotalPower - sumSignalPower
	d.stats.noisePowerSum += noisePower
	d.stats.noisePowerCount += int64(mlen)

	// Record in stats collector (average noise per sample)
	if d.statsCollector != nil && mlen > 0 {
		avgNoise := noisePower / float64(mlen)
		for i := 0; i < mlen; i++ {
			d.statsCollector.AddNoisePower(avgNoise)
		}
		d.statsCollector.AddSamplesProcessed(uint64(mlen))
	}
}

// Byte decoding functions for each phase
func (d *Demodulator) decodeBytePhase0(p []uint16) byte {
	var b byte
	if slicePhase0(p) > 0 {
		b |= 0x80
	}
	if slicePhase2(p[2:]) > 0 {
		b |= 0x40
	}
	if slicePhase4(p[4:]) > 0 {
		b |= 0x20
	}
	if slicePhase1(p[7:]) > 0 {
		b |= 0x10
	}
	if slicePhase3(p[9:]) > 0 {
		b |= 0x08
	}
	if slicePhase0(p[12:]) > 0 {
		b |= 0x04
	}
	if slicePhase2(p[14:]) > 0 {
		b |= 0x02
	}
	if slicePhase4(p[16:]) > 0 {
		b |= 0x01
	}
	return b
}

func (d *Demodulator) decodeBytePhase1(p []uint16) byte {
	var b byte
	if slicePhase1(p) > 0 {
		b |= 0x80
	}
	if slicePhase3(p[2:]) > 0 {
		b |= 0x40
	}
	if slicePhase0(p[5:]) > 0 {
		b |= 0x20
	}
	if slicePhase2(p[7:]) > 0 {
		b |= 0x10
	}
	if slicePhase4(p[9:]) > 0 {
		b |= 0x08
	}
	if slicePhase1(p[12:]) > 0 {
		b |= 0x04
	}
	if slicePhase3(p[14:]) > 0 {
		b |= 0x02
	}
	if slicePhase0(p[17:]) > 0 {
		b |= 0x01
	}
	return b
}

func (d *Demodulator) decodeBytePhase2(p []uint16) byte {
	var b byte
	if slicePhase2(p) > 0 {
		b |= 0x80
	}
	if slicePhase4(p[2:]) > 0 {
		b |= 0x40
	}
	if slicePhase1(p[5:]) > 0 {
		b |= 0x20
	}
	if slicePhase3(p[7:]) > 0 {
		b |= 0x10
	}
	if slicePhase0(p[10:]) > 0 {
		b |= 0x08
	}
	if slicePhase2(p[12:]) > 0 {
		b |= 0x04
	}
	if slicePhase4(p[14:]) > 0 {
		b |= 0x02
	}
	if slicePhase1(p[17:]) > 0 {
		b |= 0x01
	}
	return b
}

func (d *Demodulator) decodeBytePhase3(p []uint16) byte {
	var b byte
	if slicePhase3(p) > 0 {
		b |= 0x80
	}
	if slicePhase0(p[3:]) > 0 {
		b |= 0x40
	}
	if slicePhase2(p[5:]) > 0 {
		b |= 0x20
	}
	if slicePhase4(p[7:]) > 0 {
		b |= 0x10
	}
	if slicePhase1(p[10:]) > 0 {
		b |= 0x08
	}
	if slicePhase3(p[12:]) > 0 {
		b |= 0x04
	}
	if slicePhase0(p[15:]) > 0 {
		b |= 0x02
	}
	if slicePhase2(p[17:]) > 0 {
		b |= 0x01
	}
	return b
}

func (d *Demodulator) decodeBytePhase4(p []uint16) byte {
	var b byte
	if slicePhase4(p) > 0 {
		b |= 0x80
	}
	if slicePhase1(p[3:]) > 0 {
		b |= 0x40
	}
	if slicePhase3(p[5:]) > 0 {
		b |= 0x20
	}
	if slicePhase0(p[8:]) > 0 {
		b |= 0x10
	}
	if slicePhase2(p[10:]) > 0 {
		b |= 0x08
	}
	if slicePhase4(p[12:]) > 0 {
		b |= 0x04
	}
	if slicePhase1(p[15:]) > 0 {
		b |= 0x02
	}
	if slicePhase3(p[17:]) > 0 {
		b |= 0x01
	}
	return b
}

// scoreModesMessage scores a Mode S message for validity.
// Returns:
//
//	> 0: valid message, higher score = more confidence
//	-1: unknown ICAO address
//	-2: bad message (checksum failed, invalid format)
func (d *Demodulator) scoreModesMessage(msg []byte, validBits int) int {
	if validBits < 56 {
		return -2
	}

	msgType := int(msg[0] >> 3) // Downlink Format
	msgBits := ModesMessageLenByType(msgType)

	if validBits < msgBits {
		return -2
	}

	// Check for all-zeros message
	allZero := true
	for i := 0; i < msgBits/8; i++ {
		if msg[i] != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return -2
	}

	crc := ModesChecksum(msg, msgBits)

	switch msgType {
	case 0, 4, 5, 16, 24, 25, 26, 27, 28, 29, 30, 31:
		// Short air-air surveillance, altitude reply, Comm-D
		if d.icaoFilterTest(crc) {
			return 1000
		}
		return -1

	case 11: // All-call reply
		iid := crc & 0x7f
		crc = crc & 0xffff80
		addr := getbits(msg, 9, 32)

		ei := ModesChecksumDiagnose(crc, msgBits)
		if ei == nil {
			return -2
		}

		// Don't attempt to fix more than single-bit errors for DF11
		if ei.Errors > 1 {
			return -2
		}

		// Fix errors in address field
		addr = correctAAField(addr, ei)

		if iid == 0 {
			if d.icaoFilterTest(addr) {
				return 1600 / (ei.Errors + 1)
			}
			return 750 / (ei.Errors + 1)
		}
		if d.icaoFilterTest(addr) {
			return 1000 / (ei.Errors + 1)
		}
		return -1

	case 17, 18: // Extended squitter
		ei := ModesChecksumDiagnose(crc, msgBits)
		if ei == nil {
			return -2
		}

		addr := getbits(msg, 9, 32)
		addr = correctAAField(addr, ei)

		if d.icaoFilterTest(addr) {
			return 1800 / (ei.Errors + 1)
		}
		return 1400 / (ei.Errors + 1)

	case 20, 21: // Comm-B, altitude/identity reply
		if d.icaoFilterTest(crc) {
			return 1000
		}
		return -2

	default:
		return -2
	}
}

// icaoFilterTest checks if an ICAO address is in the known filter.
func (d *Demodulator) icaoFilterTest(addr uint32) bool {
	if d.icaoFilter == nil {
		return false
	}
	return d.icaoFilter.Test(addr)
}

// correctAAField corrects errors in the AA (address) field.
func correctAAField(addr uint32, ei *ErrorInfo) uint32 {
	if ei == nil || ei.Errors == 0 {
		return addr
	}

	for i := 0; i < ei.Errors; i++ {
		bitPos := int(ei.Bit[i])
		if bitPos >= 0 && bitPos < 24 {
			// Bit is in the address field (bits 9-32 = indices 8-31)
			// Error bit positions are 0-indexed from start of message
			addr ^= 1 << (23 - bitPos)
		}
	}
	return addr
}

// GetStats returns demodulator statistics.
func (d *Demodulator) GetStats() DemodStats {
	return DemodStats{
		Preambles:           d.stats.preambles,
		RejectedBad:         d.stats.rejectedBad,
		RejectedUnknownICAO: d.stats.rejectedUnknownICAO,
		Accepted:            d.stats.accepted,
		ModeAC:              d.stats.modeAC,
		SignalPowerSum:      d.stats.signalPowerSum,
		SignalPowerCount:    d.stats.signalPowerCount,
		NoisePowerSum:       d.stats.noisePowerSum,
		NoisePowerCount:     d.stats.noisePowerCount,
		PeakSignalPower:     d.stats.peakSignalPower,
		StrongSignalCount:   d.stats.strongSignalCount,
	}
}

// DemodStats contains demodulator statistics.
type DemodStats struct {
	Preambles           int64
	RejectedBad         int64
	RejectedUnknownICAO int64
	Accepted            [MODES_MAX_BITERRORS + 1]int64
	ModeAC              int64
	SignalPowerSum      float64
	SignalPowerCount    int64
	NoisePowerSum       float64
	NoisePowerCount     int64
	PeakSignalPower     float64
	StrongSignalCount   int64
}

// Demodulate2400AC demodulates Mode A/C replies from magnitude samples at 2.4MHz.
// Mode A/C (SSR) uses a different modulation scheme than Mode S:
// - Each bit period is 1.45us (87 samples at 60MHz, ~3.5 samples at 2.4MHz)
// - Pulse width is 0.45us (about 1 sample at 2.4MHz)
// - F1 and F2 framing pulses bracket the data
//
// Bit layout (24 bits total):
//
//	bit  value  meaning
//	-1    0     quiet zone
//	 0    1     F1 (framing pulse 1)
//	 1   C1     \
//	 2   A1      |
//	 3   C2      |
//	 4   A2      | data bits
//	 5   C4      |
//	 6   A4     /
//	 7    0     X1 (quiet zone)
//	 8   B1     \
//	 9   D1      |
//	10   B2      |
//	11   D2      | data bits
//	12   B4      |
//	13   D4     /
//	14    1     F2 (framing pulse 2)
//	15    0     X2 (quiet zone)
//	16    0     X3 (quiet zone)
//	17   SPI    squawk ident
//	18    0     X4 (quiet zone)
//	19-23  0    X5-X9 (quiet zone)
func (d *Demodulator) Demodulate2400AC(mag *MagBuf) {
	if mag == nil || len(mag.Data) == 0 {
		return
	}

	m := mag.Data
	mlen := int(mag.Length)

	// At 2.4MHz sample rate:
	// - 1 bit period = 87/25 samples ≈ 3.48 samples
	// - F1 pulse covers about 3.5 samples
	// - F2 is 14 bit periods after F1

	for f1Sample := 1; f1Sample < mlen-100; f1Sample++ {
		// Look for rising edge (start of F1)
		if m[f1Sample-1] >= m[f1Sample] {
			continue // not a rising edge
		}

		// Check quiet part of F1 bit
		if m[f1Sample+2] > m[f1Sample] || m[f1Sample+2] > m[f1Sample+1] {
			continue // quiet part wasn't sufficiently quiet
		}

		// Estimate F1 signal and noise levels
		f1Noise := (uint32(m[f1Sample-1]) + uint32(m[f1Sample+2])) / 2
		f1Signal := (uint32(m[f1Sample]) + uint32(m[f1Sample+1])) / 2

		// Require 12dB SNR (signal > 4 * noise)
		if f1Noise*4 > f1Signal {
			continue
		}

		// Estimate initial clock phase based on power distribution
		// Clock is in units of 1/25 sample (to match C version's 87 units per bit)
		f1Clock := uint32(25 * f1Sample)
		if m[f1Sample+1] > uint16(f1Noise) {
			f1Clock += 25 * uint32(m[f1Sample+1]-uint16(f1Noise)) / (2 * (f1Signal - f1Noise))
		}

		// Find F2: 14 bit periods after F1
		// Each bit period is 87/25 samples, so 14*87 = 1218 clock units
		f2Clock := f1Clock + (87 * 14)
		f2Sample := int(f2Clock / 25)

		if f2Sample+3 >= mlen {
			break // not enough samples
		}

		// Check F2 rising edge
		if m[f2Sample-1] >= m[f2Sample] {
			continue
		}

		// Check F2 quiet part
		if m[f2Sample+2] > m[f2Sample] || m[f2Sample+2] > m[f2Sample+1] {
			continue
		}

		// Estimate F2 signal and noise
		f2Noise := (uint32(m[f2Sample-1]) + uint32(m[f2Sample+2])) / 2
		f2Signal := (uint32(m[f2Sample]) + uint32(m[f2Sample+1])) / 2

		// Require 12dB SNR
		if f2Noise*4 > f2Signal {
			continue
		}

		f1f2Signal := (f1Signal + f2Signal) / 2

		// Check X1 quiet zone (bit 7)
		x1Clock := f1Clock + (87 * 7)
		x1Sample := int(x1Clock / 25)
		x1Noise := (uint32(m[x1Sample]) + uint32(m[x1Sample+1]) + uint32(m[x1Sample+2])) / 3
		if x1Noise*4 >= f1f2Signal {
			continue
		}

		// Check X2 quiet zone (bit 15)
		x2Clock := f1Clock + (87 * 15)
		x2Sample := int(x2Clock / 25)
		x2Noise := (uint32(m[x2Sample]) + uint32(m[x2Sample+1]) + uint32(m[x2Sample+2])) / 3
		if x2Noise*4 >= f1f2Signal {
			continue
		}

		// Check X3 quiet zone (bit 16)
		x3Clock := f1Clock + (87 * 16)
		x3Sample := int(x3Clock / 25)
		x3Noise := (uint32(m[x3Sample]) + uint32(m[x3Sample+1]) + uint32(m[x3Sample+2])) / 3
		if x3Noise*4 >= f1f2Signal {
			continue
		}

		// Combined noise from quiet zones
		x1x2x3Noise := (x1Noise + x2Noise + x3Noise) / 3
		if x1x2x3Noise*4 >= f1f2Signal {
			continue // require 12dB separation
		}

		// Calculate thresholds using geometric mean for midpoint
		// This ensures signal/midpoint == midpoint/noise
		midpoint := sqrt32(float32(x1x2x3Noise * f1f2Signal))
		quietThreshold := uint32(midpoint)
		noiseThreshold := uint32(midpoint*0.707107 + 0.5)  // -3dB from midpoint
		signalThreshold := uint32(midpoint*1.414214 + 0.5) // +3dB from midpoint

		// Recheck F/X bits with calculated thresholds
		if f1Signal < signalThreshold || f2Signal < signalThreshold {
			continue
		}
		if x1Noise > noiseThreshold || x2Noise > noiseThreshold || x3Noise > noiseThreshold {
			continue
		}

		// Demodulate all 24 bits
		var bits uint32
		var noisyBits uint32
		clock := f1Clock

		for bit := 0; bit < 24; bit++ {
			sample := int(clock / 25)
			if sample+3 >= mlen {
				break
			}

			bits <<= 1
			noisyBits <<= 1

			// Check for excessive noise in quiet period
			if uint32(m[sample+2]) >= quietThreshold {
				noisyBits |= 1
				clock += 87
				continue
			}

			// Decide if bit is on or off
			bitSignal := (uint32(m[sample]) + uint32(m[sample+1])) / 2
			if bitSignal >= signalThreshold {
				bits |= 1
			} else if bitSignal > noiseThreshold {
				// Uncertain bit
				noisyBits |= 1
			}

			clock += 87
		}

		// Reject if any bits were noisy
		if noisyBits != 0 {
			continue
		}

		// Framing bits (F1=bit0, F2=bit14) must be on
		if bits&0x800200 != 0x800200 {
			continue
		}

		// Quiet bits (X1=bit7, X2-X9=bits15-23 except SPI=bit17) must be off
		// Mask: 0000 0001 0000 0001 1011 1111 = 0x0101BF
		if bits&0x0101BF != 0 {
			continue
		}

		// Convert bit layout to standard Mode A/C format:
		// 00 A4 A2 A1  00 B4 B2 B1  SPI C4 C2 C1  00 D4 D2 D1
		modeAC := uint32(0)
		if bits&0x400000 != 0 {
			modeAC |= 0x0010
		} // C1
		if bits&0x200000 != 0 {
			modeAC |= 0x1000
		} // A1
		if bits&0x100000 != 0 {
			modeAC |= 0x0020
		} // C2
		if bits&0x080000 != 0 {
			modeAC |= 0x2000
		} // A2
		if bits&0x040000 != 0 {
			modeAC |= 0x0040
		} // C4
		if bits&0x020000 != 0 {
			modeAC |= 0x4000
		} // A4
		if bits&0x008000 != 0 {
			modeAC |= 0x0100
		} // B1
		if bits&0x004000 != 0 {
			modeAC |= 0x0001
		} // D1
		if bits&0x002000 != 0 {
			modeAC |= 0x0200
		} // B2
		if bits&0x001000 != 0 {
			modeAC |= 0x0002
		} // D2
		if bits&0x000800 != 0 {
			modeAC |= 0x0400
		} // B4
		if bits&0x000400 != 0 {
			modeAC |= 0x0004
		} // D4
		if bits&0x000040 != 0 {
			modeAC |= 0x0080
		} // SPI

		// Decode the Mode A/C message
		mm := DecodeModeAMessage(int(modeAC))
		if mm == nil {
			continue
		}

		// Set timestamp
		mm.Timestamp = mag.SampleTimestamp + uint64(f1Clock/5) // 60MHz -> 12MHz

		// Pass to handler
		if d.messageHandler != nil {
			d.messageHandler(mm)
		}

		// Skip past this message (24 bits * 87/25 samples ≈ 83 samples)
		f1Sample += int(24 * 87 / 25)
		d.stats.modeAC++
		if d.statsCollector != nil {
			d.statsCollector.AddModeACMessage()
		}
	}
}

// sqrt32 computes the square root of a float32
func sqrt32(x float32) float32 {
	if x <= 0 {
		return 0
	}
	// Newton-Raphson iteration
	r := x
	for i := 0; i < 10; i++ {
		r = (r + x/r) / 2
	}
	return r
}
