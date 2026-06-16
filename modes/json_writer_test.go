// Package modes provides Mode S message decoding and output formats.
//
// json_writer_test.go: tests for JSON file writing functionality
package modes

import (
	"encoding/json"
	"fmt"
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
		Dir:             t.TempDir(),
		JSONInterval:    500, // 500ms
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
		Dir:          t.TempDir(),
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
		Dir: t.TempDir(),
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
		Dir:     t.TempDir(),
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
		Dir:              t.TempDir(),
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
	tmpDir := t.TempDir()

	tracker := NewTracker()
	var totalMessages uint64

	cfg := JSONWriterConfig{
		Dir:     tmpDir,
		Version: "1.0.0",
	}

	w := NewJSONWriter(cfg, tracker, &totalMessages)

	// Write a test file
	content := []byte(`{"test": "data"}`)
	err := w.writeJsonToFile("test.json", content)
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
	tmpDir := t.TempDir()

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
	tmpDir := t.TempDir()

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
		Dir:         t.TempDir(),
		HistorySize: 5,
	}

	w := NewJSONWriter(cfg, tracker, &totalMessages)

	// Initially should be 0
	if w.getHistoryCount() != 0 {
		t.Errorf("Initial history count = %d, want 0", w.getHistoryCount())
	}
}

func TestWriteInitialFilesWritesStatsJSON(t *testing.T) {
	tmpDir := t.TempDir()

	tracker := NewTracker()
	var totalMessages uint64

	sc := NewStatsCollector()
	w := NewJSONWriter(JSONWriterConfig{
		Dir:     tmpDir,
		Version: "1.0.0",
	}, tracker, &totalMessages)
	w.SetStatsCollector(sc)
	w.WriteInitialFiles()

	if _, err := os.Stat(filepath.Join(tmpDir, "stats.json")); os.IsNotExist(err) {
		t.Error("stats.json not created by WriteInitialFiles")
	}
}

func TestJSONStatsFullSchema(t *testing.T) {
	tracker := NewTracker()
	var totalMessages uint64

	sc := NewStatsCollector()
	w := NewJSONWriter(JSONWriterConfig{
		Dir:     t.TempDir(),
		Version: "1.0.0",
	}, tracker, &totalMessages)
	w.SetStatsCollector(sc)

	statsJSON := w.generateStatsJSON()

	var stats map[string]interface{}
	if err := json.Unmarshal(statsJSON, &stats); err != nil {
		t.Fatalf("stats JSON is not valid: %v\nbody: %s", err, string(statsJSON))
	}

	requiredKeys := []string{"latest", "last1min", "last5min", "last15min", "total"}
	for _, key := range requiredKeys {
		if _, ok := stats[key]; !ok {
			t.Errorf("stats JSON missing required key %q", key)
		}
	}
}

func TestJSONStatsFullSchemaWithoutCollector(t *testing.T) {
	tracker := NewTracker()
	var totalMessages uint64

	// Create JSONWriter without calling SetStatsCollector
	w := NewJSONWriter(JSONWriterConfig{
		Dir:     t.TempDir(),
		Version: "1.0.0",
	}, tracker, &totalMessages)

	statsJSON := w.generateStatsJSON()

	var stats map[string]interface{}
	if err := json.Unmarshal(statsJSON, &stats); err != nil {
		t.Fatalf("stats JSON without collector is not valid JSON: %v\nbody: %s", err, string(statsJSON))
	}

	requiredKeys := []string{"latest", "last1min", "last5min", "last15min", "total"}
	for _, key := range requiredKeys {
		if _, ok := stats[key]; !ok {
			t.Errorf("stats JSON without collector missing required key %q", key)
		}
	}
}

func TestSharedReceiverJSONMilliseconds(t *testing.T) {
	r := GenerateReceiverJSONWithAccuracy(ReceiverJSONParams{
		Version:          "1.0.0",
		RefreshMs:        1000,
		History:          120,
		Lat:              51.5074,
		Lon:              -0.1278,
		LocationAccuracy: 2,
	})

	if r.Refresh != 1000 {
		t.Errorf("Refresh = %v, want 1000 (milliseconds)", r.Refresh)
	}
}

func TestSharedReceiverJSONAccuracy0OmitsLocation(t *testing.T) {
	r := GenerateReceiverJSONWithAccuracy(ReceiverJSONParams{
		Version:          "1.0.0",
		RefreshMs:        1000,
		History:          120,
		Lat:              51.5074,
		Lon:              -0.1278,
		LocationAccuracy: 0,
	})

	if r.Lat != 0 || r.Lon != 0 {
		t.Errorf("accuracy 0 should omit lat/lon, got lat=%v lon=%v", r.Lat, r.Lon)
	}
}

func TestSharedReceiverJSONAccuracy1Rounds(t *testing.T) {
	r := GenerateReceiverJSONWithAccuracy(ReceiverJSONParams{
		Version:          "1.0.0",
		RefreshMs:        1000,
		History:          120,
		Lat:              51.5074,
		Lon:              -0.1278,
		LocationAccuracy: 1,
	})

	if r.Lat != 51.51 {
		t.Errorf("accuracy 1 lat = %v, want 51.51", r.Lat)
	}
	if r.Lon != -0.13 {
		t.Errorf("accuracy 1 lon = %v, want -0.13", r.Lon)
	}
}

