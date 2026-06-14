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
	// Without EnableNet, all network disable flags should effectively block listeners.
	// Verify the flags are not forcing network on.
	if config.NetOnly {
		t.Error("NetOnly should be false by default")
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
	if config.InputFile != "" {
		t.Error("--net-only should not set an input file")
	}
}

func TestDefaultBeastInputPortsInclude30104(t *testing.T) {
	config := DefaultConfig()

	if !reflect.DeepEqual(config.BeastInPorts, PortList{30004, 30104}) {
		t.Errorf("default BeastInPorts = %v, want [30004 30104]", config.BeastInPorts)
	}
}
