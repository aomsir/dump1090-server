// Package modes provides Mode S message decoding and output formats.
//
// json_writer_test.go: tests for JSON file writing functionality
package modes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewJSONWriter(t *testing.T) {
	tracker := NewTracker()
	var totalMessages uint64

	cfg := JSONWriterConfig{
		Dir:             "/tmp/test-json",
		JSONInterval:    500,  // 500ms
		HistorySize:     10,
		HistoryInterval: 5000, // 5s
	}

	w := NewJSONWriter(cfg, tracker, &totalMessages)

	if w.jsonInterval != 500 {
		t.Errorf("JSONInterval = %d, want 500", w.jsonInterval)
	}
	if w.historySize != 10 {
		t.Errorf("HistorySize = %d, want 10", w.historySize)
	}
	if w.historyInterval != 5000 {
		t.Errorf("HistoryInterval = %d, want 5000", w.historyInterval)
	}
}

func TestJSONWriterMinInterval(t *testing.T) {
	tracker := NewTracker()
	var totalMessages uint64

	// Test minimum interval enforcement
	cfg := JSONWriterConfig{
		Dir:          "/tmp/test-json",
		JSONInterval: 50, // Below minimum
	}

	w := NewJSONWriter(cfg, tracker, &totalMessages)

	if w.jsonInterval < 100 {
		t.Errorf("JSONInterval = %d, should be at least 100ms", w.jsonInterval)
	}
}

func TestJSONWriterDefaults(t *testing.T) {
	tracker := NewTracker()
	var totalMessages uint64

	// Test default values
	cfg := JSONWriterConfig{
		Dir: "/tmp/test-json",
	}

	w := NewJSONWriter(cfg, tracker, &totalMessages)

	if w.jsonInterval != DefaultJSONInterval {
		t.Errorf("JSONInterval = %d, want %d", w.jsonInterval, DefaultJSONInterval)
	}
	if w.historySize != DefaultHistorySize {
		t.Errorf("HistorySize = %d, want %d", w.historySize, DefaultHistorySize)
	}
	if w.historyInterval != DefaultHistoryInterval {
		t.Errorf("HistoryInterval = %d, want %d", w.historyInterval, DefaultHistoryInterval)
	}
}

func TestJSONWriterGenerateAircraftJSON(t *testing.T) {
	tracker := NewTracker()
	var totalMessages uint64 = 100

	cfg := JSONWriterConfig{
		Dir:     "/tmp/test-json",
		Version: "1.0.0",
	}

	w := NewJSONWriter(cfg, tracker, &totalMessages)

	json := w.generateAircraftJSON()

	// Check JSON structure
	if !strings.Contains(string(json), `"now"`) {
		t.Error("JSON missing 'now' field")
	}
	if !strings.Contains(string(json), `"messages"`) {
		t.Error("JSON missing 'messages' field")
	}
	if !strings.Contains(string(json), `"aircraft"`) {
		t.Error("JSON missing 'aircraft' field")
	}
}

func TestJSONWriterGenerateReceiverJSON(t *testing.T) {
	tracker := NewTracker()
	var totalMessages uint64

	cfg := JSONWriterConfig{
		Dir:              "/tmp/test-json",
		Version:          "1.0.0",
		ReceiverLat:      51.5074,
		ReceiverLon:      -0.1278,
		LocationAccuracy: 2, // Exact
	}

	w := NewJSONWriter(cfg, tracker, &totalMessages)

	json := w.generateReceiverJSON()

	// Check JSON structure
	if !strings.Contains(string(json), `"version"`) {
		t.Error("JSON missing 'version' field")
	}
	if !strings.Contains(string(json), `"refresh"`) {
		t.Error("JSON missing 'refresh' field")
	}
	if !strings.Contains(string(json), `"history"`) {
		t.Error("JSON missing 'history' field")
	}
	if !strings.Contains(string(json), `"lat"`) {
		t.Error("JSON missing 'lat' field")
	}
	if !strings.Contains(string(json), `"lon"`) {
		t.Error("JSON missing 'lon' field")
	}
}

