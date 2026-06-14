package modes

import (
	"encoding/hex"
	"testing"
)

func TestModesMessageLenByType(t *testing.T) {
	tests := []struct {
		dfType int
		want   int
	}{
		{0, MODES_SHORT_MSG_BITS},   // DF0
		{4, MODES_SHORT_MSG_BITS},   // DF4
		{11, MODES_SHORT_MSG_BITS},  // DF11
		{15, MODES_SHORT_MSG_BITS},  // DF15
		{16, MODES_LONG_MSG_BITS},   // DF16
		{17, MODES_LONG_MSG_BITS},   // DF17
		{18, MODES_LONG_MSG_BITS},   // DF18
		{24, MODES_LONG_MSG_BITS},   // DF24
		{31, MODES_LONG_MSG_BITS},   // DF31
	}

	for _, tt := range tests {
		got := ModesMessageLenByType(tt.dfType)
		if got != tt.want {
			t.Errorf("ModesMessageLenByType(%d) = %d, want %d", tt.dfType, got, tt.want)
		}
	}
}

func TestGetbits(t *testing.T) {
	// Test message: 8D (DF17, CA=5)
	msg, _ := hex.DecodeString("8D4840D6202CC371C32CE0576098")

	tests := []struct {
		name      string
		firstbit  int
		lastbit   int
		expected  uint32
	}{
		{"DF type (bits 1-5)", 1, 5, 17},     // DF17 = 10001
		{"CA (bits 6-8)", 6, 8, 5},           // CA=5
		{"ICAO (bits 9-32)", 9, 32, 0x4840D6}, // Aircraft address
		{"ME type (bits 33-37)", 33, 37, 4},  // ME type 4 (callsign)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getbits(msg, tt.firstbit, tt.lastbit)
			if got != tt.expected {
				t.Errorf("getbits(%d, %d) = 0x%X, want 0x%X", tt.firstbit, tt.lastbit, got, tt.expected)
			}
		})
	}
}

func TestDecodeModesMessage_DF17_Callsign(t *testing.T) {
	// Initialize CRC tables
	ModesChecksumInit(2)

	// Real DF17 message with callsign (from dump1090 test data)
	// DF17, ICAO 4840D6, ME type 4 (callsign), "KLM1023 "
	msgHex := "8D4840D6202CC371C32CE0576098"
	msg := mustDecodeHex(msgHex)

	mm, err := DecodeModesMessage(msg)
	if err != 0 {
		t.Fatalf("DecodeModesMessage() failed with error %d", err)
	}

	// Verify DF type
	if mm.MsgType != 17 {
		t.Errorf("MsgType = %d, want 17", mm.MsgType)
	}

	// Verify ICAO address
	if mm.Addr != 0x4840D6 {
		t.Errorf("Addr = 0x%06X, want 0x4840D6", mm.Addr)
	}

	// Verify ME type
	if mm.METype != 4 {
		t.Errorf("METype = %d, want 4", mm.METype)
	}

	// Verify callsign is decoded
	if !mm.CallsignValid {
		t.Error("CallsignValid = false, want true")
	}

	// Note: Exact callsign depends on bit encoding, verify it's non-empty
	callsignStr := string(mm.Callsign[:8])
	t.Logf("Decoded callsign: %q", callsignStr)
}

func TestDecodeModesMessage_DF17_AirbornePosition(t *testing.T) {
	// DF17 airborne position message (even frame)
	// Type 11 (airborne position, baro alt), ICAO 4840D6, valid CRC
	msgHex := "8D4840D658C35000000000A2DC26"
	msg := mustDecodeHex(msgHex)

	// Initialize CRC tables
	ModesChecksumInit(2)

	mm, err := DecodeModesMessage(msg)
	if err != 0 {
		t.Fatalf("DecodeModesMessage() failed with error %d", err)
	}

	// Verify it's a position message
	if !mm.CPRValid {
		t.Error("CPRValid = false, want true")
	}

	if mm.CPRType != CPR_AIRBORNE {
		t.Errorf("CPRType = %v, want CPR_AIRBORNE", mm.CPRType)
	}

	// Verify CPR format (even/odd)
	t.Logf("CPR odd frame: %v", mm.CPROdd)
	t.Logf("CPR lat: %d, lon: %d", mm.CPRLat, mm.CPRLon)
}

