// dump1090go - ADS-B Mode S decoder for RTL-SDR devices
//
// A Go implementation of dump1090-mutability, providing:
// - RTL-SDR device support
// - 2.4MHz Mode S demodulation
// - Aircraft tracking
// - Network output (HTTP/JSON, Beast, AVR, SBS)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/aomsir/dump1090-mutability-go/internal/modes"
	"github.com/aomsir/dump1090-mutability-go/internal/rtlsdr"
	"github.com/aomsir/dump1090-mutability-go/internal/ui"
)

const (
	// Default values
	defaultFrequency  = 1090000000 // 1090 MHz
	defaultSampleRate = 2400000    // 2.4 MHz
	defaultGain       = -1         // Auto gain
	defaultHTTPPort   = 8080

	// Network output ports
	defaultBeastOutPort = 30005
	defaultAVROutPort   = 30002
	defaultSBSPort      = 30003

	// Network input ports
	defaultRawInPort   = 30001
	defaultBeastInPort = 30004

	// Buffer sizes
	asyncBufNum = 12
	asyncBufLen = 256 * 1024 // 256K samples per buffer

	// Preamble and message sizes (in samples at 2.4MHz)
	preambleSamples = 16  // 8us * 2 samples/us
	longMsgSamples  = 240 // 112 bits * 12/5 samples/bit
	overlapSamples  = preambleSamples + longMsgSamples
)

// Config holds the application configuration
type Config struct {
	// Device settings
	DeviceIndex   int
	Frequency     uint32
	SampleRate    uint32
	Gain          int
	PPMCorrection int
	EnableAGC     bool
	EnableBiasTee bool

	// Input source
	InputFile string
	Filename  string

	// Network output settings
	HTTPPort     int
	BeastOutPort int
	AVROutPort   int
	SBSPort      int
	DisableHTTP  bool
	DisableBeast bool
	DisableAVR   bool
	DisableSBS   bool

	// Network input settings
	RawInPort      int
	BeastInPort    int
	DisableRawIn   bool
	DisableBeastIn bool

	// Receiver location
	Latitude  float64
	Longitude float64
	MaxRange  float64

	// Other settings
	Quiet      bool
	FixCRC     bool
	Aggressive bool
	Metric     bool

	// Net-only mode
	NetOnly bool

	// Interactive mode settings
	Interactive     bool
	InteractiveRows int
	InteractiveTTL  int
	RTL1090         bool

	// JSON file output settings
	WriteJSON         string // Directory for JSON output
	WriteJSONEvery    float64 // Interval in seconds for aircraft.json
	HistorySize       int    // Number of history files
	HistoryInterval   int    // Interval in seconds for history files
	LocationAccuracy  int    // 0=none, 1=rough, 2=exact
}

