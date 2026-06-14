// Package modes implements Mode S message decoding.
//
// This is a direct translation from dump1090-mutability's mode_s.c,
// maintaining bit-exact compatibility with the C implementation.
//
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// and Copyright (C) 2012 Salvatore Sanfilippo <antirez@gmail.com>
// Licensed under GPL v2+ / BSD
package modes

import (
	"math"
)

// AIS character set for callsign decoding
const aisCharset = "@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_ !\"#$%&'()*+,-./0123456789:;<=>?"

// Angle type constants
const (
	ANGLE_HEADING = 0
	ANGLE_TRACK   = 1
)

// TSS altitude type constants
const (
	TSS_ALTITUDE_MCP = 0
	TSS_ALTITUDE_FMS = 1
)

// ModesMessageLenByType returns the message length in bits based on the Downlink Format.
// DF 16 or greater = 112 bits (long), DF 15 or less = 56 bits (short).
func ModesMessageLenByType(dfType int) int {
	if dfType&0x10 != 0 {
		return MODES_LONG_MSG_BITS
	}
	return MODES_SHORT_MSG_BITS
}

// getbit extracts a single bit from a message (1-indexed).
func getbit(msg []byte, bitnum int) uint32 {
	bi := bitnum - 1
	by := bi >> 3
	mask := byte(1 << (7 - (bi & 7)))
	if msg[by]&mask != 0 {
		return 1
	}
	return 0
}

