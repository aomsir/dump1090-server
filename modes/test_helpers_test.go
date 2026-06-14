package modes

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func mustReadFixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	pathParts := append([]string{"..", "testdata", "compat"}, parts...)
	data, err := os.ReadFile(filepath.Join(pathParts...))
	if err != nil {
		t.Fatalf("read fixture %v: %v", parts, err)
	}
	return data
}

func requireCloseFloat(t *testing.T, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Fatalf("got %f want %f tolerance %f", got, want, tolerance)
	}
}
