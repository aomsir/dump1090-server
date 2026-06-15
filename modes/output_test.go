package modes

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEncodeBeast(t *testing.T) {
	ModesChecksumInit(2)

	tests := []struct {
		name     string
		msg      *Message
		wantNil  bool
		wantType byte
	}{
		{
			name:    "nil message",
			msg:     nil,
			wantNil: true,
		},
		{
			name: "short message (DF11)",
			msg: &Message{
				Msg:         [14]byte{0x5D, 0x48, 0x40, 0xD6, 0xC3, 0x85, 0x81},
				MsgBits:     56,
				Timestamp:   12000000,
				SignalLevel: 0.5,
			},
			wantNil:  false,
			wantType: BeastTypeShort,
		},
		{
			name: "long message (DF17)",
			msg: &Message{
				Msg:         [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
				MsgBits:     112,
				Timestamp:   24000000,
				SignalLevel: 0.8,
			},
			wantNil:  false,
			wantType: BeastTypeLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EncodeBeast(tt.msg)
			if tt.wantNil {
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Check start byte
			if result[0] != BeastEscape {
				t.Errorf("Expected escape byte 0x1A, got 0x%02X", result[0])
			}

			// Check type byte
			if result[1] != tt.wantType {
				t.Errorf("Expected type %c, got %c", tt.wantType, result[1])
			}
		})
	}
}

func TestEncodeBeastEscaping(t *testing.T) {
	// Message containing 0x1A bytes should be escaped
	msg := &Message{
		Msg:         [14]byte{0x1A, 0x1A, 0x1A, 0x1A, 0x1A, 0x1A, 0x1A},
		MsgBits:     56,
		Timestamp:   0x1A1A1A1A1A1A, // Contains escape bytes
		SignalLevel: 0.5,
	}

	result := EncodeBeast(msg)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Count 0x1A bytes - each should be doubled
	escapeCount := 0
	for i := 2; i < len(result); i++ { // Skip header
		if result[i] == BeastEscape {
			escapeCount++
		}
	}

	// All 0x1A in timestamp (6 bytes) + signal (potentially) + message (7 bytes) should be doubled
	// This means we should see even number of escape bytes after the header
	if escapeCount%2 != 0 {
		t.Errorf("Escape bytes should be paired, found %d", escapeCount)
	}
}

func TestDecodeBeast(t *testing.T) {
	ModesChecksumInit(2)

	// Create a known message and encode it
	original := &Message{
		Msg:         [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
		MsgBits:     112,
		Timestamp:   12345678,
		SignalLevel: 0.25, // Will be sqrt'd and scaled
	}

	encoded := EncodeBeast(original)
	if encoded == nil {
		t.Fatal("Encoding failed")
	}

	// Decode it back
	decoded, remaining, err := DecodeBeast(encoded)
	if err != nil {
		t.Fatalf("Decoding failed: %v", err)
	}

	if len(remaining) != 0 {
		t.Errorf("Expected no remaining data, got %d bytes", len(remaining))
	}

	// Check decoded values
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp mismatch: got %d, want %d", decoded.Timestamp, original.Timestamp)
	}

	// Check message bytes
	for i := 0; i < 14; i++ {
		if decoded.Msg[i] != original.Msg[i] {
			t.Errorf("Message byte %d mismatch: got 0x%02X, want 0x%02X", i, decoded.Msg[i], original.Msg[i])
		}
	}
}

func TestDecodeBeastErrors(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "too short",
			data:    []byte{0x1A, '3'},
			wantErr: true,
		},
		{
			name:    "no escape byte",
			data:    []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			wantErr: true,
		},
		{
			name:    "unknown type",
			data:    []byte{0x1A, 'X', 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := DecodeBeast(tt.data)
			if tt.wantErr && err == nil {
				t.Error("Expected error, got nil")
			}
		})
	}
}

