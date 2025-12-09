// Package modes implements Mode S message decoding.
//
// This is a progressive translation from dump1090-mutability's mode_s.c,
// starting with core DF types (especially DF17/18 Extended Squitter).
//
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// and Copyright (C) 2012 Salvatore Sanfilippo <antirez@gmail.com>
// Licensed under GPL v2+ / BSD
package modes

// ModesMessageLenByType returns the message length in bits based on the Downlink Format.
// DF 16 or greater = 112 bits (long), DF 15 or less = 56 bits (short).
func ModesMessageLenByType(dfType int) int {
	if dfType&0x10 != 0 {
		return MODES_LONG_MSG_BITS
	}
	return MODES_SHORT_MSG_BITS
}

// getbits extracts a field from a message.
// Bits are numbered from 1 (MSB of first byte) to N (LSB of last byte).
// firstbit: first bit to extract (1-indexed)
// lastbit: last bit to extract (inclusive, 1-indexed)
func getbits(msg []byte, firstbit, lastbit int) uint32 {
	var result uint32
	for i := firstbit; i <= lastbit; i++ {
		byteIdx := (i - 1) / 8
		bitIdx := 7 - ((i - 1) % 8)
		if msg[byteIdx]&(1<<bitIdx) != 0 {
			result |= 1 << (lastbit - i)
		}
	}
	return result
}

// DecodeModesMessage decodes a raw Mode S message into a Message structure.
// Returns:
//   0: success
//   -1: message rejected (bad CRC, unknown aircraft)
//   -2: message undecodable (bad format, unsupported DF)
func DecodeModesMessage(msg []byte) (*Message, int) {
	mm := &Message{}

	// Copy message
	copy(mm.Msg[:], msg)
	copy(mm.Verbatim[:], msg) // Keep original for forwarding

	// Reject all-zeros messages
	allZero := true
	for i := 0; i < 7; i++ {
		if msg[i] != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return nil, -2
	}

	// Get message type (Downlink Format, bits 1-5)
	mm.MsgType = int(getbits(msg, 1, 5))
	mm.MsgBits = ModesMessageLenByType(mm.MsgType)

	// Calculate CRC
	mm.CRC = ModesChecksum(msg, mm.MsgBits)
	mm.Corrected = 0

	// Handle CRC and address extraction based on DF type
	switch mm.MsgType {
	case 0, 4, 5, 16, 24, 25, 26, 27, 28, 29, 30, 31:
		// These use Address/Parity: CRC syndrome = sender's ICAO address
		// We can't verify CRC without knowing the address, so we only accept
		// messages from previously seen aircraft (requires ICAO filter, not implemented yet)
		// For now, accept all and use CRC as address
		mm.Source = SOURCE_MODE_S
		mm.Addr = mm.CRC
		mm.AddrType = ADDR_ADSB_ICAO

	case 11:
		// All-call reply
		// Uses Parity/Interrogator: lower 7 bits of CRC = IID
		mm.IID = mm.CRC & 0x7f

		// Try to fix errors in the upper bits
		if mm.CRC&0xffff80 != 0 {
			errInfo := ModesChecksumDiagnose(mm.CRC&0xffff80, mm.MsgBits)
			if errInfo == nil || errInfo.Errors > 1 {
				return nil, -2 // Can't fix or too many errors
			}

			mm.Corrected = errInfo.Errors
			ModesChecksumFix(mm.Msg[:], errInfo)

			// Verify corrected address looks reasonable (would need ICAO filter)
			addr := getbits(mm.Msg[:], 9, 32)
			mm.Addr = addr
		}
		mm.Source = SOURCE_MODE_S_CHECKED

	case 17, 18:
		// Extended Squitter / Non-transponder
		// CRC should be 0 for valid messages

		if mm.CRC != 0 {
			// Try to fix errors
			errInfo := ModesChecksumDiagnose(mm.CRC, mm.MsgBits)
			if errInfo == nil {
				return nil, -2 // Can't fix
			}

			addr1 := getbits(msg, 9, 32)
			mm.Corrected = errInfo.Errors
			ModesChecksumFix(mm.Msg[:], errInfo)
			addr2 := getbits(mm.Msg[:], 9, 32)

			// Conservative: only accept if address didn't change or matches known aircraft
			if addr1 != addr2 {
				// Would check ICAO filter here
				// For now, reject
				return nil, -1
			}
		}

		mm.Source = SOURCE_ADSB
		mm.AddrType = ADDR_ADSB_ICAO

	case 20, 21:
		// Comm-B altitude/identity reply
		// Complex: uses Address/Parity or Data Parity
		// For now, reject these
		return nil, -1

	default:
		// Unknown/unsupported DF type
		return nil, -2
	}

	// Decode common fields

	// AA (Address Announced) - bits 9-32 for DF11, DF17, DF18
	if mm.MsgType == 11 || mm.MsgType == 17 || mm.MsgType == 18 {
		mm.AA = getbits(mm.Msg[:], 9, 32)
		mm.Addr = mm.AA
	}

	// CA (Capability) - bits 6-8 for DF17/18
	if mm.MsgType == 17 || mm.MsgType == 18 {
		mm.CA = getbits(mm.Msg[:], 6, 8)
	}

	// Decode Extended Squitter (DF17/18) message content
	if mm.MsgType == 17 || mm.MsgType == 18 {
		decodeExtendedSquitter(mm)
	}

	return mm, 0
}