// App holds the application state
type App struct {
	config      *Config
	tracker     *modes.Tracker
	demod       *modes.Demodulator
	converter   *rtlsdr.ConverterState
	device      *rtlsdr.Device
	interactive *ui.Interactive
	jsonWriter  *modes.JSONWriter

	// Statistics
	totalMessages   uint64
	validMessages   uint64
	decodedMessages uint64

	// Network clients
	beastClients sync.Map
	avrClients   sync.Map
	sbsClients   sync.Map

	// Buffers
	magBuf *modes.MagBuf

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func main() {
	config := parseFlags()

	// Initialize CRC tables
	errorBits := 0
	if config.FixCRC {
		errorBits = 1
		if config.Aggressive {
			errorBits = 2
		}
	}
	modes.ModesChecksumInit(errorBits)

	// Initialize magnitude lookup table
	rtlsdr.InitMagLUT()

	// Create application
	app := &App{
		config:  config,
		tracker: modes.NewTracker(),
		demod:   modes.NewDemodulator(),
	}

	// Initialize interactive mode if enabled
	if config.Interactive {
		app.interactive = ui.NewInteractive()
		app.interactive.Rows = config.InteractiveRows
		app.interactive.DisplayTTL = config.InteractiveTTL * 1000 // Convert seconds to milliseconds
		app.interactive.RTL1090 = config.RTL1090
		app.interactive.Metric = config.Metric

		// Suppress log output in interactive mode (like C version)
		log.SetOutput(io.Discard)
	}

	// Initialize JSON file writer if directory specified
	if config.WriteJSON != "" {
		app.jsonWriter = modes.NewJSONWriter(modes.JSONWriterConfig{
			Dir:              config.WriteJSON,
			JSONInterval:     int(config.WriteJSONEvery * 1000), // Convert seconds to milliseconds
			HistorySize:      config.HistorySize,
			HistoryInterval:  config.HistoryInterval * 1000, // Convert seconds to milliseconds
			ReceiverLat:      config.Latitude,
			ReceiverLon:      config.Longitude,
			LocationAccuracy: config.LocationAccuracy,
			Version:          "1.0.0",
		}, app.tracker, &app.totalMessages)

		// Write initial files
		app.jsonWriter.WriteInitialFiles()
		log.Printf("JSON output enabled: %s (every %.1fs, %d history files)",
			config.WriteJSON, config.WriteJSONEvery, config.HistorySize)
	}

	// Set receiver location if provided
	if config.Latitude != 0 || config.Longitude != 0 {
		app.tracker.SetReceiverLocation(config.Latitude, config.Longitude)
	}
	if config.MaxRange > 0 {
		app.tracker.SetMaxRange(config.MaxRange)
	}

	// Create context for graceful shutdown
	app.ctx, app.cancel = context.WithCancel(context.Background())

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutting down...")
		app.cancel()
	}()

	// Start network output services
	if !config.DisableHTTP {
		app.wg.Add(1)
		go app.runHTTPServer()
	}
	if !config.DisableBeast {
		app.wg.Add(1)
		go app.runBeastOutputServer()
	}
	if !config.DisableAVR {
		app.wg.Add(1)
		go app.runAVROutputServer()
	}
	if !config.DisableSBS {
		app.wg.Add(1)
		go app.runSBSServer()
	}

	// Start network input services
	if !config.DisableRawIn {
		app.wg.Add(1)
		go app.runRawInputServer()
	}
	if !config.DisableBeastIn {
		app.wg.Add(1)
		go app.runBeastInputServer()
	}

	// Start periodic tasks
	app.wg.Add(1)
	go app.runPeriodicTasks()

	// Run main processing
	var err error
	if config.NetOnly {
		// Net-only mode: just wait for shutdown
		log.Println("Running in network-only mode")
		<-app.ctx.Done()
	} else if config.InputFile != "" {
		err = app.processFile(config.InputFile)
	} else if config.Filename != "" {
		err = app.processFile(config.Filename)
	} else {
		err = app.processRTLSDR()
	}

	if err != nil {
		log.Printf("Error: %v", err)
	}

	// Wait for shutdown
	app.cancel()
	app.wg.Wait()

	// Print statistics
	log.Printf("Statistics: %d total messages, %d valid, %d decoded",
		app.totalMessages, app.validMessages, app.decodedMessages)
}

