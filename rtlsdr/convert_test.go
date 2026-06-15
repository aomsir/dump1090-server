package rtlsdr

import (
	"math"
	"testing"
)

func TestMagLUTInit(t *testing.T) {
	InitMagLUT()

	// Check some known values
	// DC offset (127, 127) should give low magnitude
	dcMag := magLUT[127<<8|127]
	if dcMag > 1000 {
		t.Errorf("DC offset magnitude too high: %d", dcMag)
	}

	// Full scale I (255, 127) should give high magnitude
	fullI := magLUT[255<<8|127]
	if fullI < 60000 {
		t.Errorf("Full scale I magnitude too low: %d", fullI)
	}

	// Full scale Q (127, 255) should give high magnitude
	fullQ := magLUT[127<<8|255]
	if fullQ < 60000 {
		t.Errorf("Full scale Q magnitude too low: %d", fullQ)
	}

	// Maximum (255, 255) should be clipped
	maxMag := magLUT[255<<8|255]
	if maxMag != 65535 {
		t.Errorf("Maximum magnitude should be 65535, got %d", maxMag)
	}

	// Minimum (0, 0) should also be clipped
	minMag := magLUT[0<<8|0]
	if minMag != 65535 {
		t.Errorf("Minimum magnitude should be 65535 (clipped), got %d", minMag)
	}
}

func TestConvertUC8NoDC(t *testing.T) {
	InitMagLUT()

	// Create test IQ data - DC signal at center (127.5 is center, but we use 127 and 128)
	iq := make([]byte, 1000)
	for i := 0; i < len(iq); i += 2 {
		iq[i] = 128   // I (closest to center)
		iq[i+1] = 128 // Q
	}

	mag := make([]uint16, len(iq)/2)
	power := ConvertUC8NoDC(iq, mag)

	// DC signal near center should give low magnitude
	for i := 0; i < len(mag); i++ {
		if mag[i] > 2000 {
			t.Errorf("DC signal magnitude at %d too high: %d", i, mag[i])
			break
		}
	}

	// Power should be very low
	if power > 0.05 {
		t.Errorf("DC signal power too high: %f", power)
	}
}

func TestConvertUC8NoDCFullScale(t *testing.T) {
	InitMagLUT()

	// Create test IQ data - full scale I
	iq := make([]byte, 1000)
	for i := 0; i < len(iq); i += 2 {
		iq[i] = 255   // I full scale
		iq[i+1] = 127 // Q at center
	}

	mag := make([]uint16, len(iq)/2)
	power := ConvertUC8NoDC(iq, mag)

	// Full scale should give high magnitude
	for i := 0; i < len(mag); i++ {
		if mag[i] < 60000 {
			t.Errorf("Full scale magnitude at %d too low: %d", i, mag[i])
			break
		}
	}

	// Power should be significant
	if power < 0.5 {
		t.Errorf("Full scale power too low: %f", power)
	}
}

func TestConverterStateUC8(t *testing.T) {
	// Create converter with DC blocking
	conv := NewConverter(2400000, true)

	// Create test IQ data - constant signal with DC offset
	// The DC blocker should reduce this to near zero over time
	iq := make([]byte, 100000) // More samples for filter to settle
	for i := 0; i < len(iq); i += 2 {
		iq[i] = 150   // I with DC offset
		iq[i+1] = 150 // Q with DC offset
	}

	mag := make([]uint16, len(iq)/2)
	conv.ConvertUC8(iq, mag)

	// After DC blocking settles, magnitude should be much lower than without DC blocking
	// The filter won't completely remove it with only a few time constants
	avgMagEnd := 0.0
	for i := len(mag) - 1000; i < len(mag); i++ {
		avgMagEnd += float64(mag[i])
	}
	avgMagEnd /= 1000

	avgMagStart := 0.0
	for i := 0; i < 1000; i++ {
		avgMagStart += float64(mag[i])
	}
	avgMagStart /= 1000

	// The signal at the end should be significantly lower than at the start
	// as the DC block removes the constant offset
	if avgMagEnd >= avgMagStart {
		t.Errorf("DC blocking not reducing signal: start=%f, end=%f", avgMagStart, avgMagEnd)
	}

	t.Logf("DC blocking: start avg=%f, end avg=%f, reduction=%.1f%%",
		avgMagStart, avgMagEnd, (1-avgMagEnd/avgMagStart)*100)
}

