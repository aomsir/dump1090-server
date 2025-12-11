package modes

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// Real ADS-B messages for testing
var realMessages = []string{
	"8d7810601ab546c1a510276b582d",
	"a8001bbfa6780030aa0000f1f319",
	"8d7820bb99951ca7105c3a378dba",
	"8d7810d6ea088858013c08fa88e9",
	"8d780101ea4a8858013c0819bbff",
	"8d78079e9911c6177044494a4fb2",
	"8d781b0f586d411bc97c61e31a0f",
	"8d78115ce83418601c3c289d669d",
	"a800172bca680034a400003cb4c8",
	"411e8d6b8c341ad8",
	"8d7810509909a413f88c39e69ca6",
	"a8000dbdfe81c30000000043b90a",
	"a8c01535805c4d35e004e4c9cea2",
	"a91151289054152a8fbcd0e9beef",
	"a8000e14b01580b0a80000f0321f",
	"8d78097f5871c75b1f28d1cc2006",
	"8f7803b19909652278043bfe74af",
	"8d7806df990c15b7b004361170e0",
	"8d79a0625899659f55f24387189a",
	"a8000901a01b6535e004e638f633",
	"8d78025eea34085e173c08ffe560",
	"a8424332f7710935a004e5dd690a",
	"8d781860f82300030049b8c8add9",
	"8db819a4e5049e40000000536f27",
	"8d780c02990d9783b80448575175",
	"8d7805c9998cfba430143adfda56",
	"a8000d15b8f83fffb790ffc0cf1a",
	"a800088ac24a3b30601c00aae552",
	"8d7805895df1d0bdecf7bb26e345",
	"8d780b14990a0485486c49ff6bb0",
}

func TestRealMessagesCRC(t *testing.T) {
	// Initialize CRC tables
	ModesChecksumInit(2)

	for _, msgHex := range realMessages {
		msgHex = strings.TrimSpace(msgHex)
		msg, err := hex.DecodeString(msgHex)
		if err != nil {
			t.Errorf("Failed to decode hex %s: %v", msgHex, err)
			continue
		}

		// Determine message length
		msgBits := len(msg) * 8
		if msgBits != 56 && msgBits != 112 {
			t.Logf("Skipping message %s: invalid length %d bits", msgHex, msgBits)
			continue
		}

		// Calculate checksum
		checksum := ModesChecksum(msg, msgBits)

		// Get DF type
		df := int(msg[0] >> 3)

		// For DF11, DF17, DF18: checksum should be 0 or correctable
		// For DF0, DF4, DF5, DF16, DF20, DF21: checksum XOR address
		t.Logf("Message: %s", msgHex)
		t.Logf("  DF: %d, Length: %d bits, Checksum: %06X", df, msgBits, checksum)

		// Try to diagnose errors
		ei := ModesChecksumDiagnose(checksum, msgBits)
		if ei != nil {
			if ei.Errors == 0 {
				t.Logf("  CRC: VALID (no errors)")
			} else if ei.Errors > 0 {
				t.Logf("  CRC: %d-bit error(s) correctable", ei.Errors)
			} else {
				t.Logf("  CRC: NOT correctable (collision)")
			}
		} else {
			t.Logf("  CRC: NOT correctable")
		}

		// For DF17/DF18, checksum should be 0
		if df == 17 || df == 18 {
			if checksum == 0 {
				t.Logf("  DF%d: CRC valid", df)
			} else if ei != nil && ei.Errors > 0 {
				t.Logf("  DF%d: CRC correctable with %d bit fix(es)", df, ei.Errors)
			} else {
				t.Logf("  DF%d: CRC INVALID", df)
			}
		}

		// For DF11, extract II and check
		if df == 11 {
			// Address is in bytes 1-3
			addr := uint32(msg[1])<<16 | uint32(msg[2])<<8 | uint32(msg[3])
			iid := checksum & 0x7F // Interrogator ID
			crcAddr := checksum & 0xFFFF80
			t.Logf("  DF11: Address=%06X, IID=%d, CRC residual=%06X", addr, iid, crcAddr)
		}

		t.Logf("")
	}
}

func TestRealMessagesDecoding(t *testing.T) {
	// Initialize CRC tables
	ModesChecksumInit(2)

	decoded := 0
	df17Valid := 0
	df17Failed := 0
	df20_21NeedFilter := 0
	otherFailed := 0

	for _, msgHex := range realMessages {
		msgHex = strings.TrimSpace(msgHex)
		msg, err := hex.DecodeString(msgHex)
		if err != nil {
			continue
		}

		// Get DF type first
		df := int(msg[0] >> 3)

		mm, errCode := DecodeModesMessage(msg)
		if errCode == 0 && mm != nil {
			decoded++

			info := fmt.Sprintf("DF%d ICAO:%06X", mm.MsgType, mm.Addr)

			if len(mm.Callsign) > 0 && mm.Callsign[0] != 0 {
				info += fmt.Sprintf(" Callsign:%s", string(mm.Callsign[:]))
			}
			if mm.AltitudeValid {
				info += fmt.Sprintf(" Alt:%d", mm.Altitude)
			}
			if mm.SpeedValid {
				info += fmt.Sprintf(" Spd:%d", mm.Speed)
			}
			if mm.HeadingValid {
				info += fmt.Sprintf(" Hdg:%d", mm.Heading)
			}
			if mm.CPRValid {
				oddEven := "even"
				if mm.CPROdd {
					oddEven = "odd"
				}
				info += fmt.Sprintf(" CPR:%s", oddEven)
			}

			t.Logf("✓ %s -> %s", msgHex, info)
			if df == 17 || df == 18 {
				df17Valid++
			}
		} else {
			// Categorize failures
			if df == 20 || df == 21 {
				// DF20/21 requires ICAO filter - this is expected behavior
				df20_21NeedFilter++
				t.Logf("○ %s -> DF%d (needs ICAO filter)", msgHex, df)
			} else if df == 17 || df == 18 {
				df17Failed++
				t.Logf("✗ %s -> DF%d CRC error (corrupted)", msgHex, df)
			} else {
				otherFailed++
				t.Logf("✗ %s -> DF%d error %d", msgHex, df, errCode)
			}
		}
	}

	t.Logf("")
	t.Logf("=== Summary ===")
	t.Logf("Total messages: %d", len(realMessages))
	t.Logf("Successfully decoded: %d", decoded)
	t.Logf("  - DF17/18 valid: %d", df17Valid)
	t.Logf("DF17/18 CRC errors (noise): %d", df17Failed)
	t.Logf("DF20/21 (need filter): %d", df20_21NeedFilter)
	t.Logf("Other failures: %d", otherFailed)

	// For DF17/18, we expect most to decode (some CRC errors from noise are normal)
	df17Total := df17Valid + df17Failed
	if df17Total > 0 && df17Valid < df17Total/2 {
		t.Errorf("Too few DF17/18 messages valid: %d/%d", df17Valid, df17Total)
	}
}
