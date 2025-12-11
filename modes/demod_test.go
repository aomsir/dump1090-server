package modes

import (
	"testing"
	"time"
)

func TestSliceFunctions(t *testing.T) {
	// Test slice functions with known values
	// These functions sum to zero, providing DC offset immunity

	// High-low transition (representing a '1' bit)
	m := []uint16{1000, 500, 100, 50}

	// Phase 0: 5*m[0] - 3*m[1] - 2*m[2]
	// = 5*1000 - 3*500 - 2*100 = 5000 - 1500 - 200 = 3300 > 0 => '1'
	if slicePhase0(m) <= 0 {
		t.Error("slicePhase0 should return > 0 for high-low transition")
	}

	// Low-high transition (representing a '0' bit)
	mLow := []uint16{100, 500, 1000, 950}

	// Phase 0: 5*100 - 3*500 - 2*1000 = 500 - 1500 - 2000 = -3000 < 0 => '0'
	if slicePhase0(mLow) >= 0 {
		t.Error("slicePhase0 should return < 0 for low-high transition")
	}
}

func TestSliceFunctionsDCImmunity(t *testing.T) {
	// Adding constant DC offset should not change result
	m := []uint16{1000, 500, 100, 50}
	result1 := slicePhase0(m)

	// Add DC offset
	mOffset := []uint16{2000, 1500, 1100, 1050}
	result2 := slicePhase0(mOffset)

	// Results should have same sign
	if (result1 > 0) != (result2 > 0) {
		t.Errorf("slicePhase0 not DC immune: %d vs %d", result1, result2)
	}
}

func TestDemodulatorCreation(t *testing.T) {
	d := NewDemodulator()
	if d == nil {
		t.Fatal("NewDemodulator returned nil")
	}

	if d.icaoFilter == nil {
		t.Error("icaoFilter not initialized")
	}
}

func TestDemodulatorICAOFilter(t *testing.T) {
	d := NewDemodulator()

	// Initially no addresses
	if d.icaoFilterTest(0x4840D6) {
		t.Error("Address should not be in filter initially")
	}

	// Add address
	d.AddKnownICAO(0x4840D6)

	if !d.icaoFilterTest(0x4840D6) {
		t.Error("Address should be in filter after adding")
	}

	// Different address not in filter
	if d.icaoFilterTest(0x4840D7) {
		t.Error("Different address should not be in filter")
	}
}

func TestScoreModesMessage(t *testing.T) {
	ModesChecksumInit(2)
	d := NewDemodulator()

	// Valid DF17 message
	msg := mustDecodeHex("8D4840D6202CC371C32CE0576098")

	// Without known ICAO, should still return positive score for valid DF17
	score := d.scoreModesMessage(msg, 112)
	if score < 0 {
		t.Errorf("Valid DF17 message scored %d, expected positive", score)
	}

	// Add ICAO to known filter
	d.AddKnownICAO(0x4840D6)

	// With known ICAO, should score higher
	scoreWithICAO := d.scoreModesMessage(msg, 112)
	if scoreWithICAO <= score {
		t.Errorf("Known ICAO should score higher: %d vs %d", scoreWithICAO, score)
	}
}

func TestScoreModesMessageInvalid(t *testing.T) {
	ModesChecksumInit(2)
	d := NewDemodulator()

	// Too short
	if d.scoreModesMessage([]byte{0x8D}, 8) != -2 {
		t.Error("Too short message should return -2")
	}

	// All zeros
	zeros := make([]byte, 14)
	if d.scoreModesMessage(zeros, 112) != -2 {
		t.Error("All-zeros message should return -2")
	}
}