// decodeExtendedSquitter decodes the content of a DF17/18 Extended Squitter message.
func decodeExtendedSquitter(mm *Message) {
	// Extract ME field (bits 33-88, 7 bytes)
	for i := 0; i < 7; i++ {
		mm.ME[i] = mm.Msg[4+i]
	}

	// Get ME type and subtype
	mm.METype = getbits(mm.Msg[:], 33, 37) // bits 33-37
	mm.MESub = getbits(mm.Msg[:], 38, 40)  // bits 38-40

	// Decode based on ME type
	switch mm.METype {
	case 1, 2, 3, 4:
		// Aircraft identification (callsign)
		decodeAircraftIdentification(mm)

	case 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 20, 21, 22:
		// Airborne position
		decodeAirbornePosition(mm)

	case 5, 6, 7, 8:
		// Surface position
		decodeSurfacePosition(mm)

	case 19:
		// Airborne velocity
		decodeAirborneVelocity(mm)

	case 28:
		// Aircraft status
		// TODO: implement

	case 29:
		// Target state and status
		// TODO: implement

	case 31:
		// Operational status
		// TODO: implement

	default:
		// Unknown or reserved ME type
	}
}

// decodeAircraftIdentification decodes ME types 1-4 (aircraft callsign).
func decodeAircraftIdentification(mm *Message) {
	// Callsign is encoded in bits 41-88 (6 bits per character, 8 characters)
	chars := "@ABCDEFGHIJKLMNOPQRSTUVWXYZ????? ???????????????0123456789??????"

	var callsign [9]byte
	for i := 0; i < 8; i++ {
		charBits := getbits(mm.Msg[:], 41+i*6, 46+i*6)
		if charBits < uint32(len(chars)) {
			callsign[i] = chars[charBits]
		} else {
			callsign[i] = '?'
		}
	}
	callsign[8] = 0 // Null terminator

	// Trim trailing spaces
	for i := 7; i >= 0; i-- {
		if callsign[i] == ' ' {
			callsign[i] = 0
		} else {
			break
		}
	}

	mm.Callsign = callsign
	mm.CallsignValid = true
}

