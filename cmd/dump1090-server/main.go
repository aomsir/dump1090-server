// dump1090-server - ADS-B Mode S decoder for RTL-SDR devices
//
// A Go implementation of dump1090-mutability, providing:
// - RTL-SDR device support
// - 2.4MHz Mode S demodulation
// - Aircraft tracking
// - Network output (HTTP/JSON, Beast, AVR, SBS)
package main

import (
	"context"
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

	"github.com/aomsir/dump1090-server/modes"
	"github.com/aomsir/dump1090-server/rtlsdr"
	"github.com/aomsir/dump1090-server/ui"
)

// NetworkClient wraps a connection with heartbeat tracking.
// Close is idempotent: safe to call from concurrent Broadcast and
// sendHeartbeats goroutines that may both observe a write failure.
type NetworkClient struct {
	conn      net.Conn
	lastWrite time.Time
	mu        sync.Mutex
	closeOnce sync.Once
	closeErr  error
}

func newNetworkClient(conn net.Conn) *NetworkClient {
	return &NetworkClient{
		conn:      conn,
		lastWrite: time.Now(),
	}
}

func (nc *NetworkClient) Write(data []byte) (int, error) {
	nc.mu.Lock()
	defer nc.mu.Unlock()

	nc.conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
	n, err := nc.conn.Write(data)
	if err == nil {
		nc.lastWrite = time.Now()
	}
	return n, err
}

func (nc *NetworkClient) Close() error {
	nc.closeOnce.Do(func() {
		nc.closeErr = nc.conn.Close()
	})
	return nc.closeErr
}

