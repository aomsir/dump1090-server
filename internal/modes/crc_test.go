package modes

import (
	"encoding/hex"
	"testing"
)

func TestModesChecksum(t *testing.T) {
	// Initialize CRC tables
	ModesChecksumInit(2)

	tests := []struct {
		name     string
		message  string // hex-encoded message
		bits     int
		expected uint32 // Expected CRC value (compared against C implementation)
	}{
		{
			name:     "DF11 Mode S All-Call Reply",
			message:  "5d4840c39a7dc5",
			bits:     56,
			expected: 0x9D252D, // DF11 encodes ICAO in CRC field
		},
		{
			name:     "DF17 with valid CRC",
			message:  "8d4840d6202cc371c32ce0576098",
			bits:     112,
			expected: 0x000000, // Valid ADS-B message, CRC = 0
		},
		{
			name:     "DF17 ADS-B invalid message",
			message:  "8d48401d58c382d690c8ac2863a7",
			bits:     112,
			expected: 0x296990, // Invalid or corrupted message
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := hex.DecodeString(tt.message)
			if err != nil {
				t.Fatalf("failed to decode hex: %v", err)
			}

			got := ModesChecksum(msg, tt.bits)
			if got != tt.expected {
				t.Errorf("ModesChecksum() = 0x%06X, want 0x%06X", got, tt.expected)
			}
		})
	}
}

func TestModesChecksumErrorCorrection(t *testing.T) {
	ModesChecksumInit(2)

	// Valid DF17 message: 8D4840D6202CC371C32CE0576098
	validMsg := mustDecodeHex("8D4840D6202CC371C32CE0576098")

	tests := []struct {
		name          string
		corruptBit    int  // bit position to corrupt (-1 = no corruption)
		shouldCorrect bool // can we correct this error?
	}{
		{
			name:          "no error",
			corruptBit:    -1,
			shouldCorrect: true,
		},
		{
			name:          "1-bit error at position 10",
			corruptBit:    10,
			shouldCorrect: true,
		},
		{
			name:          "1-bit error at position 50",
			corruptBit:    50,
			shouldCorrect: true,
		},
		{
			name:          "1-bit error at position 100",
			corruptBit:    100,
			shouldCorrect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy and corrupt it
			msg := make([]byte, len(validMsg))
			copy(msg, validMsg)

			if tt.corruptBit >= 0 {
				bytePos := tt.corruptBit / 8
				bitPos := 7 - (tt.corruptBit % 8)
				msg[bytePos] ^= 1 << bitPos
			}

			// Compute syndrome
			syndrome := ModesChecksum(msg, MODES_LONG_MSG_BITS)

			// Diagnose
			errInfo := ModesChecksumDiagnose(syndrome, MODES_LONG_MSG_BITS)

			if tt.shouldCorrect {
				if errInfo == nil {
					t.Errorf("expected to find error correction, got nil")
					return
				}

				// Apply fix
				ModesChecksumFix(msg, errInfo)

				// Verify it's now valid
				if ModesChecksum(msg, MODES_LONG_MSG_BITS) != 0 {
					t.Errorf("after correction, checksum should be 0")
				}

				// Verify it matches original
				for i := range msg {
					if msg[i] != validMsg[i] {
						t.Errorf("corrected message doesn't match original at byte %d: got 0x%02X, want 0x%02X",
							i, msg[i], validMsg[i])
						break
					}
				}
			}
		})
	}
}

func TestModesChecksumTwoBitErrors(t *testing.T) {
	ModesChecksumInit(2)

	// Valid DF17 message
	validMsg := mustDecodeHex("8D4840D6202CC371C32CE0576098")

	// Corrupt 2 bits (positions 10 and 50, outside DF field)
	msg := make([]byte, len(validMsg))
	copy(msg, validMsg)

	// Flip bit 10
	msg[10/8] ^= 1 << (7 - (10 % 8))
	// Flip bit 50
	msg[50/8] ^= 1 << (7 - (50 % 8))

	syndrome := ModesChecksum(msg, MODES_LONG_MSG_BITS)
	errInfo := ModesChecksumDiagnose(syndrome, MODES_LONG_MSG_BITS)

	if errInfo == nil {
		t.Fatalf("expected to find 2-bit error correction")
	}

	if errInfo.Errors != 2 {
		t.Errorf("expected 2 errors, got %d", errInfo.Errors)
	}

	// Apply fix
	ModesChecksumFix(msg, errInfo)

	// Verify it's now valid
	if ModesChecksum(msg, MODES_LONG_MSG_BITS) != 0 {
		t.Errorf("after correction, checksum should be 0, got 0x%06X", ModesChecksum(msg, MODES_LONG_MSG_BITS))
	}
}

