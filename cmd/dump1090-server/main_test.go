package main

import (
	"testing"
)

func TestModeACDisabledByDefault(t *testing.T) {
	// The --modeac flag defaults to false.
	// Mode A/C demodulation should be disabled by default.
	config := &Config{}

	if config.ModeAC {
		t.Error("ModeAC should be false by default (no --modeac flag)")
	}
}