// getbits extracts a field from a message.
// Bits are numbered from 1 (MSB of first byte) to N (LSB of last byte).
func getbits(msg []byte, firstbit, lastbit int) uint32 {
	fbi := firstbit - 1
	lbi := lastbit - 1

	fby := fbi >> 3
	lby := lbi >> 3
	nby := (lby - fby) + 1

	shift := 7 - (lbi & 7)
	topmask := byte(0xFF >> (fbi & 7))

	switch nby {
	case 5:
		return (uint32(msg[fby]&topmask) << (32 - shift)) |
			(uint32(msg[fby+1]) << (24 - shift)) |
			(uint32(msg[fby+2]) << (16 - shift)) |
			(uint32(msg[fby+3]) << (8 - shift)) |
			(uint32(msg[fby+4]) >> shift)
	case 4:
		return (uint32(msg[fby]&topmask) << (24 - shift)) |
			(uint32(msg[fby+1]) << (16 - shift)) |
			(uint32(msg[fby+2]) << (8 - shift)) |
			(uint32(msg[fby+3]) >> shift)
	case 3:
		return (uint32(msg[fby]&topmask) << (16 - shift)) |
			(uint32(msg[fby+1]) << (8 - shift)) |
			(uint32(msg[fby+2]) >> shift)
	case 2:
		return (uint32(msg[fby]&topmask) << (8 - shift)) |
			(uint32(msg[fby+1]) >> shift)
	case 1:
		return uint32(msg[fby]&topmask) >> shift
	default:
		// Slow path for edge cases
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
}

// decodeAC12Field decodes the 12-bit AC altitude field (in DF 17 and others).
func decodeAC12Field(ac12Field int) (altitude int, unit AltitudeUnit) {
	qBit := ac12Field & 0x10 // Bit 48 = Q

	unit = UNIT_FEET
	if qBit != 0 {
		// N is the 11 bit integer resulting from the removal of bit Q at bit 4
		n := ((ac12Field & 0x0FE0) >> 1) | (ac12Field & 0x000F)
		if n == 0 {
			return INVALID_ALTITUDE, unit
		}
		// The final altitude is the resulting number multiplied by 25, minus 1000
		return n*25 - 1000, unit
	}

	// Make N a 13 bit Gillham coded altitude by inserting M=0 at bit 6
	n := ((ac12Field & 0x0FC0) << 1) | (ac12Field & 0x003F)
	modeC := ModeAToModeC(uint32(DecodeID13Field(n)))
	if modeC < -12 {
		return INVALID_ALTITUDE, unit
	}
	return 100 * modeC, unit
}

// setIMF handles setting a non-ICAO address based on IMF bit
func setIMF(mm *Message) {
	mm.Addr |= MODES_NON_ICAO_ADDRESS
	switch mm.AddrType {
	case ADDR_ADSB_ICAO, ADDR_ADSB_ICAO_NT:
		mm.AddrType = ADDR_ADSB_OTHER
	case ADDR_TISB_ICAO:
		mm.AddrType = ADDR_TISB_TRACKFILE
	case ADDR_ADSR_ICAO:
		mm.AddrType = ADDR_ADSR_OTHER
	}
}

// DecodeModesMessage decodes a raw Mode S message into a Message structure.
// Returns:
//
//	0: success
//	-1: message might be valid, but we couldn't validate CRC against known ICAO
//	-2: bad message or unrepairable CRC error
func DecodeModesMessage(msg []byte) (*Message, int) {
	return DecodeModesMessageWithConfig(msg, DecodeConfig{})
}

// DecodeModesMessageWithFilter decodes with optional ICAO filter for validation.
func DecodeModesMessageWithFilter(msg []byte, filter *ICAOFilter) (*Message, int) {
	mm := &Message{}

	// Copy message
	copy(mm.Msg[:], msg)
	copy(mm.Verbatim[:], msg)

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
	mm.CRC = ModesChecksum(msg, mm.MsgBits)
	mm.Corrected = 0

	// Handle CRC and address extraction based on DF type
	switch mm.MsgType {
	case 0, 4, 5, 16, 24, 25, 26, 27, 28, 29, 30, 31:
		// Address/Parity: CRC syndrome = sender's ICAO address
		if filter != nil && !filter.Test(mm.CRC) {
			return nil, -1
		}
		mm.Source = SOURCE_MODE_S
		mm.Addr = mm.CRC

	case 11: // All-call reply
		mm.IID = mm.CRC & 0x7f
		if mm.CRC&0xffff80 != 0 {
			ei := ModesChecksumDiagnose(mm.CRC&0xffff80, mm.MsgBits)
			if ei == nil {
				return nil, -2
			}
			// Don't attempt to fix more than single-bit errors for DF11
			if ei.Errors > 1 {
				return nil, -2
			}
			mm.Corrected = ei.Errors
			ModesChecksumFix(mm.Msg[:], ei)

			// Check corrected address against filter
			addr := getbits(mm.Msg[:], 9, 32)
			if filter != nil && !filter.Test(addr) {
				return nil, -1
			}
		}
		mm.Source = SOURCE_MODE_S_CHECKED

	case 17, 18: // Extended Squitter
		if mm.CRC != 0 {
			ei := ModesChecksumDiagnose(mm.CRC, mm.MsgBits)
			if ei == nil {
				return nil, -2
			}

			addr1 := getbits(msg, 9, 32)
			mm.Corrected = ei.Errors
			ModesChecksumFix(mm.Msg[:], ei)
			addr2 := getbits(mm.Msg[:], 9, 32)

			// Only accept if address didn't change or matches known aircraft
			if addr1 != addr2 {
				if filter != nil && !filter.Test(addr2) {
					return nil, -1
				}
			}
		}
		mm.Source = SOURCE_ADSB // TIS-B decoding will override if needed

	case 20, 21: // Comm-B
		// Try exact match
		if filter != nil && filter.Test(mm.CRC) {
			mm.Source = SOURCE_MODE_S
			mm.Addr = mm.CRC
		} else {
			return nil, -1
		}

	default:
		return nil, -2
	}

	// Decode common fields
	msg = mm.Msg[:]

	// AA (Address announced) - bits 9-32 for DF11, DF17, DF18
	if mm.MsgType == 11 || mm.MsgType == 17 || mm.MsgType == 18 {
		mm.AA = getbits(msg, 9, 32)
		mm.Addr = mm.AA
	}

	// AC (Altitude Code) - bits 20-32 for DF0,4,16,20
	if mm.MsgType == 0 || mm.MsgType == 4 || mm.MsgType == 16 || mm.MsgType == 20 {
		mm.AC = getbits(msg, 20, 32)
		if mm.AC != 0 {
			mm.Altitude, mm.AltitudeUnit = DecodeAC13Field(int(mm.AC))
			if mm.Altitude != INVALID_ALTITUDE {
				mm.AltitudeValid = true
			}
			mm.AltitudeSource = ALTITUDE_BARO
		}
	}

	// CA (Capability) - bits 6-8 for DF11,17
	if mm.MsgType == 11 || mm.MsgType == 17 {
		mm.CA = getbits(msg, 6, 8)
		switch mm.CA {
		case 0:
			mm.AirGround = AG_UNCERTAIN
		case 4:
			mm.AirGround = AG_GROUND
		case 5:
			mm.AirGround = AG_AIRBORNE
		case 6, 7:
			mm.AirGround = AG_UNCERTAIN
		}
	}

	// CC (Cross-link capability) - bit 7 for DF0
	if mm.MsgType == 0 {
		mm.CC = getbit(msg, 7)
	}

	// CF (Control field) - bits 5-8 for DF18
	if mm.MsgType == 18 {
		mm.CF = getbits(msg, 6, 8)
	}

	// DR (Downlink Request) - bits 9-13 for DF4,5,20,21
	if mm.MsgType == 4 || mm.MsgType == 5 || mm.MsgType == 20 || mm.MsgType == 21 {
		mm.DR = getbits(msg, 9, 13)
	}

	// FS (Flight Status) - bits 6-8 for DF4,5,20,21
	if mm.MsgType == 4 || mm.MsgType == 5 || mm.MsgType == 20 || mm.MsgType == 21 {
		mm.FS = getbits(msg, 6, 8)
		mm.AlertValid = true
		mm.SPIValid = true

		switch mm.FS {
		case 0:
			mm.AirGround = AG_UNCERTAIN
		case 1:
			mm.AirGround = AG_GROUND
		case 2:
			mm.AirGround = AG_UNCERTAIN
			mm.Alert = true
		case 3:
			mm.AirGround = AG_GROUND
			mm.Alert = true
		case 4:
			mm.AirGround = AG_UNCERTAIN
			mm.Alert = true
			mm.SPI = true
		case 5:
			mm.AirGround = AG_UNCERTAIN
			mm.SPI = true
		default:
			mm.SPIValid = false
			mm.AlertValid = false
		}
	}

	// ID (Identity) - bits 20-32 for DF5,21
	if mm.MsgType == 5 || mm.MsgType == 21 {
		mm.ID = getbits(msg, 20, 32)
		if mm.ID != 0 {
			mm.Squawk = uint32(DecodeID13Field(int(mm.ID)))
			mm.SquawkValid = true
		}
	}

	// KE (Control, ELM) - bit 4 for DF24-31
	if mm.MsgType >= 24 && mm.MsgType <= 31 {
		mm.KE = getbit(msg, 4)
	}

	// MB (message, Comm-B) - bytes 4-10 for DF20,21
	if mm.MsgType == 20 || mm.MsgType == 21 {
		copy(mm.MB[:], msg[4:11])
		decodeCommB(mm)
	}

	// MD (message, Comm-D) - bytes 1-10 for DF24-31
	if mm.MsgType >= 24 && mm.MsgType <= 31 {
		copy(mm.MD[:], msg[1:11])
	}

	// ME (message, extended squitter) - bytes 4-10 for DF17,18
	if mm.MsgType == 17 || mm.MsgType == 18 {
		copy(mm.ME[:], msg[4:11])
		decodeExtendedSquitter(mm)
	}

	// MV (message, ACAS) - bytes 4-10 for DF16
	if mm.MsgType == 16 {
		copy(mm.MV[:], msg[4:11])
	}

	// ND (number of D-segment) - bits 5-8 for DF24-31
	if mm.MsgType >= 24 && mm.MsgType <= 31 {
		mm.ND = getbits(msg, 5, 8)
	}

	// RI (Reply information, ACAS) - bits 14-17 for DF0,16
	if mm.MsgType == 0 || mm.MsgType == 16 {
		mm.RI = getbits(msg, 14, 17)
	}

	// SL (Sensitivity level, ACAS) - bits 9-11 for DF0,16
	if mm.MsgType == 0 || mm.MsgType == 16 {
		mm.SL = getbits(msg, 9, 11)
	}

	// UM (Utility Message) - bits 14-19 for DF4,5,20,21
	if mm.MsgType == 4 || mm.MsgType == 5 || mm.MsgType == 20 || mm.MsgType == 21 {
		mm.UM = getbits(msg, 14, 19)
	}

	// VS (Vertical Status) - bit 6 for DF0,16
	if mm.MsgType == 0 || mm.MsgType == 16 {
		mm.VS = getbit(msg, 6)
		if mm.VS != 0 {
			mm.AirGround = AG_GROUND
		} else {
			mm.AirGround = AG_UNCERTAIN
		}
	}

	return mm, 0
}

// decodeCommB decodes Comm-B message content
func decodeCommB(mm *Message) {
	// Try to decode BDS 2,0 (Aircraft Identification)
	// This is speculative since we don't know the BDS code for sure
	decodeBDS20(mm)
}

// decodeBDS20 decodes BDS 2,0 (Aircraft Identification)
func decodeBDS20(mm *Message) {
	msg := mm.Msg[:]

	for i := 0; i < 8; i++ {
		charBits := getbits(msg, 41+i*6, 46+i*6)
		if int(charBits) < len(aisCharset) {
			mm.Callsign[i] = aisCharset[charBits]
		} else {
			mm.Callsign[i] = '?'
		}
	}
	mm.Callsign[8] = 0

	// Validate: accept only alphanumeric data
	mm.CallsignValid = true
	for i := 0; i < 8; i++ {
		c := mm.Callsign[i]
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == ' ') {
			mm.CallsignValid = false
			break
		}
	}
}

