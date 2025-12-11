// Package ui provides interactive display functionality for dump1090.
//
// interactive.go: aircraft tracking and interactive display
//
// This is a translation from dump1090-mutability's interactive.c.
// Original Copyright (c) 2014,2015 Oliver Jowett <oliver@mutability.co.uk>
// and Copyright (C) 2012 Salvatore Sanfilippo <antirez@gmail.com>
// Licensed under GPL v2+ / BSD
package ui

import (
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"github.com/aomsir/dump1090-mutability-go/modes"
)

// Interactive mode constants (matching C version)
const (
	MODES_INTERACTIVE_REFRESH_TIME = 250   // Milliseconds
	MODES_INTERACTIVE_ROWS         = 22    // Rows on screen
	MODES_INTERACTIVE_DISPLAY_TTL  = 60000 // Delete from display after 60 seconds
)

// Interactive holds the state for interactive display mode.
type Interactive struct {
	// Configuration
	Rows       int  // Maximum rows to display
	DisplayTTL int  // Display timeout in milliseconds
	RTL1090    bool // Use RTL1090 display format
	Metric     bool // Use metric units
	UseGNSS    bool // Prefer GNSS altitude

	// State
	nextUpdate uint64
}

// NewInteractive creates a new Interactive display with default settings.
func NewInteractive() *Interactive {
	return &Interactive{
		Rows:       MODES_INTERACTIVE_ROWS,
		DisplayTTL: MODES_INTERACTIVE_DISPLAY_TTL,
		RTL1090:    false,
		Metric:     false,
		UseGNSS:    false,
	}
}

// convertAltitude converts altitude from feet to meters if metric mode is enabled.
func convertAltitude(ft int, metric bool) int {
	if metric {
		return int(float64(ft) * 0.3048)
	}
	return ft
}

// convertSpeed converts speed from knots to km/h if metric mode is enabled.
func convertSpeed(kts int, metric bool) int {
	if metric {
		return int(float64(kts) * 1.852)
	}
	return kts
}

// spinner returns a spinner character based on current time.
var spinnerChars = [4]byte{'|', '/', '-', '\\'}

func spinner(now uint64) byte {
	return spinnerChars[(now/1000)%4]
}