func parseFlags() *Config {
	config := &Config{
		Frequency:  defaultFrequency,
		SampleRate: defaultSampleRate,
	}

	// Device settings
	flag.IntVar(&config.DeviceIndex, "device", 0, "RTL-SDR device index")
	freq := flag.Uint("freq", defaultFrequency, "Center frequency in Hz")
	rate := flag.Uint("rate", defaultSampleRate, "Sample rate in Hz")
	flag.IntVar(&config.Gain, "gain", defaultGain, "Tuner gain in tenths of dB (-1 for auto)")
	flag.IntVar(&config.PPMCorrection, "ppm", 0, "PPM frequency correction")
	flag.BoolVar(&config.EnableAGC, "agc", true, "Enable RTL2832 AGC")
	flag.BoolVar(&config.EnableBiasTee, "bias-tee", false, "Enable bias tee")

	// Input source
	flag.StringVar(&config.InputFile, "infile", "", "Read samples from file")
	flag.StringVar(&config.Filename, "filename", "", "Read samples from file (alias)")

	// Network output settings
	flag.IntVar(&config.HTTPPort, "http-port", defaultHTTPPort, "HTTP server port")
	flag.IntVar(&config.BeastOutPort, "beast-out-port", defaultBeastOutPort, "Beast output port")
	flag.IntVar(&config.AVROutPort, "avr-out-port", defaultAVROutPort, "AVR output port")
	flag.IntVar(&config.SBSPort, "sbs-port", defaultSBSPort, "SBS output port")
	flag.BoolVar(&config.DisableHTTP, "no-http", false, "Disable HTTP server")
	flag.BoolVar(&config.DisableBeast, "no-beast-out", false, "Disable Beast output")
	flag.BoolVar(&config.DisableAVR, "no-avr-out", false, "Disable AVR output")
	flag.BoolVar(&config.DisableSBS, "no-sbs", false, "Disable SBS output")

	// Network input settings
	flag.IntVar(&config.RawInPort, "raw-in-port", defaultRawInPort, "Raw/AVR input port")
	flag.IntVar(&config.BeastInPort, "beast-in-port", defaultBeastInPort, "Beast input port")
	flag.BoolVar(&config.DisableRawIn, "no-raw-in", false, "Disable raw input")
	flag.BoolVar(&config.DisableBeastIn, "no-beast-in", false, "Disable Beast input")

	// Receiver location
	flag.Float64Var(&config.Latitude, "lat", 0, "Receiver latitude")
	flag.Float64Var(&config.Longitude, "lon", 0, "Receiver longitude")
	flag.Float64Var(&config.MaxRange, "max-range", 300, "Maximum range in nautical miles")

	// Other settings
	flag.BoolVar(&config.Quiet, "quiet", false, "Suppress output")
	flag.BoolVar(&config.FixCRC, "fix", false, "Fix single-bit CRC errors")
	flag.BoolVar(&config.Aggressive, "aggressive", false, "Fix two-bit CRC errors")
	flag.BoolVar(&config.Metric, "metric", false, "Use metric units")

	// Interactive mode
	flag.BoolVar(&config.Interactive, "interactive", false, "Interactive mode with TTY display")
	flag.IntVar(&config.InteractiveRows, "interactive-rows", 22, "Maximum number of rows in interactive mode")
	flag.IntVar(&config.InteractiveTTL, "interactive-ttl", 60, "Display TTL in seconds for interactive mode")
	flag.BoolVar(&config.RTL1090, "interactive-rtl1090", false, "Use RTL1090 display format")

	// JSON file output
	flag.StringVar(&config.WriteJSON, "write-json", "", "Directory for JSON output files")
	flag.Float64Var(&config.WriteJSONEvery, "write-json-every", 1.0, "Interval in seconds for aircraft.json")
	flag.IntVar(&config.HistorySize, "history-size", 120, "Number of history snapshot files")
	flag.IntVar(&config.HistoryInterval, "history-interval", 30, "Interval in seconds for history snapshots")
	flag.IntVar(&config.LocationAccuracy, "json-location-accuracy", 0, "Location accuracy in JSON (0=none, 1=rough, 2=exact)")

	// Net-only mode
	flag.BoolVar(&config.NetOnly, "net-only", false, "Network only mode, no RTL-SDR device")

	flag.Parse()

	config.Frequency = uint32(*freq)
	config.SampleRate = uint32(*rate)

	return config
}

func (app *App) processRTLSDR() error {
	cfg := rtlsdr.DeviceConfig{
		Index:         app.config.DeviceIndex,
		Frequency:     app.config.Frequency,
		SampleRate:    app.config.SampleRate,
		Gain:          app.config.Gain,
		PPMCorrection: app.config.PPMCorrection,
		EnableAGC:     app.config.EnableAGC,
		EnableBiasTee: app.config.EnableBiasTee,
	}

	var err error
	app.device, err = rtlsdr.OpenWithConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to open RTL-SDR: %w", err)
	}
	defer app.device.Close()

	log.Printf("RTL-SDR device opened: %s", rtlsdr.GetDeviceName(app.config.DeviceIndex))
	log.Printf("Frequency: %.3f MHz, Sample rate: %.3f MHz",
		float64(app.config.Frequency)/1e6, float64(app.config.SampleRate)/1e6)

	if app.config.Gain >= 0 {
		log.Printf("Gain: %.1f dB", float64(app.config.Gain)/10.0)
	} else {
		log.Printf("Gain: Auto")
	}

	// Create converter
	app.converter = rtlsdr.NewConverter(float64(app.config.SampleRate), true)

	// Set up demodulator message handler
	app.demod.SetMessageHandler(app.handleMessage)

	// Allocate magnitude buffer
	app.magBuf = &modes.MagBuf{
		Data:   make([]uint16, asyncBufLen+overlapSamples),
		Length: asyncBufLen,
	}

	// Start async reading
	errChan := make(chan error, 1)
	go func() {
		errChan <- app.device.ReadAsync(app.handleSamples, asyncBufNum, asyncBufLen*2)
	}()

	// Wait for completion or cancellation
	select {
	case <-app.ctx.Done():
		app.device.CancelAsync()
		return nil
	case err := <-errChan:
		return err
	}
}