// decodeExtendedSquitter decodes the content of a DF17/18 Extended Squitter message.
func decodeExtendedSquitter(mm *Message) {
	me := mm.ME[:]
	metype := getbits(me, 1, 5)
	mm.METype = metype
	checkIMF := false

	// Handle DF18 CF field
	if mm.MsgType == 18 {
		switch mm.CF {
		case 0: // ADS-B from non-transponder
			mm.AddrType = ADDR_ADSB_ICAO_NT
		case 1: // Reserved
			mm.AddrType = ADDR_ADSB_OTHER
			mm.Addr |= MODES_NON_ICAO_ADDRESS
		case 2: // Fine TIS-B
			mm.Source = SOURCE_TISB
			mm.AddrType = ADDR_TISB_ICAO
			checkIMF = true
		case 3: // Coarse TIS-B
			mm.Source = SOURCE_TISB
			mm.AddrType = ADDR_TISB_ICAO
			if getbit(me, 1) != 0 {
				setIMF(mm)
			}
			return
		case 5: // Fine TIS-B, non-ICAO
			mm.AddrType = ADDR_TISB_OTHER
			mm.Addr |= MODES_NON_ICAO_ADDRESS
			checkIMF = true
		case 6: // ADS-R
			mm.AddrType = ADDR_ADSR_ICAO
			checkIMF = true
		default:
			mm.AddrType = ADDR_UNKNOWN
		}
	}

	switch metype {
	case 1, 2, 3, 4:
		decodeESIdentAndCategory(mm)
	case 5, 6, 7, 8:
		decodeESSurfacePosition(mm, checkIMF)
	case 9, 10, 11, 12, 13, 14, 15, 16, 17, 18:
		decodeESAirbornePosition(mm, checkIMF)
	case 19:
		decodeESAirborneVelocity(mm, checkIMF)
	case 20, 21, 22:
		decodeESAirbornePosition(mm, checkIMF)
	case 23:
		decodeESTestMessage(mm)
	case 28:
		decodeESAircraftStatus(mm, checkIMF)
	case 29:
		decodeESTargetStatus(mm, checkIMF)
	case 31:
		decodeESOperationalStatus(mm, checkIMF)
	}
}

