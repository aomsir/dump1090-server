// Package rtlsdr provides Go bindings to librtlsdr for RTL-SDR USB devices.
//
// convert.go: IQ to magnitude conversion for RTL-SDR data
package rtlsdr

import (
	"math"
	"sync"
)

// MagLUT is a lookup table for fast UC8 -> magnitude conversion
// Indexed by [I<<8 | Q] where I and Q are unsigned 8-bit values
var magLUT [65536]uint16
var magLUTOnce sync.Once

// InitMagLUT initializes the magnitude lookup table
// This should be called once before using ConvertUC8
func InitMagLUT() {
	magLUTOnce.Do(func() {
		for i := 0; i < 256; i++ {
			for q := 0; q < 256; q++ {
				// Convert to centered float
				fi := (float64(i) - 127.5) / 127.5
				fq := (float64(q) - 127.5) / 127.5

				// Magnitude (with clipping at 1.0)
				magsq := fi*fi + fq*fq
				if magsq > 1.0 {
					magsq = 1.0
				}

				// Scale to uint16
				mag := uint16(math.Sqrt(magsq)*65535 + 0.5)
				magLUT[i<<8|q] = mag
			}
		}
	})
}

// ConverterState holds DC blocking filter state
type ConverterState struct {
	dcA  float32 // DC block coefficient a
	dcB  float32 // DC block coefficient b
	z1I  float32 // IIR state for I
	z1Q  float32 // IIR state for Q
}

// NewConverter creates a new converter state with optional DC blocking
// sampleRate is the sample rate in Hz
// filterDC enables DC blocking filter at 1Hz
func NewConverter(sampleRate float64, filterDC bool) *ConverterState {
	state := &ConverterState{}

	if filterDC {
		// DC block filter at 1Hz
		state.dcB = float32(math.Exp(-2.0 * math.Pi * 1.0 / sampleRate))
		state.dcA = 1.0 - state.dcB
	} else {
		state.dcB = 1.0
		state.dcA = 0.0
	}

	return state
}

// ConvertUC8NoDC converts unsigned 8-bit IQ samples to magnitude using lookup table
// This is the fastest converter but has no DC blocking
// iq is interleaved I,Q,I,Q... data
// mag is the output magnitude buffer (must be len(iq)/2)
// Returns total power (normalized)
func ConvertUC8NoDC(iq []byte, mag []uint16) float64 {
	InitMagLUT()

	nsamples := len(iq) / 2
	if len(mag) < nsamples {
		return 0
	}

	var power uint64

	// Process 8 samples at a time
	i := 0
	for ; i < nsamples-7; i += 8 {
		idx := i * 2

		m := magLUT[uint16(iq[idx])<<8|uint16(iq[idx+1])]
		mag[i] = m
		power += uint64(m) * uint64(m)

		m = magLUT[uint16(iq[idx+2])<<8|uint16(iq[idx+3])]
		mag[i+1] = m
		power += uint64(m) * uint64(m)

		m = magLUT[uint16(iq[idx+4])<<8|uint16(iq[idx+5])]
		mag[i+2] = m
		power += uint64(m) * uint64(m)

		m = magLUT[uint16(iq[idx+6])<<8|uint16(iq[idx+7])]
		mag[i+3] = m
		power += uint64(m) * uint64(m)

		m = magLUT[uint16(iq[idx+8])<<8|uint16(iq[idx+9])]
		mag[i+4] = m
		power += uint64(m) * uint64(m)

		m = magLUT[uint16(iq[idx+10])<<8|uint16(iq[idx+11])]
		mag[i+5] = m
		power += uint64(m) * uint64(m)

		m = magLUT[uint16(iq[idx+12])<<8|uint16(iq[idx+13])]
		mag[i+6] = m
		power += uint64(m) * uint64(m)

		m = magLUT[uint16(iq[idx+14])<<8|uint16(iq[idx+15])]
		mag[i+7] = m
		power += uint64(m) * uint64(m)
	}

	// Handle remaining samples
	for ; i < nsamples; i++ {
		idx := i * 2
		m := magLUT[uint16(iq[idx])<<8|uint16(iq[idx+1])]
		mag[i] = m
		power += uint64(m) * uint64(m)
	}

	return float64(power) / 65535.0 / 65535.0
}