func (nc *NetworkClient) shouldSendHeartbeat(interval time.Duration) bool {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	return time.Since(nc.lastWrite) >= interval
}

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
	defaultFATSVPort    = 10001

	// Network input ports
	defaultRawInPort = 30001

	// Default heartbeat interval (matching C version)
	defaultHeartbeatInterval = 60 * time.Second

	// Buffer sizes
	asyncBufNum = 12
	asyncBufLen = 256 * 1024 // 256K samples per buffer

	// overlapSamples must cover the 19-sample preamble detection window
	// plus the longest Mode S message (112 bits * 12/5 = 268 samples =
	// 287 total); 300 adds margin for phase offset.
	overlapSamples = 300
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
	FATSVPort    int
	DisableHTTP  bool
	DisableBeast bool
	DisableAVR   bool
	DisableSBS   bool
	DisableFATSV bool

	// Network input settings
	RawInPort      int
	BeastInPorts   PortList // Beast input ports (default 30004,30104)
	DisableRawIn   bool
	DisableBeastIn bool

	// Network control
	EnableNet bool // --net: enable networking
	NetOnly   bool // --net-only: network only, no RTL/file input (implies --net)

	// Network forwarding policy
	ForwardMLAT bool // --forward-mlat: forward MLAT messages to Beast output
	NetVerbatim bool // --net-verbatim: forward 2-bit-corrected messages

	// Network bind address
	NetBindAddress string

	// Heartbeat interval for output clients. 0 disables heartbeats.
	HeartbeatInterval time.Duration

	// Receiver location
	Latitude  float64
	Longitude float64
	MaxRange  float64

	// Output control (matching C version)
	Quiet    bool   // --quiet: suppress all output
	Raw      bool   // --raw: output only raw messages (*HEXDATA;)
	OnlyAddr bool   // --onlyaddr: output only ICAO addresses
	MLAT     bool   // --mlat: include MLAT timestamp in raw output
	ShowOnly uint32 // --show-only: only show messages from this ICAO

	// Statistics output (matching C version)
	Stats      bool // --stats: enable statistics display
	StatsEvery int  // --stats-every: interval in seconds for stats display

	// CRC error correction
	FixCRC     bool
	Aggressive bool
	Metric     bool

	// Mode A/C demodulation
	ModeAC bool

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
	demod          *modes.Demodulator
	statsCollector *modes.StatsCollector
	converter      *rtlsdr.ConverterState
	device         *rtlsdr.Device
	interactive    *ui.Interactive
	jsonWriter     *modes.JSONWriter
	fatsvWriter    *modes.FATSVWriter

	// Statistics
	totalMessages   uint64
	validMessages   uint64
	decodedMessages uint64

	// Network clients
	beastSvc *NetworkService
	avrSvc   *NetworkService
	sbsSvc   *NetworkService
	fatsvSvc *NetworkService

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

	// Create statistics collector
	statsCollector := modes.NewStatsCollector()

	// Create application
	app := &App{
		config:         config,
		tracker:        modes.NewTracker(),
		demod:          modes.NewDemodulator(),
		statsCollector: statsCollector,
		beastSvc:       newNetworkService("Beast output"),
		avrSvc:         newNetworkService("AVR output"),
		sbsSvc:         newNetworkService("SBS"),
		fatsvSvc:       newNetworkService("FATSV"),
	}

	// Inject stats collector into components
	app.demod.SetStatsCollector(statsCollector)
	app.tracker.SetStatsCollector(statsCollector)

	// Initialize FATSV writer
	app.fatsvWriter = modes.NewFATSVWriter(app.tracker)

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

		// Inject stats collector into JSON writer
		app.jsonWriter.SetStatsCollector(statsCollector)

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
		app.tracker.SetMaxRange(config.MaxRange * 1852) // Convert NM to meters
	}

	// Create context for graceful shutdown
	app.ctx, app.cancel = context.WithCancel(context.Background())

	// Handle signals (matching C version dump1090.c signal handlers)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		if sig == syscall.SIGINT {
			modes.LogSigint()
		} else {
			modes.LogSigterm()
		}
		app.cancel()
	}()


	// Start network services (only when --net or --net-only is set)
	if config.EnableNet {
		// Start network output services
		if !config.DisableHTTP {
			app.wg.Add(1)
			go app.runHTTPServer()
		}
		if !config.DisableBeast {
			app.wg.Add(1)
			go app.runOutputTCPServer(app.beastSvc, config.BeastOutPort)
		}
		if !config.DisableAVR {
			app.wg.Add(1)
			go app.runOutputTCPServer(app.avrSvc, config.AVROutPort)
		}
		if !config.DisableSBS {
			app.wg.Add(1)
			go app.runOutputTCPServer(app.sbsSvc, config.SBSPort)
		}
		if !config.DisableFATSV {
			app.wg.Add(1)
			go app.runOutputTCPServer(app.fatsvSvc, config.FATSVPort)
		}

		// Start network input services
		if !config.DisableRawIn {
			app.wg.Add(1)
			go app.runRawInputServer()
		}
		if !config.DisableBeastIn {
			// Start a Beast input listener for each port in BeastInPorts.
			for _, port := range config.BeastInPorts {
				app.wg.Add(1)
				go app.runBeastInputServerOnPort(port)
			}
		}
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
	config, err := ParseFlagsFromSet(flag.CommandLine, os.Args[1:])
	if err != nil {
		log.Fatalf("flag parse error: %v", err)
	}
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
	// Convert IQ to magnitude, capturing total power
	totalPower := rtlsdr.ConvertUC8NoDC(iq, app.magBuf.Data[overlapSamples:])
	app.magBuf.TotalPower = totalPower

	// Demodulate Mode S (overlap region Data[:overlapSamples] still holds
	// the tail of the previous buffer from the last iteration)
	app.demod.Demodulate2400(app.magBuf)

	// Demodulate Mode A/C (only when --modeac is set)
	if app.config.ModeAC {
		app.demod.Demodulate2400AC(app.magBuf)
	}

	// Save tail of current data as overlap for the next buffer
	copy(app.magBuf.Data[:overlapSamples], app.magBuf.Data[app.magBuf.Length:])
}