func TestConverterStateReset(t *testing.T) {
	conv := NewConverter(2400000, true)

	// Set some state
	conv.z1I = 0.5
	conv.z1Q = 0.5

	// Reset
	conv.Reset()

	if conv.z1I != 0 || conv.z1Q != 0 {
		t.Error("Reset did not clear state")
	}
}

func TestNewConverterNoDC(t *testing.T) {
	conv := NewConverter(2400000, false)

	// No DC blocking should have dcA=0, dcB=1
	if conv.dcA != 0 || conv.dcB != 1 {
		t.Errorf("No DC blocking coefficients wrong: a=%f, b=%f", conv.dcA, conv.dcB)
	}
}

func TestNewConverterWithDC(t *testing.T) {
	conv := NewConverter(2400000, true)

	// DC blocking should have dcA > 0, dcB < 1
	if conv.dcA <= 0 || conv.dcB >= 1 {
		t.Errorf("DC blocking coefficients wrong: a=%f, b=%f", conv.dcA, conv.dcB)
	}

	// dcA + dcB should be approximately 1
	sum := float64(conv.dcA) + float64(conv.dcB)
	if math.Abs(sum-1.0) > 0.0001 {
		t.Errorf("DC blocking coefficients don't sum to 1: %f", sum)
	}
}

func TestConvertSC16(t *testing.T) {
	conv := NewConverter(2400000, false)

	// Create test SC16 data - DC at zero
	iq := make([]byte, 4000) // 1000 samples
	// Leave as zeros (DC at zero for signed)

	mag := make([]uint16, len(iq)/4)
	conv.ConvertSC16(iq, mag)

	// Zero signal should give zero magnitude
	for i := 0; i < len(mag); i++ {
		if mag[i] > 100 {
			t.Errorf("Zero signal magnitude at %d too high: %d", i, mag[i])
			break
		}
	}
}

func TestConvertSC16Q11(t *testing.T) {
	conv := NewConverter(2400000, false)

	// Create test SC16Q11 data - DC at zero
	iq := make([]byte, 4000) // 1000 samples
	// Leave as zeros (DC at zero for signed)

	mag := make([]uint16, len(iq)/4)
	conv.ConvertSC16Q11(iq, mag)

	// Zero signal should give zero magnitude
	for i := 0; i < len(mag); i++ {
		if mag[i] > 100 {
			t.Errorf("Zero signal magnitude at %d too high: %d", i, mag[i])
			break
		}
	}
}

func TestConverterStateWithDCFilter(t *testing.T) {
	// Test that DC filter is properly applied for all formats
	convUC8 := NewConverter(2400000, true)
	convSC16 := NewConverter(2400000, true)
	convSC16Q11 := NewConverter(2400000, true)

	// Verify DC filter coefficients are set
	if convUC8.dcA <= 0 {
		t.Error("UC8 converter DC filter coefficient dcA should be > 0")
	}
	if convSC16.dcA <= 0 {
		t.Error("SC16 converter DC filter coefficient dcA should be > 0")
	}
	if convSC16Q11.dcA <= 0 {
		t.Error("SC16Q11 converter DC filter coefficient dcA should be > 0")
	}
}

func BenchmarkConvertUC8NoDC(b *testing.B) {
	InitMagLUT()

	// Typical buffer size for dump1090
	iq := make([]byte, 262144*2) // 256K samples
	for i := range iq {
		iq[i] = byte(i % 256)
	}
	mag := make([]uint16, len(iq)/2)

	b.SetBytes(int64(len(iq)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ConvertUC8NoDC(iq, mag)
	}
}

func BenchmarkConvertUC8WithDC(b *testing.B) {
	conv := NewConverter(2400000, true)

	// Typical buffer size for dump1090
	iq := make([]byte, 262144*2)
	for i := range iq {
		iq[i] = byte(i % 256)
	}
	mag := make([]uint16, len(iq)/2)

	b.SetBytes(int64(len(iq)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		conv.ConvertUC8(iq, mag)
	}
}

func BenchmarkMagLUTLookup(b *testing.B) {
	InitMagLUT()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = magLUT[uint16(i&0xFF)<<8|uint16((i>>8)&0xFF)]
	}
}

func TestDefaultConfigAGCOff(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.EnableAGC {
		t.Error("DefaultConfig().EnableAGC should be false (AGC off by default)")
	}
}