// decodeAirbornePosition decodes ME types 9-18, 20-22 (airborne position).
func decodeAirbornePosition(mm *Message) {
	// Bit 54 = CPR format (0 = even, 1 = odd)
	mm.CPROdd = (getbits(mm.Msg[:], 54, 54) == 1)
	mm.CPRValid = true
	mm.CPRType = CPR_AIRBORNE

	// Extract altitude (bits 41-52, 12 bits)
	ac12 := int(getbits(mm.Msg[:], 41, 52))
	altitude := decodeAC12Field(ac12)
	if altitude != INVALID_ALTITUDE {
		mm.Altitude = altitude
		mm.AltitudeUnit = UNIT_FEET
		mm.AltitudeSource = ALTITUDE_BARO
		mm.AltitudeValid = true
	}

	// Extract CPR latitude and longitude (bits 55-71 and 72-88)
	mm.CPRLat = getbits(mm.Msg[:], 55, 71) // 17 bits
	mm.CPRLon = getbits(mm.Msg[:], 72, 88) // 17 bits
}

// decodeSurfacePosition decodes ME types 5-8 (surface position).
func decodeSurfacePosition(mm *Message) {
	// Similar to airborne but different encoding
	mm.CPROdd = (getbits(mm.Msg[:], 54, 54) == 1)
	mm.CPRValid = true
	mm.CPRType = CPR_SURFACE

	mm.CPRLat = getbits(mm.Msg[:], 55, 71)
	mm.CPRLon = getbits(mm.Msg[:], 72, 88)

	mm.AirGround = AG_GROUND
}

// decodeAirborneVelocity decodes ME type 19 (airborne velocity).
func decodeAirborneVelocity(mm *Message) {
	subtype := mm.MESub

	if subtype == 1 || subtype == 2 {
		// Ground speed
		ewDir := getbits(mm.Msg[:], 46, 46)  // 0 = east, 1 = west
		ewVel := int(getbits(mm.Msg[:], 47, 56)) - 1
		nsDir := getbits(mm.Msg[:], 57, 57)  // 0 = north, 1 = south
		nsVel := int(getbits(mm.Msg[:], 58, 67)) - 1

		if ewVel >= 0 && nsVel >= 0 {
			if ewDir == 1 {
				ewVel = -ewVel
			}
			if nsDir == 1 {
				nsVel = -nsVel
			}

			mm.EWVelocityValid = true
			mm.NSVelocityValid = true

			// TODO: Calculate speed and heading from EW/NS components
		}
	}

	// Vertical rate (bits 37-45)
	vrSign := getbits(mm.Msg[:], 69, 69)
	vrRaw := int(getbits(mm.Msg[:], 70, 78))
	if vrRaw != 0 {
		vr := (vrRaw - 1) * 64
		if vrSign == 1 {
			vr = -vr
		}
		mm.VertRate = vr
		mm.VertRateValid = true
		mm.VertRateSource = ALTITUDE_BARO // or GNSS, depending on bit 36
	}
}

// decodeAC12Field decodes a 12-bit altitude field (DF17/18).
// Returns altitude in feet, or INVALID_ALTITUDE.
// The 12 bits are: D2 D4 A1 A2 A4 B1 Q B2 B4 C1 C2 C4
// Q bit (bit 4 of the 12-bit field, bit 48 of message) determines encoding
func decodeAC12Field(ac12 int) int {
	qBit := ac12 & 0x10 // Q is bit 4 of the 12-bit field

	if qBit != 0 {
		// Q=1: 25-foot resolution
		// Remove Q bit and form 11-bit integer N
		// Bits above Q (bits 11-5) shift down, bits below Q (bits 3-0) stay
		n := ((ac12 & 0x0FE0) >> 1) | (ac12 & 0x000F)
		if n == 0 {
			return INVALID_ALTITUDE
		}
		return n*25 - 1000
	}

	// Q=0: Gillham code (100-foot resolution)
	// Make it a 13-bit field by inserting M=0 at bit 6
	_ = ((ac12 & 0x0FC0) << 1) | (ac12 & 0x003F)

	// TODO: Implement Gillham code decoding (requires mode_ac.c translation)
	// For now, return invalid
	return INVALID_ALTITUDE
}
