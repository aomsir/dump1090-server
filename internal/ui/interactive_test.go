// Package ui provides interactive display functionality for dump1090.
//
// interactive_test.go: tests for interactive display
package ui

import (
	"testing"

	"github.com/aomsir/dump1090-mutability-go/internal/modes"
)

func TestConvertAltitude(t *testing.T) {
	tests := []struct {
		name   string
		ft     int
		metric bool
		want   int
	}{
		{"feet no convert", 10000, false, 10000},
		{"feet to meters", 10000, true, 3048},
		{"feet to meters small", 1000, true, 304},
		{"zero", 0, true, 0},
		{"negative", -500, true, -152},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertAltitude(tt.ft, tt.metric)
			if got != tt.want {
				t.Errorf("convertAltitude(%d, %v) = %d, want %d", tt.ft, tt.metric, got, tt.want)
			}
		})
	}
}

func TestConvertSpeed(t *testing.T) {
	tests := []struct {
		name   string
		kts    int
		metric bool
		want   int
	}{
		{"knots no convert", 100, false, 100},
		{"knots to kmh", 100, true, 185},
		{"knots to kmh small", 10, true, 18},
		{"zero", 0, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertSpeed(tt.kts, tt.metric)
			if got != tt.want {
				t.Errorf("convertSpeed(%d, %v) = %d, want %d", tt.kts, tt.metric, got, tt.want)
			}
		})
	}
}

func TestSpinner(t *testing.T) {
	// Test that spinner cycles through characters
	chars := []byte{'|', '/', '-', '\\'}
	for i := 0; i < 4; i++ {
		got := spinner(uint64(i * 1000))
		if got != chars[i] {
			t.Errorf("spinner(%d) = %c, want %c", i*1000, got, chars[i])
		}
	}

	// Test wrap around
	if got := spinner(4000); got != '|' {
		t.Errorf("spinner(4000) = %c, want |", got)
	}
}

func TestCallsignStr(t *testing.T) {
	tests := []struct {
		name     string
		callsign [9]byte
		want     string
	}{
		{"full callsign", [9]byte{'U', 'A', 'L', '1', '2', '3', ' ', ' ', 0}, "UAL123  "},
		{"short callsign", [9]byte{'A', 'B', 'C', 0, 0, 0, 0, 0, 0}, "ABC"},
		{"empty", [9]byte{0, 0, 0, 0, 0, 0, 0, 0, 0}, ""},
		{"8 chars", [9]byte{'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 0}, "ABCDEFGH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callsignStr(tt.callsign)
			if got != tt.want {
				t.Errorf("callsignStr(%v) = %q, want %q", tt.callsign, got, tt.want)
			}
		})
	}
}

func TestNewInteractive(t *testing.T) {
	i := NewInteractive()
	if i.Rows != MODES_INTERACTIVE_ROWS {
		t.Errorf("NewInteractive().Rows = %d, want %d", i.Rows, MODES_INTERACTIVE_ROWS)
	}
	if i.DisplayTTL != MODES_INTERACTIVE_DISPLAY_TTL {
		t.Errorf("NewInteractive().DisplayTTL = %d, want %d", i.DisplayTTL, MODES_INTERACTIVE_DISPLAY_TTL)
	}
	if i.RTL1090 != false {
		t.Error("NewInteractive().RTL1090 should be false")
	}
	if i.Metric != false {
		t.Error("NewInteractive().Metric should be false")
	}
}

func TestDataValid(t *testing.T) {
	tests := []struct {
		name   string
		source modes.DataSource
		want   bool
	}{
		{"invalid", modes.SOURCE_INVALID, false},
		{"mlat", modes.SOURCE_MLAT, true},
		{"mode_s", modes.SOURCE_MODE_S, true},
		{"adsb", modes.SOURCE_ADSB, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &modes.DataValidity{Source: tt.source}
			got := dataValid(v)
			if got != tt.want {
				t.Errorf("dataValid(%v) = %v, want %v", tt.source, got, tt.want)
			}
		})
	}
}

// BenchmarkShowData benchmarks the interactive display rendering
func BenchmarkShowData(b *testing.B) {
	tracker := modes.NewTracker()

	// Add some aircraft
	for i := 0; i < 20; i++ {
		mm := &modes.Message{
			Addr:          uint32(0xA00000 + i),
			MsgType:       17,
			SignalLevel:   0.5,
			AltitudeValid: true,
			Altitude:      35000 + i*100,
			SpeedValid:    true,
			Speed:         450,
			HeadingValid:  true,
			Heading:       uint32(i * 18),
			CPRValid:      true,
			CPRLat:        uint32(50000 + i*1000),
			CPRLon:        uint32(60000 + i*1000),
		}
		copy(mm.Callsign[:], []byte("TST"+string(rune('0'+i))))
		mm.CallsignValid = true
		tracker.UpdateFromMessage(mm)
	}

	interactive := NewInteractive()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset next update to force refresh
		interactive.nextUpdate = 0
		interactive.ShowData(tracker)
	}
}