func (app *App) handleMessage(mm *modes.Message) {
	atomic.AddUint64(&app.totalMessages, 1)

	// Increment stats collector message counter
	if app.statsCollector != nil {
		app.statsCollector.AddMessage()
	}

	// Message is already decoded by the demodulator
	// Just count valid messages
	atomic.AddUint64(&app.validMessages, 1)

	var aircraft *modes.Aircraft

	// Mode A/C messages (MsgType=32) get special handling
	if mm.MsgType == 32 {
		// Mode A/C messages are matched with existing Mode S aircraft
		// UpdateFromModeAC returns nil (Mode A/C doesn't create independent tracks)
		app.tracker.UpdateFromModeAC(mm)
		aircraft = nil
	} else {
		// Normal Mode S message processing
		aircraft = app.tracker.UpdateFromMessage(mm)
		if aircraft != nil {
			atomic.AddUint64(&app.decodedMessages, 1)

			// Add valid ICAO address to filter for future scoring
			// (matching C version logic: only high-confidence addresses)
			if app.demod != nil && mm.Corrected == 0 &&
				(mm.MsgType == 17 || mm.MsgType == 18 || (mm.MsgType == 11 && mm.IID == 0)) {
				app.demod.AddKnownICAO(mm.Addr)
			}
		}
	}

	// Track remote message stats for network-sourced messages
	if mm.Remote && app.statsCollector != nil {
		isModeAC := mm.MsgType == 32
		isUnknownICAO := !isModeAC && aircraft == nil && mm.Corrected == 0
		app.statsCollector.AddRemoteMessage(isModeAC, mm.Corrected, isUnknownICAO)
	}

	// Broadcast to network clients
	app.broadcastMessage(mm, aircraft)

	// Message display matching C version mode_s.c:useModesMessage()
	// In non-interactive non-quiet mode, display messages on standard output
	if !app.config.Interactive && !app.config.Quiet {
		// Apply --show-only filter
		if app.config.ShowOnly == 0 || mm.Addr == app.config.ShowOnly {
			modes.DisplayModesMessage(mm, modes.DisplayConfig{
				OnlyAddr: app.config.OnlyAddr,
				Raw:      app.config.Raw,
				MLAT:     app.config.MLAT,
			}, os.Stdout)
		}
	}
}


func (app *App) broadcastMessage(mm *modes.Message, aircraft *modes.Aircraft) {
	dest := modes.ForwardingDestination(mm, app.config.NetVerbatim, app.config.ForwardMLAT)

	// Beast output
	if !app.config.DisableBeast && dest&modes.DestBeast != 0 {
		beastData := modes.EncodeBeast(mm)
		app.beastSvc.Broadcast(beastData)
	}

	// AVR output
	if !app.config.DisableAVR && dest&modes.DestRaw != 0 {
		// C version defaults to no timestamp (only in MLAT mode)
		avrData := modes.EncodeAVR(mm, false)
		app.avrSvc.Broadcast([]byte(avrData))
	}

	// SBS output
	if !app.config.DisableSBS && dest&modes.DestSBS != 0 {
		sbsData := modes.EncodeSBS(mm, aircraft)
		if sbsData != "" {
			app.sbsSvc.Broadcast([]byte(sbsData))
		}
	}

	// FATSV event output (matching C version net_io.c:1470-1475)
	// Only for specific message types that trigger immediate output
	if !app.config.DisableFATSV && dest&modes.DestFATSV != 0 {
		fatsvEvent := app.fatsvWriter.WriteFATSVEvent(mm)
		if fatsvEvent != nil {
			app.fatsvSvc.Broadcast(fatsvEvent)
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

		// Convert IQ to magnitude, capturing total power
		totalPower := rtlsdr.ConvertUC8NoDC(iqBuf[:n], app.magBuf.Data[overlapSamples:])
		app.magBuf.Length = uint32(n / 2)
		app.magBuf.TotalPower = totalPower

		// Demodulate Mode S (overlap region still holds previous tail)
		app.demod.Demodulate2400(app.magBuf)

		// Demodulate Mode A/C (only when --modeac is set)
		if app.config.ModeAC {
			app.demod.Demodulate2400AC(app.magBuf)
		}

		// Save tail of current data as overlap for the next buffer.
		// Guard against short reads: if Length < overlapSamples the source
		// region still contains stale previous overlap, so skip the copy.
		if app.magBuf.Length >= overlapSamples {
			copy(app.magBuf.Data[:overlapSamples], app.magBuf.Data[app.magBuf.Length:])
		}
	}

	return nil
}

func (app *App) runHTTPServer() {
	defer app.wg.Done()

	mux := app.newHTTPMux()

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", app.config.NetBindAddress, app.config.HTTPPort),
		Handler: mux,
	}

	go func() {
		<-app.ctx.Done()
		server.Shutdown(context.Background())
	}()

	log.Printf("HTTP server listening on %s:%d", app.config.NetBindAddress, app.config.HTTPPort)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Printf("HTTP server error: %v", err)
	}
}

