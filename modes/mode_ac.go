// Package modes implements Mode A/C decoding.
//
// This is a direct translation from dump1090-mutability's mode_ac.c.
// Mode A/C is an older transponder protocol using Gillham encoding.
//
// Original Copyright (C) 2012 by Salvatore Sanfilippo <antirez@gmail.com>
// Licensed under BSD
package modes

// ModeAToModeC converts a Mode A code to Mode C altitude.
// Input format is : 00:A4:A2:A1:00:B4:B2:B1:00:C4:C2:C1:00:D4:D2:D1
// Returns altitude in 100s of feet, or INVALID_ALTITUDE on error.
func ModeAToModeC(modeA uint32) int {
	var fiveHundreds uint32
	var oneHundreds uint32

	// Check zero bits are zero, D1 set is illegal
	if (modeA&0xFFFF8889) != 0 || (modeA&0x000000F0) == 0 {
		// C1..C4 cannot be Zero
		return INVALID_ALTITUDE
	}

	if modeA&0x0010 != 0 {
		oneHundreds ^= 0x007
	} // C1
	if modeA&0x0020 != 0 {
		oneHundreds ^= 0x003
	} // C2
	if modeA&0x0040 != 0 {
		oneHundreds ^= 0x001
	} // C4

	// Remove 7s from OneHundreds (Make 7->5, and 5->7)
	if (oneHundreds & 5) == 5 {
		oneHundreds ^= 2
	}

	// Check for invalid codes, only 1 to 5 are valid
	if oneHundreds > 5 {
		return INVALID_ALTITUDE
	}

	// D1 never used for altitude
	if modeA&0x0002 != 0 {
		fiveHundreds ^= 0x0FF
	} // D2
	if modeA&0x0004 != 0 {
		fiveHundreds ^= 0x07F
	} // D4

	if modeA&0x1000 != 0 {
		fiveHundreds ^= 0x03F
	} // A1
	if modeA&0x2000 != 0 {
		fiveHundreds ^= 0x01F
	} // A2
	if modeA&0x4000 != 0 {
		fiveHundreds ^= 0x00F
	} // A4

	if modeA&0x0100 != 0 {
		fiveHundreds ^= 0x007
	} // B1
	if modeA&0x0200 != 0 {
		fiveHundreds ^= 0x003
	} // B2
	if modeA&0x0400 != 0 {
		fiveHundreds ^= 0x001
	} // B4

	// Correct order of OneHundreds
	if fiveHundreds&1 != 0 {
		oneHundreds = 6 - oneHundreds
	}

	return int((fiveHundreds * 5) + oneHundreds - 13)
}

// DecodeID13Field decodes a 13-bit identity (squawk) field.
// Bits are interleaved as: C1-A1-C2-A2-C4-A4-ZERO-B1-D1-B2-D2-B4-D4
// Returns hexadecimal representation of the four octal digits.
func DecodeID13Field(id13Field int) int {
	var hexGillham int

	if id13Field&0x1000 != 0 {
		hexGillham |= 0x0010
	} // Bit 12 = C1
	if id13Field&0x0800 != 0 {
		hexGillham |= 0x1000
	} // Bit 11 = A1
	if id13Field&0x0400 != 0 {
		hexGillham |= 0x0020
	} // Bit 10 = C2
	if id13Field&0x0200 != 0 {
		hexGillham |= 0x2000
	} // Bit  9 = A2
	if id13Field&0x0100 != 0 {
		hexGillham |= 0x0040
	} // Bit  8 = C4
	if id13Field&0x0080 != 0 {
		hexGillham |= 0x4000
	} // Bit  7 = A4
	// Bit 6 = X or M (not used)
	if id13Field&0x0020 != 0 {
		hexGillham |= 0x0100
	} // Bit  5 = B1
	if id13Field&0x0010 != 0 {
		hexGillham |= 0x0001
	} // Bit  4 = D1 or Q
	if id13Field&0x0008 != 0 {
		hexGillham |= 0x0200
	} // Bit  3 = B2
	if id13Field&0x0004 != 0 {
		hexGillham |= 0x0002
	} // Bit  2 = D2
	if id13Field&0x0002 != 0 {
		hexGillham |= 0x0400
	} // Bit  1 = B4
	if id13Field&0x0001 != 0 {
		hexGillham |= 0x0004
	} // Bit  0 = D4

	return hexGillham
}