// ConvertUC8 converts unsigned 8-bit IQ samples to magnitude with DC blocking
// iq is interleaved I,Q,I,Q... data
// mag is the output magnitude buffer (must be len(iq)/2)
// Returns total power (normalized)
func (s *ConverterState) ConvertUC8(iq []byte, mag []uint16) float64 {
	nsamples := len(iq) / 2
	if len(mag) < nsamples {
		return 0
	}

	var power float32
	z1I := s.z1I
	z1Q := s.z1Q
	dcA := s.dcA
	dcB := s.dcB

	for i := 0; i < nsamples; i++ {
		idx := i * 2
		I := iq[idx]
		Q := iq[idx+1]

		// Convert to centered float
		fI := (float32(I) - 127.5) / 127.5
		fQ := (float32(Q) - 127.5) / 127.5

		// DC block filter
		z1I = fI*dcA + z1I*dcB
		z1Q = fQ*dcA + z1Q*dcB
		fI -= z1I
		fQ -= z1Q

		// Magnitude squared
		magsq := fI*fI + fQ*fQ
		if magsq > 1.0 {
			magsq = 1.0
		}

		power += magsq
		mag[i] = uint16(math.Sqrt(float64(magsq))*65535.0 + 0.5)
	}

	s.z1I = z1I
	s.z1Q = z1Q

	return float64(power)
}

// ConvertSC16 converts signed 16-bit IQ samples to magnitude with DC blocking
// iq is interleaved I,Q,I,Q... data (little-endian)
// mag is the output magnitude buffer (must be len(iq)/4)
// Returns total power (normalized)
func (s *ConverterState) ConvertSC16(iq []byte, mag []uint16) float64 {
	nsamples := len(iq) / 4
	if len(mag) < nsamples {
		return 0
	}

	var power float32
	z1I := s.z1I
	z1Q := s.z1Q
	dcA := s.dcA
	dcB := s.dcB

	for i := 0; i < nsamples; i++ {
		idx := i * 4
		// Little-endian 16-bit signed
		I := int16(uint16(iq[idx]) | uint16(iq[idx+1])<<8)
		Q := int16(uint16(iq[idx+2]) | uint16(iq[idx+3])<<8)

		// Convert to float [-1, 1]
		fI := float32(I) / 32768.0
		fQ := float32(Q) / 32768.0

		// DC block filter
		z1I = fI*dcA + z1I*dcB
		z1Q = fQ*dcA + z1Q*dcB
		fI -= z1I
		fQ -= z1Q

		// Magnitude squared
		magsq := fI*fI + fQ*fQ
		if magsq > 1.0 {
			magsq = 1.0
		}

		power += magsq
		mag[i] = uint16(math.Sqrt(float64(magsq))*65535.0 + 0.5)
	}

	s.z1I = z1I
	s.z1Q = z1Q

	return float64(power)
}

// Reset resets the converter state
func (s *ConverterState) Reset() {
	s.z1I = 0
	s.z1Q = 0
}

// ConvertSC16Q11 converts signed 16-bit IQ samples with Q11 scaling to magnitude
// This format is used by USRP B200 and RTL-SDR Pro Plus devices
// The samples are 11-bit quantization scaled to 16-bit, so divide by 2048 instead of 32768
// iq is interleaved I,Q,I,Q... data (little-endian)
// mag is the output magnitude buffer (must be len(iq)/4)
// Returns total power (normalized)
func (s *ConverterState) ConvertSC16Q11(iq []byte, mag []uint16) float64 {
	nsamples := len(iq) / 4
	if len(mag) < nsamples {
		return 0
	}

	var power float32
	z1I := s.z1I
	z1Q := s.z1Q
	dcA := s.dcA
	dcB := s.dcB

	for i := 0; i < nsamples; i++ {
		idx := i * 4
		// Little-endian 16-bit signed
		I := int16(uint16(iq[idx]) | uint16(iq[idx+1])<<8)
		Q := int16(uint16(iq[idx+2]) | uint16(iq[idx+3])<<8)

		// Convert to float [-1, 1] using Q11 scaling (divide by 2048)
		fI := float32(I) / 2048.0
		fQ := float32(Q) / 2048.0

		// DC block filter
		z1I = fI*dcA + z1I*dcB
		z1Q = fQ*dcA + z1Q*dcB
		fI -= z1I
		fQ -= z1Q

		// Magnitude squared
		magsq := fI*fI + fQ*fQ
		if magsq > 1.0 {
			magsq = 1.0
		}

		power += magsq
		mag[i] = uint16(math.Sqrt(float64(magsq))*65535.0 + 0.5)
	}

	s.z1I = z1I
	s.z1Q = z1Q

	return float64(power)
}
