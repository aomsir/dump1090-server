package modes

import (
	"testing"
)

func TestModeAToModeCAltitude(t *testing.T) {
	// Known Gillham/Mode A to Mode C altitude conversions.
	// Mode A input format: 00:A4:A2:A1:00:B4:B2:B1:00:C4:C2:C1:00:D4:D2:D1
	// Returns altitude in hundreds of feet (can be negative for below sea level).
	tests := []struct {
		name    string
		modeA   uint32
		wantAlt int
	}{
		// Valid altitude conversions (verified against dump1090-mutability C code)
		{"sea_level_0ft", 0x0620, 0},          // B2+B4+C2 → 0 (= 0 ft)
		{"one_hundred_200ft", 0x0610, 2},       // B2+B4+C1 → 2 (= 200 ft)
		{"two_hundred_300ft", 0x0210, 3},       // B2+C1 → 3 (= 300 ft)
		{"four_hundred_500ft", 0x0220, 5},      // B2+C2 → 5 (= 500 ft)
		{"five_hundred_600ft", 0x0260, 6},      // B2+C2+C4 → 6 (= 600 ft)
		{"six_hundred_700ft", 0x0240, 7},       // B2+C4 → 7 (= 700 ft)
		{"negative_800ft", 0x0010, -8},         // C1 → -8 (= -800 ft)
		{"negative_1000ft", 0x0020, -10},       // C2 → -10 (= -1000 ft)
		{"negative_1200ft", 0x0040, -12},       // C4 → -12 (= -1200 ft)
		{"B1_altitude_2300ft", 0x0110, 23},     // B1+C1 → 23 (= 2300 ft)
		{"B1_altitude_2500ft", 0x0120, 25},     // B1+C2 → 25 (= 2500 ft)
		{"negative_500ft", 0x0420, -5},         // B4+C2 → -5 (= -500 ft)
		{"negative_300ft", 0x0440, -3},         // B4+C4 → -3 (= -300 ft)

		// Invalid codes
		{"zero_all_bits_off", 0x0000, INVALID_ALTITUDE}, // C bits all zero
		{"D1_illegal", 0x0001, INVALID_ALTITUDE},        // D1 set is illegal
		{"C1_D1_illegal", 0x0011, INVALID_ALTITUDE},     // C1+D1
		{"A_only_no_C", 0x1000, INVALID_ALTITUDE},       // A1 only, C bits zero
		{"B_only_no_C", 0x0100, INVALID_ALTITUDE},       // B1 only, C bits zero
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ModeAToModeC(tt.modeA)
			if got != tt.wantAlt {
				t.Errorf("ModeAToModeC(0x%04X) = %d, want %d", tt.modeA, got, tt.wantAlt)
			}
		})
	}
}

func TestModeAToModeCInvalidCodes(t *testing.T) {
	// All C bits zero → invalid
	if ModeAToModeC(0x0000) != INVALID_ALTITUDE {
		t.Error("All-zero C bits should be invalid")
	}

	// D1 set is illegal
	if ModeAToModeC(0x0001) != INVALID_ALTITUDE {
		t.Error("D1 set should be invalid")
	}

	// No C bits (only A/B bits with no C) → invalid
	if ModeAToModeC(0x1000) != INVALID_ALTITUDE {
		t.Error("A1 only (no C bits) should be invalid")
	}

	// C1+C4 → oneHundreds = 7^1 = 6 → INVALID (> 5)
	if ModeAToModeC(0x0050) != INVALID_ALTITUDE {
		t.Error("C1+C4 should be invalid (oneHundreds > 5)")
	}
}