func TestDecodeBytePhases(t *testing.T) {
	d := NewDemodulator()

	// Create synthetic preamble pattern for a known byte
	// This is a simplified test - real signals would have proper Manchester encoding

	// For testing, we'll create samples where slice functions return predictable values
	// High value followed by low = '1' bit
	// Low value followed by high = '0' bit

	// Test pattern that should decode to 0xAA (10101010)
	// Each bit needs 2-3 samples

	// We need at least 20 samples for decodeBytePhase0
	// Let's just verify the function doesn't crash
	samples := make([]uint16, 25)
	for i := range samples {
		if i%3 == 0 {
			samples[i] = 1000
		} else {
			samples[i] = 100
		}
	}

	// Should not panic
	_ = d.decodeBytePhase0(samples)
	_ = d.decodeBytePhase1(samples)
	_ = d.decodeBytePhase2(samples)
	_ = d.decodeBytePhase3(samples)
	_ = d.decodeBytePhase4(samples)
}

func TestDemodulate2400EmptyBuffer(t *testing.T) {
	d := NewDemodulator()

	// Nil buffer
	d.Demodulate2400(nil)

	// Empty buffer
	d.Demodulate2400(&MagBuf{})

	// Too short buffer
	d.Demodulate2400(&MagBuf{
		Data:   make([]uint16, 10),
		Length: 10,
	})

	// No crash = success
}

func TestDemodulate2400WithHandler(t *testing.T) {
	ModesChecksumInit(2)
	d := NewDemodulator()

	var receivedMessages []*Message
	d.SetMessageHandler(func(mm *Message) {
		receivedMessages = append(receivedMessages, mm)
	})

	// Handler should be set
	if d.messageHandler == nil {
		t.Error("Handler not set")
	}
}

func TestCorrectAAField(t *testing.T) {
	// No errors
	addr := uint32(0x4840D6)
	ei := &ErrorInfo{Errors: 0}
	result := correctAAField(addr, ei)
	if result != addr {
		t.Errorf("No-error correction changed address: 0x%06X -> 0x%06X", addr, result)
	}

	// Nil error info
	result = correctAAField(addr, nil)
	if result != addr {
		t.Error("Nil error info changed address")
	}

	// Single bit error in address field
	ei = &ErrorInfo{
		Errors: 1,
		Bit:    [2]int8{0, -1}, // Bit 0 is in address range
	}
	result = correctAAField(addr, ei)
	// Bit 0 should flip bit 23 of address
	expected := addr ^ (1 << 23)
	if result != expected {
		t.Errorf("Single bit correction wrong: got 0x%06X, want 0x%06X", result, expected)
	}
}

func TestDemodStats(t *testing.T) {
	d := NewDemodulator()

	// Initial stats should be zero
	stats := d.GetStats()
	if stats.Preambles != 0 {
		t.Error("Initial preambles should be 0")
	}
	if stats.RejectedBad != 0 {
		t.Error("Initial rejected should be 0")
	}
}

func TestMagBufTimestamp(t *testing.T) {
	// Test MagBuf timestamp handling
	now := time.Now()
	mag := &MagBuf{
		Data:            make([]uint16, 1000),
		Length:          1000,
		SampleTimestamp: 12000000, // 12MHz clock
		SysTimestamp:    now,
	}

	// Verify fields
	if mag.SampleTimestamp != 12000000 {
		t.Error("SampleTimestamp not set correctly")
	}
	if !mag.SysTimestamp.Equal(now) {
		t.Error("SysTimestamp not set correctly")
	}
}

func BenchmarkSlicePhase0(b *testing.B) {
	m := []uint16{1000, 500, 100, 50}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = slicePhase0(m)
	}
}

func BenchmarkDecodeBytePhase0(b *testing.B) {
	d := NewDemodulator()
	samples := make([]uint16, 25)
	for i := range samples {
		samples[i] = uint16(i * 100)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.decodeBytePhase0(samples)
	}
}

func BenchmarkScoreModesMessage(b *testing.B) {
	ModesChecksumInit(2)
	d := NewDemodulator()
	msg := mustDecodeHex("8D4840D6202CC371C32CE0576098")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.scoreModesMessage(msg, 112)
	}
}