// DecodeAC13Field decodes the 13-bit AC altitude field (in DF 20 and others).
// Returns altitude in feet and sets unit to UNIT_FEET or UNIT_METERS.
func DecodeAC13Field(ac13Field int) (altitude int, unit AltitudeUnit) {
	mBit := ac13Field & 0x0040 // set = meters, clear = feet
	qBit := ac13Field & 0x0010 // set = 25 ft encoding, clear = Gillham Mode C

	if mBit == 0 {
		unit = UNIT_FEET
		if qBit != 0 {
			// N is the 11 bit integer resulting from the removal of bit Q and M
			n := ((ac13Field & 0x1F80) >> 2) |
				((ac13Field & 0x0020) >> 1) |
				(ac13Field & 0x000F)
			// The final altitude is resulting number multiplied by 25, minus 1000
			return n*25 - 1000, unit
		}
		// N is an 11 bit Gillham coded altitude
		n := ModeAToModeC(uint32(DecodeID13Field(ac13Field)))
		if n < -12 {
			return INVALID_ALTITUDE, unit
		}
		return 100 * n, unit
	}

	// Meter unit selected
	unit = UNIT_METERS
	// TODO: Implement altitude when meter unit is selected
	return INVALID_ALTITUDE, unit
}

// DecodeModeAMessage creates a Mode S style message from a Mode A/C reply.
func DecodeModeAMessage(modeA int) *Message {
	mm := &Message{}

	mm.MsgType = 32 // Use 32 to indicate Mode A/C (valid DF's are 0-31)
	mm.MsgBits = 16

	// Fudge up a Mode S style data stream
	mm.Msg[0] = byte(modeA >> 8)
	mm.Msg[1] = byte(modeA)
	mm.Verbatim[0] = mm.Msg[0]
	mm.Verbatim[1] = mm.Msg[1]

	// Fudge an address based on Mode A (remove the Ident bit)
	mm.Addr = uint32(modeA&0x0000FF7F) | MODES_NON_ICAO_ADDRESS

	// Set the Identity field to ModeA
	mm.Squawk = uint32(modeA & 0x7777)
	mm.SquawkValid = true

	// Flag ident in flight status
	if modeA&0x0080 != 0 {
		mm.SPI = true
	}
	mm.SPIValid = true

	// Decode an altitude if this looks like a possible Mode C
	if !mm.SPI {
		modeC := ModeAToModeC(uint32(modeA))
		if modeC != INVALID_ALTITUDE {
			mm.Altitude = modeC * 100
			mm.AltitudeUnit = UNIT_FEET
			mm.AltitudeSource = ALTITUDE_BARO
			mm.AltitudeValid = true
		}
	}

	mm.Corrected = 0
	return mm
}

// decodeMovementField decodes the 7-bit ground movement field.
// Uses PWL exponential style scale.
// Movement codes 0, 125, 126, 127 are invalid and should be trapped before calling.
func decodeMovementField(movement uint32) uint32 {
	var gspeed uint32

	if movement > 123 {
		gspeed = 199 // > 175kt
	} else if movement > 108 {
		gspeed = ((movement - 108) * 5) + 100
	} else if movement > 93 {
		gspeed = ((movement - 93) * 2) + 70
	} else if movement > 38 {
		gspeed = (movement - 38) + 15
	} else if movement > 12 {
		gspeed = ((movement - 11) >> 1) + 2
	} else if movement > 8 {
		gspeed = ((movement - 6) >> 2) + 1
	} else {
		gspeed = 0
	}

	return gspeed
}