// sendHeartbeats sends heartbeat messages to idle connections
func (app *App) sendHeartbeats() {
	interval := app.config.HeartbeatInterval
	if interval <= 0 {
		return
	}

	// Raw/AVR heartbeat: *0000;\n
	avrHeartbeat := []byte("*0000;\n")

	// Beast heartbeat: 0x1a, '1', followed by 9 zeros
	beastHeartbeat := []byte{0x1a, '1', 0, 0, 0, 0, 0, 0, 0, 0, 0}

	// SBS heartbeat: \r\n
	sbsHeartbeat := []byte("\r\n")

	// FATSV heartbeat: empty line (matching C version net_io.c:2105)
	fatsvHeartbeat := []byte("\n")

	app.beastSvc.sendHeartbeats(beastHeartbeat, interval)
	app.avrSvc.sendHeartbeats(avrHeartbeat, interval)
	app.sbsSvc.sendHeartbeats(sbsHeartbeat, interval)
	app.fatsvSvc.sendHeartbeats(fatsvHeartbeat, interval)
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
	var statsSeconds int // For --stats-every countdown

	// Default stats-every to 60 seconds if --stats is set but --stats-every is not
	statsEvery := app.config.StatsEvery
	if app.config.Stats && statsEvery == 0 {
		statsEvery = 60
	}

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

				// Update statistics collector
				if app.statsCollector != nil {
					app.statsCollector.Update()
				}

				// Expire old ICAO filter entries (every second is enough,
				// actual expiry happens every 60 seconds internally)
				if app.demod != nil {
					if filter := app.demod.GetICAOFilter(); filter != nil {
						filter.Expire()
					}
				}

				// Send heartbeats to idle connections
				app.sendHeartbeats()

				// FATSV periodic output (matching C version net_io.c:1508-1512)
				// Once per second scan of all aircraft
				if !app.config.DisableFATSV && app.fatsvWriter != nil {
					fatsvData := app.fatsvWriter.WriteFATSV()
					if len(fatsvData) > 0 {
						app.fatsvSvc.Broadcast(fatsvData)
					}
				}

				// Statistics display (matching C version --stats / --stats-every)
				if app.config.Stats && statsEvery > 0 {
					statsSeconds++
					if statsSeconds >= statsEvery {
						stats := app.statsCollector.GetAllTime()
						nfixCRC := 0
						if app.config.FixCRC {
							nfixCRC = 1
							if app.config.Aggressive {
								nfixCRC = 2
							}
						}
						modes.DisplayStats(&stats, os.Stdout, app.config.NetOnly, nfixCRC, app.config.MaxRange*1852)
						statsSeconds = 0
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

	addr := fmt.Sprintf("%s:%d", app.config.NetBindAddress, app.config.RawInPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Raw input server error: %v", err)
		return
	}
	defer listener.Close()

	log.Printf("Raw input server listening on %s", addr)

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

		go app.handleRawInputConnection(conn)
	}
}

// handleRawInputConnection handles a single raw/AVR input connection
func (app *App) handleRawInputConnection(conn net.Conn) {
	defer conn.Close()


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
// Malformed raw network records are dropped silently to keep normal logs quiet.
func (app *App) decodeRawMessage(line string) *modes.Message {
	// Trim whitespace
	line = trimSpace(line)
	if len(line) == 0 {
		return nil
	}

	// Use the new ParseRawAVR function from modes package
	// Remote flag is set by ParseRawAVR for network input
	mm, err := modes.ParseRawAVR(line, app.config.ModeAC)
	if err != nil {
		// Silently drop malformed records to keep normal logs quiet
		return nil
	}
	if mm == nil {
		return nil
	}

	return mm
}

// runBeastInputServerOnPort listens for Beast format messages on the given port.
func (app *App) runBeastInputServerOnPort(port int) {
	defer app.wg.Done()

	addr := fmt.Sprintf("%s:%d", app.config.NetBindAddress, port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Beast input server error: %v", err)
		return
	}
	defer listener.Close()

	log.Printf("Beast input server listening on %s", addr)

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

		go app.handleBeastInputConnection(conn)
	}
}

// handleBeastInputConnection handles a single Beast input connection
func (app *App) handleBeastInputConnection(conn net.Conn) {
	defer conn.Close()


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
			mm, remaining, err := modes.DecodeBeastWithConfig(buffer, app.config.ModeAC)
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
				// Remote flag is already set by DecodeBeastWithConfig
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
