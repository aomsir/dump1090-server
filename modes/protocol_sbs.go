// Package modes implements Mode S message decoding and output formats.
//
// protocol_sbs.go: SBS-1 BaseStation format encoding translated from
// dump1090-mutability's net_io.c, maintaining compatibility with the
// C implementation.
//
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// and Copyright (C) 2012 Salvatore Sanfilippo <antirez@gmail.com>
// Licensed under GPL v2+ / BSD
package modes

import (
	"bytes"
	"fmt"
	"time"
)

// EncodeSBS encodes a message in SBS-1 BaseStation format.
// Supports DF4 (altitude), DF5 (squawk), DF11 (all-call), DF17/18 (ES),
// DF20 (Comm-B altitude), DF21 (Comm-B squawk).
// Returns empty string for unsupported message types or messages with no useful content.
// Line endings are CRLF to match BaseStation protocol.
func EncodeSBS(mm *Message, a *Aircraft) string {
	if mm == nil {
		return ""
	}

	msgType := sbsMessageType(mm)
	if msgType == 0 {
		return ""
	}

	now := time.Now()
	dateStr := now.Format("2006/01/02")
	timeStr := now.Format("15:04:05.000")

	var buf bytes.Buffer

	// MSG,type,1,1,hex,1,date,time,date,time,
	fmt.Fprintf(&buf, "MSG,%d,1,1,%06X,1,%s,%s,%s,%s,",
		msgType, mm.Addr, dateStr, timeStr, dateStr, timeStr)

	// Callsign (field 11)
	if mm.CallsignValid {
		callsign := string(bytes.TrimRight(mm.Callsign[:], "\x00 "))
		buf.WriteString(callsign)
	}
	buf.WriteByte(',')

	// Altitude (field 12)
	if mm.AltitudeValid {
		fmt.Fprintf(&buf, "%d", mm.Altitude)
	}
	buf.WriteByte(',')

	// Ground speed (field 13)
	if mm.SpeedValid {
		fmt.Fprintf(&buf, "%d", mm.Speed)
	}
	buf.WriteByte(',')

	// Track/heading (field 14)
	if mm.HeadingValid {
		fmt.Fprintf(&buf, "%d", mm.Heading)
	}
	buf.WriteByte(',')

	// Position lat,lon (fields 15,16)
	if mm.CPRDecoded {
		fmt.Fprintf(&buf, "%.6f,%.6f", mm.DecodedLat, mm.DecodedLon)
	} else {
		buf.WriteByte(',')
	}
	buf.WriteByte(',')

	// Vertical rate (field 17)
	if mm.VertRateValid {
		fmt.Fprintf(&buf, "%d", mm.VertRate)
	}
	buf.WriteByte(',')

	// Squawk (field 18)
	if mm.SquawkValid {
		fmt.Fprintf(&buf, "%04X", mm.Squawk)
	}
	buf.WriteByte(',')

	// Alert (field 19)
	if mm.AlertValid && mm.Alert {
		buf.WriteByte('1')
	}
	buf.WriteByte(',')

	// Emergency (field 20) - not decoded
	buf.WriteByte(',')

	// SPI (field 21)
	if mm.SPIValid && mm.SPI {
		buf.WriteByte('1')
	}
	buf.WriteByte(',')

	// Ground (field 22)
	if mm.AirGround == AG_GROUND {
		buf.WriteByte('1')
	}

	// CRLF line ending
	buf.WriteString("\r\n")

	return buf.String()
}

// sbsMessageType determines the SBS MSG type from a decoded message.
// Returns 0 if the message should not produce SBS output.
//
// SBS MSG types:
//   1 = ES Identification and Category (callsign)
//   3 = ES Airborne Position (altitude, position)
//   4 = ES Airborne Velocity (speed, heading)
//   5 = Surveillance Altitude (short reply altitude)
//   6 = Surveillance ID (short reply squawk)
//   7 = Air-to-Air Message (DF16 altitude)
//
// Supported DF types:
//   DF4  -> MSG 5 (altitude) or MSG 3 (if altitude valid)
//   DF5  -> MSG 6 (squawk)
//   DF11 -> MSG 3 (altitude if available) or MSG 6 (squawk if available)
//   DF17 -> MSG 1/3/4 based on decoded content
//   DF18 -> MSG 1/3/4 based on decoded content
//   DF20 -> MSG 3 (altitude from Comm-B) or MSG 1 (callsign from BDS 2,0)
//   DF21 -> MSG 6 (squawk from Comm-B) or MSG 1 (callsign from BDS 2,0)
func sbsMessageType(mm *Message) int {
	switch mm.MsgType {
	case 4:
		// DF4: Surveillance Reply, Altitude
		if mm.AltitudeValid {
			return 3
		}
		return 0

	case 5:
		// DF5: Surveillance Reply, Identity (squawk)
		if mm.SquawkValid {
			return 6
		}
		return 0

	case 11:
		// DF11: All-call reply - may have altitude
		if mm.AltitudeValid {
			return 3
		}
		return 0

	case 17, 18:
		// DF17/18: Extended Squitter - prioritize by content
		if mm.CallsignValid {
			return 1
		}
		if mm.AltitudeValid {
			return 3
		}
		if mm.SpeedValid || mm.HeadingValid {
			return 4
		}
		if mm.SquawkValid {
			return 6
		}
		return 0

	case 20:
		// DF20: Comm-B, Altitude Reply
		if mm.CallsignValid {
			return 1
		}
		if mm.AltitudeValid {
			return 3
		}
		return 0

	case 21:
		// DF21: Comm-B, Identity Reply
		if mm.CallsignValid {
			return 1
		}
		if mm.SquawkValid {
			return 6
		}
		return 0

	default:
		return 0
	}
}