// decodeESIdentAndCategory decodes ME types 1-4 (aircraft identification and category)
func decodeESIdentAndCategory(mm *Message) {
	me := mm.ME[:]

	mm.MESub = getbits(me, 6, 8)

	for i := 0; i < 8; i++ {
		charBits := getbits(me, 9+i*6, 14+i*6)
		if int(charBits) < len(aisCharset) {
			mm.Callsign[i] = aisCharset[charBits]
		} else {
			mm.Callsign[i] = '?'
		}
	}
	mm.Callsign[8] = 0

	// Check for all-@ which is a common failure mode
	allAt := true
	for i := 0; i < 8; i++ {
		if mm.Callsign[i] != '@' {
			allAt = false
			break
		}
	}
	mm.CallsignValid = !allAt

	mm.Category = ((0x0E - mm.METype) << 4) | mm.MESub
	mm.CategoryValid = true
}

// decodeESSurfacePosition decodes ME types 5-8 (surface position)
func decodeESSurfacePosition(mm *Message, checkIMF bool) {
	me := mm.ME[:]

	if checkIMF && getbit(me, 21) != 0 {
		setIMF(mm)
	}

	mm.AirGround = AG_GROUND
	mm.CPRLat = getbits(me, 23, 39)
	mm.CPRLon = getbits(me, 40, 56)
	mm.CPROdd = getbit(me, 22) == 1
	mm.CPRNUCP = 14 - mm.METype
	mm.CPRValid = true
	mm.CPRType = CPR_SURFACE

	movement := getbits(me, 6, 12)
	if movement > 0 && movement < 125 {
		mm.SpeedValid = true
		mm.Speed = decodeMovementField(movement)
		mm.SpeedSource = SPEED_GROUNDSPEED
	}

	if getbit(me, 13) != 0 {
		mm.HeadingValid = true
		mm.HeadingSource = HEADING_TRUE
		mm.Heading = getbits(me, 14, 20) * 360 / 128
	}
}

