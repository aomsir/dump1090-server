// Package modes implements Mode S message decoding and output formats.
//
// output.go: output format encoders translated from dump1090-mutability's
// net_io.c, maintaining compatibility with the C implementation.
//
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// and Copyright (C) 2012 Salvatore Sanfilippo <antirez@gmail.com>
// Licensed under GPL v2+ / BSD
package modes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// Beast binary format constants
const (
	BeastEscape       = 0x1A
	BeastTypeModeAC   = '1' // 2-byte Mode A/C
	BeastTypeShort    = '2' // 7-byte short message
	BeastTypeLong     = '3' // 14-byte long message
	BeastTypeStatus   = '4' // Status message (radarcape)
	BeastHeartbeat    = '1' // Same as Mode A/C but with no payload
)

// EncodeBeast encodes a message in Beast binary format.
// The Beast format is:
//   - 0x1A escape byte
//   - Type: '1' (Mode A/C), '2' (short), '3' (long)
//   - 6 bytes timestamp (big-endian, 12MHz clock)
//   - 1 byte signal level (0-255)
//   - Message bytes
//   - Any 0x1A bytes in the data are doubled (escaped)
func EncodeBeast(mm *Message) []byte {
	if mm == nil {
		return nil
	}

	msgLen := mm.MsgBits / 8
	if msgLen == 0 {
		return nil
	}

	// Allocate buffer with maximum possible size (all bytes escaped)
	buf := make([]byte, 0, 2+2*(7+msgLen))

	// Start byte
	buf = append(buf, BeastEscape)

	// Type byte
	switch msgLen {
	case 2:
		buf = append(buf, BeastTypeModeAC)
	case MODES_SHORT_MSG_BYTES:
		buf = append(buf, BeastTypeShort)
	case MODES_LONG_MSG_BYTES:
		buf = append(buf, BeastTypeLong)
	default:
		return nil
	}

	// Timestamp (6 bytes, big-endian) with escaping
	ts := mm.Timestamp
	buf = appendBeastByte(buf, byte(ts>>40))
	buf = appendBeastByte(buf, byte(ts>>32))
	buf = appendBeastByte(buf, byte(ts>>24))
	buf = appendBeastByte(buf, byte(ts>>16))
	buf = appendBeastByte(buf, byte(ts>>8))
	buf = appendBeastByte(buf, byte(ts))

	// Signal level (0-255)
	sig := int(math.Round(math.Sqrt(mm.SignalLevel) * 255))
	if mm.SignalLevel > 0 && sig < 1 {
		sig = 1
	}
	if sig > 255 {
		sig = 255
	}
	buf = appendBeastByte(buf, byte(sig))

	// Message bytes with escaping
	for i := 0; i < msgLen; i++ {
		buf = appendBeastByte(buf, mm.Msg[i])
	}

	return buf
}

// appendBeastByte appends a byte to the buffer, escaping 0x1A.
func appendBeastByte(buf []byte, b byte) []byte {
	buf = append(buf, b)
	if b == BeastEscape {
		buf = append(buf, b) // Double the escape byte
	}
	return buf
}

// BeastHeartbeatMessage returns a Beast heartbeat message.
func BeastHeartbeatMessage() []byte {
	// Heartbeat is a Mode A/C type with no actual payload
	return []byte{BeastEscape, BeastHeartbeat, 0, 0, 0, 0, 0, 0, 0, 0, 0}
}

