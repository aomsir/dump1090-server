package main

import (
	"flag"
	"testing"
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
