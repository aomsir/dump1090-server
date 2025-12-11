// Package modes implements Mode S message decoding and output formats.
//
// json_writer.go: periodic JSON file output
//
// This is a translation from dump1090-mutability's JSON writing functionality.
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// Licensed under GPL v2+
package modes

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Default values matching C version
const (
	DefaultJSONInterval    = 1000  // 1 second for aircraft.json
	DefaultHistorySize     = 120   // 120 history files (2 hours at 30s interval)
	DefaultHistoryInterval = 30000 // 30 seconds between history snapshots
	DefaultStatsInterval   = 60000 // 60 seconds between stats updates
)

// HistoryEntry holds a single history snapshot
type HistoryEntry struct {
	Content []byte
}

// JSONWriter manages periodic JSON file output
type JSONWriter struct {
	mu sync.Mutex

	// Configuration
	dir             string // Output directory
	jsonInterval    int    // Interval for aircraft.json in milliseconds
	historySize     int    // Number of history entries
	historyInterval int    // Interval for history snapshots in milliseconds
	statsInterval   int    // Interval for stats.json in milliseconds

	// Receiver location (for receiver.json)
	receiverLat         float64
	receiverLon         float64
	locationAccuracy    int  // 0=none, 1=rough (2dp), 2=exact

	// State
	history         []HistoryEntry
	historyNext     int    // Next history index to write
	nextJSON        uint64 // Next time to write aircraft.json
	nextHistory     uint64 // Next time to write history
	nextStats       uint64 // Next time to write stats.json
	receiverWritten bool   // Whether receiver.json has been written

	// References
	tracker       *Tracker
	totalMessages *uint64
	version       string
}

// JSONWriterConfig holds configuration for JSONWriter
type JSONWriterConfig struct {
	Dir             string
	JSONInterval    int // milliseconds, 0 = use default (1000)
	HistorySize     int // 0 = use default (120)
	HistoryInterval int // milliseconds, 0 = use default (30000)
	StatsInterval   int // milliseconds, 0 = use default (60000)
	ReceiverLat     float64
	ReceiverLon     float64
	LocationAccuracy int // 0=none, 1=rough, 2=exact
	Version         string
}

// NewJSONWriter creates a new JSON file writer
func NewJSONWriter(cfg JSONWriterConfig, tracker *Tracker, totalMessages *uint64) *JSONWriter {
	if cfg.JSONInterval <= 0 {
		cfg.JSONInterval = DefaultJSONInterval
	}
	if cfg.JSONInterval < 100 {
		cfg.JSONInterval = 100 // Minimum 100ms like C version
	}
	if cfg.HistorySize <= 0 {
		cfg.HistorySize = DefaultHistorySize
	}
	if cfg.HistoryInterval <= 0 {
		cfg.HistoryInterval = DefaultHistoryInterval
	}
	if cfg.StatsInterval <= 0 {
		cfg.StatsInterval = DefaultStatsInterval
	}
	if cfg.Version == "" {
		cfg.Version = "1.0"
	}

	return &JSONWriter{
		dir:              cfg.Dir,
		jsonInterval:     cfg.JSONInterval,
		historySize:      cfg.HistorySize,
		historyInterval:  cfg.HistoryInterval,
		statsInterval:    cfg.StatsInterval,
		receiverLat:      cfg.ReceiverLat,
		receiverLon:      cfg.ReceiverLon,
		locationAccuracy: cfg.LocationAccuracy,
		version:          cfg.Version,
		history:          make([]HistoryEntry, cfg.HistorySize),
		tracker:          tracker,
		totalMessages:    totalMessages,
	}
}

