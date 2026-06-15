package modes

import (
	"math"
	"testing"
)

const floatTolerance = 0.000001 // Tolerance for float comparisons

func TestDecodeCPRAirborne(t *testing.T) {
	// Test data from cprtests.c - cprGlobalAirborneTests
	tests := []struct {
		name          string
		evenCPRLat    int
		evenCPRLon    int
		oddCPRLat     int
		oddCPRLon     int
		fflag         bool
		expectedLat   float64
		expectedLon   float64
		expectedError int
	}{
		{
			name:          "Airborne position 1 (even message latest)",
			evenCPRLat:    80536,
			evenCPRLon:    9432,
			oddCPRLat:     61720,
			oddCPRLon:     9192,
			fflag:         false,
			expectedLat:   51.686646,
			expectedLon:   0.700156,
			expectedError: 0,
		},
		{
			name:          "Airborne position 1 (odd message latest)",
			evenCPRLat:    80536,
			evenCPRLon:    9432,
			oddCPRLat:     61720,
			oddCPRLon:     9192,
			fflag:         true,
			expectedLat:   51.686763,
			expectedLon:   0.701294,
			expectedError: 0,
		},
		{
			name:          "Airborne position 2 (even message latest)",
			evenCPRLat:    80534,
			evenCPRLon:    9413,
			oddCPRLat:     61714,
			oddCPRLon:     9144,
			fflag:         false,
			expectedLat:   51.686554,
			expectedLon:   0.698745,
			expectedError: 0,
		},
		{
			name:          "Airborne position 2 (odd message latest)",
			evenCPRLat:    80534,
			evenCPRLon:    9413,
			oddCPRLat:     61714,
			oddCPRLon:     9144,
			fflag:         true,
			expectedLat:   51.686484,
			expectedLon:   0.697632,
			expectedError: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lat, lon, err := DecodeCPRAirborne(tt.evenCPRLat, tt.evenCPRLon, tt.oddCPRLat, tt.oddCPRLon, tt.fflag)

			if err != tt.expectedError {
				t.Errorf("DecodeCPRAirborne() error = %d, want %d", err, tt.expectedError)
				return
			}

			if tt.expectedError == 0 {
				if math.Abs(lat-tt.expectedLat) > floatTolerance {
					t.Errorf("DecodeCPRAirborne() lat = %.6f, want %.6f (diff: %.9f)",
						lat, tt.expectedLat, math.Abs(lat-tt.expectedLat))
				}
				if math.Abs(lon-tt.expectedLon) > floatTolerance {
					t.Errorf("DecodeCPRAirborne() lon = %.6f, want %.6f (diff: %.9f)",
						lon, tt.expectedLon, math.Abs(lon-tt.expectedLon))
				}
			}
		})
	}
}

func TestDecodeCPRSurface(t *testing.T) {
	// Test data from cprtests.c - cprGlobalSurfaceTests
	// Real position: Cambridge (UK) airport at 52.21N 0.177E
	tests := []struct {
		name          string
		reflat        float64
		reflon        float64
		evenCPRLat    int
		evenCPRLon    int
		oddCPRLat     int
		oddCPRLon     int
		fflag         bool
		expectedLat   float64
		expectedLon   float64
		expectedError int
	}{
		{
			name:          "Surface position - reference at origin",
			reflat:        52.00,
			reflon:        0.00,
			evenCPRLat:    105730,
			evenCPRLon:    9259,
			oddCPRLat:     29693,
			oddCPRLon:     8997,
			fflag:         false,
			expectedLat:   52.209984,
			expectedLon:   0.176601,
			expectedError: 0,
		},
		{
			name:          "Surface position - quadrant test 1",
			reflat:        52.00,
			reflon:        -180.00,
			evenCPRLat:    105730,
			evenCPRLon:    9259,
			oddCPRLat:     29693,
			oddCPRLon:     8997,
			fflag:         false,
			expectedLat:   52.209984,
			expectedLon:   0.176601 - 180.0,
			expectedError: 0,
		},
		{
			name:          "Surface position - quadrant test 2",
			reflat:        52.00,
			reflon:        50.00,
			evenCPRLat:    105730,
			evenCPRLon:    9259,
			oddCPRLat:     29693,
			oddCPRLon:     8997,
			fflag:         false,
			expectedLat:   52.209984,
			expectedLon:   0.176601 + 90.0,
			expectedError: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lat, lon, err := DecodeCPRSurface(tt.reflat, tt.reflon, tt.evenCPRLat, tt.evenCPRLon, tt.oddCPRLat, tt.oddCPRLon, tt.fflag)

			if err != tt.expectedError {
				t.Errorf("DecodeCPRSurface() error = %d, want %d", err, tt.expectedError)
				return
			}

			if tt.expectedError == 0 {
				if math.Abs(lat-tt.expectedLat) > floatTolerance {
					t.Errorf("DecodeCPRSurface() lat = %.6f, want %.6f (diff: %.9f)",
						lat, tt.expectedLat, math.Abs(lat-tt.expectedLat))
				}
				if math.Abs(lon-tt.expectedLon) > floatTolerance {
					t.Errorf("DecodeCPRSurface() lon = %.6f, want %.6f (diff: %.9f)",
						lon, tt.expectedLon, math.Abs(lon-tt.expectedLon))
				}
			}
		})
	}
}

