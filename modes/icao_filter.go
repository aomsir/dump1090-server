// Package modes implements ICAO address filtering.
//
// This is a direct translation from dump1090-mutability's icao_filter.c.
// Uses open-addressed hash table with linear probing and double buffering
// for age-out of stale entries.
//
// Original Copyright (c) 2014,2015 Oliver Jowett <oliver@mutability.co.uk>
// Licensed under GPL v2+
package modes

import (
	"sync"
	"time"
)

const (
	// ICAO_FILTER_SIZE is the hash table size, must be a power of two
	ICAO_FILTER_SIZE = 4096

	// ICAO_FILTER_TTL is millis between filter expiry flips
	ICAO_FILTER_TTL = 60000
)

// ICAOFilter maintains a set of recently seen ICAO addresses.
// It uses two hash tables and periodically swaps them to age out old entries.
type ICAOFilter struct {
	mu       sync.RWMutex
	filterA  [ICAO_FILTER_SIZE]uint32
	filterB  [ICAO_FILTER_SIZE]uint32
	active   *[ICAO_FILTER_SIZE]uint32
	nextFlip int64 // Unix millis
}

// NewICAOFilter creates a new ICAO address filter.
func NewICAOFilter() *ICAOFilter {
	f := &ICAOFilter{}
	f.active = &f.filterA
	f.nextFlip = time.Now().UnixMilli() + ICAO_FILTER_TTL
	return f
}

// icaoHash computes the Jenkins one-at-a-time hash for a 24-bit address.
func icaoHash(a uint32) uint32 {
	var hash uint32

	hash += a & 0xff
	hash += hash << 10
	hash ^= hash >> 6

	hash += (a >> 8) & 0xff
	hash += hash << 10
	hash ^= hash >> 6

	hash += (a >> 16) & 0xff
	hash += hash << 10
	hash ^= hash >> 6

	hash += hash << 3
	hash ^= hash >> 11
	hash += hash << 15

	return hash & (ICAO_FILTER_SIZE - 1)
}

// Add adds an ICAO address to the filter.
func (f *ICAOFilter) Add(addr uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Add full address
	h0 := icaoHash(addr)
	h := h0
	for f.active[h] != 0 && f.active[h] != addr {
		h = (h + 1) & (ICAO_FILTER_SIZE - 1)
		if h == h0 {
			// Table full
			return
		}
	}
	if f.active[h] == 0 {
		f.active[h] = addr
	}

	// Also add with zeroed top byte for DF20/21 Data Parity handling
	partial := addr & 0x00ffff
	h0 = icaoHash(partial)
	h = h0
	for f.active[h] != 0 && (f.active[h]&0x00ffff) != partial {
		h = (h + 1) & (ICAO_FILTER_SIZE - 1)
		if h == h0 {
			return
		}
	}
	if f.active[h] == 0 {
		f.active[h] = addr
	}
}

// Test checks if an ICAO address is in the filter.
func (f *ICAOFilter) Test(addr uint32) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Check filter A
	h0 := icaoHash(addr)
	h := h0
	for f.filterA[h] != 0 && f.filterA[h] != addr {
		h = (h + 1) & (ICAO_FILTER_SIZE - 1)
		if h == h0 {
			break
		}
	}
	if f.filterA[h] == addr {
		return true
	}

	// Check filter B
	h = h0
	for f.filterB[h] != 0 && f.filterB[h] != addr {
		h = (h + 1) & (ICAO_FILTER_SIZE - 1)
		if h == h0 {
			break
		}
	}
	if f.filterB[h] == addr {
		return true
	}

	return false
}

// TestFuzzy checks if a partial ICAO address (lower 16 bits) is in the filter.
// Returns the full address if found, 0 otherwise.
func (f *ICAOFilter) TestFuzzy(partial uint32) uint32 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	partial &= 0x00ffff

	// Check filter A
	h0 := icaoHash(partial)
	h := h0
	for f.filterA[h] != 0 && (f.filterA[h]&0x00ffff) != partial {
		h = (h + 1) & (ICAO_FILTER_SIZE - 1)
		if h == h0 {
			break
		}
	}
	if (f.filterA[h] & 0x00ffff) == partial {
		return f.filterA[h]
	}

	// Check filter B
	h = h0
	for f.filterB[h] != 0 && (f.filterB[h]&0x00ffff) != partial {
		h = (h + 1) & (ICAO_FILTER_SIZE - 1)
		if h == h0 {
			break
		}
	}
	if (f.filterB[h] & 0x00ffff) == partial {
		return f.filterB[h]
	}

	return 0
}

// Expire should be called periodically to age out old entries.
func (f *ICAOFilter) Expire() {
	now := time.Now().UnixMilli()

	f.mu.Lock()
	defer f.mu.Unlock()

	if now >= f.nextFlip {
		if f.active == &f.filterA {
			// Clear B and switch to it
			f.filterB = [ICAO_FILTER_SIZE]uint32{}
			f.active = &f.filterB
		} else {
			// Clear A and switch to it
			f.filterA = [ICAO_FILTER_SIZE]uint32{}
			f.active = &f.filterA
		}
		f.nextFlip = now + ICAO_FILTER_TTL
	}
}

// Count returns the approximate number of entries in the filter.
func (f *ICAOFilter) Count() int {
	f.mu.RLock()
	defer f.mu.RUnlock()

	seen := make(map[uint32]bool)
	for _, addr := range f.filterA {
		if addr != 0 {
			seen[addr] = true
		}
	}
	for _, addr := range f.filterB {
		if addr != 0 {
			seen[addr] = true
		}
	}
	return len(seen)
}
