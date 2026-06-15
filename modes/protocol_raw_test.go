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

func TestParseRawAVRBeastAVRFormatWithSignal(t *testing.T) {
	// <TIMESTAMP+SIGNAL*HEXDATA; format should be supported
	// Format: < + 12 hex timestamp + 2 hex signal + * + HEXDATA + ;
	// Example: <000000000001FF*8D4840D6202CC371C32CE0576098;
	ModesChecksumInit(2)
	input := "<000000000001FF*8D4840D6202CC371C32CE0576098;"
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
	// Verify signal level was parsed (0xFF = 255, normalized and squared)
	// Signal level formula: (sig/255)^2 = (255/255)^2 = 1.0
	if mm.SignalLevel != 1.0 {
		t.Errorf("Expected SignalLevel=1.0, got %f", mm.SignalLevel)
	}
	// Verify message is valid DF17
	if mm.MsgBits != 112 {
		t.Errorf("Expected 112 bits, got %d", mm.MsgBits)
	}
}

func TestParseRawAVRBeastAVRFormatMLATSource(t *testing.T) {
	// <TIMESTAMP+SIGNAL*HEXDATA; with MLAT magic timestamp should set SOURCE_MLAT
	// MLAT timestamp: 0xFF004D4C4154 (MAGIC_MLAT_TIMESTAMP)
	ModesChecksumInit(2)
	input := "<FF004D4C4154FF*8D4840D6202CC371C32CE0576098;"
	mm, err := ParseRawAVR(input, false)
	if err != nil {
		t.Fatalf("ParseRawAVR() failed: %v", err)
	}
	if mm == nil {
		t.Fatal("ParseRawAVR() returned nil message")
	}
	// Verify timestamp was parsed
	if mm.TimestampMsg != MAGIC_MLAT_TIMESTAMP {
		t.Errorf("Expected TimestampMsg=0x%012X, got 0x%012X", MAGIC_MLAT_TIMESTAMP, mm.TimestampMsg)
	}
	// Verify source is set to SOURCE_MLAT
	if mm.Source != SOURCE_MLAT {
		t.Errorf("Expected Source=SOURCE_MLAT (%d), got %d", SOURCE_MLAT, mm.Source)
	}
	// Verify Remote is set
	if !mm.Remote {
		t.Error("Expected Remote=true")
	}
}

func TestParseRawAVRBeastAVRFormatRequiresStarSeparator(t *testing.T) {
	// <TIMESTAMP+SIGNAL*HEXDATA; format should require * separator
	// Without *, it should fail
	input := "<000000000001FF8D4840D6202CC371C32CE0576098;"
	_, err := ParseRawAVR(input, false)
	if err == nil {
		t.Error("Expected error for missing * separator in Beast AVR format")
	}
}

func TestParseRawAVRSignalLevelSquared(t *testing.T) {
	// Signal level should be squared (sig/255)^2 for backward compatibility
	// This matches old behavior in main.go
	ModesChecksumInit(2)
	input := "<00000000000180*8D4840D6202CC371C32CE0576098;"
	mm, err := ParseRawAVR(input, false)
	if err != nil {
		t.Fatalf("ParseRawAVR() failed: %v", err)
	}
	if mm == nil {
		t.Fatal("ParseRawAVR() returned nil message")
	}
	// 0x80 = 128, (128/255)^2 ≈ 0.252
	expectedSig := float64(128) / 255.0
	expectedSig = expectedSig * expectedSig
	if mm.SignalLevel < expectedSig-0.001 || mm.SignalLevel > expectedSig+0.001 {
		t.Errorf("Expected SignalLevel≈%f, got %f", expectedSig, mm.SignalLevel)
	}
}

func TestParseRawAVRSetsRemoteFlag(t *testing.T) {
	// ParseRawAVR should set Remote=true for network input
	ModesChecksumInit(2)
	input := "*8D4840D6202CC371C32CE0576098;"
	mm, err := ParseRawAVR(input, false)
	if err != nil {
		t.Fatalf("ParseRawAVR() failed: %v", err)
	}
	if mm == nil {
		t.Fatal("ParseRawAVR() returned nil message")
	}
	if !mm.Remote {
		t.Error("Expected Remote=true for network input")
	}
}

func TestParseRawAVREmptyString(t *testing.T) {
	// Empty string should return nil message, no error
	mm, err := ParseRawAVR("", false)
	if err != nil {
		t.Fatalf("ParseRawAVR() unexpected error: %v", err)
	}
	if mm != nil {
		t.Error("Expected nil message for empty string")
	}
}

func TestParseRawAVROddLengthHex(t *testing.T) {
	// Odd-length hex should return error
	input := "*ABC;"
	_, err := ParseRawAVR(input, false)
	if err == nil {
		t.Error("Expected error for odd-length hex")
	}
}

func TestParseRawAVRInvalidHexChars(t *testing.T) {
	// Invalid hex characters should return error
	input := "*ZZZZ;"
	_, err := ParseRawAVR(input, false)
	if err == nil {
		t.Error("Expected error for invalid hex chars")
	}
}

func TestParseRawAVRTooShortTimestamp(t *testing.T) {
	// Timestamp format with less than 12 hex chars should fail
	input := "@12345*8D4840D6202CC371C32CE0576098;"
	_, err := ParseRawAVR(input, false)
	if err == nil {
		t.Error("Expected error for too-short timestamp")
	}
}

func TestParseRawAVREmptyPayload(t *testing.T) {
	// Empty payload after * should return error
	input := "*;"
	_, err := ParseRawAVR(input, false)
	if err == nil {
		t.Error("Expected error for empty payload")
	}
}
