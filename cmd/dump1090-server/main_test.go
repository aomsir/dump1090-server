package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/aomsir/dump1090-server/modes"
)

func TestModeACDisabledByDefault(t *testing.T) {
	config := &Config{}
	if config.ModeAC {
		t.Error("ModeAC should be false by default (zero-value Config)")
	}
}

func TestModeACFlagParsing(t *testing.T) {
	// Save and restore global state so this test does not interfere with others.
	origArgs := os.Args
	origCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origCommandLine
	}()

	// Without --modeac: default should be false.
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test"}
	config := parseFlags()
	if config.ModeAC {
		t.Error("ModeAC should be false without --modeac flag")
	}

	// With --modeac: should be true.
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test", "--modeac"}
	config = parseFlags()
	if !config.ModeAC {
		t.Error("ModeAC should be true with --modeac flag")
	}
}

func TestParsePortsSupportsListsAndZeroDisable(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    PortList
		wantErr bool
	}{
		{
			name:  "comma separated",
			input: "30004,30104",
			want:  PortList{30004, 30104},
		},
		{
			name:  "whitespace separated",
			input: "30004 30105",
			want:  PortList{30004, 30105},
		},
		{
			name:  "mixed comma and whitespace",
			input: "30004,30104 30105",
			want:  PortList{30004, 30104, 30105},
		},
		{
			name:  "zero disables",
			input: "0",
			want:  PortList{},
		},
		{
			name:    "invalid token",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "port out of range",
			input:   "70000",
			wantErr: true,
		},
		{
			name:    "negative port",
			input:   "-1",
			wantErr: true,
		},
		{
			name:  "single valid port",
			input: "30004",
			want:  PortList{30004},
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "duplicate ports",
			input:   "30004,30004",
			wantErr: true,
		},
		{
			name:    "duplicate ports with whitespace",
			input:   "30004 30004",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePortList(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParsePortList(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParsePortList(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDefaultNetworkingDisabledUntilNetFlag(t *testing.T) {
	config := DefaultConfig()

	if config.EnableNet {
		t.Error("EnableNet should be false by default")
	}
	if config.NetOnly {
		t.Error("NetOnly should be false by default")
	}
	// The runtime decision: default config should not start any network listeners.
	if NetworkServicesEnabled(config) {
		t.Error("default config should not enable network services")
	}
}

func TestNetFlagEnablesNetworking(t *testing.T) {
	origArgs := os.Args
	origCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test", "--net"}
	config, err := ParseFlagsFromSet(flag.CommandLine, os.Args[1:])
	if err != nil {
		t.Fatalf("ParseFlagsFromSet returned error: %v", err)
	}

	if !config.EnableNet {
		t.Error("--net should enable networking (EnableNet=true)")
	}
	if config.NetOnly {
		t.Error("--net should not set NetOnly")
	}
	if !NetworkServicesEnabled(config) {
		t.Error("--net should enable network services")
	}
}

func TestNetOnlyEnablesNetworkingWithoutRTL(t *testing.T) {
	origArgs := os.Args
	origCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test", "--net-only"}
	config, err := ParseFlagsFromSet(flag.CommandLine, os.Args[1:])
	if err != nil {
		t.Fatalf("ParseFlagsFromSet returned error: %v", err)
	}

	if !config.EnableNet {
		t.Error("--net-only should enable networking (EnableNet=true)")
	}
	if !config.NetOnly {
		t.Error("--net-only should set NetOnly=true")
	}
	if !NetworkServicesEnabled(config) {
		t.Error("--net-only should enable network services")
	}
	if config.InputFile != "" {
		t.Error("--net-only should not set an input file")
	}
	if config.Filename != "" {
		t.Error("--net-only should not set a filename")
	}
}

func TestDefaultBeastInputPortsInclude30104(t *testing.T) {
	config := DefaultConfig()

	if !reflect.DeepEqual(config.BeastInPorts, PortList{30004, 30104}) {
		t.Errorf("default BeastInPorts = %v, want [30004 30104]", config.BeastInPorts)
	}
}

func TestNetWithIndividualDisableKeepsSomeListeners(t *testing.T) {
	origArgs := os.Args
	origCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test", "--net", "--no-http", "--no-sbs"}
	config, err := ParseFlagsFromSet(flag.CommandLine, os.Args[1:])
	if err != nil {
		t.Fatalf("ParseFlagsFromSet returned error: %v", err)
	}

	if !NetworkServicesEnabled(config) {
		t.Error("--net with individual disables should still enable some services")
	}
	if !config.DisableHTTP {
		t.Error("--no-http should set DisableHTTP")
	}
	if !config.DisableSBS {
		t.Error("--no-sbs should set DisableSBS")
	}
}

func TestBeastInPortFlagSinglePort(t *testing.T) {
	origArgs := os.Args
	origCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test", "--net", "--beast-in-port", "30004"}
	config, err := ParseFlagsFromSet(flag.CommandLine, os.Args[1:])
	if err != nil {
		t.Fatalf("ParseFlagsFromSet returned error: %v", err)
	}

	if !reflect.DeepEqual(config.BeastInPorts, PortList{30004}) {
		t.Errorf("BeastInPorts with --beast-in-port 30004 = %v, want [30004]", config.BeastInPorts)
	}
}

func TestBeastInPortFlagMultiplePorts(t *testing.T) {
	origArgs := os.Args
	origCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test", "--net", "--beast-in-port", "30004,30104"}
	config, err := ParseFlagsFromSet(flag.CommandLine, os.Args[1:])
	if err != nil {
		t.Fatalf("ParseFlagsFromSet returned error: %v", err)
	}

	if !reflect.DeepEqual(config.BeastInPorts, PortList{30004, 30104}) {
		t.Errorf("BeastInPorts with --beast-in-port 30004,30104 = %v, want [30004 30104]", config.BeastInPorts)
	}
}

func TestBeastInPortFlagZeroDisables(t *testing.T) {
	origArgs := os.Args
	origCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test", "--net", "--beast-in-port", "0"}
	config, err := ParseFlagsFromSet(flag.CommandLine, os.Args[1:])
	if err != nil {
		t.Fatalf("ParseFlagsFromSet returned error: %v", err)
	}

	if len(config.BeastInPorts) != 0 {
		t.Errorf("BeastInPorts with --beast-in-port 0 = %v, want empty", config.BeastInPorts)
	}
	// Runtime should not start any Beast input listener for port 0.
	for _, p := range config.BeastInPorts {
		if p == 0 {
			t.Error("BeastInPorts should not contain port 0")
		}
	}
}

func TestBeastInPortDefaultWhenFlagAbsent(t *testing.T) {
	origArgs := os.Args
	origCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"test", "--net"}
	config, err := ParseFlagsFromSet(flag.CommandLine, os.Args[1:])
	if err != nil {
		t.Fatalf("ParseFlagsFromSet returned error: %v", err)
	}

	if !reflect.DeepEqual(config.BeastInPorts, PortList{30004, 30104}) {
		t.Errorf("default BeastInPorts = %v, want [30004 30104]", config.BeastInPorts)
	}
}

func TestDeadBeastInPortFieldRemoved(t *testing.T) {
	config := DefaultConfig()
	// BeastInPort (single int) should no longer exist; BeastInPorts is the source of truth.
	if config.BeastInPorts == nil {
		t.Error("BeastInPorts should be initialized in DefaultConfig")
	}
}

func TestBeastInPortFlagRejectsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "non-numeric", value: "abc"},
		{name: "out of range", value: "70000"},
		{name: "negative", value: "-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origArgs := os.Args
			origCommandLine := flag.CommandLine
			defer func() {
				os.Args = origArgs
				flag.CommandLine = origCommandLine
			}()

			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
			flag.CommandLine.SetOutput(io.Discard)
			os.Args = []string{"test", "--net", "--beast-in-port", tt.value}
			_, err := ParseFlagsFromSet(flag.CommandLine, os.Args[1:])
			if err == nil {
				t.Fatalf("ParseFlagsFromSet with --beast-in-port %q should return error", tt.value)
			}
		})
	}
}

