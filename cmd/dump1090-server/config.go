package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PortList holds a list of parsed port numbers.
type PortList []int

// ParsePortList parses a comma/whitespace-separated list of port numbers.
// A bare "0" disables the list and returns an empty PortList.
// Valid ports are 1..65535.
func ParsePortList(s string) (PortList, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty port list")
	}

	// Split on commas and whitespace.
	tokens := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})

	if len(tokens) == 1 && tokens[0] == "0" {
		return PortList{}, nil
	}

	ports := make(PortList, 0, len(tokens))
	seen := make(map[int]bool, len(tokens))
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		n, err := strconv.Atoi(tok)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q: %w", tok, err)
		}
		if n < 1 || n > 65535 {
			return nil, fmt.Errorf("port %d out of range 1-65535", n)
		}
		if seen[n] {
			return nil, fmt.Errorf("duplicate port %d", n)
		}
		seen[n] = true
		ports = append(ports, n)
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("no valid ports parsed from %q", s)
	}
	return ports, nil
}

// String returns a comma-separated representation of the port list.
func (pl PortList) String() string {
	parts := make([]string, len(pl))
	for i, p := range pl {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ",")
}

// portListValue implements flag.Value for PortList flags.
type portListValue struct {
	target *PortList
}

func (v portListValue) String() string {
	if v.target == nil {
		return ""
	}
	return v.target.String()
}

func (v portListValue) Set(s string) error {
	pl, err := ParsePortList(s)
	if err != nil {
		return err
	}
	*v.target = pl
	return nil
}

// NetworkServicesEnabled reports whether the config requests any network
// listeners to start. This is the runtime gate: without --net or --net-only,
// no HTTP/raw/beast/SBS/FATSV listeners are started regardless of individual
// disable flags.
func NetworkServicesEnabled(c *Config) bool {
	return c.EnableNet
}

// DefaultConfig returns a Config with upstream dump1090-mutability defaults:
// networking disabled, Beast input ports 30004+30104.
func DefaultConfig() *Config {
	return &Config{
		Frequency:  defaultFrequency,
		SampleRate: defaultSampleRate,

		// Networking disabled by default.
		EnableNet: false,

		// Beast input listens on both 30004 and 30104 by default.
		BeastInPorts: PortList{30004, 30104},

		// Single-value input/output port defaults.
		RawInPort:   defaultRawInPort,
		HTTPPort:    defaultHTTPPort,
		BeastOutPort: defaultBeastOutPort,
		AVROutPort:  defaultAVROutPort,
		SBSPort:     defaultSBSPort,
		FATSVPort:   defaultFATSVPort,

		Gain:     defaultGain,
		MaxRange: 300,

		HeartbeatInterval: defaultHeartbeatInterval,

		WriteJSONEvery:   1.0,
		HistorySize:      120,
		HistoryInterval:  30,
		InteractiveRows:  22,
		InteractiveTTL:   60,
		EnableAGC:        true,
	}
}

