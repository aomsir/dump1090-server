// Package modes implements CPR (Compact Position Reporting) decoding.
//
// This is a direct translation from dump1090-mutability's cpr.c,
// maintaining bit-exact compatibility with the C implementation.
//
// Original Copyright (c) 2014,2015 Oliver Jowett <oliver@mutability.co.uk>
// and Copyright (C) 2012 by Salvatore Sanfilippo <antirez@gmail.com>
// Licensed under GPL v2+ / BSD
package modes

import "math"

// CPR decoding constants
const (
	cprMaxValue = 131072.0 // 2^17, CPR coordinates are encoded in 17 bits
)

// cprModInt returns the always-positive modulo operation for integers.
func cprModInt(a, b int) int {
	res := a % b
	if res < 0 {
		res += b
	}
	return res
}

// cprModDouble returns the always-positive modulo operation for floats.
func cprModDouble(a, b float64) float64 {
	res := math.Mod(a, b)
	if res < 0 {
		res += b
	}
	return res
}

// cprNLFunction returns the number of longitude zones for a given latitude.
// This uses the precomputed table from 1090-WP-9-14.
func cprNLFunction(lat float64) int {
	if lat < 0 {
		lat = -lat // Table is symmetric about the equator
	}

	// Lookup table (59 entries)
	if lat < 10.47047130 {
		return 59
	}
	if lat < 14.82817437 {
		return 58
	}
	if lat < 18.18626357 {
		return 57
	}
	if lat < 21.02939493 {
		return 56
	}
	if lat < 23.54504487 {
		return 55
	}
	if lat < 25.82924707 {
		return 54
	}
	if lat < 27.93898710 {
		return 53
	}
	if lat < 29.91135686 {
		return 52
	}
	if lat < 31.77209708 {
		return 51
	}
	if lat < 33.53993436 {
		return 50
	}
	if lat < 35.22899598 {
		return 49
	}
	if lat < 36.85025108 {
		return 48
	}
	if lat < 38.41241892 {
		return 47
	}
	if lat < 39.92256684 {
		return 46
	}
	if lat < 41.38651832 {
		return 45
	}
	if lat < 42.80914012 {
		return 44
	}
	if lat < 44.19454951 {
		return 43
	}
	if lat < 45.54626723 {
		return 42
	}
	if lat < 46.86733252 {
		return 41
	}
	if lat < 48.16039128 {
		return 40
	}
	if lat < 49.42776439 {
		return 39
	}
	if lat < 50.67150166 {
		return 38
	}
	if lat < 51.89342469 {
		return 37
	}
	if lat < 53.09516153 {
		return 36
	}
	if lat < 54.27817472 {
		return 35
	}
	if lat < 55.44378444 {
		return 34
	}
	if lat < 56.59318756 {
		return 33
	}
	if lat < 57.72747354 {
		return 32
	}
	if lat < 58.84763776 {
		return 31
	}
	if lat < 59.95459277 {
		return 30
	}
	if lat < 61.04917774 {
		return 29
	}
	if lat < 62.13216659 {
		return 28
	}
	if lat < 63.20427479 {
		return 27
	}
	if lat < 64.26616523 {
		return 26
	}
	if lat < 65.31845310 {
		return 25
	}
	if lat < 66.36171008 {
		return 24
	}
	if lat < 67.39646774 {
		return 23
	}
	if lat < 68.42322022 {
		return 22
	}
	if lat < 69.44242631 {
		return 21
	}
	if lat < 70.45451075 {
		return 20
	}
	if lat < 71.45986473 {
		return 19
	}
	if lat < 72.45884545 {
		return 18
	}
	if lat < 73.45177442 {
		return 17
	}
	if lat < 74.43893416 {
		return 16
	}
	if lat < 75.42056257 {
		return 15
	}
	if lat < 76.39684391 {
		return 14
	}
	if lat < 77.36789461 {
		return 13
	}
	if lat < 78.33374083 {
		return 12
	}
	if lat < 79.29428225 {
		return 11
	}
	if lat < 80.24923213 {
		return 10
	}
	if lat < 81.19801349 {
		return 9
	}
	if lat < 82.13956981 {
		return 8
	}
	if lat < 83.07199445 {
		return 7
	}
	if lat < 83.99173563 {
		return 6
	}
	if lat < 84.89166191 {
		return 5
	}
	if lat < 85.75541621 {
		return 4
	}
	if lat < 86.53536998 {
		return 3
	}
	if lat < 87.00000000 {
		return 2
	}
	return 1
}