// writeJsonToFile writes content to a file atomically (write to temp, then rename)
// This matches the C version's writeJsonToFile function
func (w *JSONWriter) writeJsonToFile(filename string, content []byte) error {
	if w.dir == "" {
		return nil
	}

	// Create temp file in same directory
	tmpPath := filepath.Join(w.dir, filename+".tmp")
	finalPath := filepath.Join(w.dir, filename)

	// Write to temp file
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	_, err = f.Write(content)
	if err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write content: %w", err)
	}

	err = f.Close()
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close file: %w", err)
	}

	// Atomic rename
	err = os.Rename(tmpPath, finalPath)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename file: %w", err)
	}

	return nil
}

// generateAircraftJSON generates aircraft.json content matching C version format
func (w *JSONWriter) generateAircraftJSON() []byte {
	now := time.Now()
	nowMs := uint64(now.UnixMilli())
	totalMsgs := atomic.LoadUint64(w.totalMessages)
	aircraft := w.tracker.GetAllAircraft()

	// Build JSON manually to match C version format exactly
	var buf []byte
	buf = append(buf, fmt.Sprintf("{ \"now\" : %.1f,\n", float64(nowMs)/1000.0)...)
	buf = append(buf, fmt.Sprintf("  \"messages\" : %d,\n", totalMsgs)...)
	buf = append(buf, "  \"aircraft\" : ["...)

	first := true
	for _, a := range aircraft {
		// Skip Mode A/C synthetic records
		if a.ModeACFlags&MODEAC_MSG_FLAG != 0 {
			continue
		}

		// Skip aircraft with only 1 message (likely bad decode)
		if a.Messages < 2 {
			continue
		}

		if !first {
			buf = append(buf, ',')
		}
		first = false

		// Address
		if a.Addr&MODES_NON_ICAO_ADDRESS != 0 {
			buf = append(buf, fmt.Sprintf("\n    {\"hex\":\"~%06x\"", a.Addr&0xFFFFFF)...)
		} else {
			buf = append(buf, fmt.Sprintf("\n    {\"hex\":\"%06x\"", a.Addr)...)
		}

		// Address type (if not standard ADSB ICAO)
		if a.AddrType != ADDR_ADSB_ICAO {
			buf = append(buf, fmt.Sprintf(",\"type\":\"%s\"", addrTypeString(a.AddrType))...)
		}

		// Squawk
		if a.SquawkValid.Source != SOURCE_INVALID {
			buf = append(buf, fmt.Sprintf(",\"squawk\":\"%04x\"", a.Squawk)...)
		}

		// Callsign
		if a.CallsignValid.Source != SOURCE_INVALID {
			callsign := callsignToString(a.Callsign)
			if callsign != "" {
				buf = append(buf, fmt.Sprintf(",\"flight\":\"%s\"", jsonEscapeString(callsign))...)
			}
		}

		// Position
		if a.PositionValid.Source != SOURCE_INVALID {
			seenPos := float64(nowMs-a.PositionValid.Updated) / 1000.0
			buf = append(buf, fmt.Sprintf(",\"lat\":%f,\"lon\":%f,\"nucp\":%d,\"seen_pos\":%.1f",
				a.Lat, a.Lon, a.PosNUC, seenPos)...)
		}

		// Altitude
		if a.AirGroundValid.Source >= SOURCE_MODE_S_CHECKED && a.AirGround == AG_GROUND {
			buf = append(buf, ",\"altitude\":\"ground\""...)
		} else if a.AltitudeValid.Source != SOURCE_INVALID {
			buf = append(buf, fmt.Sprintf(",\"altitude\":%d", a.Altitude)...)
		}

		// Vertical rate
		if a.VertRateValid.Source != SOURCE_INVALID {
			buf = append(buf, fmt.Sprintf(",\"vert_rate\":%d", a.VertRate)...)
		}

		// Track
		if a.HeadingValid.Source != SOURCE_INVALID {
			buf = append(buf, fmt.Sprintf(",\"track\":%d", a.Heading)...)
		}

		// Speed
		if a.SpeedValid.Source != SOURCE_INVALID {
			buf = append(buf, fmt.Sprintf(",\"speed\":%d", a.Speed)...)
		}

		// Category
		if a.CategoryValid.Source != SOURCE_INVALID {
			buf = append(buf, fmt.Sprintf(",\"category\":\"%02X\"", a.Category)...)
		}

		// MLAT flags
		buf = append(buf, ",\"mlat\":"...)
		buf = append(buf, w.appendFlags(a, SOURCE_MLAT)...)

		// TISB flags
		buf = append(buf, ",\"tisb\":"...)
		buf = append(buf, w.appendFlags(a, SOURCE_TISB)...)

		// Messages, seen, RSSI
		var sum float64
		for i := 0; i < 8; i++ {
			sum += a.SignalLevel[i]
		}
		rssi := 10 * math.Log10((sum+1e-5)/8)
		seen := float64(nowMs-a.Seen) / 1000.0
		buf = append(buf, fmt.Sprintf(",\"messages\":%d,\"seen\":%.1f,\"rssi\":%.1f}",
			a.Messages, seen, rssi)...)
	}

	buf = append(buf, "\n  ]\n}\n"...)
	return buf
}

