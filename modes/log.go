// Package modes provides logging utilities matching dump1090-mutability.
//
// log.go: timestamped logging functions
//
// This matches the log_with_timestamp() function from dump1090.c
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// Licensed under GPL v2+
package modes

import (
	"fmt"
	"io"
	"os"
	"time"
)

// Logger provides timestamped logging functionality matching C version
type Logger struct {
	output io.Writer
}

// DefaultLogger is the default logger instance writing to stderr
var DefaultLogger = &Logger{output: os.Stderr}

// SetOutput sets the output destination for the default logger
func SetOutput(w io.Writer) {
	DefaultLogger.output = w
}

// LogWithTimestamp prints a timestamped message to stderr
// Matching C version dump1090.c:log_with_timestamp()
func LogWithTimestamp(format string, args ...interface{}) {
	DefaultLogger.LogWithTimestamp(format, args...)
}

// LogWithTimestamp prints a timestamped message
// Format: "Wed Dec 11 17:53:52 2024 CST  message"
func (l *Logger) LogWithTimestamp(format string, args ...interface{}) {
	now := time.Now()
	// Format matching C version: strftime(timebuf, 128, "%c %Z", &local)
	// %c is locale's date and time, %Z is timezone name
	timeStr := now.Format("Mon Jan 02 15:04:05 2006 MST")

	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(l.output, "%s  %s\n", timeStr, msg)
}

// Signal handler log messages (matching C version)
const (
	SigintMessage  = "Caught SIGINT, shutting down.."
	SigtermMessage = "Caught SIGTERM, shutting down.."
)

// LogSigint logs SIGINT signal reception
func LogSigint() {
	LogWithTimestamp(SigintMessage)
}

// LogSigterm logs SIGTERM signal reception
func LogSigterm() {
	LogWithTimestamp(SigtermMessage)
}

// RTL-SDR connection log messages (matching C version)
const (
	RTLSDRLostConnection   = "Warning: lost the connection to the RTLSDR device."
	RTLSDRReconnecting     = "Trying to reconnect to the RTLSDR device.."
	RTLSDRFoundDevices     = "Found %d device(s):"
	RTLSDRDeviceInfo       = "  %d:  %s, %s, SN: %s"
	RTLSDRUsingDevice      = "Using device %d: %s"
	RTLSDRGainReported     = "Gain reported by device: %.2f"
	RTLSDRSampleRateModes  = "Sample rate set to %d Hz."
	RTLSDRCenterFreq       = "Tuned to %.3f MHz."
)

// LogRTLSDRLost logs loss of RTL-SDR connection
func LogRTLSDRLost() {
	LogWithTimestamp(RTLSDRLostConnection)
}

// LogRTLSDRReconnect logs RTL-SDR reconnection attempt
func LogRTLSDRReconnect() {
	LogWithTimestamp(RTLSDRReconnecting)
}

// LogFoundDevices logs number of RTL-SDR devices found
func LogFoundDevices(count int) {
	LogWithTimestamp(RTLSDRFoundDevices, count)
}

// LogDeviceInfo logs RTL-SDR device information
func LogDeviceInfo(index int, vendor, product, serial string) {
	LogWithTimestamp(RTLSDRDeviceInfo, index, vendor, product, serial)
}

// LogUsingDevice logs which RTL-SDR device is being used
func LogUsingDevice(index int, name string) {
	LogWithTimestamp(RTLSDRUsingDevice, index, name)
}

// LogGainReported logs the gain reported by the device
func LogGainReported(gain float64) {
	LogWithTimestamp(RTLSDRGainReported, gain)
}

// LogSampleRate logs the sample rate
func LogSampleRate(rate int) {
	LogWithTimestamp(RTLSDRSampleRateModes, rate)
}

// LogCenterFreq logs the center frequency in MHz
func LogCenterFreq(freqHz uint32) {
	LogWithTimestamp(RTLSDRCenterFreq, float64(freqHz)/1e6)
}

// Network log messages (matching C version net_io.c)
const (
	NetListening       = "%s port %s"
	NetClientConnected = "Client connected: %s port %s"
	NetClientDisconnected = "Client disconnected: %s port %s"
)

// Initialization complete messages
const (
	InitComplete = "Initialization complete, entering main loop."
)

// LogInitComplete logs that initialization is complete
func LogInitComplete() {
	LogWithTimestamp(InitComplete)
}