// cprNFunction computes the N value for a given latitude and fflag.
func cprNFunction(lat float64, fflag bool) int {
	nl := cprNLFunction(lat)
	if fflag {
		nl--
	}
	if nl < 1 {
		nl = 1
	}
	return nl
}

// cprDlonFunction computes the longitude zone size for a given latitude.
// surface indicates if this is a surface (true) or airborne (false) position.
func cprDlonFunction(lat float64, fflag, surface bool) float64 {
	maxLon := 360.0
	if surface {
		maxLon = 90.0
	}
	return maxLon / float64(cprNFunction(lat, fflag))
}

// DecodeCPRAirborne decodes a global airborne position from even and odd CPR frames.
// Returns decoded latitude and longitude, or an error code:
//
//	0: success
//	-1: positions crossed a latitude zone boundary, try again with newer data
//	-2: bad data (latitude out of range)
//
// fflag: true if the odd message is the most recent, false if even is most recent
func DecodeCPRAirborne(evenCPRLat, evenCPRLon, oddCPRLat, oddCPRLon int, fflag bool) (lat, lon float64, err int) {
	const airDlat0 = 360.0 / 60.0
	const airDlat1 = 360.0 / 59.0

	lat0 := float64(evenCPRLat)
	lat1 := float64(oddCPRLat)
	lon0 := float64(evenCPRLon)
	lon1 := float64(oddCPRLon)

	// Compute the Latitude Index "j"
	j := int(math.Floor(((59*lat0 - 60*lat1) / cprMaxValue) + 0.5))
	rlat0 := airDlat0 * (float64(cprModInt(j, 60)) + lat0/cprMaxValue)
	rlat1 := airDlat1 * (float64(cprModInt(j, 59)) + lat1/cprMaxValue)

	if rlat0 >= 270 {
		rlat0 -= 360
	}
	if rlat1 >= 270 {
		rlat1 -= 360
	}

	// Check latitude is in valid range
	if rlat0 < -90 || rlat0 > 90 || rlat1 < -90 || rlat1 > 90 {
		return 0, 0, -2 // bad data
	}

	// Check that both positions are in the same latitude zone
	if cprNLFunction(rlat0) != cprNLFunction(rlat1) {
		return 0, 0, -1 // positions crossed a latitude zone, try again
	}

	// Compute longitude
	var rlat, rlon float64
	if fflag {
		// Use odd packet
		ni := cprNFunction(rlat1, true)
		m := int(math.Floor((((lon0 * float64(cprNLFunction(rlat1)-1)) -
			(lon1 * float64(cprNLFunction(rlat1)))) / cprMaxValue) + 0.5))
		rlon = cprDlonFunction(rlat1, true, false) * (float64(cprModInt(m, ni)) + lon1/cprMaxValue)
		rlat = rlat1
	} else {
		// Use even packet
		ni := cprNFunction(rlat0, false)
		m := int(math.Floor((((lon0 * float64(cprNLFunction(rlat0)-1)) -
			(lon1 * float64(cprNLFunction(rlat0)))) / cprMaxValue) + 0.5))
		rlon = cprDlonFunction(rlat0, false, false) * (float64(cprModInt(m, ni)) + lon0/cprMaxValue)
		rlat = rlat0
	}

	// Renormalize to -180 .. +180
	rlon -= math.Floor((rlon+180)/360) * 360

	return rlat, rlon, 0
}

