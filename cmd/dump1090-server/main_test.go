package main

import (
	"flag"
	"os"
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
