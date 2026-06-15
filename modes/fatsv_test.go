package modes

import (
	"strings"
	"testing"
	"time"
)

func TestWriteFATSVEventGating(t *testing.T) {
	ModesChecksumInit(2)
	tracker := NewTracker()
	writer := NewFATSVWriter(tracker)

	now := uint64(time.Now().UnixMilli())
	a := createTestAircraft(tracker, 0x4840D6)
	a.Messages = 10
	a.Seen = now

	// DF20 with BDS 1,0 (datalink capabilities)
	mm := &Message{
		Msg:     [14]byte{0xA0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		MsgBits: 112,
		MsgType: 20,
		Addr:    0x4840D6,
		MB:      [7]byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70},
	}

	// First emission should produce output
	result1 := writer.WriteFATSVEvent(mm, a)
	if result1 == nil {
		t.Fatal("First BDS 1,0 event should produce output")
	}
	if !strings.Contains(string(result1), "datalink_caps") {
		t.Errorf("Expected datalink_caps field, got %q", string(result1))
	}

	// Same data again should be gated (no output)
	result2 := writer.WriteFATSVEvent(mm, a)
	if result2 != nil {
		t.Errorf("Duplicate BDS 1,0 event should be gated, got %q", string(result2))
	}

	// Changed BDS 1,0 data should produce output
	mm.MB = [7]byte{0x10, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77}
	result3 := writer.WriteFATSVEvent(mm, a)
	if result3 == nil {
		t.Error("Changed BDS 1,0 event should produce output")
	}
}