func TestBeastHeartbeat(t *testing.T) {
	hb := BeastHeartbeatMessage()
	if len(hb) != 11 {
		t.Errorf("Heartbeat should be 11 bytes, got %d", len(hb))
	}
	if hb[0] != BeastEscape {
		t.Errorf("Heartbeat should start with escape byte")
	}
	if hb[1] != BeastHeartbeat {
		t.Errorf("Heartbeat type should be '1', got %c", hb[1])
	}
}

func TestEncodeAVR(t *testing.T) {
	tests := []struct {
		name      string
		msg       *Message
		timestamp bool
		want      string
	}{
		{
			name: "short message no timestamp",
			msg: &Message{
				Msg:     [14]byte{0x5D, 0x48, 0x40, 0xD6, 0xC3, 0x85, 0x81},
				MsgBits: 56,
			},
			timestamp: false,
			want:      "5D4840D6C38581;\n",
		},
		{
			name: "long message no timestamp",
			msg: &Message{
				Msg:     [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
				MsgBits: 112,
			},
			timestamp: false,
			want:      "8D4840D6202CC371C32CE0576098;\n",
		},
		{
			name: "with timestamp",
			msg: &Message{
				Msg:       [14]byte{0x5D, 0x48, 0x40, 0xD6, 0xC3, 0x85, 0x81},
				MsgBits:   56,
				Timestamp: 0x123456789ABC,
			},
			timestamp: true,
			want:      "@123456789ABC5D4840D6C38581;\n",
		},
		{
			name:      "nil message",
			msg:       nil,
			timestamp: false,
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EncodeAVR(tt.msg, tt.timestamp)
			if got != tt.want {
				t.Errorf("EncodeAVR() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEncodeSBS(t *testing.T) {
	tests := []struct {
		name    string
		msg     *Message
		wantMsg int // expected SBS MSG type
		want    []string
		notWant []string
	}{
		{
			name: "DF17 callsign",
			msg: &Message{
				Msg:           [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
				MsgBits:       112,
				MsgType:       17,
				Addr:          0x4840D6,
				CallsignValid: true,
				Callsign:      [9]byte{'K', 'L', 'M', '1', '0', '2', '3', ' ', 0},
			},
			wantMsg: 1,
			want:    []string{"MSG,1,", "4840D6", "KLM1023"},
		},
		{
			name: "DF17 altitude",
			msg: &Message{
				Msg:           [14]byte{0x8D, 0x48, 0x40, 0xD6},
				MsgBits:       112,
				MsgType:       17,
				Addr:          0x4840D6,
				AltitudeValid: true,
				Altitude:      35000,
			},
			wantMsg: 3,
			want:    []string{"MSG,3,", "4840D6", "35000"},
		},
		{
			name: "DF17 velocity",
			msg: &Message{
				Msg:          [14]byte{0x8D, 0x48, 0x40, 0xD6},
				MsgBits:      112,
				MsgType:      17,
				Addr:         0x4840D6,
				SpeedValid:   true,
				Speed:        450,
				HeadingValid: true,
				Heading:      180,
			},
			wantMsg: 4,
			want:    []string{"MSG,4,", "4840D6", "450", "180"},
		},
		{
			name: "DF4 altitude only",
			msg: &Message{
				Msg:           [14]byte{0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
				MsgBits:       56,
				MsgType:       4,
				Addr:          0x4840D6,
				AltitudeValid: true,
				Altitude:      12000,
			},
			wantMsg: 5,
			want:    []string{"MSG,5,", "4840D6", "12000"},
		},
		{
			name: "DF5 squawk only",
			msg: &Message{
				Msg:         [14]byte{0x28, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
				MsgBits:     56,
				MsgType:     5,
				Addr:        0x4840D6,
				SquawkValid: true,
				Squawk:      0x1234,
			},
			wantMsg: 6,
			want:    []string{"MSG,6,", "4840D6", "1234"},
		},
		{
			name: "DF11 all-call reply with altitude",
			msg: &Message{
				Msg:           [14]byte{0x5D, 0x48, 0x40, 0xD6, 0xC3, 0x85, 0x81},
				MsgBits:       56,
				MsgType:       11,
				Addr:          0x4840D6,
				AltitudeValid: true,
				Altitude:      8000,
			},
			wantMsg: 8,
			want:    []string{"MSG,8,", "4840D6", "8000"},
		},
		{
			name: "DF11 all-call reply without altitude",
			msg: &Message{
				Msg:     [14]byte{0x5D, 0x48, 0x40, 0xD6, 0x00, 0x00, 0x00},
				MsgBits: 56,
				MsgType: 11,
				Addr:    0x4840D6,
			},
			wantMsg: 8,
			want:    []string{"MSG,8,", "4840D6"},
		},
		{
			name: "DF20 altitude from Comm-B",
			msg: &Message{
				Msg:           [14]byte{0xA0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
				MsgBits:       112,
				MsgType:       20,
				Addr:          0x4840D6,
				AltitudeValid: true,
				Altitude:      25000,
			},
			wantMsg: 3,
			want:    []string{"MSG,3,", "4840D6", "25000"},
		},
		{
			name: "DF21 squawk from Comm-B",
			msg: &Message{
				Msg:         [14]byte{0xA8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
				MsgBits:     112,
				MsgType:     21,
				Addr:        0x4840D6,
				SquawkValid: true,
				Squawk:      0x7700,
			},
			wantMsg: 6,
			want:    []string{"MSG,6,", "4840D6", "7700"},
		},
		{
			name: "DF18 callsign with position",
			msg: &Message{
				Msg:           [14]byte{0x90, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
				MsgBits:       112,
				MsgType:       18,
				Addr:          0x4840D6,
				CallsignValid: true,
				Callsign:      [9]byte{'U', 'A', 'L', '1', '2', '3', ' ', ' ', 0},
				AltitudeValid: true,
				Altitude:      38000,
			},
			wantMsg: 1,
			want:    []string{"MSG,1,", "4840D6", "UAL123"},
		},
		{
			name:    "nil message returns empty",
			msg:     nil,
			wantMsg: 0,
			notWant: []string{"MSG,"},
		},
		{
			name: "unhandled DF type returns empty",
			msg: &Message{
				Msg:     [14]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
				MsgBits: 56,
				MsgType: 0,
				Addr:    0x4840D6,
			},
			wantMsg: 0,
			notWant: []string{"MSG,"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EncodeSBS(tt.msg, nil)

			if tt.wantMsg == 0 {
				if result != "" {
					t.Errorf("Expected empty result, got %q", result)
				}
				return
			}

			if result == "" {
				t.Fatal("Expected non-empty SBS output")
			}

			// Verify CRLF line endings
			if !strings.HasSuffix(result, "\r\n") {
				t.Errorf("SBS output must end with CRLF (\\r\\n), got suffix %q", result[len(result)-4:])
			}

			// Verify MSG type prefix
			prefix := fmt.Sprintf("MSG,%d,", tt.wantMsg)
			if !strings.HasPrefix(result, prefix) {
				t.Errorf("Expected prefix %q, got %q", prefix, result[:20])
			}

			// Verify expected content
			for _, want := range tt.want {
				if !strings.Contains(result, want) {
					t.Errorf("Expected output to contain %q, got %q", want, result)
				}
			}

			// Verify excluded content
			for _, notWant := range tt.notWant {
				if strings.Contains(result, notWant) {
					t.Errorf("Expected output NOT to contain %q, got %q", notWant, result)
				}
			}
		})
	}
}

// createTestAircraft creates an aircraft in the tracker for testing
func createTestAircraft(tracker *Tracker, addr uint32) *Aircraft {
	ModesChecksumInit(2)
	// Create a minimal message to add aircraft
	mm := &Message{
		Addr:    addr,
		MsgType: 17,
		MsgBits: 112,
	}
	return tracker.UpdateFromMessage(mm)
}

func TestGenerateAircraftJSON(t *testing.T) {
	tracker := NewTracker()
	tracker.SetReceiverLocation(52.0, 0.0)

	// Add some aircraft
	now := uint64(time.Now().UnixMilli())
	a1 := createTestAircraft(tracker, 0x4840D6)
	a1.Messages = 10
	a1.Seen = now
	a1.CallsignValid = DataValidity{Source: SOURCE_ADSB, Updated: now}
	copy(a1.Callsign[:], "TEST123 ")
	a1.AltitudeValid = DataValidity{Source: SOURCE_ADSB, Updated: now}
	a1.Altitude = 35000
	a1.PositionValid = DataValidity{Source: SOURCE_ADSB, Updated: now}
	a1.Lat = 52.21
	a1.Lon = 0.177

	result := GenerateAircraftJSON(tracker, 1000)

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Messages != 1000 {
		t.Errorf("Messages should be 1000, got %d", result.Messages)
	}

	if len(result.Aircraft) != 1 {
		t.Errorf("Expected 1 aircraft, got %d", len(result.Aircraft))
	}

	if len(result.Aircraft) > 0 {
		ac := result.Aircraft[0]
		if ac.Hex != "4840d6" {
			t.Errorf("Hex should be '4840d6', got '%s'", ac.Hex)
		}
		if ac.Flight != "TEST123" {
			t.Errorf("Flight should be 'TEST123', got '%s'", ac.Flight)
		}
		if ac.Altitude != 35000 {
			t.Errorf("Altitude should be 35000, got %v", ac.Altitude)
		}
		if ac.Lat == nil || *ac.Lat != 52.21 {
			t.Errorf("Lat should be 52.21")
		}
	}
}

func TestGenerateAircraftJSONFilters(t *testing.T) {
	tracker := NewTracker()

	now := uint64(time.Now().UnixMilli())

	// Add aircraft with only 1 message (should be filtered)
	a1 := createTestAircraft(tracker, 0x111111)
	a1.Messages = 1
	a1.Seen = now

	// Add Mode A/C synthetic (should be filtered)
	a2 := createTestAircraft(tracker, 0x222222)
	a2.Messages = 10
	a2.Seen = now
	a2.ModeACFlags = MODEAC_MSG_FLAG

	// Add valid aircraft
	a3 := createTestAircraft(tracker, 0x333333)
	a3.Messages = 10
	a3.Seen = now

	result := GenerateAircraftJSON(tracker, 100)

	if len(result.Aircraft) != 1 {
		t.Errorf("Expected 1 aircraft (filtered), got %d", len(result.Aircraft))
	}

	if len(result.Aircraft) > 0 && result.Aircraft[0].Hex != "333333" {
		t.Errorf("Expected aircraft 333333, got %s", result.Aircraft[0].Hex)
	}
}

func TestAircraftJSONNonICAO(t *testing.T) {
	tracker := NewTracker()

	now := uint64(time.Now().UnixMilli())

	// Add non-ICAO aircraft
	a1 := createTestAircraft(tracker, 0x123456|MODES_NON_ICAO_ADDRESS)
	a1.Messages = 10
	a1.Seen = now
	a1.AddrType = ADDR_TISB_OTHER

	result := GenerateAircraftJSON(tracker, 100)

	if len(result.Aircraft) != 1 {
		t.Fatalf("Expected 1 aircraft, got %d", len(result.Aircraft))
	}

	// Non-ICAO should have ~ prefix
	if !strings.HasPrefix(result.Aircraft[0].Hex, "~") {
		t.Errorf("Non-ICAO address should have ~ prefix, got %s", result.Aircraft[0].Hex)
	}

	// Should have type set
	if result.Aircraft[0].Type != "tisb_other" {
		t.Errorf("Expected type 'tisb_other', got '%s'", result.Aircraft[0].Type)
	}
}

func TestReceiverJSON(t *testing.T) {
	r := GenerateReceiverJSON("1.0.0", 1.0, 120, 52.0, 0.0)

	if r.Version != "1.0.0" {
		t.Errorf("Version mismatch")
	}
	if r.Refresh != 1.0 {
		t.Errorf("Refresh mismatch")
	}
	if r.History != 120 {
		t.Errorf("History mismatch")
	}
	if r.Lat != 52.0 {
		t.Errorf("Lat mismatch")
	}
	if r.Lon != 0.0 {
		t.Errorf("Lon mismatch")
	}
}

func TestAddrTypeString(t *testing.T) {
	tests := []struct {
		addrType AddrType
		want     string
	}{
		{ADDR_ADSB_ICAO, "adsb_icao"},
		{ADDR_ADSB_ICAO_NT, "adsb_icao_nt"},
		{ADDR_ADSR_ICAO, "adsr_icao"},
		{ADDR_TISB_ICAO, "tisb_icao"},
		{ADDR_ADSB_OTHER, "adsb_other"},
		{ADDR_ADSR_OTHER, "adsr_other"},
		{ADDR_TISB_OTHER, "tisb_other"},
		{ADDR_TISB_TRACKFILE, "tisb_trackfile"},
		{AddrType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := addrTypeString(tt.addrType)
			if got != tt.want {
				t.Errorf("addrTypeString(%d) = %s, want %s", tt.addrType, got, tt.want)
			}
		})
	}
}

func TestMarshalAircraftJSON(t *testing.T) {
	tracker := NewTracker()

	now := uint64(time.Now().UnixMilli())
	a1 := createTestAircraft(tracker, 0x4840D6)
	a1.Messages = 10
	a1.Seen = now

	data, err := MarshalAircraftJSON(tracker, 100)
	if err != nil {
		t.Fatalf("MarshalAircraftJSON failed: %v", err)
	}

	// Should be valid JSON
	if !bytes.Contains(data, []byte(`"aircraft"`)) {
		t.Error("JSON should contain aircraft field")
	}
	if !bytes.Contains(data, []byte(`"messages"`)) {
		t.Error("JSON should contain messages field")
	}
	if !bytes.Contains(data, []byte(`"now"`)) {
		t.Error("JSON should contain now field")
	}
}

func TestForwardingPolicySuppressesMLATForRawAndSBS(t *testing.T) {
	msg := &Message{
		Msg:           [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
		MsgBits:       112,
		MsgType:       17,
		Addr:          0x4840D6,
		Source:        SOURCE_MLAT,
		AltitudeValid: true,
		Altitude:      35000,
	}

	dest := ForwardingDestination(msg, false, false)
	if dest&DestRaw != 0 {
		t.Error("MLAT messages must NOT route to Raw output")
	}
	if dest&DestSBS != 0 {
		t.Error("MLAT messages must NOT route to SBS output")
	}
	if dest&DestFATSV != 0 {
		t.Error("MLAT messages must NOT route to FATSV output")
	}
	if dest&DestBeast != 0 {
		t.Error("MLAT messages must NOT route to Beast when forward-mlat is disabled")
	}
}

func TestForwardingPolicyAllowsMLATForBeastOnlyWhenEnabled(t *testing.T) {
	msg := &Message{
		Msg:     [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
		MsgBits: 112,
		MsgType: 17,
		Addr:    0x4840D6,
		Source:  SOURCE_MLAT,
	}

	// MLAT disabled: Beast should be excluded
	dest := ForwardingDestination(msg, false, false)
	if dest&DestBeast != 0 {
		t.Error("MLAT messages must NOT route to Beast when forward-mlat is disabled")
	}

	// MLAT enabled: Beast should be included
	dest = ForwardingDestination(msg, false, true)
	if dest&DestBeast == 0 {
		t.Error("MLAT messages MUST route to Beast when forward-mlat is enabled")
	}
}

func TestForwardingPolicySuppressesTwoBitCorrectedUnlessVerbatim(t *testing.T) {
	msg := &Message{
		Msg:       [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
		MsgBits:   112,
		MsgType:   17,
		Addr:      0x4840D6,
		Source:    SOURCE_ADSB,
		Corrected: 2,
	}

	// Without verbatim: should be suppressed
	dest := ForwardingDestination(msg, false, false)
	if dest != DestNone {
		t.Errorf("2-bit corrected messages without verbatim should produce no destinations, got %v", dest)
	}

	// With verbatim: should forward to all outputs
	dest = ForwardingDestination(msg, true, false)
	wantDest := DestRaw | DestBeast | DestSBS | DestFATSV
	if dest != wantDest {
		t.Errorf("2-bit corrected with verbatim: expected %v, got %v", wantDest, dest)
	}
}

func TestForwardingPolicyNormalADSBAllOutputs(t *testing.T) {
	msg := &Message{
		Msg:           [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
		MsgBits:       112,
		MsgType:       17,
		Addr:          0x4840D6,
		Source:        SOURCE_ADSB,
		AltitudeValid: true,
		Altitude:      35000,
	}

	dest := ForwardingDestination(msg, false, false)
	if dest&DestRaw == 0 {
		t.Error("Normal ADSB should route to Raw")
	}
	if dest&DestBeast == 0 {
		t.Error("Normal ADSB should route to Beast")
	}
	if dest&DestSBS == 0 {
		t.Error("Normal ADSB should route to SBS")
	}
	if dest&DestFATSV == 0 {
		t.Error("Normal ADSB should route to FATSV")
	}
}

func TestForwardingPolicySingleBitCorrectedAllowed(t *testing.T) {
	msg := &Message{
		Msg:           [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
		MsgBits:       112,
		MsgType:       17,
		Addr:          0x4840D6,
		Source:        SOURCE_ADSB,
		Corrected:     1,
		AltitudeValid: true,
		Altitude:      35000,
	}

	dest := ForwardingDestination(msg, false, false)
	if dest == DestNone {
		t.Error("1-bit corrected messages should be forwarded")
	}
}

func TestForwardingPolicyNilMessage(t *testing.T) {
	dest := ForwardingDestination(nil, false, false)
	if dest != DestNone {
		t.Errorf("Nil message should return DestNone, got %v", dest)
	}
	dest = ForwardingDestination(nil, true, true)
	if dest != DestNone {
		t.Errorf("Nil message should return DestNone regardless of config, got %v", dest)
	}
}

func BenchmarkEncodeBeast(b *testing.B) {
	msg := &Message{
		Msg:         [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
		MsgBits:     112,
		Timestamp:   12345678,
		SignalLevel: 0.5,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EncodeBeast(msg)
	}
}

func BenchmarkEncodeAVR(b *testing.B) {
	msg := &Message{
		Msg:       [14]byte{0x8D, 0x48, 0x40, 0xD6, 0x20, 0x2C, 0xC3, 0x71, 0xC3, 0x2C, 0xE0, 0x57, 0x60, 0x98},
		MsgBits:   112,
		Timestamp: 12345678,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EncodeAVR(msg, true)
	}
}

func BenchmarkGenerateAircraftJSON(b *testing.B) {
	ModesChecksumInit(2)
	tracker := NewTracker()

	now := uint64(time.Now().UnixMilli())
	for i := 0; i < 100; i++ {
		a := createTestAircraft(tracker, uint32(0x400000+i))
		a.Messages = 10
		a.Seen = now
		a.AltitudeValid = DataValidity{Source: SOURCE_ADSB, Updated: now}
		a.Altitude = 35000
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GenerateAircraftJSON(tracker, 1000)
	}
}
