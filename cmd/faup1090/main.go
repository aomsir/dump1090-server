// faup1090 - FlightAware ADS-B uploader proxy
//
// Connects to a Beast binary TCP output (typically dump1090-server port 30005),
// decodes and tracks Mode S messages, and writes FlightAware TSV (FATSV) to
// stdout. Compatible with dump1090-mutability faup1090 flags.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/aomsir/dump1090-server/modes"
)

// Config holds the faup1090 configuration.
type Config struct {
	BeastAddr      string // host:port for Beast TCP input
	ReconnectDelay int    // seconds between reconnect attempts (0 = no reconnect)
	ModeAC         bool
	Quiet          bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		BeastAddr:      "127.0.0.1:30005",
		ReconnectDelay: 5,
	}
}

// ParseFlagsFromSet parses flags from the given FlagSet, returning a Config.
// Testable without touching global flag state.
func ParseFlagsFromSet(fs *flag.FlagSet, args []string) (*Config, error) {
	cfg := DefaultConfig()

	fs.StringVar(&cfg.BeastAddr, "beast", cfg.BeastAddr, "Beast TCP input address (host:port)")
	fs.IntVar(&cfg.ReconnectDelay, "reconnect", cfg.ReconnectDelay, "Reconnect delay in seconds (0 to disable)")
	fs.BoolVar(&cfg.ModeAC, "modeac", false, "Enable Mode A/C decoding")
	fs.BoolVar(&cfg.Quiet, "quiet", false, "Suppress stderr logging")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	return cfg, nil
}

// App holds the faup1090 application state.
type App struct {
	config     *Config
	tracker    *modes.Tracker
	fatsv      *modes.FATSVWriter
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	stdoutLock sync.Mutex
}

func main() {
	config, err := ParseFlagsFromSet(flag.CommandLine, os.Args[1:])
	if err != nil {
		log.Fatalf("flag parse error: %v", err)
	}

	if config.Quiet {
		log.SetOutput(log.Writer()) // keep log but suppress in quiet mode
	}

	modes.ModesChecksumInit(0)

	tracker := modes.NewTracker()
	app := &App{
		config:  config,
		tracker: tracker,
		fatsv:   modes.NewFATSVWriter(tracker),
	}
	app.ctx, app.cancel = context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		app.cancel()
	}()

	app.wg.Add(1)
	go app.runPeriodic()

	if err := app.run(); err != nil {
		log.Printf("Error: %v", err)
	}

	app.cancel()
	app.wg.Wait()
}

// run connects to the Beast source, processes messages, and handles reconnects.
func (app *App) run() error {
	for {
		select {
		case <-app.ctx.Done():
			return nil
		default:
		}

		err := app.connectAndProcess()
		if err == nil {
			return nil
		}

		if app.config.ReconnectDelay <= 0 {
			return err
		}

		select {
		case <-app.ctx.Done():
			return nil
		default:
		}

		if !app.config.Quiet {
			log.Printf("Connection lost: %v, reconnecting in %ds...", err, app.config.ReconnectDelay)
		}

		select {
		case <-app.ctx.Done():
			return nil
		case <-time.After(time.Duration(app.config.ReconnectDelay) * time.Second):
		}
	}
}

// connectAndProcess dials the Beast TCP source and processes messages until
// the connection drops or context is cancelled.
func (app *App) connectAndProcess() error {
	conn, err := net.DialTimeout("tcp", app.config.BeastAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", app.config.BeastAddr, err)
	}
	defer conn.Close()

	if !app.config.Quiet {
		log.Printf("Connected to %s", app.config.BeastAddr)
	}

	buf := make([]byte, 4096)
	var data []byte

	for {
		select {
		case <-app.ctx.Done():
			return nil
		default:
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			return err
		}

		data = append(data, buf[:n]...)

		for len(data) > 0 {
			mm, remaining, derr := modes.DecodeBeastWithConfig(data, app.config.ModeAC)
			if derr != nil {
				if len(data) > 1 {
					next := -1
					for i := 1; i < len(data); i++ {
						if data[i] == 0x1A {
							next = i
							break
						}
					}
					if next > 0 {
						data = data[next:]
						continue
					}
				}
				break
			}
			data = remaining
			if mm != nil {
				app.handleMessage(mm)
			}
		}

		if len(data) > 4096 {
			data = data[len(data)-2048:]
		}
	}
}

// handleMessage decodes a message, updates tracker, and writes FATSV events.
func (app *App) handleMessage(mm *modes.Message) {
	if mm.MsgType == 32 {
		app.tracker.UpdateFromModeAC(mm)
		return
	}

	aircraft := app.tracker.UpdateFromMessage(mm)

	// Write FATSV event output for specific message types
	if aircraft != nil {
		fatsvEvent := app.fatsv.WriteFATSVEvent(mm)
		if fatsvEvent != nil {
			app.writeOutput(fatsvEvent)
		}
	}
}

// runPeriodic writes periodic FATSV output and updates tracker state.
func (app *App) runPeriodic() {
	defer app.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			app.tracker.PeriodicUpdate()

			fatsvData := app.fatsv.WriteFATSV()
			if len(fatsvData) > 0 {
				app.writeOutput(fatsvData)
			}
		}
	}
}

// writeOutput writes data to stdout with locking to avoid interleaved writes.
func (app *App) writeOutput(data []byte) {
	app.stdoutLock.Lock()
	defer app.stdoutLock.Unlock()
	os.Stdout.Write(data)
}