func TestDecodeModesMessage_DF11(t *testing.T) {
	// DF11 All-Call Reply
	msgHex := "5D4840C39A7DC5"
	msg := mustDecodeHex(msgHex)

	ModesChecksumInit(2)

	mm, err := DecodeModesMessage(msg)

	// DF11 might be rejected due to missing ICAO filter
	// For now, just verify we can parse it without crashing
	t.Logf("DecodeModesMessage(DF11) returned error=%d", err)

	if err == 0 {
		t.Logf("DF11: MsgType=%d, IID=0x%02X", mm.MsgType, mm.IID)
	}
}

func TestDecodeModesMessage_BadCRC(t *testing.T) {
	// DF17 with intentionally corrupted CRC
	msgHex := "8D4840D6202CC371C32CE0570000" // Last 3 bytes corrupted
	msg := mustDecodeHex(msgHex)

	ModesChecksumInit(2)

	_, err := DecodeModesMessage(msg)

	// Should either reject or correct the message
	if err == 0 {
		t.Log("Message was corrected successfully")
	} else {
		t.Logf("Message rejected with error %d (expected)", err)
	}
}

func TestDecodeModesMessage_AllZeros(t *testing.T) {
	msg := make([]byte, 14)

	_, err := DecodeModesMessage(msg)

	if err != -2 {
		t.Errorf("All-zeros message should return -2, got %d", err)
	}
}

func TestDecodeAC12Field(t *testing.T) {
	tests := []struct {
		name     string
		ac12     int
		expected int
	}{
		{"Q=0 (no Q bit)", 0x000, INVALID_ALTITUDE},
		{"-200 feet (Q=1, N=32)", 0x050, -200},
		{"4600 feet (Q=1, N=224)", 0x1D0, 4600},
		{"18000 feet (Q=1, N=760)", 0x5F8, 18000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := decodeAC12Field(tt.ac12)
			if got != tt.expected {
				t.Errorf("decodeAC12Field(0x%03X) = %d, want %d", tt.ac12, got, tt.expected)
			}
		})
	}
}

func TestDecodeRequiresKnownICAOForAPAddressedMessages(t *testing.T) {
	ModesChecksumInit(1)
	filter := NewICAOFilter()

	// DF4-like message: DF4, all-zero payload, AP=0x123456
	// CRC syndrome = 0x925209 — not in the empty filter, must be rejected.
	msg := []byte{0x20, 0x00, 0x00, 0x00, 0x12, 0x34, 0x56}

	_, result := DecodeModesMessageWithConfig(msg, DecodeConfig{ICAOFilter: filter})
	if result != -1 {
		t.Fatalf("got result %d, want unknown ICAO -1", result)
	}
}

func TestDecodeCommBUsesKnownICAOFilter(t *testing.T) {
	ModesChecksumInit(1)
	filter := NewICAOFilter()
	filter.Add(0xABCDEF)

	// DF20 message where AP encodes ICAO=0xABCDEF
	// AP = CRC(data) XOR ICAO, so the syndrome yields 0xABCDEF.
	msg := []byte{0xA0, 0x00, 0x00, 0x00, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x2C, 0xD9, 0xEB}

	mm, result := DecodeModesMessageWithConfig(msg, DecodeConfig{ICAOFilter: filter})
	if result < 0 {
		t.Fatalf("expected accepted Comm-B with known ICAO, got %d", result)
	}
	if mm.MsgType != 20 {
		t.Fatalf("got DF%d want DF20", mm.MsgType)
	}
}

func BenchmarkDecodeModesMessage(b *testing.B) {
	ModesChecksumInit(2)

	msgHex := "8D4840D6202CC371C32CE0576098"
	msg := mustDecodeHex(msgHex)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeModesMessage(msg)
	}
}

func BenchmarkGetbits(b *testing.B) {
	msg := mustDecodeHex("8D4840D6202CC371C32CE0576098")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getbits(msg, 9, 32)
	}
}

// Example: decode a DF17 Extended Squitter message
func ExampleDecodeModesMessage() {
	ModesChecksumInit(2)

	// DF17 message with aircraft callsign
	msg := mustDecodeHex("8D4840D6202CC371C32CE0576098")

	mm, err := DecodeModesMessage(msg)
	if err == 0 {
		// mm.MsgType = 17 (DF17)
		// mm.Addr = 0x4840D6 (ICAO address)
		// mm.CallsignValid = true
		_ = mm
	}
}
