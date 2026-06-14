package main

import (
	"flag"
	"os"
	"reflect"
	"testing"
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