// decodeESAirbornePosition decodes ME types 9-18, 20-22 (airborne position)
func decodeESAirbornePosition(mm *Message, checkIMF bool) {
	me := mm.ME[:]

	if checkIMF && getbit(me, 8) != 0 {
		setIMF(mm)
	}

	ac12Field := int(getbits(me, 9, 20))

	if mm.METype == 0 {
		mm.CPRNUCP = 0
	} else {
		mm.CPRLat = getbits(me, 23, 39)
		mm.CPRLon = getbits(me, 40, 56)

		// Filter out common invalid patterns
		if !(ac12Field == 0 && mm.CPRLon == 0 && (mm.CPRLat&0x0fff) == 0 && mm.METype == 15) {
			mm.CPRValid = true
			mm.CPRType = CPR_AIRBORNE
			mm.CPROdd = getbit(me, 22) == 1

			if mm.METype == 18 || mm.METype == 22 {
				mm.CPRNUCP = 0
			} else if mm.METype < 18 {
				mm.CPRNUCP = 18 - mm.METype
			} else {
				mm.CPRNUCP = 29 - mm.METype
			}
		}
	}

	if ac12Field != 0 {
		mm.Altitude, mm.AltitudeUnit = decodeAC12Field(ac12Field)
		if mm.Altitude != INVALID_ALTITUDE {
			mm.AltitudeValid = true
		}
		if mm.METype == 20 || mm.METype == 21 || mm.METype == 22 {
			mm.AltitudeSource = ALTITUDE_GNSS
		} else {
			mm.AltitudeSource = ALTITUDE_BARO
		}
	}
}

