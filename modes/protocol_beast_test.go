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

func TestDecodeBeastHeartbeatIsAllZeros(t *testing.T) {
	// Heartbeat detection: type 1 with all-zero timestamp/signal/message bytes
	// This matches BeastHeartbeatMessage() which returns all zeros after type byte
	heartbeat := BeastHeartbeatMessage()

	mm, remaining, err := DecodeBeast(heartbeat)
	if err != nil {
		t.Fatalf("DecodeBeast() heartbeat returned error: %v", err)
	}
	if mm != nil {
		t.Errorf("Expected nil message for heartbeat, got %+v", mm)
	}
	if len(remaining) != 0 {
		t.Errorf("Expected no remaining data, got %d bytes", len(remaining))
	}
}

func TestDecodeBeastModeACWithZeroTimestampNonzeroPayload(t *testing.T) {
	// Type 1 Mode A/C with zero timestamp but nonzero payload should NOT be heartbeat
	// Frame: 0x1A, '1', 6 bytes zero timestamp, 1 byte signal, 2 bytes nonzero message
	ModesChecksumInit(2)
	msg := []byte{0x1A, '1', 0, 0, 0, 0, 0, 0, 0x80, 0x78, 0x01}

	mm, _, err := DecodeBeastWithConfig(msg, true)
	if err != nil {
		t.Fatalf("DecodeBeastWithConfig() returned error: %v", err)
	}
	if mm == nil {
		t.Fatal("Expected non-nil message for Mode A/C with nonzero payload")
	}
	if mm.MsgType != 32 {
		t.Errorf("Expected MsgType=32 for Mode A/C, got %d", mm.MsgType)
	}
	// Verify it has nonzero payload (0x7801)
	if mm.Msg[0] == 0 && mm.Msg[1] == 0 {
		t.Error("Expected nonzero Mode A/C payload")
	}
}

func TestDecodeBeastStatusTypeSkipped(t *testing.T) {
	// Beast status type ('4') should return error to signal caller to find next escape
	// Status message: 0x1A, '4', followed by status data
	status := []byte{0x1A, '4', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	mm, remaining, err := DecodeBeast(status)
	// Should return error to signal status skip
	if err == nil {
		t.Fatal("Expected error for status skip")
	}
	// Should return nil message (status not decoded)
	if mm != nil {
		t.Errorf("Expected nil message for status type, got %+v", mm)
	}
	// Should have consumed some bytes (at least the type header)
	if len(remaining) >= len(status) {
		t.Error("Expected status type to consume some bytes")
	}
}

func TestDecodeBeastStatusFollowedByValidMessage(t *testing.T) {
	// Status message followed by valid Beast message should recover correctly
	// Status: 0x1A, '4', 10 bytes of status data
	// Valid message: 0x1A, '3', 6 bytes timestamp, 1 byte signal, 14 bytes long message
	ModesChecksumInit(2)

	// Build status + valid message
	statusPayload := []byte{0x1A, '4', 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A}
	validMsg := &Message{
		Msg:         [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
		MsgBits:     112,
		Timestamp:   12345678,
		SignalLevel: 0.5,
	}
	encodedValid := EncodeBeast(validMsg)
	if encodedValid == nil {
		t.Fatal("EncodeBeast() failed")
	}

	// Combine: status payload + valid message
	combined := append(statusPayload, encodedValid...)

	// First call should return error for status skip
	mm, remaining, err := DecodeBeast(combined)
	if err == nil {
		t.Fatal("Expected error for status skip")
	}
	if mm != nil {
		t.Errorf("Expected nil message for status skip, got %+v", mm)
	}
	if len(remaining) == 0 {
		t.Fatal("Expected remaining data after status skip")
	}

	// Caller's loop will find next escape byte and call DecodeBeast again
	// Find next escape in remaining data
	nextEscape := -1
	for i := 0; i < len(remaining); i++ {
		if remaining[i] == 0x1A {
			nextEscape = i
			break
		}
	}
	if nextEscape < 0 {
		t.Fatal("Expected to find next escape byte in remaining data")
	}

	// Second call should decode the valid message
	mm, remaining, err = DecodeBeast(remaining[nextEscape:])
	if err != nil {
		t.Fatalf("DecodeBeast() second call returned error: %v", err)
	}
	if mm == nil {
		t.Fatal("Expected non-nil message for valid message")
	}
	if mm.MsgBits != 112 {
		t.Errorf("Expected 112 bits, got %d", mm.MsgBits)
	}
	// Should have no remaining data
	if len(remaining) != 0 {
		t.Errorf("Expected no remaining data, got %d bytes", len(remaining))
	}
}

func TestDecodeBeastStatusWithEscapeInPayload(t *testing.T) {
	// Status payload containing 0x1A (escape-like byte) should not confuse parser
	// The 0x1A in status payload is NOT a frame start, just data
	// Status: 0x1A, '4', followed by payload with 0x1A bytes
	// Then a valid message
	ModesChecksumInit(2)

	// Status with 0x1A in payload (not escaped, just raw data)
	statusWithEscape := []byte{0x1A, '4', 0x1A, 0x00, 0x1A, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	validMsg := &Message{
		Msg:         [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
		MsgBits:     112,
		Timestamp:   12345678,
		SignalLevel: 0.5,
	}
	encodedValid := EncodeBeast(validMsg)
	if encodedValid == nil {
		t.Fatal("EncodeBeast() failed")
	}

	combined := append(statusWithEscape, encodedValid...)

	// First call should return error for status skip
	mm, remaining, err := DecodeBeast(combined)
	if err == nil {
		t.Fatal("Expected error for status skip")
	}
	if mm != nil {
		t.Errorf("Expected nil message for status skip, got %+v", mm)
	}
	if len(remaining) == 0 {
		t.Fatal("Expected remaining data after status skip")
	}

	// Caller's loop will find next escape byte and call DecodeBeast again
	// Find next escape in remaining data that's followed by a valid type byte
	// (skip escapes that are part of status payload)
	nextEscape := -1
	for i := 0; i < len(remaining)-1; i++ {
		if remaining[i] == 0x1A {
			nextType := remaining[i+1]
			if nextType == '1' || nextType == '2' || nextType == '3' || nextType == '4' {
				nextEscape = i
				break
			}
		}
	}
	if nextEscape < 0 {
		t.Fatal("Expected to find next escape byte in remaining data")
	}

	// Second call should decode the valid message
	mm, remaining, err = DecodeBeast(remaining[nextEscape:])
	if err != nil {
		t.Fatalf("DecodeBeast() second call returned error: %v", err)
	}
	if mm == nil {
		t.Fatal("Expected non-nil message for valid message")
	}
	if mm.MsgBits != 112 {
		t.Errorf("Expected 112 bits, got %d", mm.MsgBits)
	}
	// Should have no remaining data
	if len(remaining) != 0 {
		t.Errorf("Expected no remaining data, got %d bytes", len(remaining))
	}
}
