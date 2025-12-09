// Package modes implements Mode S message decoding and error correction.
//
// This is a direct translation from dump1090-mutability's crc.c,
// maintaining bit-exact compatibility with the C implementation.
//
// Original Copyright (c) 2014,2015 Oliver Jowett <oliver@mutability.co.uk>
// Licensed under GPL v2+
package modes

import (
	"sort"
)

const (
	// MODES_GENERATOR_POLY is the generator polynomial for Mode S CRC
	MODES_GENERATOR_POLY = 0xfff409

	// MODES_MAX_BITERRORS is the maximum number of bit errors we can fix
	MODES_MAX_BITERRORS = 2

	// Message lengths in bits
	MODES_SHORT_MSG_BITS = 56
	MODES_LONG_MSG_BITS  = 112
)

// ErrorInfo describes a correctable error pattern.
type ErrorInfo struct {
	Syndrome uint32  // CRC syndrome
	Errors   int     // number of errors (-1 = collision, don't use)
	Bit      [MODES_MAX_BITERRORS]int8 // bit positions to fix (-1 = no bit)
}

// noErrors is the ErrorInfo returned for messages with no errors
var noErrors = ErrorInfo{
	Syndrome: 0,
	Errors:   0,
	Bit:      [MODES_MAX_BITERRORS]int8{-1, -1},
}

// CRC lookup tables
var (
	// crcTable speeds up CRC calculation (CRC for each single-byte message)
	crcTable [256]uint32

	// singleBitSyndrome contains syndrome values for all single-bit errors
	singleBitSyndrome [112]uint32

	// Error correction tables for short and long messages
	bitErrorTableShort     []ErrorInfo
	bitErrorTableLong      []ErrorInfo
)

// initLookupTables initializes the CRC and syndrome lookup tables.
// This must be called before any CRC operations.
func initLookupTables() {
	// Build the CRC table
	for i := 0; i < 256; i++ {
		c := uint32(i) << 16
		for j := 0; j < 8; j++ {
			if c&0x800000 != 0 {
				c = (c << 1) ^ MODES_GENERATOR_POLY
			} else {
				c = c << 1
			}
		}
		crcTable[i] = c & 0x00ffffff
	}

	// Build single-bit syndrome table
	msg := make([]byte, 112/8)
	for i := 0; i < 112; i++ {
		msg[i/8] ^= 1 << (7 - (i & 7))
		singleBitSyndrome[i] = ModesChecksum(msg, 112)
		msg[i/8] ^= 1 << (7 - (i & 7)) // flip back
	}
}

// ModesChecksum computes the Mode S checksum for a message.
// bits must be a multiple of 8 and at least 24.
func ModesChecksum(message []byte, bits int) uint32 {
	if bits%8 != 0 || bits < 24 {
		panic("invalid message length for checksum")
	}

	n := bits / 8
	var rem uint32

	for i := 0; i < n-3; i++ {
		rem = (rem << 8) ^ crcTable[message[i]^byte((rem&0xff0000)>>16)]
		rem &= 0xffffff
	}

	rem ^= uint32(message[n-3])<<16 | uint32(message[n-2])<<8 | uint32(message[n-1])
	return rem
}

// prepareErrorTable builds an error correction table for messages of a given length.
// maxCorrect: maximum number of bit errors to correct
// maxDetect: maximum number of bit errors to detect (must be >= maxCorrect)
func prepareErrorTable(bits, maxCorrect, maxDetect int) []ErrorInfo {
	if bits < 0 || bits > 112 {
		panic("invalid message length")
	}
	if maxCorrect < 0 || maxCorrect > MODES_MAX_BITERRORS {
		panic("invalid maxCorrect")
	}
	if maxDetect < maxCorrect {
		panic("maxDetect must be >= maxCorrect")
	}

	if maxCorrect == 0 {
		return nil
	}

	// Calculate maximum table size
	maxSize := 0
	for i := 1; i <= maxCorrect; i++ {
		maxSize += combinations(bits-5, i)
	}

	table := make([]ErrorInfo, 0, maxSize)
	baseEntry := ErrorInfo{
		Syndrome: 0,
		Errors:   0,
		Bit:      [MODES_MAX_BITERRORS]int8{-1, -1},
	}

	// Ignore the first 5 bits (DF type)
	table = prepareSubtable(table, 112-bits, 5, bits, baseEntry, 0, maxCorrect)

	// Sort by syndrome for binary search
	sort.Slice(table, func(i, j int) bool {
		return table[i].Syndrome < table[j].Syndrome
	})

	// Remove duplicate syndromes (ambiguous corrections)
	table = removeDuplicates(table)

	// Flag collisions for higher-order errors we want to detect but not correct
	if maxDetect > maxCorrect {
		table = flagCollisions(table, bits, maxCorrect, maxDetect)
	}

	return table
}