func TestBeastInPortFlagRejectsDuplicate(t *testing.T) {
	origArgs := os.Args
	origCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	os.Args = []string{"test", "--net", "--beast-in-port", "30004,30004"}
	_, err := ParseFlagsFromSet(flag.CommandLine, os.Args[1:])
	if err == nil {
		t.Fatal("ParseFlagsFromSet with duplicate ports should return error")
	}
}

func TestPortListString(t *testing.T) {
	tests := []struct {
		pl   PortList
		want string
	}{
		{PortList{30004, 30104}, "30004,30104"},
		{PortList{30004}, "30004"},
		{PortList{}, ""},
	}
	for _, tt := range tests {
		if got := tt.pl.String(); got != tt.want {
			t.Errorf("PortList(%v).String() = %q, want %q", []int(tt.pl), got, tt.want)
		}
	}
}

func TestPortListValueSetError(t *testing.T) {
	var pl PortList
	v := portListValue{target: &pl}
	if err := v.Set("abc"); err == nil {
		t.Error("portListValue.Set(\"abc\") should return error")
	}
	if err := v.Set("30004,30004"); err == nil {
		t.Error("portListValue.Set with duplicates should return error")
	}
}

// newTestApp creates a minimal App for HTTP handler tests.
// It sets up a tracker, stats collector, and optionally a JSONWriter.
func newTestApp(config *Config) *App {
	tracker := modes.NewTracker()
	statsCollector := modes.NewStatsCollector()
	app := &App{
		config:         config,
		tracker:        tracker,
		statsCollector: statsCollector,
	}
	return app
}

