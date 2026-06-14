package modes

import (
	"testing"
)

func TestDecodeBeastHeartbeatDoesNotPanic(t *testing.T) {
	// Type 1 heartbeat should return no message and no panic
	// Heartbeat: 0x1A, '1', 6 bytes timestamp, 1 byte signal, 0 bytes message
	heartbeat := []byte{0x1A, '1', 0, 0, 0, 0, 0, 0, 0, 0, 0}

	// Should not panic
	mm, remaining, err := DecodeBeast(heartbeat)
	if err != nil {
		t.Fatalf("DecodeBeast() heartbeat returned error: %v", err)
	}
	// Heartbeat with no payload should return nil message
	if mm != nil {
		t.Errorf("Expected nil message for heartbeat, got %+v", mm)
	}
	// Should consume all data
	if len(remaining) != 0 {
		t.Errorf("Expected no remaining data, got %d bytes", len(remaining))
	}
}

func TestDecodeBeastModeACRequiresModeACEnabled(t *testing.T) {
	// Type 1 Mode A/C payload should be ignored when Mode A/C is disabled
	// and decoded when enabled
	// Mode A/C message: 0x1A, '1', 6 bytes timestamp, 1 byte signal, 2 bytes message
	msg := []byte{0x1A, '1', 0, 0, 0, 0, 0, 0, 0x80, 0x78, 0x01}

	// With Mode A/C disabled
	mm, _, err := DecodeBeastWithConfig(msg, false)
	if err != nil {
		t.Fatalf("DecodeBeastWithConfig() with Mode A/C disabled returned error: %v", err)
	}
	if mm != nil {
		t.Error("Expected nil message for Mode A/C when disabled")
	}

	// With Mode A/C enabled
	mm, _, err = DecodeBeastWithConfig(msg, true)
	if err != nil {
		t.Fatalf("DecodeBeastWithConfig() with Mode A/C enabled returned error: %v", err)
	}
	if mm == nil {
		t.Fatal("Expected non-nil message for Mode A/C when enabled")
	}
	if mm.MsgType != 32 {
		t.Errorf("Expected MsgType=32 for Mode A/C, got %d", mm.MsgType)
	}
}

func TestDecodeBeastSetsRemoteAndMLATBeforeSourceDecision(t *testing.T) {
	// Beast MLAT magic timestamp should produce SOURCE_MLAT
	// MLAT timestamp: 0xFF004D4C4154 (MAGIC_MLAT_TIMESTAMP)
	// Use a valid DF17 message
	ModesChecksumInit(2)
	msg := &Message{
		Msg:         [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
		MsgBits:     112,
		Timestamp:   0x123456789ABC,
		SignalLevel: 0.5,
	}
	msg.TimestampMsg = MAGIC_MLAT_TIMESTAMP

	encoded := EncodeBeast(msg)
	if encoded == nil {
		t.Fatal("EncodeBeast() failed")
	}

	// Decode with Mode A/C disabled (default for DF17)
	mm, _, err := DecodeBeastWithConfig(encoded, false)
	if err != nil {
		t.Fatalf("DecodeBeastWithConfig() failed: %v", err)
	}
	if mm == nil {
		t.Fatal("Expected non-nil message")
	}

	// Should have Remote=true and Source=SOURCE_MLAT
	if !mm.Remote {
		t.Error("Expected Remote=true")
	}
	if mm.Source != SOURCE_MLAT {
		t.Errorf("Expected Source=SOURCE_MLAT, got %d", mm.Source)
	}
}

func TestDecodeBeastEscapedBytes(t *testing.T) {
	// Doubled 0x1A bytes should be unescaped exactly once
	// Use a valid DF17 message with some 0x1A bytes in the payload
	ModesChecksumInit(2)
	// Original message: 8D4840D6202CC371C32CE0576098
	// Let's use a message that has 0x1A in it - we'll modify a valid message
	// For testing escaping, we can use any message and just verify the round-trip
	original := &Message{
		Msg:         [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
		MsgBits:     112,
		Timestamp:   0x1A1A1A1A1A1A, // Contains escape bytes
		SignalLevel: 0.5,
	}

	encoded := EncodeBeast(original)
	if encoded == nil {
		t.Fatal("EncodeBeast() failed")
	}

	// Verify that 0x1A bytes in timestamp are escaped
	// Count 0x1A bytes in encoded data (after header)
	escapeCount := 0
	for i := 2; i < len(encoded); i++ {
		if encoded[i] == 0x1A {
			escapeCount++
		}
	}
	// Each 0x1A in timestamp should be doubled, so we should see even number
	if escapeCount%2 != 0 {
		t.Errorf("Escape bytes should be paired, found %d", escapeCount)
	}

	// Decode it back
	decoded, _, err := DecodeBeast(encoded)
	if err != nil {
		t.Fatalf("DecodeBeast() failed: %v", err)
	}
	if decoded == nil {
		t.Fatal("Expected non-nil message")
	}

	// Verify timestamp was properly unescaped
	if decoded.Timestamp != 0x1A1A1A1A1A1A {
		t.Errorf("Timestamp mismatch: got 0x%012X, want 0x1A1A1A1A1A1A", decoded.Timestamp)
	}

	// Verify message bytes match
	for i := 0; i < 14; i++ {
		if decoded.Msg[i] != original.Msg[i] {
			t.Errorf("Message byte %d mismatch: got 0x%02X, want 0x%02X", i, decoded.Msg[i], original.Msg[i])
		}
	}
}
