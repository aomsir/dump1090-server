// Package modes implements Mode S message decoding and output formats.
//
// protocol_beast.go: Beast binary protocol parsing and encoding translated
// from dump1090-mutability's net_io.c, maintaining compatibility with the
// C implementation.
//
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// Licensed under GPL v2+
package modes

import (
	"bytes"
	"fmt"
)

// unescapeBeastPayload unescapes Beast binary payload data.
// In Beast format, 0x1A bytes are doubled (escaped).
// This function removes the duplicate 0x1A bytes.
// Returns the unescaped bytes and the position in the original data.
func unescapeBeastPayload(data []byte, want int) (raw []byte, pos int) {
	raw = make([]byte, 0, want)
	pos = 0
	for len(raw) < want && pos < len(data) {
		b := data[pos]
		pos++
		if b == BeastEscape && pos < len(data) {
			// Skip the duplicate escape byte
			if data[pos] == BeastEscape {
				pos++
			}
		}
		raw = append(raw, b)
	}
	return raw, pos
}

// DecodeBeastWithConfig decodes a Beast binary format message with configuration.
// If modeAC is true, type 1 Mode A/C payloads are decoded.
// Returns the message, remaining data, and any error.
func DecodeBeastWithConfig(data []byte, modeAC bool) (*Message, []byte, error) {
	if len(data) < 11 {
		return nil, data, fmt.Errorf("buffer too short")
	}

	// Find start byte
	idx := bytes.IndexByte(data, BeastEscape)
	if idx < 0 {
		return nil, nil, fmt.Errorf("no beast message found")
	}
	data = data[idx:]

	if len(data) < 11 {
		return nil, data, fmt.Errorf("incomplete message")
	}

	// Get type
	msgType := data[1]
	var msgLen int
	switch msgType {
	case BeastTypeModeAC:
		msgLen = 2
	case BeastTypeShort:
		msgLen = MODES_SHORT_MSG_BYTES
	case BeastTypeLong:
		msgLen = MODES_LONG_MSG_BYTES
	case BeastTypeStatus:
		// Status messages are not decoded by this project.
		// Skip conservatively: consume the type header (2 bytes) and return nil.
		// We don't know exact status payload length, so we just skip the header
		// and let the caller find the next escape byte.
		return nil, data[2:], nil
	default:
		return nil, data[2:], fmt.Errorf("unknown beast type: %c", msgType)
	}

	// Handle heartbeat detection.
	// Heartbeat is defined as type 1 with all-zero timestamp/signal/message bytes.
	// This matches BeastHeartbeatMessage() which returns:
	//   {0x1A, '1', 0, 0, 0, 0, 0, 0, 0, 0, 0}
	// We check all 9 bytes after the type byte are zero to avoid misclassifying
	// valid Mode A/C frames with zero timestamp but nonzero payload.
	if msgType == BeastTypeModeAC {
		isHeartbeat := true
		for i := 2; i < 11 && i < len(data); i++ {
			if data[i] != 0 {
				isHeartbeat = false
				break
			}
		}
		if isHeartbeat {
			// Heartbeat: return nil message, consume the heartbeat bytes
			return nil, data[11:], nil
		}
	}

	// For Mode A/C messages, check if Mode A/C is enabled
	if msgType == BeastTypeModeAC && !modeAC {
		// Skip this message but still need to unescape to find the end
		_, pos := unescapeBeastPayload(data[2:], 7+msgLen)
		return nil, data[2+pos:], nil
	}

	// Unescape and extract timestamp + signal + message
	raw, pos := unescapeBeastPayload(data[2:], 7+msgLen)
	pos += 2 // Adjust for type byte

	if len(raw) < 7+msgLen {
		return nil, data, fmt.Errorf("incomplete message data")
	}

	// Extract timestamp (6 bytes big-endian)
	ts := uint64(raw[0])<<40 | uint64(raw[1])<<32 | uint64(raw[2])<<24 |
		uint64(raw[3])<<16 | uint64(raw[4])<<8 | uint64(raw[5])

	// Extract signal level
	sigByte := raw[6]
	signalLevel := float64(sigByte) / 255.0
	signalLevel = signalLevel * signalLevel // Convert back from sqrt

	// Extract message
	msgBytes := raw[7 : 7+msgLen]

	// Decode the message
	var mm *Message
	var result int

	if msgType == BeastTypeModeAC && modeAC {
		// Mode A/C message
		modeA := int(msgBytes[0])<<8 | int(msgBytes[1])
		mm = DecodeModeAMessage(modeA)
		result = 0
	} else {
		// Mode S message
		mm, result = DecodeModesMessage(msgBytes)
		if result < 0 {
			return nil, data[pos:], fmt.Errorf("failed to decode message: %d", result)
		}
	}

	// Set metadata
	mm.Timestamp = ts
	mm.TimestampMsg = ts // Beast format timestamp is used for MLAT detection
	mm.SignalLevel = signalLevel
	mm.Remote = true

	// Check for MLAT magic timestamp
	if mm.TimestampMsg == MAGIC_MLAT_TIMESTAMP {
		mm.Source = SOURCE_MLAT
	}

	return mm, data[pos:], nil
}
