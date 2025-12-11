// Package modes implements string conversion utilities for enumeration types.
//
// strings.go: enum to string conversions for debugging
//
// This is a translation from dump1090-mutability's mode_s.c helper functions.
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// Licensed under GPL v2+
package modes

import "fmt"

// DFToString returns the name of a downlink format
// Matching C version mode_s.c df_to_string
func DFToString(df int) string {
	switch df {
	case 0:
		return "Short Air-Air Surveillance"
	case 4:
		return "Surveillance, Altitude Reply"
	case 5:
		return "Surveillance, Identity Reply"
	case 11:
		return "All Call Reply"
	case 16:
		return "Long Air-Air Surveillance"
	case 17:
		return "Extended Squitter"
	case 18:
		return "Extended Squitter/Supplementary"
	case 19:
		return "Military Extended Squitter"
	case 20:
		return "Comm-B, Altitude Reply"
	case 21:
		return "Comm-B, Identity Reply"
	case 22:
		return "Military Use"
	case 24:
		return "Comm-D Extended Length Message"
	default:
		return fmt.Sprintf("DF %d (unknown)", df)
	}
}

// ESTypeName returns the name of an extended squitter type
// Matching C version mode_s.c esTypeName
func ESTypeName(metype, mesub uint32) string {
	switch metype {
	case 0:
		return "No position information"
	case 1, 2, 3, 4:
		return "Aircraft identification and category"
	case 5, 6, 7, 8:
		return "Surface position"
	case 9, 10, 11, 12, 13, 14, 15, 16, 17, 18:
		return "Airborne position (barometric altitude)"
	case 19:
		return "Airborne velocity"
	case 20, 21, 22:
		return "Airborne position (GNSS HAE)"
	case 23:
		switch mesub {
		case 0:
			return "Test message"
		case 7:
			return "National use / 1090-WP-15-20"
		default:
			return fmt.Sprintf("Test/reserved (sub %d)", mesub)
		}
	case 24:
		return "Surface system status"
	case 25, 26, 27:
		return "Reserved for surface system status"
	case 28:
		switch mesub {
		case 1:
			return "Emergency/priority status"
		case 2:
			return "ACAS RA broadcast"
		default:
			return fmt.Sprintf("Aircraft status (sub %d)", mesub)
		}
	case 29:
		switch mesub {
		case 0, 1:
			return "Target state and status (V.1/2)"
		default:
			return fmt.Sprintf("Target state and status (sub %d)", mesub)
		}
	case 30:
		return "Aircraft operational coordination"
	case 31:
		switch mesub {
		case 0, 1:
			return "Aircraft operational status (V.1/2)"
		default:
			return fmt.Sprintf("Aircraft operational status (sub %d)", mesub)
		}
	default:
		return fmt.Sprintf("Unknown (type %d)", metype)
	}
}

// ESTypeHasSubtype returns true if the ES type has a subtype
// Matching C version mode_s.c esTypeHasSubtype
func ESTypeHasSubtype(metype uint32) bool {
	switch metype {
	case 23, 28, 29, 31:
		return true
	default:
		return false
	}
}

// AddrTypeToString converts address type to string
// Matching C version mode_s.c addrtype_to_string
func AddrTypeToString(t AddrType) string {
	switch t {
	case ADDR_ADSB_ICAO:
		return "Mode S / ADS-B"
	case ADDR_ADSB_ICAO_NT:
		return "ADS-B, non-transponder"
	case ADDR_ADSR_ICAO:
		return "ADS-R"
	case ADDR_TISB_ICAO:
		return "TIS-B"
	case ADDR_ADSB_OTHER:
		return "ADS-B, other addressing scheme"
	case ADDR_ADSR_OTHER:
		return "ADS-R, other addressing scheme"
	case ADDR_TISB_TRACKFILE:
		return "TIS-B, Mode A code + track file number"
	case ADDR_TISB_OTHER:
		return "TIS-B, other addressing scheme"
	default:
		return "unknown addressing scheme"
	}
}

// AirGroundToString converts air/ground state to string
// Matching C version mode_s.c airground_to_string
func AirGroundToString(ag AirGround) string {
	switch ag {
	case AG_GROUND:
		return "ground"
	case AG_AIRBORNE:
		return "airborne"
	case AG_UNCERTAIN:
		return "airborne?"
	default:
		return "invalid"
	}
}

// AltitudeUnitToString converts altitude unit to string
// Matching C version mode_s.c altitude_unit_to_string
func AltitudeUnitToString(unit AltitudeUnit) string {
	switch unit {
	case UNIT_FEET:
		return "feet"
	case UNIT_METERS:
		return "meters"
	default:
		return "unknown"
	}
}

// AltitudeSourceToString converts altitude source to string
// Matching C version mode_s.c altitude_source_to_string
func AltitudeSourceToString(source AltitudeSource) string {
	switch source {
	case ALTITUDE_BARO:
		return "barometric"
	case ALTITUDE_GNSS:
		return "GNSS"
	default:
		return "unknown"
	}
}

// SpeedSourceToString converts speed source to string
// Matching C version mode_s.c speed_source_to_string
func SpeedSourceToString(source SpeedSource) string {
	switch source {
	case SPEED_GROUNDSPEED:
		return "ground speed"
	case SPEED_IAS:
		return "IAS"
	case SPEED_TAS:
		return "TAS"
	default:
		return "unknown"
	}
}

// HeadingSourceToString converts heading source to string
// Matching C version mode_s.c heading_source_to_string (inferred)
func HeadingSourceToString(source HeadingSource) string {
	switch source {
	case HEADING_TRUE:
		return "true"
	case HEADING_MAGNETIC:
		return "magnetic"
	default:
		return "unknown"
	}
}

// CPRTypeToString converts CPR type to string
// Matching C version mode_s.c cpr_type_to_string
func CPRTypeToString(cprType CPRType) string {
	switch cprType {
	case CPR_SURFACE:
		return "Surface"
	case CPR_AIRBORNE:
		return "Airborne"
	case CPR_COARSE:
		return "Coarse TIS-B"
	default:
		return "unknown"
	}
}

// SILTypeToString converts SIL type to string
// Matching C version mode_s.c (inferred from context)
func SILTypeToString(silType SILType) string {
	switch silType {
	case SIL_PER_SAMPLE:
		return "per sample"
	case SIL_PER_HOUR:
		return "per hour"
	default:
		return "unknown"
	}
}

// DataSourceToString converts data source to string (for internal use)
func DataSourceToString(source DataSource) string {
	switch source {
	case SOURCE_INVALID:
		return "invalid"
	case SOURCE_MLAT:
		return "MLAT"
	case SOURCE_MODE_S:
		return "Mode S"
	case SOURCE_MODE_S_CHECKED:
		return "Mode S (CRC)"
	case SOURCE_TISB:
		return "TIS-B"
	case SOURCE_ADSB:
		return "ADS-B"
	default:
		return "unknown"
	}
}