func (app *App) handleSamples(iq []byte) {
	// Convert IQ to magnitude
	rtlsdr.ConvertUC8NoDC(iq, app.magBuf.Data[overlapSamples:])

	// Copy overlap from previous buffer
	copy(app.magBuf.Data[:overlapSamples], app.magBuf.Data[app.magBuf.Length:])

	// Demodulate Mode S
	app.demod.Demodulate2400(app.magBuf)

	// Demodulate Mode A/C (older transponders)
	app.demod.Demodulate2400AC(app.magBuf)
}

func (app *App) handleMessage(mm *modes.Message) {
	atomic.AddUint64(&app.totalMessages, 1)

	// Message is already decoded by the demodulator
	// Just count valid messages
	atomic.AddUint64(&app.validMessages, 1)

	// Update tracker
	aircraft := app.tracker.UpdateFromMessage(mm)
	if aircraft != nil {
		atomic.AddUint64(&app.decodedMessages, 1)

		// Add valid ICAO address to filter for future scoring
		if app.demod != nil {
			app.demod.AddKnownICAO(mm.Addr)
		}
	}

	// Broadcast to network clients
	app.broadcastMessage(mm, aircraft)

	// Log if not quiet
	if !app.config.Quiet && mm.MsgType == 17 {
		app.logMessage(mm, aircraft)
	}
}

func (app *App) broadcastMessage(mm *modes.Message, aircraft *modes.Aircraft) {
	// Beast output
	if !app.config.DisableBeast {
		beastData := modes.EncodeBeast(mm)
		app.beastClients.Range(func(key, value interface{}) bool {
			conn := value.(net.Conn)
			conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
			conn.Write(beastData)
			return true
		})
	}

	// AVR output
	if !app.config.DisableAVR {
		avrData := modes.EncodeAVR(mm, true)
		app.avrClients.Range(func(key, value interface{}) bool {
			conn := value.(net.Conn)
			conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
			conn.Write([]byte(avrData))
			return true
		})
	}

	// SBS output
	if !app.config.DisableSBS {
		sbsData := modes.EncodeSBS(mm, aircraft)
		if sbsData != "" {
			app.sbsClients.Range(func(key, value interface{}) bool {
				conn := value.(net.Conn)
				conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
				conn.Write([]byte(sbsData))
				return true
			})
		}
	}
}

func (app *App) logMessage(mm *modes.Message, aircraft *modes.Aircraft) {
	addr := fmt.Sprintf("%06X", mm.Addr)

	var info string
	if mm.CallsignValid {
		info = fmt.Sprintf("callsign=%s", string(mm.Callsign[:8]))
	} else if mm.AltitudeValid {
		info = fmt.Sprintf("alt=%d", mm.Altitude)
	} else if mm.SpeedValid {
		info = fmt.Sprintf("spd=%d hdg=%d", mm.Speed, mm.Heading)
	} else if mm.CPRValid {
		info = fmt.Sprintf("CPR lat=%d lon=%d", mm.CPRLat, mm.CPRLon)
	}

	log.Printf("[%s] DF%d %s", addr, mm.MsgType, info)
}