// DecodeCPRSurface decodes a global surface position from even and odd CPR frames.
// Requires a reference position (reflat, reflon) to resolve ambiguity.
// Returns the same error codes as DecodeCPRAirborne.
func DecodeCPRSurface(reflat, reflon float64, evenCPRLat, evenCPRLon, oddCPRLat, oddCPRLon int, fflag bool) (lat, lon float64, err int) {
	const airDlat0 = 90.0 / 60.0
	const airDlat1 = 90.0 / 59.0

	lat0 := float64(evenCPRLat)
	lat1 := float64(oddCPRLat)
	lon0 := float64(evenCPRLon)
	lon1 := float64(oddCPRLon)

	// Compute the Latitude Index "j"
	j := int(math.Floor(((59*lat0 - 60*lat1) / cprMaxValue) + 0.5))
	rlat0 := airDlat0 * (float64(cprModInt(j, 60)) + lat0/cprMaxValue)
	rlat1 := airDlat1 * (float64(cprModInt(j, 59)) + lat1/cprMaxValue)

	// Pick the quadrant closest to the reference location
	// Only -90..0 and 0..90 are valid quadrants
	// Handle special cases where latitude encodes to 0 (which represents -90, 0, or +90)
	if rlat0 == 0 {
		if reflat < -45 {
			rlat0 = -90
		} else if reflat > 45 {
			rlat0 = 90
		}
	} else if (rlat0 - reflat) > 45 {
		rlat0 -= 90
	}

	if rlat1 == 0 {
		if reflat < -45 {
			rlat1 = -90
		} else if reflat > 45 {
			rlat1 = 90
		}
	} else if (rlat1 - reflat) > 45 {
		rlat1 -= 90
	}

	// Check latitude is in valid range
	if rlat0 < -90 || rlat0 > 90 || rlat1 < -90 || rlat1 > 90 {
		return 0, 0, -2 // bad data
	}

	// Check that both positions are in the same latitude zone
	if cprNLFunction(rlat0) != cprNLFunction(rlat1) {
		return 0, 0, -1 // positions crossed a latitude zone
	}

	// Compute longitude
	var rlat, rlon float64
	if fflag {
		// Use odd packet
		ni := cprNFunction(rlat1, true)
		m := int(math.Floor((((lon0 * float64(cprNLFunction(rlat1)-1)) -
			(lon1 * float64(cprNLFunction(rlat1)))) / cprMaxValue) + 0.5))
		rlon = cprDlonFunction(rlat1, true, true) * (float64(cprModInt(m, ni)) + lon1/cprMaxValue)
		rlat = rlat1
	} else {
		// Use even packet
		ni := cprNFunction(rlat0, false)
		m := int(math.Floor((((lon0 * float64(cprNLFunction(rlat0)-1)) -
			(lon1 * float64(cprNLFunction(rlat0)))) / cprMaxValue) + 0.5))
		rlon = cprDlonFunction(rlat0, false, true) * (float64(cprModInt(m, ni)) + lon0/cprMaxValue)
		rlat = rlat0
	}

	// Pick the longitude quadrant closest to the reference
	// All four quadrants are valid for longitude
	rlon += math.Floor((reflon-rlon+45)/90) * 90

	// Renormalize to -180 .. +180
	rlon -= math.Floor((rlon+180)/360) * 360

	return rlat, rlon, 0
}

// DecodeCPRRelative decodes a position relative to a reference location.
// This requires only a single CPR message (either even or odd).
// surface: true for surface positions, false for airborne
// Returns 0 on success, -1 on error (position too far from reference or out of range)
func DecodeCPRRelative(reflat, reflon float64, cprlat, cprlon int, fflag, surface bool) (lat, lon float64, err int) {
	var airDlat float64
	if surface {
		if fflag {
			airDlat = 90.0 / 59.0
		} else {
			airDlat = 90.0 / 60.0
		}
	} else {
		if fflag {
			airDlat = 360.0 / 59.0
		} else {
			airDlat = 360.0 / 60.0
		}
	}

	fractionalLat := float64(cprlat) / cprMaxValue
	fractionalLon := float64(cprlon) / cprMaxValue

	// Compute the Latitude Index "j"
	j := int(math.Floor(reflat/airDlat) +
		math.Floor(0.5+cprModDouble(reflat, airDlat)/airDlat-fractionalLat))
	rlat := airDlat * (float64(j) + fractionalLat)

	if rlat >= 270 {
		rlat -= 360
	}

	// Check latitude is in valid range
	if rlat < -90 || rlat > 90 {
		return 0, 0, -1
	}

	// Check answer is reasonable (no more than 1/2 cell away)
	if math.Abs(rlat-reflat) > (airDlat / 2) {
		return 0, 0, -1
	}

	// Compute the Longitude Index "m"
	airDlon := cprDlonFunction(rlat, fflag, surface)
	m := int(math.Floor(reflon/airDlon) +
		math.Floor(0.5+cprModDouble(reflon, airDlon)/airDlon-fractionalLon))
	rlon := airDlon * (float64(m) + fractionalLon)

	if rlon > 180 {
		rlon -= 360
	}

	// Check answer is reasonable (no more than 1/2 cell away)
	if math.Abs(rlon-reflon) > (airDlon / 2) {
		return 0, 0, -1
	}

	return rlat, rlon, 0
}