func TestCombinations(t *testing.T) {
	tests := []struct {
		n, k int
		want int
	}{
		{5, 0, 1},
		{5, 1, 5},
		{5, 2, 10},
		{5, 3, 10},
		{5, 4, 5},
		{5, 5, 1},
		{10, 2, 45},
		{107, 1, 107}, // for 1-bit errors in 112-bit message (ignoring first 5 bits)
		{107, 2, 5671}, // for 2-bit errors
	}

	for _, tt := range tests {
		got := combinations(tt.n, tt.k)
		if got != tt.want {
			t.Errorf("combinations(%d, %d) = %d, want %d", tt.n, tt.k, got, tt.want)
		}
	}
}

func TestModesChecksumInit(t *testing.T) {
	// Test initialization with different fix levels
	for fixBits := 0; fixBits <= 2; fixBits++ {
		t.Run(string(rune('0'+fixBits))+" bit correction", func(t *testing.T) {
			ModesChecksumInit(fixBits)

			if fixBits == 0 {
				if bitErrorTableShort != nil || bitErrorTableLong != nil {
					t.Error("with fixBits=0, error tables should be nil")
				}
			} else {
				if bitErrorTableShort == nil || bitErrorTableLong == nil {
					t.Error("error tables should not be nil")
				}
				if len(bitErrorTableShort) == 0 || len(bitErrorTableLong) == 0 {
					t.Error("error tables should not be empty")
				}
			}
		})
	}
}

func BenchmarkModesChecksum(b *testing.B) {
	ModesChecksumInit(2)
	msg := mustDecodeHex("8D4840D6202CC371C32CE0576098")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ModesChecksum(msg, MODES_LONG_MSG_BITS)
	}
}

func BenchmarkModesChecksumDiagnose(b *testing.B) {
	ModesChecksumInit(2)
	msg := mustDecodeHex("8D4840D6202CC371C32CE0576098")

	// Corrupt 1 bit
	msg[5] ^= 0x01
	syndrome := ModesChecksum(msg, MODES_LONG_MSG_BITS)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ModesChecksumDiagnose(syndrome, MODES_LONG_MSG_BITS)
	}
}

func BenchmarkModesChecksumFix(b *testing.B) {
	ModesChecksumInit(2)
	validMsg := mustDecodeHex("8D4840D6202CC371C32CE0576098")

	// Pre-compute error info
	corruptMsg := make([]byte, len(validMsg))
	copy(corruptMsg, validMsg)
	corruptMsg[5] ^= 0x01
	syndrome := ModesChecksum(corruptMsg, MODES_LONG_MSG_BITS)
	errInfo := ModesChecksumDiagnose(syndrome, MODES_LONG_MSG_BITS)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := make([]byte, len(validMsg))
		copy(msg, corruptMsg)
		ModesChecksumFix(msg, errInfo)
	}
}

// Helper function
func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// Example: basic CRC calculation
func ExampleModesChecksum() {
	ModesChecksumInit(0)

	// DF11 Mode S All-Call Reply with valid CRC
	msg := mustDecodeHex("5d4840c39a7dc5")
	crc := ModesChecksum(msg, 56)

	// Valid messages have CRC = 0
	if crc == 0 {
		println("Valid message")
	}
}

// Example: error correction
func ExampleModesChecksumFix() {
	ModesChecksumInit(2)

	// Valid message
	validMsg := mustDecodeHex("8D4840D6202CC371C32CE0576098")

	// Corrupt it (flip bit 10)
	msg := make([]byte, len(validMsg))
	copy(msg, validMsg)
	msg[1] ^= 0x20

	// Detect and fix
	syndrome := ModesChecksum(msg, MODES_LONG_MSG_BITS)
	errInfo := ModesChecksumDiagnose(syndrome, MODES_LONG_MSG_BITS)

	if errInfo != nil {
		ModesChecksumFix(msg, errInfo)
		// Now msg is corrected
	}
}