// ParseFlagsFromSet parses flags from the given FlagSet and args slice,
// returning a Config without touching the global flag.CommandLine.
// This allows testable flag parsing without side effects.
func ParseFlagsFromSet(fs *flag.FlagSet, args []string) (*Config, error) {
	config := DefaultConfig()

	// Device settings
	fs.IntVar(&config.DeviceIndex, "device", 0, "RTL-SDR device index")
	freq := fs.Uint("freq", defaultFrequency, "Center frequency in Hz")
	rate := fs.Uint("rate", defaultSampleRate, "Sample rate in Hz")
	fs.IntVar(&config.Gain, "gain", defaultGain, "Tuner gain in tenths of dB (-1 for auto)")
	fs.IntVar(&config.PPMCorrection, "ppm", 0, "PPM frequency correction")
	fs.BoolVar(&config.EnableAGC, "agc", true, "Enable RTL2832 AGC")
	fs.BoolVar(&config.EnableBiasTee, "bias-tee", false, "Enable bias tee")

	// Input source
	fs.StringVar(&config.InputFile, "infile", "", "Read samples from file")
	fs.StringVar(&config.Filename, "filename", "", "Read samples from file (alias)")

	// Network output settings
	fs.IntVar(&config.HTTPPort, "http-port", defaultHTTPPort, "HTTP server port")
	fs.IntVar(&config.BeastOutPort, "beast-out-port", defaultBeastOutPort, "Beast output port")
	fs.IntVar(&config.AVROutPort, "avr-out-port", defaultAVROutPort, "AVR output port")
	fs.IntVar(&config.SBSPort, "sbs-port", defaultSBSPort, "SBS output port")
	fs.IntVar(&config.FATSVPort, "fatsv-port", defaultFATSVPort, "FATSV output port")
	fs.BoolVar(&config.DisableHTTP, "no-http", false, "Disable HTTP server")
	fs.BoolVar(&config.DisableBeast, "no-beast-out", false, "Disable Beast output")
	fs.BoolVar(&config.DisableAVR, "no-avr-out", false, "Disable AVR output")
	fs.BoolVar(&config.DisableSBS, "no-sbs", false, "Disable SBS output")
	fs.BoolVar(&config.DisableFATSV, "no-fatsv", false, "Disable FATSV output")

	// Network input settings
	fs.IntVar(&config.RawInPort, "raw-in-port", defaultRawInPort, "Raw/AVR input port")
	fs.Var(portListValue{target: &config.BeastInPorts}, "beast-in-port", "Beast input port(s), comma/whitespace list (0 to disable)")
	fs.BoolVar(&config.DisableRawIn, "no-raw-in", false, "Disable raw input")
	fs.BoolVar(&config.DisableBeastIn, "no-beast-in", false, "Disable Beast input")

	// Network bind address
	fs.StringVar(&config.NetBindAddress, "net-bind-address", "", "Network bind address (empty for all interfaces)")

	// Receiver location
	fs.Float64Var(&config.Latitude, "lat", 0, "Receiver latitude")
	fs.Float64Var(&config.Longitude, "lon", 0, "Receiver longitude")
	fs.Float64Var(&config.MaxRange, "max-range", 300, "Maximum range in nautical miles")

	// Output control
	fs.BoolVar(&config.Quiet, "quiet", false, "Suppress all output except errors")
	fs.BoolVar(&config.Raw, "raw", false, "Output only raw messages (*HEXDATA;)")
	fs.BoolVar(&config.OnlyAddr, "onlyaddr", false, "Output only ICAO addresses, one per line")
	fs.BoolVar(&config.MLAT, "mlat", false, "Include MLAT timestamp in raw output (@TIMESTAMP*HEXDATA;)")
	showOnly := fs.Uint("show-only", 0, "Only show messages from this ICAO address (hex)")

	// Statistics output
	fs.BoolVar(&config.Stats, "stats", false, "Enable periodic statistics display")
	fs.IntVar(&config.StatsEvery, "stats-every", 0, "Display statistics every N seconds (implies --stats)")

	// CRC error correction
	fs.BoolVar(&config.FixCRC, "fix", false, "Fix single-bit CRC errors")
	fs.BoolVar(&config.Aggressive, "aggressive", false, "Fix two-bit CRC errors (more aggressive)")
	fs.BoolVar(&config.Metric, "metric", false, "Use metric units")

	// Interactive mode
	fs.BoolVar(&config.Interactive, "interactive", false, "Interactive mode with TTY display")
	fs.IntVar(&config.InteractiveRows, "interactive-rows", 22, "Maximum number of rows in interactive mode")
	fs.IntVar(&config.InteractiveTTL, "interactive-ttl", 60, "Display TTL in seconds for interactive mode")
	fs.BoolVar(&config.RTL1090, "interactive-rtl1090", false, "Use RTL1090 display format")

	// JSON file output
	fs.StringVar(&config.WriteJSON, "write-json", "/run/dump1090-mutability", "Directory for JSON output files")
	fs.Float64Var(&config.WriteJSONEvery, "write-json-every", 1.0, "Interval in seconds for aircraft.json")
	fs.IntVar(&config.HistorySize, "history-size", 120, "Number of history snapshot files")
	fs.IntVar(&config.HistoryInterval, "history-interval", 30, "Interval in seconds for history snapshots")
	fs.IntVar(&config.LocationAccuracy, "json-location-accuracy", 0, "Location accuracy in JSON (0=none, 1=rough, 2=exact)")

	// Network control
	fs.BoolVar(&config.EnableNet, "net", false, "Enable networking (start HTTP, Beast, AVR, SBS, FATSV servers)")
	fs.BoolVar(&config.NetOnly, "net-only", false, "Network only mode, no RTL-SDR device (implies --net)")

	// Heartbeat
	heartbeatSec := fs.Int("heartbeat-interval", 60, "Output client heartbeat interval in seconds (0 to disable)")

	// Mode A/C demodulation
	fs.BoolVar(&config.ModeAC, "modeac", false, "Enable Mode A/C demodulation (legacy transponders)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	config.Frequency = uint32(*freq)
	config.SampleRate = uint32(*rate)
	config.ShowOnly = uint32(*showOnly)
	config.HeartbeatInterval = time.Duration(*heartbeatSec) * time.Second

	// --net-only implies --net
	if config.NetOnly {
		config.EnableNet = true
	}

	// If --stats-every is set, enable stats
	if config.StatsEvery > 0 {
		config.Stats = true
	}

	return config, nil
}