func (app *App) processFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	log.Printf("Reading from file: %s", filename)

	// Create converter
	app.converter = rtlsdr.NewConverter(float64(app.config.SampleRate), true)

	// Set up demodulator message handler
	app.demod.SetMessageHandler(app.handleMessage)

	// Allocate buffers
	bufSize := 256 * 1024 * 2 // 256K samples
	iqBuf := make([]byte, bufSize)
	app.magBuf = &modes.MagBuf{
		Data:   make([]uint16, bufSize/2+overlapSamples),
		Length: uint32(bufSize / 2),
	}

	for {
		select {
		case <-app.ctx.Done():
			return nil
		default:
		}

		n, err := file.Read(iqBuf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}

		// Convert IQ to magnitude
		rtlsdr.ConvertUC8NoDC(iqBuf[:n], app.magBuf.Data[overlapSamples:])
		app.magBuf.Length = uint32(n / 2)

		// Copy overlap from previous buffer
		copy(app.magBuf.Data[:overlapSamples], app.magBuf.Data[app.magBuf.Length:])

		// Demodulate Mode S
		app.demod.Demodulate2400(app.magBuf)

		// Demodulate Mode A/C (older transponders)
		app.demod.Demodulate2400AC(app.magBuf)
	}

	return nil
}

func (app *App) runHTTPServer() {
	defer app.wg.Done()

	mux := http.NewServeMux()

	// Aircraft JSON endpoint
	mux.HandleFunc("/data/aircraft.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		data, err := modes.MarshalAircraftJSON(app.tracker, atomic.LoadUint64(&app.totalMessages))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(data)
	})

	// Receiver JSON endpoint
	mux.HandleFunc("/data/receiver.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// If JSON writer is enabled, use its receiver.json format
		if app.jsonWriter != nil {
			historyCount := 0
			for i := 0; i < app.jsonWriter.GetHistorySize(); i++ {
				if app.jsonWriter.GetHistoryJSON(i) != nil {
					historyCount++
				}
			}
			receiver := modes.GenerateReceiverJSON("1.0.0",
				float64(app.jsonWriter.GetJSONInterval())/1000.0,
				historyCount, app.config.Latitude, app.config.Longitude)
			data, _ := json.MarshalIndent(receiver, "", "  ")
			w.Write(data)
		} else {
			receiver := modes.GenerateReceiverJSON("1.0.0", 1.0, 120, app.config.Latitude, app.config.Longitude)
			data, _ := json.MarshalIndent(receiver, "", "  ")
			w.Write(data)
		}
	})

	// Stats endpoint
	mux.HandleFunc("/data/stats.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		fmt.Fprintf(w, `{"messages":%d,"valid":%d,"decoded":%d}`,
			atomic.LoadUint64(&app.totalMessages),
			atomic.LoadUint64(&app.validMessages),
			atomic.LoadUint64(&app.decodedMessages))
	})

	// History JSON endpoints (if JSON writer is enabled)
	mux.HandleFunc("/data/history_", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		if app.jsonWriter == nil {
			http.NotFound(w, r)
			return
		}

		// Parse history index from URL (e.g., /data/history_0.json)
		path := r.URL.Path
		var index int
		_, err := fmt.Sscanf(path, "/data/history_%d.json", &index)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		// Get history content
		content := app.jsonWriter.GetHistoryJSON(index)
		if content == nil {
			http.NotFound(w, r)
			return
		}

		w.Write(content)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", app.config.HTTPPort),
		Handler: mux,
	}

	go func() {
		<-app.ctx.Done()
		server.Shutdown(context.Background())
	}()

	log.Printf("HTTP server listening on port %d", app.config.HTTPPort)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Printf("HTTP server error: %v", err)
	}
}

func (app *App) runBeastOutputServer() {
	defer app.wg.Done()
	app.runTCPServer("Beast output", app.config.BeastOutPort, &app.beastClients)
}

func (app *App) runAVROutputServer() {
	defer app.wg.Done()
	app.runTCPServer("AVR output", app.config.AVROutPort, &app.avrClients)
}

func (app *App) runSBSServer() {
	defer app.wg.Done()
	app.runTCPServer("SBS", app.config.SBSPort, &app.sbsClients)
}

