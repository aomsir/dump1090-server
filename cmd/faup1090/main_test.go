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
	if cfg.ReconnectDelay != 5 {
		t.Errorf("ReconnectDelay = %d, want 5", cfg.ReconnectDelay)
	}
	if cfg.ModeAC {
		t.Error("ModeAC should be false by default")
	}
	if cfg.Quiet {
		t.Error("Quiet should be false by default")
	}
}

func TestParseFlagsAllOptions(t *testing.T) {
	args := []string{
		"--beast", "192.168.1.10:30104",
		"--reconnect", "10",
		"--modeac",
		"--quiet",
	}
	cfg, err := ParseFlagsFromSet(flag.NewFlagSet("test", flag.ContinueOnError), args)
	if err != nil {
		t.Fatalf("ParseFlagsFromSet error: %v", err)
	}
	if cfg.BeastAddr != "192.168.1.10:30104" {
		t.Errorf("BeastAddr = %q, want %q", cfg.BeastAddr, "192.168.1.10:30104")
	}
	if cfg.ReconnectDelay != 10 {
		t.Errorf("ReconnectDelay = %d, want 10", cfg.ReconnectDelay)
	}
	if !cfg.ModeAC {
		t.Error("ModeAC should be true")
	}
	if !cfg.Quiet {
		t.Error("Quiet should be true")
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
