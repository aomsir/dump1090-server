// Package modes implements Mode S message debugging and display functions.
//
// debug.go: message display and debugging utilities
//
// This is a translation from dump1090-mutability's mode_s.c displayModesMessage.
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// Licensed under GPL v2+
package modes

import (
	"fmt"
	"io"
	"math"
	"os"
)

// DisplayConfig controls what information DisplayModesMessage shows
type DisplayConfig struct {
	OnlyAddr bool // Show only hex address
	Raw      bool // Show only raw message
	MLAT     bool // Show MLAT timestamp
}

// DefaultDisplayConfig returns the default display configuration
func DefaultDisplayConfig() DisplayConfig {
	return DisplayConfig{
		OnlyAddr: false,
		Raw:      false,
		MLAT:     false,
	}
}

// DisplayModesMessage displays a decoded message with details
// Matching C version mode_s.c:1512-1772
func DisplayModesMessage(mm *Message, cfg DisplayConfig, w io.Writer) {
	if w == nil {
		w = os.Stdout
	}

	// Handle only addresses mode first (matching C version mode_s.c:1516-1519)
	if cfg.OnlyAddr {
		fmt.Fprintf(w, "%06x\n", mm.Addr)
		return
	}

	// Show the raw message (matching C version mode_s.c:1522-1528)
	if cfg.MLAT && mm.TimestampMsg != 0 {
		fmt.Fprintf(w, "@%012X", mm.TimestampMsg)
	} else {
		fmt.Fprint(w, "*")
	}

	msgLen := mm.MsgBits / 8
	for i := 0; i < msgLen; i++ {
		fmt.Fprintf(w, "%02X", mm.Msg[i])
	}
	fmt.Fprintln(w, ";")

	if cfg.Raw {
		return // Enough for --raw mode
	}

	// CRC for messages < DF32 (matching C version mode_s.c:1535-1536)
	if mm.MsgType < 32 {
		fmt.Fprintf(w, "CRC: %06X\n", mm.CRC)
	}

	// Number of corrected bits (matching C version mode_s.c:1538-1539)
	if mm.Corrected != 0 {
		fmt.Fprintf(w, "No. of bit errors fixed: %d\n", mm.Corrected)
	}

	// RSSI (matching C version mode_s.c:1541-1542)
	if mm.SignalLevel > 0 {
		fmt.Fprintf(w, "RSSI: %.1f dBFS\n", 10*math.Log10(mm.SignalLevel))
	}

	// Score (matching C version mode_s.c:1544-1545)
	if mm.Score > 0 {
		fmt.Fprintf(w, "Score: %d\n", mm.Score)
	}

	// Timestamp (matching C version mode_s.c:1547-1552)
	if mm.TimestampMsg != 0 {
		if mm.TimestampMsg == MAGIC_MLAT_TIMESTAMP {
			fmt.Fprintln(w, "This is a synthetic MLAT message.")
		} else {
			fmt.Fprintf(w, "Time: %.2fus\n", float64(mm.TimestampMsg)/12.0)
		}
	}

	// DF type specific output (matching C version mode_s.c:1554-1623)
	switch mm.MsgType {
	case 0:
		fmt.Fprintf(w, "DF:0 addr:%06X VS:%d CC:%d SL:%d RI:%d AC:%d\n",
			mm.Addr, mm.VS, mm.CC, mm.SL, mm.RI, mm.AC)

	case 4:
		fmt.Fprintf(w, "DF:4 addr:%06X FS:%d DR:%d UM:%d AC:%d\n",
			mm.Addr, mm.FS, mm.DR, mm.UM, mm.AC)

	case 5:
		fmt.Fprintf(w, "DF:5 addr:%06X FS:%d DR:%d UM:%d ID:%d\n",
			mm.Addr, mm.FS, mm.DR, mm.UM, mm.ID)

	case 11:
		fmt.Fprintf(w, "DF:11 AA:%06X IID:%d CA:%d\n",
			mm.AA, mm.IID, mm.CA)

	case 16:
		fmt.Fprintf(w, "DF:16 addr:%06X VS:%d SL:%d RI:%d AC:%d MV:",
			mm.Addr, mm.VS, mm.SL, mm.RI, mm.AC)
		printHexBytes(w, mm.MV[:])
		fmt.Fprintln(w)

	case 17:
		fmt.Fprintf(w, "DF:17 AA:%06X CA:%d ME:",
			mm.AA, mm.CA)
		printHexBytes(w, mm.ME[:])
		fmt.Fprintln(w)

	case 18:
		fmt.Fprintf(w, "DF:18 AA:%06X CF:%d ME:",
			mm.AA, mm.CF)
		printHexBytes(w, mm.ME[:])
		fmt.Fprintln(w)

	case 20:
		fmt.Fprintf(w, "DF:20 addr:%06X FS:%d DR:%d UM:%d AC:%d MB:",
			mm.Addr, mm.FS, mm.DR, mm.UM, mm.AC)
		printHexBytes(w, mm.MB[:])
		fmt.Fprintln(w)

	case 21:
		fmt.Fprintf(w, "DF:21 addr:%06X FS:%d DR:%d UM:%d ID:%d MB:",
			mm.Addr, mm.FS, mm.DR, mm.UM, mm.ID)
		printHexBytes(w, mm.MB[:])
		fmt.Fprintln(w)

	case 24, 25, 26, 27, 28, 29, 30, 31:
		fmt.Fprintf(w, "DF:%d addr:%06X KE:%d ND:%d MD:",
			mm.MsgType, mm.Addr, mm.KE, mm.ND)
		printHexBytes(w, mm.MD[:])
		fmt.Fprintln(w)
	}

	// DF type name (matching C version mode_s.c:1625-1638)
	fmt.Fprintf(w, " %s", DFToString(mm.MsgType))
	if mm.MsgType == 17 || mm.MsgType == 18 {
		if ESTypeHasSubtype(mm.METype) {
			fmt.Fprintf(w, " %s (%d/%d)",
				ESTypeName(mm.METype, mm.MESub),
				mm.METype,
				mm.MESub)
		} else {
			fmt.Fprintf(w, " %s (%d)",
				ESTypeName(mm.METype, mm.MESub),
				mm.METype)
		}
	}
	fmt.Fprintln(w)

	// Address information (matching C version mode_s.c:1640-1644)
	if mm.Addr&MODES_NON_ICAO_ADDRESS != 0 {
		fmt.Fprintf(w, "  Other Address: %06X (%s)\n",
			mm.Addr&0xFFFFFF, AddrTypeToString(mm.AddrType))
	} else {
		fmt.Fprintf(w, "  ICAO Address:  %06X (%s)\n",
			mm.Addr, AddrTypeToString(mm.AddrType))
	}

	// Air/Ground state (matching C version mode_s.c:1646-1649)
	if mm.AirGround != AG_INVALID {
		fmt.Fprintf(w, "  Air/Ground:    %s\n", AirGroundToString(mm.AirGround))
	}

	// Altitude (matching C version mode_s.c:1651-1656)
	if mm.AltitudeValid {
		fmt.Fprintf(w, "  Altitude:      %d %s %s\n",
			mm.Altitude,
			AltitudeUnitToString(mm.AltitudeUnit),
			AltitudeSourceToString(mm.AltitudeSource))
	}

	// GNSS delta (matching C version mode_s.c:1658-1661)
	if mm.GNSSDeltaValid {
		fmt.Fprintf(w, "  GNSS delta:    %d ft\n", mm.GNSSDelta)
	}

	// Heading (matching C version mode_s.c:1663-1665)
	if mm.HeadingValid {
		fmt.Fprintf(w, "  Heading:       %d %s\n",
			mm.Heading, HeadingSourceToString(mm.HeadingSource))
	}

	// Speed (matching C version mode_s.c:1667-1671)
	if mm.SpeedValid {
		fmt.Fprintf(w, "  Speed:         %d kt %s\n",
			mm.Speed, SpeedSourceToString(mm.SpeedSource))
	}

	// Vertical rate (matching C version mode_s.c:1673-1677)
	if mm.VertRateValid {
		fmt.Fprintf(w, "  Vertical rate: %d ft/min %s\n",
			mm.VertRate, AltitudeSourceToString(mm.VertRateSource))
	}

	// Squawk (matching C version mode_s.c:1679-1682)
	if mm.SquawkValid {
		fmt.Fprintf(w, "  Squawk:        %04X\n", mm.Squawk)
	}

	// Callsign (matching C version mode_s.c:1684-1687)
	if mm.CallsignValid {
		fmt.Fprintf(w, "  Ident:         %s\n", callsignToString(mm.Callsign))
	}

	// Category (matching C version mode_s.c:1689-1692)
	if mm.CategoryValid {
		fmt.Fprintf(w, "  Category:      %02X\n", mm.Category)
	}

	// CPR (matching C version mode_s.c:1694-1722)
	if mm.CPRValid {
		fmt.Fprintf(w, "  CPR type:      %s\n"+
			"  CPR odd flag:  %s\n"+
			"  CPR NUCp/NIC:  %d\n",
			CPRTypeToString(mm.CPRType),
			boolToOddEven(mm.CPROdd),
			mm.CPRNUCP)

		if mm.CPRDecoded {
			relativeStr := ""
			if mm.CPRRelative {
				relativeStr = " (relative)"
			}
			fmt.Fprintf(w, "  CPR latitude:  %.5f (%d)\n"+
				"  CPR longitude: %.5f (%d)\n"+
				"  CPR decoding:  %s%s\n",
				mm.DecodedLat,
				mm.CPRLat,
				mm.DecodedLon,
				mm.CPRLon,
				cprDecodingTypeToString(mm.CPROdd, mm.CPRRelative),
				relativeStr)
		} else {
			fmt.Fprintf(w, "  CPR latitude:  (%d)\n"+
				"  CPR longitude: (%d)\n",
				mm.CPRLat,
				mm.CPRLon)
		}
	}

	// Operational status (matching C version mode_s.c:1724-1772)
	if mm.OpStatus.Valid {
		fmt.Fprintf(w, "  ADS-B version: %d\n", mm.OpStatus.Version)

		fmt.Fprintf(w, "  Capability:   ")
		if mm.OpStatus.CCACAS {
			fmt.Fprint(w, " ACAS")
		}
		if mm.OpStatus.CCCDTI {
			fmt.Fprint(w, " CDTI")
		}
		if mm.OpStatus.CCARV {
			fmt.Fprint(w, " ARV")
		}
		if mm.OpStatus.CCTS {
			fmt.Fprintf(w, " TC=%d", mm.OpStatus.CCTC)
		}
		if mm.OpStatus.CC1090In {
			fmt.Fprint(w, " 1090-in")
		}
		if mm.OpStatus.CCUATIn {
			fmt.Fprint(w, " UAT-in")
		}
		if mm.OpStatus.CCPOA {
			fmt.Fprint(w, " POA")
		}
		if mm.OpStatus.CCB2Low {
			fmt.Fprint(w, " B2-low")
		}
		if mm.OpStatus.CCNACv != 0 {
			fmt.Fprintf(w, " NACv=%d", mm.OpStatus.CCNACv)
		}
		if mm.OpStatus.CCNICSupp {
			fmt.Fprint(w, " NIC-suppl")
		}
		if mm.OpStatus.CCLWValid {
			fmt.Fprintf(w, " class-lw=%d", mm.OpStatus.CCLW)
		}
		if mm.OpStatus.CCAntennaOff != 0 {
			fmt.Fprintf(w, " antenna-offset=%d", mm.OpStatus.CCAntennaOff)
		}
		fmt.Fprintln(w)

		fmt.Fprintf(w, "  Operational:  ")
		if mm.OpStatus.OMACASRa {
			fmt.Fprint(w, " ACASRa")
		}
		if mm.OpStatus.OMIdent {
			fmt.Fprint(w, " Ident")
		}
		if mm.OpStatus.OMATC {
			fmt.Fprint(w, " ATC")
		}
		if mm.OpStatus.OMSAF {
			fmt.Fprint(w, " SAF")
		}
		if mm.OpStatus.OMSDA != 0 {
			fmt.Fprintf(w, " SDA=%d", mm.OpStatus.OMSDA)
		}
		fmt.Fprintln(w)

		if mm.OpStatus.NACp != 0 {
			fmt.Fprintf(w, "  NACp:          %d\n", mm.OpStatus.NACp)
		}
		if mm.OpStatus.GVA != 0 {
			fmt.Fprintf(w, "  GVA:           %d\n", mm.OpStatus.GVA)
		}
		if mm.OpStatus.SIL != 0 {
			fmt.Fprintf(w, "  SIL:           %d (%s)\n",
				mm.OpStatus.SIL, SILTypeToString(mm.OpStatus.SILType))
		}
		if mm.OpStatus.NICBaro {
			fmt.Fprintf(w, "  NIC-baro:      %d\n", 1)
		}
	}

	fmt.Fprintln(w)
}

// printHexBytes prints a byte array as hex (matching C version mode_s.c print_hex_bytes)
func printHexBytes(w io.Writer, data []byte) {
	for _, b := range data {
		fmt.Fprintf(w, "%02X", b)
	}
}

// boolToOddEven converts boolean to "odd" or "even" string
func boolToOddEven(odd bool) string {
	if odd {
		return "odd"
	}
	return "even"
}

// cprDecodingTypeToString returns CPR decoding type description
func cprDecodingTypeToString(odd, relative bool) string {
	typeStr := "global"
	if relative {
		typeStr = "local"
	}
	frame := "even"
	if odd {
		frame = "odd"
	}
	return fmt.Sprintf("%s, %s frame", typeStr, frame)
}
