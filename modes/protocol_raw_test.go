package modes

import (
	"testing"
)

func TestParseRawAVRAcceptsUntimedAsterisk(t *testing.T) {
	// *HEX; records should decode to a Mode S message
	// DF17 message (valid CRC): 8D4840D6202CC371C32CE0576098
	input := "*8D4840D6202CC371C32CE0576098;"
	ModesChecksumInit(2)
	mm, err := ParseRawAVR(input, false)
	if err != nil {
		t.Fatalf("ParseRawAVR() failed: %v", err)
	}
	if mm == nil {
		t.Fatal("ParseRawAVR() returned nil message")
	}
	// Verify it's a DF17 (long message)
	if mm.MsgBits != 112 {
		t.Errorf("Expected 112 bits, got %d", mm.MsgBits)
	}
	// Verify first byte is 0x8D (DF17)
	if mm.Msg[0] != 0x8D {
		t.Errorf("Expected first byte 0x8D, got 0x%02X", mm.Msg[0])
	}
}

func TestParseRawAVRAcceptsTimedAtWithoutStarSeparator(t *testing.T) {
	// @TIMESTAMPHEX; records should decode and set TimestampMsg without requiring *
	// Upstream format: @0000000000018D4840D6202CC371C32CE0576098;
	input := "@0000000000018D4840D6202CC371C32CE0576098;"
	ModesChecksumInit(2)
	mm, err := ParseRawAVR(input, false)
	if err != nil {
		t.Fatalf("ParseRawAVR() failed: %v", err)
	}
	if mm == nil {
		t.Fatal("ParseRawAVR() returned nil message")
	}
	// Verify timestamp was parsed
	if mm.TimestampMsg != 1 {
		t.Errorf("Expected TimestampMsg=1, got %d", mm.TimestampMsg)
	}
	// Verify message is valid DF17
	if mm.MsgBits != 112 {
		t.Errorf("Expected 112 bits, got %d", mm.MsgBits)
	}
	if mm.Msg[0] != 0x8D {
		t.Errorf("Expected first byte 0x8D, got 0x%02X", mm.Msg[0])
	}
}

func TestParseRawAVRAcceptsTimedAtWithStarSeparator(t *testing.T) {
	// @TIMESTAMP*HEX; format should also work (backward compatible)
	input := "@000000000001*8D4840D6202CC371C32CE0576098;"
	ModesChecksumInit(2)
	mm, err := ParseRawAVR(input, false)
	if err != nil {
		t.Fatalf("ParseRawAVR() failed: %v", err)
	}
	if mm == nil {
		t.Fatal("ParseRawAVR() returned nil message")
	}
	if mm.TimestampMsg != 1 {
		t.Errorf("Expected TimestampMsg=1, got %d", mm.TimestampMsg)
	}
}

func TestEncodeAVRTimestampMatchesUpstreamFormat(t *testing.T) {
	// Timed raw output should use @%012XHEX;\n, NOT @%012X*HEX;\n
	msg := &Message{
		Msg:       [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
		MsgBits:   112,
		Timestamp: 0x123456789ABC,
	}

	result := EncodeAVR(msg, true)
	// Should NOT contain * after timestamp
	expected := "@123456789ABC8D4840D6202CC371C32CE0576098;\n"
	if result != expected {
		t.Errorf("EncodeAVR() = %q, want %q", result, expected)
	}
}

func TestParseRawAVRModeACWhenEnabled(t *testing.T) {
	// Two-byte Mode A/C raw records should decode only when Mode A/C is enabled
	// Mode A/C format: *HEX; where HEX is 4 chars (2 bytes)
	input := "*7801;"

	// With Mode A/C disabled (default)
	mm, err := ParseRawAVR(input, false)
	if err != nil {
		t.Fatalf("ParseRawAVR() failed: %v", err)
	}
	if mm != nil {
		t.Error("Expected nil message for Mode A/C when disabled")
	}

	// With Mode A/C enabled
	mm, err = ParseRawAVR(input, true)
	if err != nil {
		t.Fatalf("ParseRawAVR() with Mode A/C enabled failed: %v", err)
	}
	if mm == nil {
		t.Fatal("Expected non-nil message for Mode A/C when enabled")
	}
	if mm.MsgType != 32 {
		t.Errorf("Expected MsgType=32 for Mode A/C, got %d", mm.MsgType)
	}
}
