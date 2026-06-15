// view1090 - interactive aircraft display client for Beast TCP streams
//
// Connects to a Beast binary TCP output (typically dump1090-server port 30005),
// decodes Mode S messages, tracks aircraft state, and displays an interactive
// table of observed aircraft. Compatible with dump1090-mutability view1090 flags.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/aomsir/dump1090-server/modes"
	"github.com/aomsir/dump1090-server/ui"
)

// Config holds the view1090 configuration.
type Config struct {
	BeastAddr       string // host:port for Beast TCP input
	InteractiveRows int
	DisplayTTL      int // seconds
	ReconnectDelay  int // seconds between reconnect attempts (0 = no reconnect)
	Metric          bool
	ModeAC          bool
	ShowOnly        uint32
	Quiet           bool
	RTL1090         bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		BeastAddr:       "127.0.0.1:30005",
		InteractiveRows: 22,
		DisplayTTL:      60,
		ReconnectDelay:  5,
	}
}

// ParseFlagsFromSet parses flags from the given FlagSet, returning a Config.
// Testable without touching global flag state.
func ParseFlagsFromSet(fs *flag.FlagSet, args []string) (*Config, error) {
	cfg := DefaultConfig()

	fs.StringVar(&cfg.BeastAddr, "beast", cfg.BeastAddr, "Beast TCP input address (host:port)")
	fs.IntVar(&cfg.InteractiveRows, "interactive-rows", cfg.InteractiveRows, "Number of display rows")
	fs.IntVar(&cfg.DisplayTTL, "interactive-ttl", cfg.DisplayTTL, "Display TTL in seconds")
	fs.IntVar(&cfg.ReconnectDelay, "reconnect", cfg.ReconnectDelay, "Reconnect delay in seconds (0 to disable)")
	fs.BoolVar(&cfg.Metric, "metric", false, "Use metric units")
	fs.BoolVar(&cfg.ModeAC, "modeac", false, "Enable Mode A/C decoding")
	showOnly := fs.String("show-only", "", "Only show this ICAO address (hex)")
	fs.BoolVar(&cfg.Quiet, "quiet", false, "Suppress interactive display")
	fs.BoolVar(&cfg.RTL1090, "rtl1090", false, "Use RTL1090 display format")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if *showOnly != "" {
		v, err := strconv.ParseUint(*showOnly, 16, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid --show-only hex value %q: %w", *showOnly, err)
		}
		cfg.ShowOnly = uint32(v)
	}

	return cfg, nil
}

// App holds the view1090 application state.
type App struct {
	config      *Config
	tracker     *modes.Tracker
	interactive *ui.Interactive
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

func main() {
	config, err := ParseFlagsFromSet(flag.CommandLine, os.Args[1:])
	if err != nil {
		log.Fatalf("flag parse error: %v", err)
	}

	modes.ModesChecksumInit(0)

	app := &App{
		config:  config,
		tracker: modes.NewTracker(),
	}
	app.ctx, app.cancel = context.WithCancel(context.Background())

	if config.InteractiveRows > 0 && !config.Quiet {
		app.interactive = ui.NewInteractive()
		app.interactive.Rows = config.InteractiveRows
		app.interactive.DisplayTTL = config.DisplayTTL * 1000
		app.interactive.Metric = config.Metric
		app.interactive.RTL1090 = config.RTL1090
	}

	if config.Quiet {
		log.SetOutput(io.Discard)
	}

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

// run connects to the Beast TCP source and processes messages.
// It reconnects on connection loss (unless context is cancelled or reconnect is disabled).
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

		log.Printf("Connection lost: %v, reconnecting in %ds...", err, app.config.ReconnectDelay)

		select {
		case <-app.ctx.Done():
			return nil
		case <-time.After(time.Duration(app.config.ReconnectDelay) * time.Second):
		}
	}
}

// connectAndProcess dials the Beast TCP source and processes messages until
// the connection drops or context is cancelled.
// NOTE: Beast parsing loop is shared with faup1090; consider extracting if more commands need it.
func (app *App) connectAndProcess() error {
	conn, err := net.DialTimeout("tcp", app.config.BeastAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", app.config.BeastAddr, err)
	}
	defer conn.Close()

	log.Printf("Connected to %s", app.config.BeastAddr)

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

// shouldTrackMessage reports whether a message should be tracked and displayed.
// When ShowOnly is set, only messages from that ICAO address are tracked.
func shouldTrackMessage(mm *modes.Message, showOnly uint32) bool {
	if showOnly == 0 {
		return true
	}
	return mm.Addr == showOnly
}

// handleMessage decodes a message and updates tracker state.
// Messages not matching the --show-only filter are dropped.
func (app *App) handleMessage(mm *modes.Message) {
	if !shouldTrackMessage(mm, app.config.ShowOnly) {
		return
	}
	if mm.MsgType == 32 {
		app.tracker.UpdateFromModeAC(mm)
		return
	}
	app.tracker.UpdateFromMessage(mm)
}

// runPeriodic updates the interactive display and tracker.
func (app *App) runPeriodic() {
	defer app.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var tickCount int

	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			if app.interactive != nil {
				app.interactive.ShowData(app.tracker)
			}
			tickCount++
			if tickCount >= 10 {
				app.tracker.PeriodicUpdate()
				tickCount = 0
			}
		}
	}
}
