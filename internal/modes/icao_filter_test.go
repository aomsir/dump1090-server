// Package modes implements Mode S message decoding and output formats.
//
// icao_filter_test.go: tests for ICAO address filtering
package modes

import (
	"testing"
	"time"
)

func TestNewICAOFilter(t *testing.T) {
	f := NewICAOFilter()
	if f == nil {
		t.Fatal("NewICAOFilter returned nil")
	}
	if f.active != &f.filterA {
		t.Error("active filter should initially be filterA")
	}
	if f.nextFlip <= 0 {
		t.Error("nextFlip should be initialized")
	}
}

func TestICAOHash(t *testing.T) {
	tests := []struct {
		addr uint32
		name string
	}{
		{0x123456, "typical address"},
		{0x000000, "zero address"},
		{0xFFFFFF, "max address"},
		{0xABCDEF, "another address"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := icaoHash(tt.addr)
			if h >= ICAO_FILTER_SIZE {
				t.Errorf("hash %d exceeds ICAO_FILTER_SIZE %d", h, ICAO_FILTER_SIZE)
			}
		})
	}

	// Test that same address produces same hash
	addr := uint32(0x123456)
	h1 := icaoHash(addr)
	h2 := icaoHash(addr)
	if h1 != h2 {
		t.Errorf("same address should produce same hash: %d != %d", h1, h2)
	}
}

func TestAddAndTest(t *testing.T) {
	f := NewICAOFilter()

	// Test initially empty
	if f.Test(0x123456) {
		t.Error("filter should be initially empty")
	}

	// Add an address
	f.Add(0x123456)

	// Test it exists
	if !f.Test(0x123456) {
		t.Error("address should exist after Add")
	}

	// Test a different address doesn't exist
	if f.Test(0x789ABC) {
		t.Error("different address should not exist")
	}
}

func TestAddMultipleAddresses(t *testing.T) {
	f := NewICAOFilter()

	addresses := []uint32{
		0x123456,
		0x789ABC,
		0xDEF012,
		0x345678,
		0x9ABCDE,
	}

	// Add all addresses
	for _, addr := range addresses {
		f.Add(addr)
	}

	// Test all addresses exist
	for _, addr := range addresses {
		if !f.Test(addr) {
			t.Errorf("address %06X should exist", addr)
		}
	}
}

func TestAddDuplicate(t *testing.T) {
	f := NewICAOFilter()

	addr := uint32(0x123456)

	// Add same address multiple times
	f.Add(addr)
	f.Add(addr)
	f.Add(addr)

	// Should still exist
	if !f.Test(addr) {
		t.Error("address should exist after duplicate adds")
	}

	// Count should reflect unique entries
	count := f.Count()
	if count < 1 {
		t.Error("count should be at least 1")
	}
}

func TestTestFuzzy(t *testing.T) {
	f := NewICAOFilter()

	// Add an address
	fullAddr := uint32(0x123456)
	f.Add(fullAddr)

	// Test fuzzy match with same low 16 bits
	partial := uint32(0x003456) // Same low 16 bits as 0x123456
	result := f.TestFuzzy(partial)
	if result != fullAddr {
		t.Errorf("TestFuzzy(%06X) = %06X, want %06X", partial, result, fullAddr)
	}

	// Test fuzzy match with different low 16 bits
	wrongPartial := uint32(0x009999)
	result = f.TestFuzzy(wrongPartial)
	if result != 0 {
		t.Errorf("TestFuzzy(%06X) should return 0, got %06X", wrongPartial, result)
	}
}

func TestTestFuzzyPartialStorage(t *testing.T) {
	f := NewICAOFilter()

	// When we add an address, it's stored twice:
	// 1. Full address (0x123456)
	// 2. Partial address (0x003456) - for DF20/21 support
	fullAddr := uint32(0x123456)
	f.Add(fullAddr)

	// Should match on low 16 bits
	matches := []uint32{
		0x003456, // Exact low 16 bits
		0xAB3456, // Different high byte, same low 16 bits
		0xFF3456, // Another different high byte
	}

	for _, partial := range matches {
		result := f.TestFuzzy(partial)
		if result != fullAddr {
			t.Errorf("TestFuzzy(%06X) = %06X, want %06X", partial, result, fullAddr)
		}
	}
}

