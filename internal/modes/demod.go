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
	// icaoFilter contains known ICAO addresses for scoring
	icaoFilter map[uint32]int64 // address -> last seen timestamp

	// Statistics
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
		icaoFilter: make(map[uint32]int64),
	}
}

// SetMessageHandler sets the callback for decoded messages.
func (d *Demodulator) SetMessageHandler(handler func(*Message)) {
	d.messageHandler = handler
}

// AddKnownICAO adds an ICAO address to the known filter.
func (d *Demodulator) AddKnownICAO(addr uint32) {
	d.icaoFilter[addr] = time.Now().UnixMilli()
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
			} else {
				d.stats.rejectedBad++
			}
			continue
		}

		msgLen := ModesMessageLenByType(int(bestMsg[0] >> 3))

		// Decode the message
		mm, result := DecodeModesMessage(bestMsg[:msgLen/8])
		if result < 0 {
			if result == -1 {
				d.stats.rejectedUnknownICAO++
			} else {
				d.stats.rejectedBad++
			}
			continue
		}

		d.stats.accepted[mm.Corrected]++

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

		// Add address to known filter
		d.AddKnownICAO(mm.Addr)

		// Skip over the message
		j += msgLen * 12 / 5

		// Pass to handler
		if d.messageHandler != nil {
			d.messageHandler(mm)
		}
	}

	// Update noise power
	sumSignalPower := float64(sumScaledSignalPower) / 65535.0 / 65535.0
	d.stats.noisePowerSum += mag.TotalPower - sumSignalPower
	d.stats.noisePowerCount += int64(mlen)
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
	_, ok := d.icaoFilter[addr]
	return ok
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