func TestWriteJsonToFile(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "dump1090-json-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tracker := NewTracker()
	var totalMessages uint64

	cfg := JSONWriterConfig{
		Dir:     tmpDir,
		Version: "1.0.0",
	}

	w := NewJSONWriter(cfg, tracker, &totalMessages)

	// Write a test file
	content := []byte(`{"test": "data"}`)
	err = w.writeJsonToFile("test.json", content)
	if err != nil {
		t.Fatalf("writeJsonToFile failed: %v", err)
	}

	// Verify file exists and content matches
	readContent, err := os.ReadFile(filepath.Join(tmpDir, "test.json"))
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}

	if string(readContent) != string(content) {
		t.Errorf("File content mismatch: got %q, want %q", readContent, content)
	}
}

func TestWriteInitialFiles(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "dump1090-json-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tracker := NewTracker()
	var totalMessages uint64

	cfg := JSONWriterConfig{
		Dir:     tmpDir,
		Version: "1.0.0",
	}

	w := NewJSONWriter(cfg, tracker, &totalMessages)
	w.WriteInitialFiles()

	// Check receiver.json exists
	if _, err := os.Stat(filepath.Join(tmpDir, "receiver.json")); os.IsNotExist(err) {
		t.Error("receiver.json not created")
	}

	// Check aircraft.json exists
	if _, err := os.Stat(filepath.Join(tmpDir, "aircraft.json")); os.IsNotExist(err) {
		t.Error("aircraft.json not created")
	}
}

func TestPeriodicUpdate(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "dump1090-json-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tracker := NewTracker()
	var totalMessages uint64

	cfg := JSONWriterConfig{
		Dir:             tmpDir,
		JSONInterval:    100, // 100ms - minimum allowed
		HistoryInterval: 200, // 200ms
		HistorySize:     3,
		Version:         "1.0.0",
	}

	w := NewJSONWriter(cfg, tracker, &totalMessages)
	w.WriteInitialFiles()

	// Wait a bit and call periodic update
	time.Sleep(150 * time.Millisecond)
	written := w.PeriodicUpdate()

	if !written {
		t.Error("PeriodicUpdate should have written files")
	}

	// Wait for history interval
	time.Sleep(150 * time.Millisecond)
	w.PeriodicUpdate()

	// Check history_0.json exists
	if _, err := os.Stat(filepath.Join(tmpDir, "history_0.json")); os.IsNotExist(err) {
		t.Error("history_0.json not created")
	}
}

func TestJSONWriterCallsignToString(t *testing.T) {
	tests := []struct {
		name     string
		callsign [9]byte
		want     string
	}{
		{"full callsign", [9]byte{'U', 'A', 'L', '1', '2', '3', ' ', ' ', 0}, "UAL123"},
		{"short callsign", [9]byte{'A', 'B', 'C', 0, 0, 0, 0, 0, 0}, "ABC"},
		{"empty", [9]byte{0, 0, 0, 0, 0, 0, 0, 0, 0}, ""},
		{"8 chars", [9]byte{'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 0}, "ABCDEFGH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callsignToString(tt.callsign)
			if got != tt.want {
				t.Errorf("callsignToString(%v) = %q, want %q", tt.callsign, got, tt.want)
			}
		})
	}
}

func TestJSONWriterJsonEscapeString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{`with "quotes"`, `with \"quotes\"`},
		{"with\nnewline", "with\\nnewline"},
		{"with\ttab", "with\\ttab"},
		{`back\slash`, `back\\slash`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := jsonEscapeString(tt.input)
			if got != tt.want {
				t.Errorf("jsonEscapeString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestJSONWriterHistoryCount(t *testing.T) {
	tracker := NewTracker()
	var totalMessages uint64

	cfg := JSONWriterConfig{
		Dir:         "/tmp/test-json",
		HistorySize: 5,
	}

	w := NewJSONWriter(cfg, tracker, &totalMessages)

	// Initially should be 0
	if w.getHistoryCount() != 0 {
		t.Errorf("Initial history count = %d, want 0", w.getHistoryCount())
	}
}
