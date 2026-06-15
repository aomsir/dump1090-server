package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/aomsir/dump1090-server/modes"
	"github.com/aomsir/dump1090-server/rtlsdr"
)

// openInput opens the input source based on config. Returns an io.ReadCloser.
// If InputFile is "-", it reads from stdin.
func openInput(config *Config) (io.ReadCloser, string, error) {
	if config.InputFile == "-" || config.Filename == "-" {
		return os.Stdin, "stdin", nil
	}

	filename := config.InputFile
	if filename == "" {
		filename = config.Filename
	}

	file, err := os.Open(filename)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open file: %w", err)
	}

	return file, filename, nil
}

// convertSamples converts IQ samples to magnitude based on input format.
// Returns total power. Format must be one of UC8, SC16, SC16Q11 (validated at config time).
func convertSamples(converter *rtlsdr.ConverterState, iq []byte, mag []uint16, format string) float64 {
	switch format {
	case "UC8":
		return converter.ConvertUC8(iq, mag)
	case "SC16":
		return converter.ConvertSC16(iq, mag)
	case "SC16Q11":
		return converter.ConvertSC16Q11(iq, mag)
	default:
		panic("convertSamples: unreachable format (config validation should reject): " + format)
	}
}

// samplesPerByte returns the number of bytes per IQ sample pair for the given format.
func samplesPerByte(format string) int {
	switch format {
	case "UC8":
		return 2 // 1 byte I + 1 byte Q
	case "SC16", "SC16Q11":
		return 4 // 2 bytes I + 2 bytes Q
	default:
		panic("samplesPerByte: unreachable format (config validation should reject): " + format)
	}
}

// processInput reads IQ data from the input source and demodulates Mode S messages.
func (app *App) processInput(reader io.Reader, source string, format string) error {
	log.Printf("Reading from %s (format: %s)", source, format)

	// Create converter with DC filtering based on config
	app.converter = rtlsdr.NewConverter(float64(app.config.SampleRate), app.config.DCFilter)

	// Set up demodulator message handler
	app.demod.SetMessageHandler(app.handleMessage)

	// Allocate buffers
	bytesPerSample := samplesPerByte(format)
	bufSize := 256 * 1024 * bytesPerSample // 256K samples
	iqBuf := make([]byte, bufSize)
	app.magBuf = &modes.MagBuf{
		Data:   make([]uint16, bufSize/bytesPerSample+overlapSamples),
		Length: uint32(bufSize / bytesPerSample),
	}

	// Throttle state
	var throttleStart time.Time
	var samplesRead uint64

	if app.config.Throttle {
		throttleStart = time.Now()
	}

	for {
		select {
		case <-app.ctx.Done():
			return nil
		default:
		}

		n, err := reader.Read(iqBuf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}

		// Align to sample boundary
		n = n - (n % bytesPerSample)
		if n == 0 {
			continue
		}

		// Convert IQ to magnitude based on format
		totalPower := convertSamples(app.converter, iqBuf[:n], app.magBuf.Data[overlapSamples:], format)
		app.magBuf.Length = uint32(n / bytesPerSample)
		app.magBuf.TotalPower = totalPower

		// Demodulate Mode S (overlap region still holds previous tail)
		app.demod.Demodulate2400(app.magBuf)

		// Demodulate Mode A/C (only when --modeac is set)
		if app.config.ModeAC {
			app.demod.Demodulate2400AC(app.magBuf)
		}

		// Save tail of current data as overlap for the next buffer.
		if app.magBuf.Length >= overlapSamples {
			copy(app.magBuf.Data[:overlapSamples], app.magBuf.Data[app.magBuf.Length:])
		}

		// Throttle file replay to realtime speed
		if app.config.Throttle {
			samplesRead += uint64(app.magBuf.Length)
			expectedDuration := time.Duration(float64(samplesRead) / float64(app.config.SampleRate) * float64(time.Second))
			elapsed := time.Since(throttleStart)
			if expectedDuration > elapsed {
				time.Sleep(expectedDuration - elapsed)
			}
		}
	}

	return nil
}