func (app *App) runTCPServer(name string, port int, clients *sync.Map) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Printf("%s server error: %v", name, err)
		return
	}
	defer listener.Close()

	log.Printf("%s server listening on port %d", name, port)

	go func() {
		<-app.ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-app.ctx.Done():
				return
			default:
				log.Printf("%s accept error: %v", name, err)
				continue
			}
		}

		clientID := conn.RemoteAddr().String()
		clients.Store(clientID, conn)
		log.Printf("%s client connected: %s", name, clientID)

		go func(id string, c net.Conn) {
			defer func() {
				clients.Delete(id)
				c.Close()
				log.Printf("%s client disconnected: %s", name, id)
			}()

			// Keep connection alive until closed or context cancelled
			buf := make([]byte, 1024)
			for {
				c.SetReadDeadline(time.Now().Add(60 * time.Second))
				_, err := c.Read(buf)
				if err != nil {
					return
				}
			}
		}(clientID, conn)
	}
}

func (app *App) runPeriodicTasks() {
	defer app.wg.Done()

	// Use 100ms ticker for interactive mode and JSON writing
	// Both need sub-second updates
	var tickInterval time.Duration
	if app.interactive != nil || app.jsonWriter != nil {
		tickInterval = 100 * time.Millisecond
	} else {
		tickInterval = 1 * time.Second
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	var tickCount int
	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			// Update interactive display if enabled
			if app.interactive != nil {
				app.interactive.ShowData(app.tracker)
			}

			// Update JSON files if enabled
			if app.jsonWriter != nil {
				app.jsonWriter.PeriodicUpdate()
			}

			// Update tracker every ~1 second (remove stale aircraft)
			tickCount++
			if (app.interactive == nil && app.jsonWriter == nil) || tickCount >= 10 {
				app.tracker.PeriodicUpdate()

				// Expire old ICAO filter entries (every second is enough,
				// actual expiry happens every 60 seconds internally)
				if app.demod != nil {
					if filter := app.demod.GetICAOFilter(); filter != nil {
						filter.Expire()
					}
				}

				tickCount = 0
			}
		}
	}
}

// runRawInputServer listens for raw/AVR format messages on port 30001
func (app *App) runRawInputServer() {
	defer app.wg.Done()

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", app.config.RawInPort))
	if err != nil {
		log.Printf("Raw input server error: %v", err)
		return
	}
	defer listener.Close()

	log.Printf("Raw input server listening on port %d", app.config.RawInPort)

	go func() {
		<-app.ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-app.ctx.Done():
				return
			default:
				log.Printf("Raw input accept error: %v", err)
				continue
			}
		}

		log.Printf("Raw input client connected: %s", conn.RemoteAddr())
		go app.handleRawInputConnection(conn)
	}
}