func TestExpire(t *testing.T) {
	f := NewICAOFilter()

	// Add address to active table (filterA)
	addr1 := uint32(0x123456)
	f.Add(addr1)

	if !f.Test(addr1) {
		t.Error("address should exist immediately after add")
	}

	// Manually trigger expiry by setting nextFlip to past
	f.mu.Lock()
	f.nextFlip = time.Now().UnixMilli() - 1
	oldActive := f.active
	f.mu.Unlock()

	// Expire should switch tables and clear the new active one
	f.Expire()

	// Verify table switched
	f.mu.Lock()
	newActive := f.active
	f.mu.Unlock()

	if oldActive == newActive {
		t.Error("active table should have switched after expiry")
	}

	// Add address to new active table
	addr2 := uint32(0x789ABC)
	f.Add(addr2)

	// addr1 should still exist (in old inactive table, within 2xTTL window)
	if !f.Test(addr1) {
		t.Error("addr1 should still exist (in inactive table, within 2xTTL)")
	}

	// addr2 should exist (was added to new active table)
	if !f.Test(addr2) {
		t.Error("addr2 should exist after expiry (in active table)")
	}

	// Trigger another expiry
	f.mu.Lock()
	f.nextFlip = time.Now().UnixMilli() - 1
	f.mu.Unlock()

	addr3 := uint32(0xABCDEF)
	f.Add(addr3)
	f.Expire()

	// addr3 should exist (just added to new active table)
	if !f.Test(addr3) {
		t.Error("addr3 should exist")
	}

	// addr2 should still exist (now in the other table)
	if !f.Test(addr2) {
		t.Error("addr2 should still exist (in inactive table)")
	}

	// addr1 should no longer exist (its table was cleared)
	if f.Test(addr1) {
		t.Error("addr1 should not exist (was in the table that got cleared)")
	}
}

func TestCount(t *testing.T) {
	f := NewICAOFilter()

	// Initially empty
	if count := f.Count(); count != 0 {
		t.Errorf("initial count = %d, want 0", count)
	}

	// Add some addresses
	addresses := []uint32{0x123456, 0x789ABC, 0xDEF012}
	for _, addr := range addresses {
		f.Add(addr)
	}

	count := f.Count()
	// Should have at least 3 unique addresses
	// (May be more due to partial address storage, but deduplicated by Count)
	if count < len(addresses) {
		t.Errorf("count = %d, want at least %d", count, len(addresses))
	}
}

func TestHashCollisions(t *testing.T) {
	f := NewICAOFilter()

	// Add many addresses to potentially trigger collisions
	for i := uint32(0); i < 100; i++ {
		addr := 0x100000 + i
		f.Add(addr)
	}

	// Verify all can be found (linear probing should handle collisions)
	for i := uint32(0); i < 100; i++ {
		addr := 0x100000 + i
		if !f.Test(addr) {
			t.Errorf("address %06X not found after collision handling", addr)
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	f := NewICAOFilter()

	// Test concurrent adds and tests
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := uint32(0); i < 1000; i++ {
			f.Add(0x100000 + i)
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := uint32(0); i < 1000; i++ {
			f.Test(0x100000 + i)
		}
		done <- true
	}()

	// Wait for both to finish
	<-done
	<-done
}

func BenchmarkAdd(b *testing.B) {
	f := NewICAOFilter()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Add(uint32(i & 0xFFFFFF))
	}
}

func BenchmarkTest(b *testing.B) {
	f := NewICAOFilter()
	// Pre-populate filter
	for i := uint32(0); i < 1000; i++ {
		f.Add(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Test(uint32(i & 0xFFFFFF))
	}
}

func BenchmarkTestFuzzy(b *testing.B) {
	f := NewICAOFilter()
	// Pre-populate filter
	for i := uint32(0); i < 1000; i++ {
		f.Add(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.TestFuzzy(uint32(i & 0xFFFF))
	}
}