// appendFlags builds the mlat/tisb flag array
func (w *JSONWriter) appendFlags(a *Aircraft, source DataSource) []byte {
	var flags []string

	if a.SquawkValid.Source == source {
		flags = append(flags, "squawk")
	}
	if a.CallsignValid.Source == source {
		flags = append(flags, "callsign")
	}
	if a.PositionValid.Source == source {
		flags = append(flags, "lat", "lon")
	}
	if a.AltitudeValid.Source == source {
		flags = append(flags, "altitude")
	}
	if a.VertRateValid.Source == source {
		flags = append(flags, "vert_rate")
	}
	if a.HeadingValid.Source == source {
		flags = append(flags, "track")
	}
	if a.SpeedValid.Source == source {
		flags = append(flags, "speed")
	}
	if a.CategoryValid.Source == source {
		flags = append(flags, "category")
	}

	if len(flags) == 0 {
		return []byte("[]")
	}

	data, _ := json.Marshal(flags)
	return data
}

// generateReceiverJSON generates receiver.json content
func (w *JSONWriter) generateReceiverJSON() []byte {
	w.mu.Lock()
	historyCount := w.getHistoryCount()
	w.mu.Unlock()

	var buf []byte
	buf = append(buf, fmt.Sprintf("{ \"version\" : \"%s\", ", w.version)...)
	buf = append(buf, fmt.Sprintf("\"refresh\" : %.0f, ", float64(w.jsonInterval))...)
	buf = append(buf, fmt.Sprintf("\"history\" : %d", historyCount)...)

	if w.locationAccuracy > 0 && (w.receiverLat != 0 || w.receiverLon != 0) {
		if w.locationAccuracy == 1 {
			// Rough location (2 decimal places ~1km accuracy)
			buf = append(buf, fmt.Sprintf(", \"lat\" : %.2f, \"lon\" : %.2f", w.receiverLat, w.receiverLon)...)
		} else {
			// Exact location
			buf = append(buf, fmt.Sprintf(", \"lat\" : %.6f, \"lon\" : %.6f", w.receiverLat, w.receiverLon)...)
		}
	}

	buf = append(buf, " }\n"...)
	return buf
}

// getHistoryCount returns the number of valid history entries
func (w *JSONWriter) getHistoryCount() int {
	// If the last entry is empty, we haven't wrapped yet
	if w.history[w.historySize-1].Content == nil {
		return w.historyNext
	}
	return w.historySize
}

// generateStatsJSON generates stats.json content matching C version format
func (w *JSONWriter) generateStatsJSON() []byte {
	now := time.Now()
	totalMsgs := atomic.LoadUint64(w.totalMessages)

	var buf []byte
	buf = append(buf, fmt.Sprintf("{ \"latest\" : { \"start\" : %.1f, \"end\" : %.1f",
		float64(now.Unix()), float64(now.Unix()))...)
	buf = append(buf, fmt.Sprintf(", \"messages\" : %d }", totalMsgs)...)
	buf = append(buf, " }\n"...)

	return buf
}