// handleRawInputConnection handles a single raw/AVR input connection
func (app *App) handleRawInputConnection(conn net.Conn) {
	defer conn.Close()
	defer log.Printf("Raw input client disconnected: %s", conn.RemoteAddr())

	reader := make([]byte, 4096)
	var buffer []byte

	for {
		select {
		case <-app.ctx.Done():
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, err := conn.Read(reader)
		if err != nil {
			return
		}

		buffer = append(buffer, reader[:n]...)

		// Process complete messages (terminated by newline or semicolon)
		for {
			idx := -1
			for i, b := range buffer {
				if b == '\n' || b == ';' {
					idx = i
					break
				}
			}
			if idx < 0 {
				break
			}

			line := string(buffer[:idx])
			buffer = buffer[idx+1:]

			// Skip empty lines
			if len(line) == 0 {
				continue
			}

			// Parse and process the message
			mm := app.decodeRawMessage(line)
			if mm != nil {
				app.handleMessage(mm)
			}
		}

		// Prevent buffer from growing too large
		if len(buffer) > 1024 {
			buffer = buffer[len(buffer)-512:]
		}
	}
}

// decodeRawMessage decodes AVR format messages
// Supports formats: *HEXDATA; @TIMESTAMP*HEXDATA; %TIMESTAMP*HEXDATA; <TIMESTAMP+SIG*HEXDATA;
func (app *App) decodeRawMessage(line string) *modes.Message {
	// Trim whitespace
	line = trimSpace(line)
	if len(line) == 0 {
		return nil
	}

	// Remove trailing semicolon if present
	if line[len(line)-1] == ';' {
		line = line[:len(line)-1]
	}

	var hexData string
	var signalLevel float64

	switch line[0] {
	case '*', ':':
		// Simple format: *HEXDATA or :HEXDATA
		hexData = line[1:]

	case '@', '%':
		// Timestamp format: @TIMESTAMP*HEXDATA or %TIMESTAMP*HEXDATA
		// Timestamp is 12 hex chars
		if len(line) < 14 {
			return nil
		}
		// Find the * separator
		starIdx := -1
		for i := 13; i < len(line); i++ {
			if line[i] == '*' {
				starIdx = i
				break
			}
		}
		if starIdx < 0 {
			return nil
		}
		hexData = line[starIdx+1:]

	case '<':
		// Beast AVR format: <TIMESTAMP+SIGNAL*HEXDATA
		// 12 hex timestamp + 2 hex signal = 14 chars after <
		if len(line) < 16 {
			return nil
		}
		// Parse signal level (2 hex chars at position 13-14)
		sig, err := parseHexByte(line[13:15])
		if err == nil {
			signalLevel = float64(sig) / 255.0
			signalLevel = signalLevel * signalLevel
		}
		hexData = line[15:]

	default:
		return nil
	}

	// Decode hex data
	msgBytes, err := decodeHex(hexData)
	if err != nil || len(msgBytes) == 0 {
		return nil
	}

	// Validate message length
	if len(msgBytes) != 7 && len(msgBytes) != 14 {
		return nil
	}

	// Decode the message
	mm, result := modes.DecodeModesMessage(msgBytes)
	if result < 0 {
		return nil
	}

	mm.SignalLevel = signalLevel
	mm.Remote = true

	return mm
}

// runBeastInputServer listens for Beast format messages on port 30004
func (app *App) runBeastInputServer() {
	defer app.wg.Done()

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", app.config.BeastInPort))
	if err != nil {
		log.Printf("Beast input server error: %v", err)
		return
	}
	defer listener.Close()

	log.Printf("Beast input server listening on port %d", app.config.BeastInPort)

	go func() {
		<-app.ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-app.ctx.Done():
				return
			default:
				log.Printf("Beast input accept error: %v", err)
				continue
			}
		}

		log.Printf("Beast input client connected: %s", conn.RemoteAddr())
		go app.handleBeastInputConnection(conn)
	}
}

// handleBeastInputConnection handles a single Beast input connection
func (app *App) handleBeastInputConnection(conn net.Conn) {
	defer conn.Close()
	defer log.Printf("Beast input client disconnected: %s", conn.RemoteAddr())

	reader := make([]byte, 4096)
	var buffer []byte

	for {
		select {
		case <-app.ctx.Done():
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, err := conn.Read(reader)
		if err != nil {
			return
		}

		buffer = append(buffer, reader[:n]...)

		// Process complete Beast messages
		for len(buffer) > 0 {
			mm, remaining, err := modes.DecodeBeast(buffer)
			if err != nil {
				// Not enough data or invalid, try to find next escape byte
				if len(buffer) > 1 {
					nextEscape := -1
					for i := 1; i < len(buffer); i++ {
						if buffer[i] == 0x1A {
							nextEscape = i
							break
						}
					}
					if nextEscape > 0 {
						buffer = buffer[nextEscape:]
						continue
					}
				}
				break
			}

			buffer = remaining
			if mm != nil {
				mm.Remote = true
				app.handleMessage(mm)
			}
		}

		// Prevent buffer from growing too large
		if len(buffer) > 4096 {
			buffer = buffer[len(buffer)-2048:]
		}
	}
}

// Helper functions

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

func parseHexByte(s string) (byte, error) {
	if len(s) != 2 {
		return 0, fmt.Errorf("invalid hex byte")
	}
	var b byte
	for _, c := range s {
		b <<= 4
		switch {
		case c >= '0' && c <= '9':
			b |= byte(c - '0')
		case c >= 'a' && c <= 'f':
			b |= byte(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			b |= byte(c - 'A' + 10)
		default:
			return 0, fmt.Errorf("invalid hex char")
		}
	}
	return b, nil
}

func decodeHex(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd length hex string")
	}
	result := make([]byte, len(s)/2)
	for i := 0; i < len(result); i++ {
		b, err := parseHexByte(s[i*2 : i*2+2])
		if err != nil {
			return nil, err
		}
		result[i] = b
	}
	return result, nil
}
