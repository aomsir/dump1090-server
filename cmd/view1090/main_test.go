package main

import (
	"flag"
	"testing"

	"github.com/aomsir/dump1090-server/modes"
)

func TestParseFlagsDefaults(t *testing.T) {
	cfg, err := ParseFlagsFromSet(flag.NewFlagSet("test", flag.ContinueOnError), nil)
	if err != nil {
		t.Fatalf("ParseFlagsFromSet(nil) error: %v", err)
	}
	if cfg.BeastAddr != "127.0.0.1:30005" {
		t.Errorf("BeastAddr = %q, want %q", cfg.BeastAddr, "127.0.0.1:30005")
	}
	if cfg.InteractiveRows != 22 {
		t.Errorf("InteractiveRows = %d, want 22", cfg.InteractiveRows)
	}
	if cfg.DisplayTTL != 60 {
		t.Errorf("DisplayTTL = %d, want 60", cfg.DisplayTTL)
	}
	if cfg.ReconnectDelay != 5 {
		t.Errorf("ReconnectDelay = %d, want 5", cfg.ReconnectDelay)
	}
	if cfg.Metric {
		t.Error("Metric should be false by default")
	}
	if cfg.ModeAC {
		t.Error("ModeAC should be false by default")
	}
	if cfg.ShowOnly != 0 {
		t.Errorf("ShowOnly = %d, want 0", cfg.ShowOnly)
	}
	if cfg.Quiet {
		t.Error("Quiet should be false by default")
	}
}

func TestParseFlagsAllOptions(t *testing.T) {
	args := []string{
		"--beast", "192.168.1.10:30104",
		"--interactive-rows", "30",
		"--interactive-ttl", "120",
		"--metric",
		"--modeac",
		"--show-only", "ABCDEF",
		"--quiet",
	}
	cfg, err := ParseFlagsFromSet(flag.NewFlagSet("test", flag.ContinueOnError), args)
	if err != nil {
		t.Fatalf("ParseFlagsFromSet error: %v", err)
	}
	if cfg.BeastAddr != "192.168.1.10:30104" {
		t.Errorf("BeastAddr = %q, want %q", cfg.BeastAddr, "192.168.1.10:30104")
	}
	if cfg.InteractiveRows != 30 {
		t.Errorf("InteractiveRows = %d, want 30", cfg.InteractiveRows)
	}
	if cfg.DisplayTTL != 120 {
		t.Errorf("DisplayTTL = %d, want 120", cfg.DisplayTTL)
	}
	if !cfg.Metric {
		t.Error("Metric should be true")
	}
	if !cfg.ModeAC {
		t.Error("ModeAC should be true")
	}
	if cfg.ShowOnly != 0xABCDEF {
		t.Errorf("ShowOnly = 0x%X, want 0xABCDEF", cfg.ShowOnly)
	}
	if !cfg.Quiet {
		t.Error("Quiet should be true")
	}
}

func TestParseFlagsShowOnlyInvalid(t *testing.T) {
	args := []string{"--show-only", "ZZZZZZ"}
	_, err := ParseFlagsFromSet(flag.NewFlagSet("test", flag.ContinueOnError), args)
	if err == nil {
		t.Fatal("ParseFlagsFromSet with invalid --show-only hex should return error")
	}
}

func TestParseFlagsShowOnlyPartialHex(t *testing.T) {
	args := []string{"--show-only", "ABCD"}
	cfg, err := ParseFlagsFromSet(flag.NewFlagSet("test", flag.ContinueOnError), args)
	if err != nil {
		t.Fatalf("ParseFlagsFromSet error: %v", err)
	}
	if cfg.ShowOnly != 0xABCD {
		t.Errorf("ShowOnly = 0x%X, want 0xABCD", cfg.ShowOnly)
	}
}

func TestParseFlagsRTL1090(t *testing.T) {
	args := []string{"--rtl1090"}
	cfg, err := ParseFlagsFromSet(flag.NewFlagSet("test", flag.ContinueOnError), args)
	if err != nil {
		t.Fatalf("ParseFlagsFromSet error: %v", err)
	}
	if !cfg.RTL1090 {
		t.Error("RTL1090 should be true")
	}
}

func TestParseFlagsReconnect(t *testing.T) {
	args := []string{"--reconnect", "10"}
	cfg, err := ParseFlagsFromSet(flag.NewFlagSet("test", flag.ContinueOnError), args)
	if err != nil {
		t.Fatalf("ParseFlagsFromSet error: %v", err)
	}
	if cfg.ReconnectDelay != 10 {
		t.Errorf("ReconnectDelay = %d, want 10", cfg.ReconnectDelay)
	}
}

func TestParseFlagsReconnectZero(t *testing.T) {
	args := []string{"--reconnect", "0"}
	cfg, err := ParseFlagsFromSet(flag.NewFlagSet("test", flag.ContinueOnError), args)
	if err != nil {
		t.Fatalf("ParseFlagsFromSet error: %v", err)
	}
	if cfg.ReconnectDelay != 0 {
		t.Errorf("ReconnectDelay = %d, want 0", cfg.ReconnectDelay)
	}
}

func TestShouldTrackMessage(t *testing.T) {
	tests := []struct {
		name     string
		showOnly uint32
		addr     uint32
		want     bool
	}{
		{"no filter, any addr", 0, 0xABCDEF, true},
		{"no filter, zero addr", 0, 0, true},
		{"filter matches", 0xABCDEF, 0xABCDEF, true},
		{"filter mismatches", 0xABCDEF, 0x123456, false},
		{"filter zero addr", 0xABCDEF, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := &modes.Message{Addr: tt.addr}
			got := shouldTrackMessage(mm, tt.showOnly)
			if got != tt.want {
				t.Errorf("shouldTrackMessage(addr=%06X, showOnly=%06X) = %v, want %v",
					tt.addr, tt.showOnly, got, tt.want)
			}
		})
	}
}

func TestShowOnlyFilterAtRuntime(t *testing.T) {
	cfg, err := ParseFlagsFromSet(flag.NewFlagSet("test", flag.ContinueOnError),
		[]string{"--show-only", "ABCDEF"})
	if err != nil {
		t.Fatalf("ParseFlagsFromSet error: %v", err)
	}

	app := &App{
		config:  cfg,
		tracker: modes.NewTracker(),
	}

	matching := &modes.Message{
		Addr:          0xABCDEF,
		MsgType:       17,
		MsgBits:       modes.MODES_SHORT_MSG_BITS,
		CallsignValid: true,
	}
	app.handleMessage(matching)

	if app.tracker.GetAircraft(0xABCDEF) == nil {
		t.Error("aircraft matching --show-only should be tracked")
	}

	nonMatching := &modes.Message{
		Addr:          0x123456,
		MsgType:       17,
		MsgBits:       modes.MODES_SHORT_MSG_BITS,
		CallsignValid: true,
	}
	app.handleMessage(nonMatching)

	if app.tracker.GetAircraft(0x123456) != nil {
		t.Error("aircraft not matching --show-only should NOT be tracked")
	}
}