// PeriodicUpdate should be called periodically to write JSON files
// Returns true if any files were written
func (w *JSONWriter) PeriodicUpdate() bool {
	if w.dir == "" {
		return false
	}

	now := uint64(time.Now().UnixMilli())
	written := false

	// Write aircraft.json
	if now >= w.nextJSON {
		content := w.generateAircraftJSON()
		w.writeJsonToFile("aircraft.json", content)
		w.nextJSON = now + uint64(w.jsonInterval)
		written = true
	}

	// Write history and possibly receiver.json
	if now >= w.nextHistory {
		w.mu.Lock()

		// Check if history count will change (for receiver.json update)
		oldCount := w.getHistoryCount()

		// Store history snapshot
		content := w.generateAircraftJSON()
		w.history[w.historyNext].Content = content

		// Write history file
		filename := fmt.Sprintf("history_%d.json", w.historyNext)
		w.writeJsonToFile(filename, content)

		// Advance history index
		w.historyNext = (w.historyNext + 1) % w.historySize
		newCount := w.getHistoryCount()

		w.mu.Unlock()

		// Update receiver.json if history count changed or first write
		if !w.receiverWritten || newCount != oldCount {
			w.writeJsonToFile("receiver.json", w.generateReceiverJSON())
			w.receiverWritten = true
		}

		w.nextHistory = now + uint64(w.historyInterval)
		written = true
	}

	// Write stats.json
	if now >= w.nextStats {
		content := w.generateStatsJSON()
		w.writeJsonToFile("stats.json", content)
		w.nextStats = now + uint64(w.statsInterval)
		written = true
	}

	return written
}

// WriteInitialFiles writes the initial JSON files (receiver.json, aircraft.json)
func (w *JSONWriter) WriteInitialFiles() {
	if w.dir == "" {
		return
	}

	// Ensure directory exists
	os.MkdirAll(w.dir, 0755)

	// Write initial files
	w.writeJsonToFile("receiver.json", w.generateReceiverJSON())
	w.writeJsonToFile("aircraft.json", w.generateAircraftJSON())
	w.receiverWritten = true

	// Initialize timing
	now := uint64(time.Now().UnixMilli())
	w.nextJSON = now + uint64(w.jsonInterval)
	w.nextHistory = now + uint64(w.historyInterval)
	w.nextStats = now + uint64(w.statsInterval)
}

// GetHistoryJSON returns a specific history entry
func (w *JSONWriter) GetHistoryJSON(index int) []byte {
	w.mu.Lock()
	defer w.mu.Unlock()

	if index < 0 || index >= w.historySize {
		return nil
	}
	return w.history[index].Content
}

// GetHistorySize returns the configured history size
func (w *JSONWriter) GetHistorySize() int {
	return w.historySize
}

// GetJSONInterval returns the configured JSON interval in milliseconds
func (w *JSONWriter) GetJSONInterval() int {
	return w.jsonInterval
}

// Helper functions

// callsignToString converts callsign bytes to string
func callsignToString(callsign [9]byte) string {
	end := 0
	for end < 8 && callsign[end] != 0 {
		end++
	}
	// Trim trailing spaces
	for end > 0 && callsign[end-1] == ' ' {
		end--
	}
	return string(callsign[:end])
}

// jsonEscapeString escapes a string for JSON output
func jsonEscapeString(s string) string {
	// For simple cases, just return as-is
	// A full implementation would escape special characters
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			result = append(result, '\\', '"')
		case '\\':
			result = append(result, '\\', '\\')
		case '\n':
			result = append(result, '\\', 'n')
		case '\r':
			result = append(result, '\\', 'r')
		case '\t':
			result = append(result, '\\', 't')
		default:
			result = append(result, c)
		}
	}
	return string(result)
}