// DecodeBeast decodes a Beast binary format message.
// Returns the message, remaining data, and any error.
func DecodeBeast(data []byte) (*Message, []byte, error) {
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
	default:
		return nil, data[2:], fmt.Errorf("unknown beast type: %c", msgType)
	}

	// Unescape and extract timestamp + signal + message
	raw := make([]byte, 0, 7+msgLen)
	pos := 2
	for len(raw) < 7+msgLen && pos < len(data) {
		b := data[pos]
		pos++
		if b == BeastEscape && pos < len(data) {
			// Skip the duplicate escape
			if data[pos] == BeastEscape {
				pos++
			}
		}
		raw = append(raw, b)
	}

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
	mm, err := DecodeModesMessage(msgBytes)
	if err != 0 {
		return nil, data[pos:], fmt.Errorf("failed to decode message: %d", err)
	}

	mm.Timestamp = ts
	mm.TimestampMsg = ts // Beast format timestamp is used for MLAT detection
	mm.SignalLevel = signalLevel

	return mm, data[pos:], nil
}

// AircraftJSON represents a single aircraft in JSON output.
type AircraftJSON struct {
	Hex      string   `json:"hex"`
	Type     string   `json:"type,omitempty"`
	Squawk   string   `json:"squawk,omitempty"`
	Flight   string   `json:"flight,omitempty"`
	Lat      *float64 `json:"lat,omitempty"`
	Lon      *float64 `json:"lon,omitempty"`
	NUCP     *uint32  `json:"nucp,omitempty"`
	SeenPos  *float64 `json:"seen_pos,omitempty"`
	Altitude any      `json:"altitude,omitempty"` // int or "ground"
	VertRate *int     `json:"vert_rate,omitempty"`
	Track    *uint32  `json:"track,omitempty"`
	Speed    *uint32  `json:"speed,omitempty"`
	Category string   `json:"category,omitempty"`
	MLAT     []string `json:"mlat"`
	TISB     []string `json:"tisb"`
	Messages int64    `json:"messages"`
	Seen     float64  `json:"seen"`
	RSSI     float64  `json:"rssi"`
}

// AircraftListJSON represents the aircraft list JSON response.
type AircraftListJSON struct {
	Now      float64        `json:"now"`
	Messages uint64         `json:"messages"`
	Aircraft []AircraftJSON `json:"aircraft"`
}

// GenerateAircraftJSON generates the aircraft list JSON from a tracker.
func GenerateAircraftJSON(tracker *Tracker, totalMessages uint64) *AircraftListJSON {
	now := float64(time.Now().UnixMilli()) / 1000.0
	aircraft := tracker.GetAllAircraft()

	result := &AircraftListJSON{
		Now:      now,
		Messages: totalMessages,
		Aircraft: make([]AircraftJSON, 0, len(aircraft)),
	}

	nowMs := uint64(time.Now().UnixMilli())

	for _, a := range aircraft {
		// Skip Mode A/C synthetic records
		if a.ModeACFlags&MODEAC_MSG_FLAG != 0 {
			continue
		}

		// Skip aircraft with only 1 message (likely bad decode)
		if a.Messages < 2 {
			continue
		}

		aj := AircraftJSON{
			Messages: a.Messages,
			Seen:     float64(nowMs-a.Seen) / 1000.0,
			MLAT:     make([]string, 0),
			TISB:     make([]string, 0),
		}

		// Format address
		if a.Addr&MODES_NON_ICAO_ADDRESS != 0 {
			aj.Hex = fmt.Sprintf("~%06x", a.Addr&0xFFFFFF)
		} else {
			aj.Hex = fmt.Sprintf("%06x", a.Addr)
		}

		// Address type
		if a.AddrType != ADDR_ADSB_ICAO {
			aj.Type = addrTypeString(a.AddrType)
		}

		// Squawk
		if a.SquawkValid.Source != SOURCE_INVALID {
			aj.Squawk = fmt.Sprintf("%04x", a.Squawk)
			appendSourceFlag(&aj, a.SquawkValid.Source, "squawk")
		}

		// Callsign
		if a.CallsignValid.Source != SOURCE_INVALID {
			callsign := string(bytes.TrimRight(a.Callsign[:], "\x00 "))
			if callsign != "" {
				aj.Flight = callsign
				appendSourceFlag(&aj, a.CallsignValid.Source, "callsign")
			}
		}

		// Position
		if a.PositionValid.Source != SOURCE_INVALID {
			aj.Lat = &a.Lat
			aj.Lon = &a.Lon
			aj.NUCP = &a.PosNUC
			seenPos := float64(nowMs-a.PositionValid.Updated) / 1000.0
			aj.SeenPos = &seenPos
			appendSourceFlag(&aj, a.PositionValid.Source, "lat")
			appendSourceFlag(&aj, a.PositionValid.Source, "lon")
		}

		// Altitude
		if a.AirGroundValid.Source >= SOURCE_MODE_S_CHECKED && a.AirGround == AG_GROUND {
			aj.Altitude = "ground"
		} else if a.AltitudeValid.Source != SOURCE_INVALID {
			aj.Altitude = a.Altitude
			appendSourceFlag(&aj, a.AltitudeValid.Source, "altitude")
		}

		// Vertical rate
		if a.VertRateValid.Source != SOURCE_INVALID {
			aj.VertRate = &a.VertRate
			appendSourceFlag(&aj, a.VertRateValid.Source, "vert_rate")
		}

		// Track/Heading
		if a.HeadingValid.Source != SOURCE_INVALID {
			aj.Track = &a.Heading
			appendSourceFlag(&aj, a.HeadingValid.Source, "track")
		}

		// Speed
		if a.SpeedValid.Source != SOURCE_INVALID {
			aj.Speed = &a.Speed
			appendSourceFlag(&aj, a.SpeedValid.Source, "speed")
		}

		// Category
		if a.CategoryValid.Source != SOURCE_INVALID {
			aj.Category = fmt.Sprintf("%02X", a.Category)
			appendSourceFlag(&aj, a.CategoryValid.Source, "category")
		}

		// RSSI (average of last 8 samples, in dBFS)
		var sum float64
		for i := 0; i < 8; i++ {
			sum += a.SignalLevel[i]
		}
		avg := (sum + 1e-5) / 8
		aj.RSSI = 10 * math.Log10(avg)

		result.Aircraft = append(result.Aircraft, aj)
	}

	return result
}