func TestDecodeModeAMessageSquawkAndAltitude(t *testing.T) {
	// Mode A 0x0620: B2+B4+C2 → altitude 0 (sea level), squawk 0x0620 & 0x7777
	modeA := 0x0620
	mm := DecodeModeAMessage(modeA)

	if mm == nil {
		t.Fatal("DecodeModeAMessage returned nil")
	}

	// MsgType must be 32 (Mode A/C)
	if mm.MsgType != 32 {
		t.Errorf("MsgType = %d, want 32", mm.MsgType)
	}

	// Squawk should be valid
	if !mm.SquawkValid {
		t.Error("SquawkValid should be true")
	}

	// Squawk is masked with 0x7777
	expectedSquawk := uint32(modeA & 0x7777)
	if mm.Squawk != expectedSquawk {
		t.Errorf("Squawk = 0x%04X, want 0x%04X", mm.Squawk, expectedSquawk)
	}

	// SPI should not be set for this code
	if mm.SPI {
		t.Error("SPI should be false for code without SPI bit")
	}

	// Altitude should be valid (Gillham decoded, Mode C → 0 ft)
	if !mm.AltitudeValid {
		t.Error("AltitudeValid should be true for valid Gillham code")
	}

	if mm.Altitude != 0 {
		t.Errorf("Altitude = %d, want 0", mm.Altitude)
	}

	if mm.AltitudeUnit != UNIT_FEET {
		t.Errorf("AltitudeUnit = %d, want UNIT_FEET", mm.AltitudeUnit)
	}

	if mm.AltitudeSource != ALTITUDE_BARO {
		t.Errorf("AltitudeSource = %d, want ALTITUDE_BARO", mm.AltitudeSource)
	}
}

func TestDecodeModeAMessageHigherAltitude(t *testing.T) {
	// Mode A 0x0110: B1+C1 → altitude 2300 ft
	modeA := 0x0110
	mm := DecodeModeAMessage(modeA)

	if mm == nil {
		t.Fatal("DecodeModeAMessage returned nil")
	}

	if mm.MsgType != 32 {
		t.Errorf("MsgType = %d, want 32", mm.MsgType)
	}

	if !mm.AltitudeValid {
		t.Fatal("AltitudeValid should be true")
	}

	if mm.Altitude != 2300 {
		t.Errorf("Altitude = %d, want 2300", mm.Altitude)
	}
}

func TestDecodeModeAMessageSPI(t *testing.T) {
	// Mode A with SPI (ident) bit set: bit 7 = 0x0080
	// When SPI is set, altitude should NOT be decoded
	modeA := 0x06A0 // B2+B4+C2(0x0620) + SPI(0x0080)
	mm := DecodeModeAMessage(modeA)

	if mm == nil {
		t.Fatal("DecodeModeAMessage returned nil")
	}

	if !mm.SPI {
		t.Error("SPI should be true when bit 7 is set")
	}

	if !mm.SPIValid {
		t.Error("SPIValid should be true")
	}

	// When SPI is set, altitude should NOT be decoded
	if mm.AltitudeValid {
		t.Error("AltitudeValid should be false when SPI is set")
	}
}

func TestDecodeModeAMessageInvalidModeC(t *testing.T) {
	// Mode A with all C bits zero (invalid Gillham) but SPI not set
	// Mode A 0x0002: D2 set, C bits zero → invalid altitude
	modeA := 0x0002
	mm := DecodeModeAMessage(modeA)

	if mm == nil {
		t.Fatal("DecodeModeAMessage returned nil")
	}

	// Altitude should NOT be valid (ModeAToModeC returns INVALID_ALTITUDE)
	if mm.AltitudeValid {
		t.Error("AltitudeValid should be false for invalid Gillham code")
	}
}

func TestDecodeModeAMessageAddrField(t *testing.T) {
	// Mode A messages get a synthetic ICAO address with MODES_NON_ICAO_ADDRESS bit set
	modeA := 0x1234
	mm := DecodeModeAMessage(modeA)

	if mm == nil {
		t.Fatal("DecodeModeAMessage returned nil")
	}

	// Address should have MODES_NON_ICAO_ADDRESS bit set
	if mm.Addr&MODES_NON_ICAO_ADDRESS == 0 {
		t.Error("Mode A/C address should have MODES_NON_ICAO_ADDRESS bit set")
	}

	// Ident bit (0x0080) should be stripped from address
	addrWithoutIdent := uint32(modeA&0x0000FF7F) | MODES_NON_ICAO_ADDRESS
	if mm.Addr != addrWithoutIdent {
		t.Errorf("Addr = 0x%06X, want 0x%06X", mm.Addr, addrWithoutIdent)
	}
}

func TestDecodeModeAMessageNegativeAltitude(t *testing.T) {
	// Mode A 0x0010: C1 → altitude -800 ft (below sea level)
	modeA := 0x0010
	mm := DecodeModeAMessage(modeA)

	if mm == nil {
		t.Fatal("DecodeModeAMessage returned nil")
	}

	// The function should still return a valid altitude (even if negative)
	if !mm.AltitudeValid {
		t.Fatal("AltitudeValid should be true")
	}

	if mm.Altitude != -800 {
		t.Errorf("Altitude = %d, want -800", mm.Altitude)
	}
}