func TestDecodeCPRRelative(t *testing.T) {
	// Test data from cprtests.c - cprRelativeTests
	tests := []struct {
		name          string
		reflat        float64
		reflon        float64
		cprlat        int
		cprlon        int
		fflag         bool
		surface       bool
		expectedLat   float64
		expectedLon   float64
		expectedError int
	}{
		{
			name:          "Relative airborne (even)",
			reflat:        52.00,
			reflon:        0.00,
			cprlat:        80536,
			cprlon:        9432,
			fflag:         false,
			surface:       false,
			expectedLat:   51.686646,
			expectedLon:   0.700156,
			expectedError: 0,
		},
		{
			name:          "Relative airborne (odd)",
			reflat:        52.00,
			reflon:        0.00,
			cprlat:        61720,
			cprlon:        9192,
			fflag:         true,
			surface:       false,
			expectedLat:   51.686763,
			expectedLon:   0.701294,
			expectedError: 0,
		},
		{
			name:          "Relative surface (even)",
			reflat:        52.00,
			reflon:        0.00,
			cprlat:        105730,
			cprlon:        9259,
			fflag:         false,
			surface:       true,
			expectedLat:   52.209984,
			expectedLon:   0.176601,
			expectedError: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lat, lon, err := DecodeCPRRelative(tt.reflat, tt.reflon, tt.cprlat, tt.cprlon, tt.fflag, tt.surface)

			if err != tt.expectedError {
				t.Errorf("DecodeCPRRelative() error = %d, want %d", err, tt.expectedError)
				return
			}

			if tt.expectedError == 0 {
				if math.Abs(lat-tt.expectedLat) > floatTolerance {
					t.Errorf("DecodeCPRRelative() lat = %.6f, want %.6f (diff: %.9f)",
						lat, tt.expectedLat, math.Abs(lat-tt.expectedLat))
				}
				if math.Abs(lon-tt.expectedLon) > floatTolerance {
					t.Errorf("DecodeCPRRelative() lon = %.6f, want %.6f (diff: %.9f)",
						lon, tt.expectedLon, math.Abs(lon-tt.expectedLon))
				}
			}
		})
	}
}

func TestCPRNLFunction(t *testing.T) {
	// Test the NL (Number of Longitude zones) function
	tests := []struct {
		lat      float64
		expected int
	}{
		{0, 59},    // Equator
		{10, 59},   // Just under first boundary
		{11, 58},   // Just over first boundary
		{45, 42},   // Mid latitude
		{86.5, 3},  // < 86.53537 → returns 3
		{86.6, 2},  // >= 86.53537 but < 87 → returns 2
		{87, 1},    // >= 87.0 → returns 1
		{90, 1},    // Pole
		{-45, 42},  // Negative latitude (symmetric)
		{-86.6, 2}, // Negative near pole
	}

	for _, tt := range tests {
		got := cprNLFunction(tt.lat)
		if got != tt.expected {
			t.Errorf("cprNLFunction(%.1f) = %d, want %d", tt.lat, got, tt.expected)
		}
	}
}

func TestCPRModInt(t *testing.T) {
	tests := []struct {
		a, b int
		want int
	}{
		{5, 3, 2},
		{-5, 3, 1}, // Always positive
		{10, 4, 2},
		{-10, 4, 2}, // Always positive
		{0, 5, 0},
	}

	for _, tt := range tests {
		got := cprModInt(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("cprModInt(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestCPRModDouble(t *testing.T) {
	tests := []struct {
		a, b float64
		want float64
	}{
		{5.5, 3.0, 2.5},
		{-5.5, 3.0, 0.5}, // Always positive
		{10.7, 4.0, 2.7},
		{-10.7, 4.0, 1.3}, // Always positive
	}

	for _, tt := range tests {
		got := cprModDouble(tt.a, tt.b)
		if math.Abs(got-tt.want) > floatTolerance {
			t.Errorf("cprModDouble(%.2f, %.2f) = %.2f, want %.2f", tt.a, tt.b, got, tt.want)
		}
	}
}

func BenchmarkDecodeCPRAirborne(b *testing.B) {
	evenCPRLat := 80536
	evenCPRLon := 9432
	oddCPRLat := 61720
	oddCPRLon := 9192

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = DecodeCPRAirborne(evenCPRLat, evenCPRLon, oddCPRLat, oddCPRLon, false)
	}
}

func BenchmarkDecodeCPRRelative(b *testing.B) {
	reflat := 52.0
	reflon := 0.0
	cprlat := 80536
	cprlon := 9432

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = DecodeCPRRelative(reflat, reflon, cprlat, cprlon, false, false)
	}
}

// Example: decode an airborne position
func ExampleDecodeCPRAirborne() {
	// Two CPR messages from the same aircraft
	evenCPRLat := 80536
	evenCPRLon := 9432
	oddCPRLat := 61720
	oddCPRLon := 9192

	// Decode using the even message as most recent
	lat, lon, err := DecodeCPRAirborne(evenCPRLat, evenCPRLon, oddCPRLat, oddCPRLon, false)
	if err == 0 {
		// lat ≈ 51.686646, lon ≈ 0.700156
		_ = lat
		_ = lon
	}
}

// Example: decode a relative position
func ExampleDecodeCPRRelative() {
	// Known reference position
	reflat := 52.0
	reflon := 0.0

	// Single CPR message
	cprlat := 80536
	cprlon := 9432

	// Decode relative to reference (even message, airborne)
	lat, lon, err := DecodeCPRRelative(reflat, reflon, cprlat, cprlon, false, false)
	if err == 0 {
		// lat ≈ 51.686646, lon ≈ 0.700156
		_ = lat
		_ = lon
	}
}