// appendSourceFlag adds a field name to MLAT or TISB list based on source.
func appendSourceFlag(aj *AircraftJSON, source DataSource, field string) {
	switch source {
	case SOURCE_MLAT:
		aj.MLAT = append(aj.MLAT, field)
	case SOURCE_TISB:
		aj.TISB = append(aj.TISB, field)
	}
}

// addrTypeString returns the string representation of an address type.
func addrTypeString(t AddrType) string {
	switch t {
	case ADDR_ADSB_ICAO:
		return "adsb_icao"
	case ADDR_ADSB_ICAO_NT:
		return "adsb_icao_nt"
	case ADDR_ADSR_ICAO:
		return "adsr_icao"
	case ADDR_TISB_ICAO:
		return "tisb_icao"
	case ADDR_ADSB_OTHER:
		return "adsb_other"
	case ADDR_ADSR_OTHER:
		return "adsr_other"
	case ADDR_TISB_OTHER:
		return "tisb_other"
	case ADDR_TISB_TRACKFILE:
		return "tisb_trackfile"
	default:
		return "unknown"
	}
}

// MarshalAircraftJSON returns the JSON bytes for the aircraft list.
func MarshalAircraftJSON(tracker *Tracker, totalMessages uint64) ([]byte, error) {
	data := GenerateAircraftJSON(tracker, totalMessages)
	return json.MarshalIndent(data, "", "  ")
}

// ReceiverJSON represents the receiver status JSON.
type ReceiverJSON struct {
	Version  string  `json:"version"`
	Refresh  float64 `json:"refresh"`
	History  int     `json:"history"`
	Lat      float64 `json:"lat,omitempty"`
	Lon      float64 `json:"lon,omitempty"`
}