// ShowData displays the current aircraft data on screen.
// This is a direct translation of interactiveShowData() from C.
func (i *Interactive) ShowData(tracker *modes.Tracker) {
	now := uint64(time.Now().UnixMilli())

	// Refresh screen every MODES_INTERACTIVE_REFRESH_TIME milliseconds
	if now < i.nextUpdate {
		return
	}
	i.nextUpdate = now + MODES_INTERACTIVE_REFRESH_TIME

	progress := spinner(now)

	// Clear the screen (ANSI escape codes) - write directly to stdout
	os.Stdout.WriteString("\x1b[H\x1b[2J")

	// Print header
	if !i.RTL1090 {
		fmt.Fprintf(os.Stdout, " Hex    Mode  Sqwk  Flight   Alt    Spd  Hdg    Lat      Long   RSSI  Msgs  Ti%c\n", progress)
	} else {
		fmt.Fprintf(os.Stdout, " Hex   Flight   Alt      V/S GS  TT  SSR  G*456^ Msgs    Seen %c\n", progress)
	}
	fmt.Fprintln(os.Stdout, "-------------------------------------------------------------------------------")

	// Get all aircraft and sort by ICAO address for consistent display
	aircraft := tracker.GetAllAircraft()
	sort.Slice(aircraft, func(i, j int) bool {
		return aircraft[i].Addr < aircraft[j].Addr
	})

	count := 0
	for _, a := range aircraft {
		if count >= i.Rows {
			break
		}

		// Check if aircraft is still within display TTL
		if (now - a.Seen) >= uint64(i.DisplayTTL) {
			continue
		}

		msgs := a.Messages
		flags := a.ModeACFlags

		// Filter logic matching C version exactly:
		// Show if:
		// - Not Mode A/C message AND messages > 1
		// - OR Mode A only hit AND messages > 4
		// - OR No Mode S hit and not Mode C old AND messages > 127
		showAircraft := false
		if (flags&modes.MODEAC_MSG_FLAG) == 0 && msgs > 1 {
			showAircraft = true
		} else if (flags&(modes.MODEAC_MSG_MODES_HIT|modes.MODEAC_MSG_MODEA_ONLY)) == modes.MODEAC_MSG_MODEA_ONLY && msgs > 4 {
			showAircraft = true
		} else if (flags&(modes.MODEAC_MSG_MODES_HIT|modes.MODEAC_MSG_MODEC_OLD)) == 0 && msgs > 127 {
			showAircraft = true
		}

		if !showAircraft {
			continue
		}

		// Format fields
		strSquawk := "    "
		strFl := "      "
		strTt := "   "
		strGs := "   "

		if dataValid(&a.SquawkValid) {
			strSquawk = fmt.Sprintf("%04x", a.Squawk)
		}

		if dataValid(&a.SpeedValid) {
			strGs = fmt.Sprintf("%3d", convertSpeed(int(a.Speed), i.Metric))
		}

		if dataValid(&a.HeadingValid) {
			strTt = fmt.Sprintf("%03d", a.Heading)
		}

		// Cap messages at 99999
		if msgs > 99999 {
			msgs = 99999
		}

		if i.RTL1090 {
			// RTL1090 display mode
			if dataValid(&a.AltitudeValid) {
				strFl = fmt.Sprintf("F%03d", (a.Altitude+50)/100)
			}
			fmt.Fprintf(os.Stdout, "%06x %-8s %-4s         %-3s %-3s %4s        %-6d  %-2.0f\n",
				a.Addr, callsignStr(a.Callsign), strFl, strGs, strTt, strSquawk, msgs, float64(now-a.Seen)/1000.0)
		} else {
			// Dump1090 display mode
			strMode := "    "
			strLat := "       "
			strLon := "        "

			// Calculate average signal level
			var signalSum float64
			for j := 0; j < 8; j++ {
				signalSum += a.SignalLevel[j]
			}
			signalAverage := signalSum / 8.0

			// Mode string
			if (flags & modes.MODEAC_MSG_FLAG) == 0 {
				strMode = "S   "
			} else if (flags & modes.MODEAC_MSG_MODEA_ONLY) != 0 {
				strMode = "A   "
			}
			modeBytes := []byte(strMode)
			if (flags & modes.MODEAC_MSG_MODEA_HIT) != 0 {
				modeBytes[2] = 'a'
			}
			if (flags & modes.MODEAC_MSG_MODEC_HIT) != 0 {
				modeBytes[3] = 'c'
			}
			strMode = string(modeBytes)

			// Position
			if dataValid(&a.PositionValid) {
				strLat = fmt.Sprintf("%7.03f", a.Lat)
				strLon = fmt.Sprintf("%8.03f", a.Lon)
			}

			// Altitude
			if dataValid(&a.AirGroundValid) && a.AirGround == modes.AG_GROUND {
				strFl = " grnd "
			} else if i.UseGNSS && dataValid(&a.AltitudeGNSSValid) {
				strFl = fmt.Sprintf("%5dH", convertAltitude(a.AltitudeGNSS, i.Metric))
			} else if dataValid(&a.AltitudeValid) {
				strFl = fmt.Sprintf("%5d ", convertAltitude(a.Altitude, i.Metric))
			}

			// Address prefix for non-ICAO addresses
			addrPrefix := " "
			if (a.Addr & modes.MODES_NON_ICAO_ADDRESS) != 0 {
				addrPrefix = "~"
			}

			// RSSI in dBFS
			rssi := 10 * math.Log10(signalAverage)

			fmt.Fprintf(os.Stdout, "%s%06X %-4s  %-4s  %-8s %6s %3s  %3s  %7s %8s %5.1f %5d %2.0f\n",
				addrPrefix, a.Addr&0xffffff,
				strMode, strSquawk, callsignStr(a.Callsign), strFl, strGs, strTt,
				strLat, strLon, rssi, msgs, float64(now-a.Seen)/1000.0)
		}
		count++
	}
}

// dataValid checks if a DataValidity record indicates valid data.
// Matches the C trackDataValid() function.
func dataValid(v *modes.DataValidity) bool {
	return v.Source != modes.SOURCE_INVALID
}

// callsignStr converts a callsign byte array to a string.
func callsignStr(callsign [9]byte) string {
	// Find null terminator or end
	end := 0
	for end < 8 && callsign[end] != 0 {
		end++
	}
	return string(callsign[:end])
}