func TestHTTPHistoryRouteMatchesHistoryFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dump1090-http-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	app := newTestApp(&Config{
		WriteJSON:       tmpDir,
		HistorySize:     10,
		HistoryInterval: 30,
		WriteJSONEvery:  1.0,
	})
	app.jsonWriter = modes.NewJSONWriter(modes.JSONWriterConfig{
		Dir:             tmpDir,
		HistorySize:     10,
		HistoryInterval: 30000,
		Version:         "1.0.0",
	}, app.tracker, &app.totalMessages)
	app.jsonWriter.SetStatsCollector(app.statsCollector)

	// Trigger a history write by calling PeriodicUpdate after initial files
	app.jsonWriter.WriteInitialFiles()

	// The first periodic update after WriteInitialFiles should write history_0.json
	// We need to wait for the history interval to elapse.
	// For testing, use a short interval.
	app.jsonWriter = modes.NewJSONWriter(modes.JSONWriterConfig{
		Dir:             tmpDir,
		HistorySize:     10,
		HistoryInterval: 100, // 100ms
		Version:         "1.0.0",
	}, app.tracker, &app.totalMessages)
	app.jsonWriter.SetStatsCollector(app.statsCollector)
	app.jsonWriter.WriteInitialFiles()

	// Wait for history interval and trigger update
	time.Sleep(150 * time.Millisecond)
	app.jsonWriter.PeriodicUpdate()

	// Now test that the HTTP handler serves history_0.json
	mux := app.newHTTPMux()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/data/history_0.json")
	if err != nil {
		t.Fatalf("GET /data/history_0.json failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Fatal("expected non-empty history JSON")
	}

	// Should contain aircraft list structure
	if !bytes.Contains(body, []byte(`"aircraft"`)) {
		t.Error("history JSON should contain 'aircraft' field")
	}
}

func TestHTTPReceiverJSONRefreshIsMilliseconds(t *testing.T) {
	app := newTestApp(&Config{
		WriteJSON:      "/tmp/test-json",
		WriteJSONEvery: 1.0, // 1 second
		HistorySize:    120,
	})
	app.jsonWriter = modes.NewJSONWriter(modes.JSONWriterConfig{
		Dir:          "/tmp/test-json",
		JSONInterval: 1000, // 1 second in ms
		HistorySize:  120,
		Version:      "1.0.0",
	}, app.tracker, &app.totalMessages)

	mux := app.newHTTPMux()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/data/receiver.json")
	if err != nil {
		t.Fatalf("GET /data/receiver.json failed: %v", err)
	}
	defer resp.Body.Close()

	var receiver map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&receiver); err != nil {
		t.Fatalf("failed to decode receiver.json: %v", err)
	}

	refresh, ok := receiver["refresh"].(float64)
	if !ok {
		t.Fatalf("refresh should be a number, got %T", receiver["refresh"])
	}
	if refresh != 1000.0 {
		t.Errorf("refresh = %v, want 1000 (milliseconds)", refresh)
	}
}