func TestSharedReceiverJSONAccuracy2Exact(t *testing.T) {
	r := GenerateReceiverJSONWithAccuracy(ReceiverJSONParams{
		Version:          "1.0.0",
		RefreshMs:        1000,
		History:          120,
		Lat:              51.5074,
		Lon:              -0.1278,
		LocationAccuracy: 2,
	})

	if r.Lat != 51.5074 {
		t.Errorf("accuracy 2 lat = %v, want 51.5074", r.Lat)
	}
	if r.Lon != -0.1278 {
		t.Errorf("accuracy 2 lon = %v, want -0.1278", r.Lon)
	}
}

func TestJSONWriterRestoresHistoryFromDisk(t *testing.T) {
	tmpDir := t.TempDir()
	writeHistoryFile(t, tmpDir, 0, []byte(`{"now":1,"aircraft":[]}`), time.Unix(100, 0))
	writeHistoryFile(t, tmpDir, 1, []byte(`{"now":2,"aircraft":[]}`), time.Unix(200, 0))

	tracker := NewTracker()
	var totalMessages uint64
	w := NewJSONWriter(JSONWriterConfig{
		Dir:         tmpDir,
		HistorySize: 5,
	}, tracker, &totalMessages)

	if got := w.GetHistoryCount(); got != 2 {
		t.Fatalf("history count = %d, want 2", got)
	}
	if got := string(w.GetHistoryJSON(0)); got != `{"now":1,"aircraft":[]}` {
		t.Errorf("history_0 content = %q", got)
	}
	if got := string(w.GetHistoryJSON(1)); got != `{"now":2,"aircraft":[]}` {
		t.Errorf("history_1 content = %q", got)
	}
}

func TestJSONWriterRestoreSetsNextWriteAfterNewestHistoryFile(t *testing.T) {
	tmpDir := t.TempDir()
	writeHistoryFile(t, tmpDir, 0, []byte(`{"now":1,"aircraft":[]}`), time.Unix(300, 0))
	writeHistoryFile(t, tmpDir, 1, []byte(`{"now":2,"aircraft":[]}`), time.Unix(100, 0))

	tracker := NewTracker()
	var totalMessages uint64
	w := NewJSONWriter(JSONWriterConfig{
		Dir:             tmpDir,
		HistorySize:     3,
		HistoryInterval: 100,
	}, tracker, &totalMessages)
	w.nextHistory = 0
	w.PeriodicUpdate()

	if got := string(w.GetHistoryJSON(1)); got == `{"now":2,"aircraft":[]}` {
		t.Error("history_1 should have been overwritten by the next ring write")
	}
	if got := w.GetHistoryJSON(2); got != nil {
		t.Errorf("history_2 should not have been written; got %q", got)
	}
}

func TestJSONWriterRestoreIgnoresOutOfRangeAndEmptyHistoryFiles(t *testing.T) {
	tmpDir := t.TempDir()
	writeHistoryFile(t, tmpDir, 1, []byte(`{"now":2,"aircraft":[]}`), time.Unix(200, 0))
	writeHistoryFile(t, tmpDir, 5, []byte(`{"now":5,"aircraft":[]}`), time.Unix(500, 0))
	if err := os.WriteFile(filepath.Join(tmpDir, "history_2.json"), nil, 0644); err != nil {
		t.Fatalf("failed to write empty history file: %v", err)
	}

	tracker := NewTracker()
	var totalMessages uint64
	w := NewJSONWriter(JSONWriterConfig{
		Dir:         tmpDir,
		HistorySize: 3,
	}, tracker, &totalMessages)

	if got := w.GetHistoryCount(); got != 1 {
		t.Fatalf("history count = %d, want 1", got)
	}
	if got := string(w.GetHistoryJSON(1)); got != `{"now":2,"aircraft":[]}` {
		t.Errorf("history_1 content = %q", got)
	}
	if got := w.GetHistoryJSON(2); got != nil {
		t.Errorf("empty history_2 should not be restored; got %q", got)
	}
}

func TestJSONWriterHistoryCountIncreasesFromRestoredSparseHistory(t *testing.T) {
	tmpDir := t.TempDir()
	writeHistoryFile(t, tmpDir, 4, []byte(`{"now":4,"aircraft":[]}`), time.Unix(400, 0))

	tracker := NewTracker()
	var totalMessages uint64
	w := NewJSONWriter(JSONWriterConfig{
		Dir:             tmpDir,
		HistorySize:     6,
		HistoryInterval: 100,
	}, tracker, &totalMessages)

	if got := w.GetHistoryCount(); got != 1 {
		t.Fatalf("restored history count = %d, want 1", got)
	}

	w.nextHistory = 0
	w.PeriodicUpdate()

	if got := w.GetHistoryCount(); got != 2 {
		t.Fatalf("history count after one new write = %d, want 2", got)
	}

	w.nextHistory = 0
	w.PeriodicUpdate()

	if got := w.GetHistoryCount(); got != 3 {
		t.Fatalf("history count after two new writes = %d, want 3", got)
	}
}

func writeHistoryFile(t *testing.T, dir string, index int, content []byte, modTime time.Time) {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("history_%d.json", index))
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("failed to set mtime for %s: %v", path, err)
	}
}
