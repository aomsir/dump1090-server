// dump1090-server - ADS-B Mode S decoder for RTL-SDR devices
//
// http.go: HTTP handler setup and route registration
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/aomsir/dump1090-server/modes"
)

// newHTTPMux creates the HTTP ServeMux with all routes registered.
// This is separated from runHTTPServer so handlers can be tested with httptest.
func (app *App) newHTTPMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/data/aircraft.json", app.handleAircraftJSON)
	mux.HandleFunc("/data/receiver.json", app.handleReceiverJSON)
	mux.HandleFunc("/data/stats.json", app.handleStatsJSON)
	mux.HandleFunc("/data/", app.handleDataCatchAll)

	return mux
}

func (app *App) handleAircraftJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	data, err := modes.MarshalAircraftJSON(app.tracker, atomic.LoadUint64(&app.totalMessages))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func (app *App) handleReceiverJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	params := modes.ReceiverJSONParams{
		Version:          "1.0.0",
		Lat:              app.config.Latitude,
		Lon:              app.config.Longitude,
		LocationAccuracy: app.config.LocationAccuracy,
	}

	if app.jsonWriter != nil {
		params.RefreshMs = app.jsonWriter.GetJSONInterval()
		params.History = app.jsonWriter.GetHistoryCount()
	} else {
		params.RefreshMs = 1000
		params.History = 120
	}

	receiver := modes.GenerateReceiverJSONWithAccuracy(params)
	data, _ := json.MarshalIndent(receiver, "", "  ")
	w.Write(data)
}

func (app *App) handleStatsJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if app.jsonWriter != nil {
		content := app.jsonWriter.GenerateStatsJSONBytes()
		w.Write(content)
	} else {
		fmt.Fprintf(w, `{"messages":%d,"valid":%d,"decoded":%d}`,
			atomic.LoadUint64(&app.totalMessages),
			atomic.LoadUint64(&app.validMessages),
			atomic.LoadUint64(&app.decodedMessages))
	}
}

// handleDataCatchAll handles /data/ paths that don't match specific routes.
// This is needed for history files like /data/history_0.json which have
// variable indexes.
func (app *App) handleDataCatchAll(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if strings.HasPrefix(path, "/data/history_") && strings.HasSuffix(path, ".json") {
		app.handleHistoryJSON(w, r)
		return
	}

	http.NotFound(w, r)
}

func (app *App) handleHistoryJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if app.jsonWriter == nil {
		http.NotFound(w, r)
		return
	}

	path := r.URL.Path
	var index int
	_, err := fmt.Sscanf(path, "/data/history_%d.json", &index)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	content := app.jsonWriter.GetHistoryJSON(index)
	if content == nil {
		http.NotFound(w, r)
		return
	}

	w.Write(content)
}
