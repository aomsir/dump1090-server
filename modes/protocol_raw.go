// Package modes implements Mode S message decoding and output formats.
//
// protocol_raw.go: Raw/AVR protocol parsing and encoding translated from
// dump1090-mutability's net_io.c, maintaining compatibility with the
// C implementation.
//
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// Licensed under GPL v2+
package modes

import (
	"fmt"
)

// ParseRawAVR parses a raw/AVR format message string.
// Supported formats:
//   - *HEX; or *HEX (untimed)
//   - @TIMESTAMP*HEX; (timed with star separator)
//   - @TIMESTAMPHEX; (timed without star separator - upstream format)
//
// If modeAC is true, two-byte Mode A/C messages are decoded.
// Returns nil message if the format is invalid or Mode A/C is disabled for 2-byte messages.
func ParseRawAVR(input string, modeAC bool) (*Message, error) {
	if len(input) == 0 {
		return nil, nil
	}

	// Remove trailing semicolon if present
	if input[len(input)-1] == ';' {
		input = input[:len(input)-1]
	}

	var hexData string
	var timestamp uint64
	hasTimestamp := false

	switch input[0] {
	case '*', ':':
		// Simple format: *HEXDATA or :HEXDATA
		hexData = input[1:]

	case '@', '%':
		// Timestamp format: @TIMESTAMPHEX or @TIMESTAMP*HEX
		// Timestamp is 12 hex chars
		if len(input) < 14 {
			return nil, fmt.Errorf("too short for timestamp format")
		}

		// Parse timestamp (12 hex chars)
		ts, err := parseHexString(input[1:13])
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp: %v", err)
		}
		timestamp = ts
		hasTimestamp = true

		// Check if there's a * separator after timestamp
		if input[13] == '*' {
			// @TIMESTAMP*HEX format
			hexData = input[14:]
		} else {
			// @TIMESTAMPHEX format (upstream format without *)
			hexData = input[13:]
		}

	default:
		return nil, nil
	}

	// Decode hex data
	msgBytes, err := decodeHexString(hexData)
	if err != nil {
		return nil, err
	}

	// Validate message length
	if len(msgBytes) == 2 {
		// Mode A/C message (2 bytes)
		if !modeAC {
			return nil, nil
		}
		mm := DecodeModeAMessage(int(msgBytes[0])<<8 | int(msgBytes[1]))
		if hasTimestamp {
			mm.Timestamp = timestamp
			mm.TimestampMsg = timestamp
		}
		return mm, nil
	}

	if len(msgBytes) != 7 && len(msgBytes) != 14 {
		return nil, fmt.Errorf("invalid message length: %d bytes", len(msgBytes))
	}

	// Decode Mode S message
	mm, result := DecodeModesMessage(msgBytes)
	if result < 0 {
		return nil, fmt.Errorf("decode failed with result %d", result)
	}

	if hasTimestamp {
		mm.Timestamp = timestamp
		mm.TimestampMsg = timestamp
	}

	return mm, nil
}

// parseHexString parses a hex string to uint64.
func parseHexString(s string) (uint64, error) {
	var result uint64
	for _, c := range s {
		result <<= 4
		switch {
		case c >= '0' && c <= '9':
			result |= uint64(c - '0')
		case c >= 'a' && c <= 'f':
			result |= uint64(c-'a') + 10
		case c >= 'A' && c <= 'F':
			result |= uint64(c-'A') + 10
		default:
			return 0, fmt.Errorf("invalid hex char: %c", c)
		}
	}
	return result, nil
}

// decodeHexString decodes a hex string to bytes.
func decodeHexString(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd length hex string")
	}
	result := make([]byte, len(s)/2)
	for i := 0; i < len(result); i++ {
		b, err := parseHexByte(s[i*2 : i*2+2])
		if err != nil {
			return nil, err
		}
		result[i] = b
	}
	return result, nil
}

// parseHexByte parses a 2-character hex string to a byte.
func parseHexByte(s string) (byte, error) {
	if len(s) != 2 {
		return 0, fmt.Errorf("invalid hex byte")
	}
	var b byte
	for _, c := range s {
		b <<= 4
		switch {
		case c >= '0' && c <= '9':
			b |= byte(c - '0')
		case c >= 'a' && c <= 'f':
			b |= byte(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			b |= byte(c - 'A' + 10)
		default:
			return 0, fmt.Errorf("invalid hex char")
		}
	}
	return b, nil
}