func TestWriteFATSVEventACASRA(t *testing.T) {
	ModesChecksumInit(2)
	tracker := NewTracker()
	writer := NewFATSVWriter(tracker)

	now := uint64(time.Now().UnixMilli())
	a := createTestAircraft(tracker, 0x4840D6)
	a.Messages = 10
	a.Seen = now

	// DF17 with ME type 28 subtype 2 (ACAS RA broadcast)
	// ME[0] should be 0xE2 for ACAS RA
	mm := &Message{
		Msg:     [14]byte{0x8D, 0x48, 0x40, 0xD6, 0xE2, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		MsgBits: 112,
		MsgType: 17,
		Addr:    0x4840D6,
		METype:  28,
		MESub:   2,
		ME:      [7]byte{0xE2, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
	}

	result := writer.WriteFATSVEvent(mm, a)
	if result == nil {
		t.Fatal("First ACAS RA event should produce output")
	}
	if !strings.Contains(string(result), "es_acas_ra") {
		t.Errorf("Expected es_acas_ra field, got %q", string(result))
	}

	// Duplicate should be gated
	result2 := writer.WriteFATSVEvent(mm, a)
	if result2 != nil {
		t.Error("Duplicate ACAS RA event should be gated")
	}
}

func TestWriteFATSVEventFormat(t *testing.T) {
	ModesChecksumInit(2)
	tracker := NewTracker()
	writer := NewFATSVWriter(tracker)

	now := uint64(time.Now().UnixMilli())
	a := createTestAircraft(tracker, 0x4840D6)
	a.Messages = 10
	a.Seen = now

	mm := &Message{
		Msg:     [14]byte{0xA0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		MsgBits: 112,
		MsgType: 20,
		Addr:    0x4840D6,
		MB:      [7]byte{0x30, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70},
	}

	result := writer.WriteFATSVEvent(mm, a)
	if result == nil {
		t.Fatal("Expected output")
	}

	s := string(result)

	// Should start with "clock"
	if !strings.HasPrefix(s, "clock\t") {
		t.Errorf("Event should start with 'clock\\t', got %q", s[:20])
	}

	// Should contain hexid
	if !strings.Contains(s, "hexid\t") {
		t.Error("Event should contain hexid field")
	}

	// Should contain commb_acas_ra (BDS 3,0)
	if !strings.Contains(s, "commb_acas_ra\t") {
		t.Error("Event should contain commb_acas_ra field")
	}

	// Should end with newline
	if !strings.HasSuffix(s, "\n") {
		t.Error("Event should end with newline")
	}
}

func TestWriteFATSVPeriodicUpdatesRealState(t *testing.T) {
	ModesChecksumInit(2)
	tracker := NewTracker()
	writer := NewFATSVWriter(tracker)

	now := uint64(time.Now().UnixMilli())
	a := createTestAircraft(tracker, 0x4840D6)
	a.Messages = 10
	a.Seen = now
	a.AltitudeValid = DataValidity{Source: SOURCE_MODE_S_CHECKED, Updated: now}
	a.Altitude = 35000
	a.PositionValid = DataValidity{Source: SOURCE_ADSB, Updated: now}
	a.Lat = 52.0
	a.Lon = 0.0
	a.HeadingValid = DataValidity{Source: SOURCE_ADSB, Updated: now}
	a.Heading = 180
	a.SpeedValid = DataValidity{Source: SOURCE_ADSB, Updated: now}
	a.Speed = 450

	// First call should produce output
	result := writer.WriteFATSV()
	if len(result) == 0 {
		t.Fatal("First WriteFATSV should produce output")
	}

	// After WriteFATSV, the real tracker aircraft should have FATSVLastEmitted updated
	// This is the bug we're fixing: GetAllAircraft() returns copies, so
	// the writer updating copies doesn't affect real state.
	realA := tracker.GetAircraft(0x4840D6)
	if realA == nil {
		t.Fatal("Aircraft should exist in tracker")
	}
	if realA.FATSVLastEmitted == 0 {
		t.Error("BUG: FATSVLastEmitted should be updated on real tracker aircraft after WriteFATSV, not just on copies")
	}

	// Second call with no changes should not produce output (since state was updated)
	result2 := writer.WriteFATSV()
	if len(result2) != 0 {
		t.Errorf("Second WriteFATSV with no changes should produce no output, got %q", string(result2))
	}
}

func TestWriteFATSVNoBlankLineHeartbeat(t *testing.T) {
	ModesChecksumInit(2)
	tracker := NewTracker()
	writer := NewFATSVWriter(tracker)

	// No aircraft: WriteFATSV should return nil/empty, NOT a blank line
	result := writer.WriteFATSV()
	if len(result) > 0 {
		t.Errorf("Empty tracker should produce no output, got %q", string(result))
	}
	if string(result) == "\n" {
		t.Error("FATSV must NOT produce blank-line heartbeat")
	}
}

func TestWriteFATSVEventFormatHexid(t *testing.T) {
	ModesChecksumInit(2)
	tracker := NewTracker()
	writer := NewFATSVWriter(tracker)

	now := uint64(time.Now().UnixMilli())
	a := createTestAircraft(tracker, 0x4840D6)
	a.Messages = 10
	a.Seen = now

	// Test with non-ICAO address
	mmNonICAO := &Message{
		Msg:     [14]byte{0xA0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		MsgBits: 112,
		MsgType: 20,
		Addr:    0x123456 | MODES_NON_ICAO_ADDRESS,
		MB:      [7]byte{0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	}

	a2 := createTestAircraft(tracker, 0x123456|MODES_NON_ICAO_ADDRESS)
	a2.Messages = 10
	a2.Seen = now

	result := writer.WriteFATSVEvent(mmNonICAO, a2)
	if result == nil {
		t.Fatal("Expected output for non-ICAO")
	}
	s := string(result)
	if !strings.Contains(s, "otherid\t") {
		t.Errorf("Non-ICAO should use 'otherid' field, got %q", s)
	}
	if strings.Contains(s, "hexid\t") {
		t.Errorf("Non-ICAO should NOT use 'hexid' field, got %q", s)
	}
}

func TestWriteFATSVPeriodicContainsFields(t *testing.T) {
	ModesChecksumInit(2)
	tracker := NewTracker()
	writer := NewFATSVWriter(tracker)

	now := uint64(time.Now().UnixMilli())
	a := createTestAircraft(tracker, 0x4840D6)
	a.Messages = 10
	a.Seen = now
	a.CallsignValid = DataValidity{Source: SOURCE_ADSB, Updated: now}
	copy(a.Callsign[:], "TEST1234")
	a.AltitudeValid = DataValidity{Source: SOURCE_MODE_S_CHECKED, Updated: now}
	a.Altitude = 35000
	a.PositionValid = DataValidity{Source: SOURCE_ADSB, Updated: now}
	a.Lat = 52.21
	a.Lon = 0.177
	a.HeadingValid = DataValidity{Source: SOURCE_ADSB, Updated: now}
	a.Heading = 180
	a.SpeedValid = DataValidity{Source: SOURCE_ADSB, Updated: now}
	a.Speed = 450

	result := writer.WriteFATSV()
	if len(result) == 0 {
		t.Fatal("Expected periodic output")
	}

	s := string(result)

	// Should contain clock field
	if !strings.Contains(s, "clock\t") {
		t.Error("Periodic output should contain clock field")
	}

	// Should contain hexid
	if !strings.Contains(s, "hexid\t") {
		t.Error("Periodic output should contain hexid field")
	}

	// Should contain ident (callsign)
	if !strings.Contains(s, "ident\t") {
		t.Error("Periodic output should contain ident field")
	}

	// Should contain altitude
	if !strings.Contains(s, "alt\t") {
		t.Error("Periodic output should contain alt field")
	}

	// Should contain speed
	if !strings.Contains(s, "speed\t") {
		t.Error("Periodic output should contain speed field")
	}

	// Should contain heading
	if !strings.Contains(s, "heading\t") {
		t.Error("Periodic output should contain heading field")
	}

	// Should contain position
	if !strings.Contains(s, "lat\t") {
		t.Error("Periodic output should contain lat field")
	}

	// Each line should end with newline
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	for _, line := range lines {
		if line == "" {
			t.Error("FATSV must not contain blank lines")
		}
	}
}

func TestFATSVEventNonICAOAddressType(t *testing.T) {
	ModesChecksumInit(2)
	tracker := NewTracker()
	writer := NewFATSVWriter(tracker)

	now := uint64(time.Now().UnixMilli())
	addr := uint32(0x123456) | MODES_NON_ICAO_ADDRESS
	a := createTestAircraft(tracker, addr)
	a.Messages = 10
	a.Seen = now

	mm := &Message{
		Msg:     [14]byte{0xA0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		MsgBits: 112,
		MsgType: 20,
		Addr:    addr,
		AddrType: ADDR_TISB_OTHER,
		MB:      [7]byte{0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	}

	result := writer.WriteFATSVEvent(mm, a)
	if result == nil {
		t.Fatal("Expected output")
	}
	s := string(result)

	// Should contain addrtype for non-standard addresses
	if !strings.Contains(s, "addrtype\t") {
		t.Errorf("Non-standard address should include addrtype, got %q", s)
	}
}

func BenchmarkWriteFATSV(b *testing.B) {
	ModesChecksumInit(2)
	tracker := NewTracker()
	writer := NewFATSVWriter(tracker)

	now := uint64(time.Now().UnixMilli())
	for i := 0; i < 100; i++ {
		a := createTestAircraft(tracker, uint32(0x400000+i))
		a.Messages = 10
		a.Seen = now
		a.AltitudeValid = DataValidity{Source: SOURCE_MODE_S_CHECKED, Updated: now}
		a.Altitude = 35000
		a.PositionValid = DataValidity{Source: SOURCE_ADSB, Updated: now}
		a.Lat = 52.0
		a.Lon = 0.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = writer.WriteFATSV()
	}
}