// decodeESAirborneVelocity decodes ME type 19 (airborne velocity)
func decodeESAirborneVelocity(mm *Message, checkIMF bool) {
	me := mm.ME[:]

	mm.MESub = getbits(me, 6, 8)

	if checkIMF && getbit(me, 9) != 0 {
		setIMF(mm)
	}

	if mm.MESub < 1 || mm.MESub > 4 {
		return
	}

	// Vertical rate
	vertRate := getbits(me, 38, 46)
	if vertRate != 0 {
		mm.VertRate = int(vertRate-1) * 64
		if getbit(me, 37) != 0 {
			mm.VertRate = -mm.VertRate
		}
		mm.VertRateValid = true
	}

	if getbit(me, 36) != 0 {
		mm.VertRateSource = ALTITUDE_GNSS
	} else {
		mm.VertRateSource = ALTITUDE_BARO
	}

	switch mm.MESub {
	case 1, 2:
		ewRaw := getbits(me, 15, 24)
		nsRaw := getbits(me, 26, 35)

		if ewRaw != 0 && nsRaw != 0 {
			multiplier := 1
			if mm.MESub == 2 {
				multiplier = 4
			}

			ewVel := int(ewRaw-1) * multiplier
			if getbit(me, 14) != 0 {
				ewVel = -ewVel
			}
			nsVel := int(nsRaw-1) * multiplier
			if getbit(me, 25) != 0 {
				nsVel = -nsVel
			}

			mm.Speed = uint32(math.Sqrt(float64(nsVel*nsVel+ewVel*ewVel)) + 0.5)
			mm.SpeedValid = true

			if mm.Speed > 0 {
				heading := int(math.Atan2(float64(ewVel), float64(nsVel))*180.0/math.Pi + 0.5)
				if heading < 0 {
					heading += 360
				}
				mm.Heading = uint32(heading)
				mm.HeadingSource = HEADING_TRUE
				mm.HeadingValid = true
			}

			mm.SpeedSource = SPEED_GROUNDSPEED
		}

	case 3, 4:
		airspeed := getbits(me, 26, 35)
		if airspeed != 0 {
			multiplier := uint32(1)
			if mm.MESub == 4 {
				multiplier = 4
			}
			mm.Speed = (airspeed - 1) * multiplier
			if getbit(me, 25) != 0 {
				mm.SpeedSource = SPEED_TAS
			} else {
				mm.SpeedSource = SPEED_IAS
			}
			mm.SpeedValid = true
		}

		if getbit(me, 14) != 0 {
			mm.Heading = getbits(me, 15, 24)
			mm.HeadingSource = HEADING_MAGNETIC
			mm.HeadingValid = true
		}
	}

	// GNSS delta
	rawDelta := getbits(me, 50, 56)
	if rawDelta != 0 {
		mm.GNSSDeltaValid = true
		mm.GNSSDelta = int(rawDelta-1) * 25
		if getbit(me, 49) != 0 {
			mm.GNSSDelta = -mm.GNSSDelta
		}
	}
}

// decodeESTestMessage decodes ME type 23 (test message)
func decodeESTestMessage(mm *Message) {
	me := mm.ME[:]

	mm.MESub = getbits(me, 6, 8)

	if mm.MESub == 7 { // Squawk in test message (1090-WP-15-20)
		id13Field := int(getbits(me, 9, 21))
		if id13Field != 0 {
			mm.SquawkValid = true
			mm.Squawk = uint32(DecodeID13Field(id13Field))
		}
	}
}