// GenerateReceiverJSON generates the receiver status JSON.
func GenerateReceiverJSON(version string, refreshInterval float64, historySize int, lat, lon float64) *ReceiverJSON {
	return &ReceiverJSON{
		Version: version,
		Refresh: refreshInterval,
		History: historySize,
		Lat:     lat,
		Lon:     lon,
	}
}

// AVR format encoding

// EncodeAVR encodes a message in AVR format (ASCII hex with optional timestamp).
// Format: *HEXMSG; or @TIMESTAMP*HEXMSG;
func EncodeAVR(mm *Message, includeTimestamp bool) string {
	if mm == nil {
		return ""
	}

	msgLen := mm.MsgBits / 8
	if msgLen == 0 {
		return ""
	}

	var buf bytes.Buffer

	if includeTimestamp {
		// @TIMESTAMP*HEXMSG;
		buf.WriteByte('@')
		// Timestamp in hex (12 characters for 48-bit timestamp)
		fmt.Fprintf(&buf, "%012X", mm.Timestamp&0xFFFFFFFFFFFF)
	}

	buf.WriteByte('*')
	for i := 0; i < msgLen; i++ {
		fmt.Fprintf(&buf, "%02X", mm.Msg[i])
	}
	buf.WriteByte(';')
	buf.WriteByte('\n')

	return buf.String()
}

// SBS-1 (BaseStation) format encoding

// EncodeSBS encodes a message in SBS-1 BaseStation format.
// This is a CSV format with fields separated by commas.
func EncodeSBS(mm *Message, a *Aircraft) string {
	if mm == nil {
		return ""
	}

	// Only generate SBS for valid decoded messages
	if mm.MsgType != 17 && mm.MsgType != 18 {
		return ""
	}

	now := time.Now()
	dateStr := now.Format("2006/01/02")
	timeStr := now.Format("15:04:05.000")

	var msgType int
	var buf bytes.Buffer

	// Determine SBS message type based on content
	if mm.CallsignValid {
		msgType = 1 // ES Identification
	} else if mm.AltitudeValid {
		msgType = 3 // ES Airborne Position
	} else if mm.SpeedValid || mm.HeadingValid {
		msgType = 4 // ES Airborne Velocity
	} else {
		return "" // No useful content
	}

	// MSG,type,1,1,hex,1,date,time,date,time,callsign,alt,gs,track,lat,lon,vr,squawk,alert,emerg,spi,ground
	fmt.Fprintf(&buf, "MSG,%d,1,1,%06X,1,%s,%s,%s,%s,",
		msgType, mm.Addr, dateStr, timeStr, dateStr, timeStr)

	// Callsign
	if mm.CallsignValid {
		callsign := string(bytes.TrimRight(mm.Callsign[:], "\x00 "))
		buf.WriteString(callsign)
	}
	buf.WriteByte(',')

	// Altitude
	if mm.AltitudeValid {
		fmt.Fprintf(&buf, "%d", mm.Altitude)
	}
	buf.WriteByte(',')

	// Ground speed
	if mm.SpeedValid {
		fmt.Fprintf(&buf, "%d", mm.Speed)
	}
	buf.WriteByte(',')

	// Track
	if mm.HeadingValid {
		fmt.Fprintf(&buf, "%d", mm.Heading)
	}
	buf.WriteByte(',')

	// Position
	if mm.CPRDecoded {
		fmt.Fprintf(&buf, "%.6f,%.6f", mm.DecodedLat, mm.DecodedLon)
	} else {
		buf.WriteString(",")
	}
	buf.WriteByte(',')

	// Vertical rate
	if mm.VertRateValid {
		fmt.Fprintf(&buf, "%d", mm.VertRate)
	}
	buf.WriteByte(',')

	// Squawk
	if mm.SquawkValid {
		fmt.Fprintf(&buf, "%04X", mm.Squawk)
	}
	buf.WriteByte(',')

	// Alert, Emergency, SPI, Ground (not available in most messages)
	buf.WriteString(",,,")

	buf.WriteByte('\n')

	return buf.String()
}
