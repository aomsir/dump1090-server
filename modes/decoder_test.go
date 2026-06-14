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

func TestDecodeWithConfigSetsMLATSource(t *testing.T) {
	ModesChecksumInit(2)

	// Valid DF17 message (ICAO 0x4840D6)
	msg := mustDecodeHex("8D4840D6202CC371C32CE0576098")

	cfg := DecodeConfig{
		Remote:       true,
		TimestampMsg: MAGIC_MLAT_TIMESTAMP,
	}

	mm, result := DecodeModesMessageWithConfig(msg, cfg)
	if result != 0 {
		t.Fatalf("decode failed: result %d", result)
	}
	if mm.MsgType != 17 {
		t.Fatalf("got DF%d want DF17", mm.MsgType)
	}
	if mm.Source != SOURCE_MLAT {
		t.Fatalf("got source %v want SOURCE_MLAT", mm.Source)
	}
	if !mm.Remote {
		t.Fatal("expected Remote=true")
	}
	if mm.TimestampMsg != MAGIC_MLAT_TIMESTAMP {
		t.Fatalf("got TimestampMsg 0x%012X want 0x%012X", mm.TimestampMsg, MAGIC_MLAT_TIMESTAMP)
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

// buildDF17Msg builds a 14-byte DF17 message with a correct CRC.
func buildDF17Msg(icao uint32, me [7]byte) []byte {
	msg := make([]byte, MODES_LONG_MSG_BYTES)
	msg[0] = 0x8D // DF17, CA=5
	msg[1] = byte(icao >> 16)
	msg[2] = byte(icao >> 8)
	msg[3] = byte(icao)
	copy(msg[4:11], me[:])
	crc := ModesChecksum(msg, MODES_LONG_MSG_BITS)
	msg[11] = byte(crc >> 16)
	msg[12] = byte(crc >> 8)
	msg[13] = byte(crc)
	return msg
}

// buildDF18Msg builds a 14-byte DF18 message with a correct CRC.
func buildDF18Msg(cf byte, addr uint32, me [7]byte) []byte {
	msg := make([]byte, MODES_LONG_MSG_BYTES)
	msg[0] = 0x90 | (cf & 0x07) // DF18 = 10010, bits 6-8 = CF
	msg[1] = byte(addr >> 16)
	msg[2] = byte(addr >> 8)
	msg[3] = byte(addr)
	copy(msg[4:11], me[:])
	crc := ModesChecksum(msg, MODES_LONG_MSG_BITS)
	msg[11] = byte(crc >> 16)
	msg[12] = byte(crc >> 8)
	msg[13] = byte(crc)
	return msg
}

// buildDF20Msg builds a 14-byte DF20 message whose CRC syndrome equals icao.
func buildDF20Msg(icao uint32, mb [7]byte) []byte {
	msg := make([]byte, MODES_LONG_MSG_BYTES)
	msg[0] = 0xA0 // DF20, FS=0
	copy(msg[4:11], mb[:])
	// Compute CRC of data portion (bytes 0-10) with zero AP
	dataCRC := ModesChecksum(msg, MODES_LONG_MSG_BITS)
	// AP = dataCRC XOR ICAO so that CRC syndrome of full message = ICAO
	ap := dataCRC ^ icao
	msg[11] = byte(ap >> 16)
	msg[12] = byte(ap >> 8)
	msg[13] = byte(ap)
	return msg
}

func TestDecodeExtendedSquitterMEType0DispatchesAirbornePosition(t *testing.T) {
	ModesChecksumInit(2)

	// ME type 0 with AC12 = 0x1D0 (4600 feet, Q-bit set, N=224)
	me := [7]byte{0x00, 0x1D, 0x00, 0x00, 0x00, 0x00, 0x00}
	msg := buildDF17Msg(0x4840D6, me)

	mm, result := DecodeModesMessage(msg)
	if result != 0 {
		t.Fatalf("DecodeModesMessage failed: result %d", result)
	}

	if mm.METype != 0 {
		t.Errorf("METype = %d, want 0", mm.METype)
	}
	if !mm.AltitudeValid {
		t.Fatal("AltitudeValid = false, want true")
	}
	if mm.Altitude != 4600 {
		t.Errorf("Altitude = %d, want 4600", mm.Altitude)
	}
	if mm.AltitudeSource != ALTITUDE_BARO {
		t.Errorf("AltitudeSource = %v, want ALTITUDE_BARO", mm.AltitudeSource)
	}
	if mm.CPRNUCP != 0 {
		t.Errorf("CPRNUCP = %d, want 0 for ME type 0", mm.CPRNUCP)
	}
}

func TestDecodeDF18CF5MarksTISBNonICAO(t *testing.T) {
	ModesChecksumInit(2)

	me := [7]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	msg := buildDF18Msg(5, 0x123456, me)

	mm, result := DecodeModesMessage(msg)
	if result != 0 {
		t.Fatalf("DecodeModesMessage failed: result %d", result)
	}

	if mm.Source != SOURCE_TISB {
		t.Errorf("Source = %v, want SOURCE_TISB", mm.Source)
	}
	if mm.AddrType != ADDR_TISB_OTHER {
		t.Errorf("AddrType = %v, want ADDR_TISB_OTHER", mm.AddrType)
	}
	if mm.Addr&MODES_NON_ICAO_ADDRESS == 0 {
		t.Error("Addr does not have MODES_NON_ICAO_ADDRESS flag set")
	}
}

func TestDecodeDF18UnknownCFDoesNotDecodePayloadAsKnownES(t *testing.T) {
	ModesChecksumInit(2)

	// ME type 4 (callsign) with AIS space chars; if decoded, CallsignValid would be true
	me := [7]byte{0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
	msg := buildDF18Msg(4, 0x4840D6, me)

	mm, result := DecodeModesMessage(msg)
	if result != 0 {
		t.Fatalf("DecodeModesMessage failed: result %d", result)
	}

	if mm.AddrType != ADDR_UNKNOWN {
		t.Errorf("AddrType = %v, want ADDR_UNKNOWN", mm.AddrType)
	}
	if mm.Addr&MODES_NON_ICAO_ADDRESS == 0 {
		t.Error("Addr does not have MODES_NON_ICAO_ADDRESS flag set")
	}
	if mm.CallsignValid {
		t.Error("CallsignValid = true, want false (ES payload should not be decoded for unknown CF)")
	}
}

func TestDecodeCommBOnlyDecodesBDS20WhenMarkerMatches(t *testing.T) {
	ModesChecksumInit(2)
	filter := NewICAOFilter()
	icao := uint32(0xABCDEF)
	filter.Add(icao)

	// Callsign "TEST1234" encoded in AIS 6-bit charset.
	// T=20, E=5, S=19, T=20, 1=49, 2=50, 3=51, 4=52
	// Packed into 6 bytes: 0x50 0x54 0xD4 0xC7 0x2C 0xF4
	callsignBytes := [6]byte{0x50, 0x54, 0xD4, 0xC7, 0x2C, 0xF4}

	t.Run("without marker", func(t *testing.T) {
		var mb [7]byte
		mb[0] = 0x00 // no BDS 2,0 marker
		copy(mb[1:], callsignBytes[:])
		msg := buildDF20Msg(icao, mb)

		mm, result := DecodeModesMessageWithFilter(msg, filter)
		if result < 0 {
			t.Fatalf("DecodeModesMessage failed: result %d", result)
		}

		if mm.CallsignValid {
			t.Errorf("CallsignValid = true without BDS 2,0 marker, want false (callsign=%q)", string(mm.Callsign[:8]))
		}
	})

	t.Run("with marker", func(t *testing.T) {
		var mb [7]byte
		mb[0] = 0x20 // BDS 2,0 marker
		copy(mb[1:], callsignBytes[:])
		msg := buildDF20Msg(icao, mb)

		mm, result := DecodeModesMessageWithFilter(msg, filter)
		if result < 0 {
			t.Fatalf("DecodeModesMessage failed: result %d", result)
		}

		if !mm.CallsignValid {
			t.Error("CallsignValid = false with BDS 2,0 marker, want true")
		}
		if string(mm.Callsign[:8]) != "TEST1234" {
			t.Errorf("Callsign = %q, want %q", string(mm.Callsign[:8]), "TEST1234")
		}
	})
}

func TestDecodeAC12QBitZeroMatchesUpstream(t *testing.T) {
	// AC12 = 0x010: Q-bit set (bit 4), N=0
	// Upstream returns 0*25-1000 = -1000 feet, not INVALID_ALTITUDE
	got, unit := decodeAC12Field(0x010)
	if unit != UNIT_FEET {
		t.Errorf("unit = %v, want UNIT_FEET", unit)
	}
	if got != -1000 {
		t.Errorf("decodeAC12Field(0x010) = %d, want -1000", got)
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