// decodeESAircraftStatus decodes ME type 28 (aircraft status)
func decodeESAircraftStatus(mm *Message, checkIMF bool) {
	me := mm.ME[:]

	mm.MESub = getbits(me, 6, 8)

	if mm.MESub == 1 { // Emergency status squawk field
		id13Field := int(getbits(me, 12, 24))
		if id13Field != 0 {
			mm.SquawkValid = true
			mm.Squawk = uint32(DecodeID13Field(id13Field))
		}

		if checkIMF && getbit(me, 56) != 0 {
			setIMF(mm)
		}
	}
}

// decodeESTargetStatus decodes ME type 29 (target state and status)
func decodeESTargetStatus(mm *Message, checkIMF bool) {
	me := mm.ME[:]

	mm.MESub = getbits(me, 6, 7) // Only 2 bits of subtype

	if checkIMF && getbit(me, 51) != 0 {
		setIMF(mm)
	}

	if mm.MESub == 1 { // Target state and status, V2
		mm.TSS.Valid = true
		if getbit(me, 8) != 0 {
			mm.TSS.SILType = SIL_PER_SAMPLE
		} else {
			mm.TSS.SILType = SIL_PER_HOUR
		}
		mm.TSS.AltitudeType = getbits(me, 9, 9)

		altBits := getbits(me, 10, 20)
		if altBits == 0 {
			mm.TSS.AltitudeValid = false
		} else {
			mm.TSS.AltitudeValid = true
			mm.TSS.Altitude = (altBits - 1) * 32
		}

		baroBits := getbits(me, 21, 29)
		if baroBits == 0 {
			mm.TSS.BaroValid = false
		} else {
			mm.TSS.BaroValid = true
			mm.TSS.Baro = 800.0 + float32(baroBits-1)*0.8
		}

		mm.TSS.HeadingValid = getbit(me, 30) == 1
		if mm.TSS.HeadingValid {
			mm.TSS.Heading = getbits(me, 31, 39) * 180 / 256
		}

		mm.TSS.NACp = getbits(me, 40, 43)
		mm.TSS.NICBaro = getbit(me, 44) == 1
		mm.TSS.SIL = getbits(me, 45, 46)
		mm.TSS.ModeValid = getbit(me, 47) == 1
		if mm.TSS.ModeValid {
			mm.TSS.ModeAutopilot = getbit(me, 48) == 1
			mm.TSS.ModeVNAV = getbit(me, 49) == 1
			mm.TSS.ModeAltHold = getbit(me, 50) == 1
			mm.TSS.ModeApproach = getbit(me, 52) == 1
		}

		mm.TSS.ACASOperational = getbit(me, 53) == 1
	}
}