// prepareSubtable recursively generates error syndromes
func prepareSubtable(table []ErrorInfo, offset, startBit, endBit int, baseEntry ErrorInfo, errorBit, maxErrors int) []ErrorInfo {
	if errorBit >= maxErrors {
		return table
	}

	for i := startBit; i < endBit; i++ {
		entry := baseEntry
		entry.Syndrome ^= singleBitSyndrome[i+offset]
		entry.Errors = errorBit + 1
		entry.Bit[errorBit] = int8(i)

		table = append(table, entry)
		table = prepareSubtable(table, offset, i+1, endBit, entry, errorBit+1, maxErrors)
	}

	return table
}

// removeDuplicates removes entries with duplicate syndromes
func removeDuplicates(table []ErrorInfo) []ErrorInfo {
	if len(table) == 0 {
		return table
	}

	result := make([]ErrorInfo, 0, len(table))
	i := 0
	for i < len(table) {
		// Check if next entry has same syndrome
		if i < len(table)-1 && table[i+1].Syndrome == table[i].Syndrome {
			// Skip all entries with this syndrome (ambiguous)
			for i < len(table)-1 && table[i+1].Syndrome == table[i].Syndrome {
				i++
			}
			i++ // skip the last duplicate too
		} else {
			result = append(result, table[i])
			i++
		}
	}

	return result
}

// flagCollisions marks entries that collide with higher-order errors
func flagCollisions(table []ErrorInfo, bits, maxCorrect, maxDetect int) []ErrorInfo {
	marked := make(map[int]bool)

	// Find all syndromes that would be produced by maxCorrect+1 to maxDetect bit errors
	var checkCollisions func(syndrome uint32, offset, startBit, endBit, errorBit, firstError, lastError int)
	checkCollisions = func(syndrome uint32, offset, startBit, endBit, errorBit, firstError, lastError int) {
		if errorBit > lastError {
			return
		}

		for i := startBit; i < endBit; i++ {
			newSyndrome := syndrome ^ singleBitSyndrome[i+offset]

			if errorBit >= firstError {
				// Look for this syndrome in the table
				idx := sort.Search(len(table), func(j int) bool {
					return table[j].Syndrome >= newSyndrome
				})
				if idx < len(table) && table[idx].Syndrome == newSyndrome {
					marked[idx] = true
				}
			}

			checkCollisions(newSyndrome, offset, i+1, endBit, errorBit+1, firstError, lastError)
		}
	}

	checkCollisions(0, 112-bits, 5, bits, 1, maxCorrect+1, maxDetect)

	// Remove marked entries
	result := make([]ErrorInfo, 0, len(table))
	for i, entry := range table {
		if !marked[i] {
			result = append(result, entry)
		}
	}

	return result
}

// combinations calculates binomial coefficient: n choose k
func combinations(n, k int) int {
	if k == 0 || k == n {
		return 1
	}
	if k > n {
		return 0
	}

	result := 1
	for i := 1; i <= k; i++ {
		result = result * n / i
		n--
	}
	return result
}

// ModesChecksumInit initializes CRC tables and error correction tables.
// fixBits: 0 = no correction, 1 = 1-bit correction, 2 = 2-bit correction
func ModesChecksumInit(fixBits int) {
	initLookupTables()

	switch fixBits {
	case 0:
		bitErrorTableShort = nil
		bitErrorTableLong = nil

	case 1:
		// 1-bit correction with 1-bit detection
		bitErrorTableShort = prepareErrorTable(MODES_SHORT_MSG_BITS, 1, 1)
		bitErrorTableLong = prepareErrorTable(MODES_LONG_MSG_BITS, 1, 1)

	default:
		// 2-bit correction with 4-bit detection
		bitErrorTableShort = prepareErrorTable(MODES_SHORT_MSG_BITS, 2, 4)
		bitErrorTableLong = prepareErrorTable(MODES_LONG_MSG_BITS, 2, 4)
	}
}

// ModesChecksumDiagnose returns error correction info for a given syndrome.
// Returns nil if the syndrome is uncorrectable.
func ModesChecksumDiagnose(syndrome uint32, bitLen int) *ErrorInfo {
	if syndrome == 0 {
		return &noErrors
	}

	var table []ErrorInfo
	switch bitLen {
	case MODES_SHORT_MSG_BITS:
		table = bitErrorTableShort
	case MODES_LONG_MSG_BITS:
		table = bitErrorTableLong
	default:
		panic("invalid bitLen for checksum diagnosis")
	}

	if table == nil {
		return nil
	}

	// Binary search for syndrome
	idx := sort.Search(len(table), func(i int) bool {
		return table[i].Syndrome >= syndrome
	})

	if idx < len(table) && table[idx].Syndrome == syndrome {
		return &table[idx]
	}

	return nil
}

// ModesChecksumFix applies error correction to a message.
// msg is modified in-place.
func ModesChecksumFix(msg []byte, info *ErrorInfo) {
	if info == nil || info.Errors == 0 {
		return
	}

	for i := 0; i < info.Errors; i++ {
		bitPos := int(info.Bit[i])
		msg[bitPos/8] ^= 1 << (7 - (bitPos & 7))
	}
}