func TestHTTPReceiverJSONRespectsLocationAccuracy(t *testing.T) {
	tests := []struct {
		name     string
		accuracy int
		lat      float64
		lon      float64
		wantLat  *float64
		wantLon  *float64
	}{
		{
			name:     "accuracy 0 omits lat/lon",
			accuracy: 0,
			lat:      51.5074,
			lon:      -0.1278,
			wantLat:  nil,
			wantLon:  nil,
		},
		{
			name:     "accuracy 1 rounds to 2dp",
			accuracy: 1,
			lat:      51.5074,
			lon:      -0.1278,
			wantLat:  float64Ptr(51.51),
			wantLon:  float64Ptr(-0.13),
		},
		{
			name:     "accuracy 2 preserves exact values",
			accuracy: 2,
			lat:      51.5074,
			lon:      -0.1278,
			wantLat:  float64Ptr(51.5074),
			wantLon:  float64Ptr(-0.1278),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(&Config{
				WriteJSON:        "/tmp/test-json",
				WriteJSONEvery:   1.0,
				HistorySize:      120,
				Latitude:         tt.lat,
				Longitude:        tt.lon,
				LocationAccuracy: tt.accuracy,
			})
			app.jsonWriter = modes.NewJSONWriter(modes.JSONWriterConfig{
				Dir:              "/tmp/test-json",
				JSONInterval:     1000,
				HistorySize:      120,
				ReceiverLat:      tt.lat,
				ReceiverLon:      tt.lon,
				LocationAccuracy: tt.accuracy,
				Version:          "1.0.0",
			}, app.tracker, &app.totalMessages)

			mux := app.newHTTPMux()
			ts := httptest.NewServer(mux)
			defer ts.Close()

			resp, err := http.Get(ts.URL + "/data/receiver.json")
			if err != nil {
				t.Fatalf("GET /data/receiver.json failed: %v", err)
			}
			defer resp.Body.Close()

			var receiver map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&receiver); err != nil {
				t.Fatalf("decode: %v", err)
			}

			lat, hasLat := receiver["lat"]
			lon, hasLon := receiver["lon"]

			if tt.wantLat == nil {
				if hasLat {
					t.Errorf("lat should be omitted, got %v", lat)
				}
				if hasLon {
					t.Errorf("lon should be omitted, got %v", lon)
				}
			} else {
				if !hasLat {
					t.Fatal("lat should be present")
				}
				gotLat := lat.(float64)
				if gotLat != *tt.wantLat {
					t.Errorf("lat = %v, want %v", gotLat, *tt.wantLat)
				}
				gotLon := lon.(float64)
				if gotLon != *tt.wantLon {
					t.Errorf("lon = %v, want %v", gotLon, *tt.wantLon)
				}
			}
		})
	}
}

func TestHTTPStatsJSONUsesFullSchema(t *testing.T) {
	app := newTestApp(&Config{})
	app.jsonWriter = modes.NewJSONWriter(modes.JSONWriterConfig{
		Dir:          "/tmp/test-json",
		JSONInterval: 1000,
		HistorySize:  120,
		Version:      "1.0.0",
	}, app.tracker, &app.totalMessages)
	app.jsonWriter.SetStatsCollector(app.statsCollector)

	mux := app.newHTTPMux()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/data/stats.json")
	if err != nil {
		t.Fatalf("GET /data/stats.json failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var stats map[string]interface{}
	if err := json.Unmarshal(body, &stats); err != nil {
		t.Fatalf("stats.json is not valid JSON: %v\nbody: %s", err, string(body))
	}

	requiredKeys := []string{"latest", "last1min", "last5min", "last15min", "total"}
	for _, key := range requiredKeys {
		if _, ok := stats[key]; !ok {
			t.Errorf("stats.json missing required key %q", key)
		}
	}
}

func float64Ptr(f float64) *float64 { return &f }