// decodeESOperationalStatus decodes ME type 31 (operational status)
func decodeESOperationalStatus(mm *Message, checkIMF bool) {
	me := mm.ME[:]

	mm.MESub = getbits(me, 6, 8)

	if checkIMF && getbit(me, 56) != 0 {
		setIMF(mm)
	}

	if mm.MESub == 0 || mm.MESub == 1 {
		mm.OpStatus.Valid = true
		mm.OpStatus.Version = getbits(me, 41, 43)

		switch mm.OpStatus.Version {
		case 0:
			// DO-260 (v0)

		case 1:
			// DO-260A (v1)
			if getbits(me, 25, 26) == 0 {
				mm.OpStatus.OMACASRa = getbit(me, 27) == 1
				mm.OpStatus.OMIdent = getbit(me, 28) == 1
				mm.OpStatus.OMATC = getbit(me, 29) == 1
			}

			if mm.MESub == 0 && getbits(me, 9, 10) == 0 && getbits(me, 13, 14) == 0 {
				// Airborne
				mm.OpStatus.CCACAS = getbit(me, 11) == 0
				mm.OpStatus.CCCDTI = getbit(me, 12) == 1
				mm.OpStatus.CCARV = getbit(me, 15) == 1
				mm.OpStatus.CCTS = getbit(me, 16) == 1
				mm.OpStatus.CCTC = getbits(me, 17, 18)
			} else if mm.MESub == 1 && getbits(me, 9, 10) == 0 && getbits(me, 13, 14) == 0 {
				// Surface
				mm.OpStatus.CCPOA = getbit(me, 11) == 1
				mm.OpStatus.CCCDTI = getbit(me, 12) == 1
				mm.OpStatus.CCB2Low = getbit(me, 15) == 1
				mm.OpStatus.CCLWValid = true
				mm.OpStatus.CCLW = getbits(me, 21, 24)
			}

			mm.OpStatus.NICSupp = getbits(me, 44, 44)
			mm.OpStatus.NACp = getbits(me, 45, 48)
			mm.OpStatus.SIL = getbits(me, 51, 52)
			if mm.MESub == 0 {
				mm.OpStatus.NICBaro = getbit(me, 53) == 1
			} else {
				if getbit(me, 53) != 0 {
					mm.OpStatus.TrackAngle = ANGLE_TRACK
				} else {
					mm.OpStatus.TrackAngle = ANGLE_HEADING
				}
			}
			if getbit(me, 54) != 0 {
				mm.OpStatus.HRD = HEADING_MAGNETIC
			} else {
				mm.OpStatus.HRD = HEADING_TRUE
			}

		default: // Version 2+
			if getbits(me, 25, 26) == 0 {
				mm.OpStatus.OMACASRa = getbit(me, 27) == 1
				mm.OpStatus.OMIdent = getbit(me, 28) == 1
				mm.OpStatus.OMATC = getbit(me, 29) == 1
				mm.OpStatus.OMSAF = getbit(me, 30) == 1
				mm.OpStatus.OMSDA = getbits(me, 31, 32)
			}

			if mm.MESub == 0 && getbits(me, 9, 10) == 0 && getbits(me, 13, 14) == 0 {
				// Airborne
				mm.OpStatus.CCACAS = getbit(me, 11) == 1
				mm.OpStatus.CC1090In = getbit(me, 12) == 1
				mm.OpStatus.CCARV = getbit(me, 15) == 1
				mm.OpStatus.CCTS = getbit(me, 16) == 1
				mm.OpStatus.CCTC = getbits(me, 17, 18)
				mm.OpStatus.CCUATIn = getbit(me, 19) == 1
			} else if mm.MESub == 1 && getbits(me, 9, 10) == 0 && getbits(me, 13, 14) == 0 {
				// Surface
				mm.OpStatus.CCPOA = getbit(me, 11) == 1
				mm.OpStatus.CC1090In = getbit(me, 12) == 1
				mm.OpStatus.CCB2Low = getbit(me, 15) == 1
				mm.OpStatus.CCUATIn = getbit(me, 16) == 1
				mm.OpStatus.CCNACv = getbits(me, 17, 19)
				mm.OpStatus.CCNICSupp = getbit(me, 20) == 1
				mm.OpStatus.CCLWValid = true
				mm.OpStatus.CCLW = getbits(me, 21, 24)
				mm.OpStatus.CCAntennaOff = getbits(me, 33, 40)
			}

			mm.OpStatus.NICSupp = getbits(me, 44, 44)
			mm.OpStatus.NACp = getbits(me, 45, 48)
			mm.OpStatus.SIL = getbits(me, 51, 52)
			if mm.MESub == 0 {
				mm.OpStatus.GVA = getbits(me, 49, 50)
				mm.OpStatus.NICBaro = getbit(me, 53) == 1
			} else {
				if getbit(me, 53) != 0 {
					mm.OpStatus.TrackAngle = ANGLE_TRACK
				} else {
					mm.OpStatus.TrackAngle = ANGLE_HEADING
				}
			}
			if getbit(me, 54) != 0 {
				mm.OpStatus.HRD = HEADING_MAGNETIC
			} else {
				mm.OpStatus.HRD = HEADING_TRUE
			}
			if getbit(me, 55) != 0 {
				mm.OpStatus.SILType = SIL_PER_SAMPLE
			} else {
				mm.OpStatus.SILType = SIL_PER_HOUR
			}
		}
	}
}
