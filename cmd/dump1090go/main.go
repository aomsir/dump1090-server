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
)

const (
	// Default values
	defaultFrequency  = 1090000000 // 1090 MHz
	defaultSampleRate = 2400000    // 2.4 MHz
	defaultGain       = -1         // Auto gain
	defaultHTTPPort   = 8080
	defaultBeastPort  = 30005
	defaultAVRPort    = 30002
	defaultSBSPort    = 30003

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

	// Network settings
	HTTPPort     int
	BeastPort    int
	AVRPort      int
	SBSPort      int
	DisableHTTP  bool
	DisableBeast bool
	DisableAVR   bool
	DisableSBS   bool

	// Receiver location
	Latitude  float64
	Longitude float64
	MaxRange  float64

	// Other settings
	Quiet     bool
	FixCRC    bool
	Aggressive bool
}

// App holds the application state
type App struct {
	config    *Config
	tracker   *modes.Tracker
	demod     *modes.Demodulator
	converter *rtlsdr.ConverterState
	device    *rtlsdr.Device

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

	// Start network services
	if !config.DisableHTTP {
		app.wg.Add(1)
		go app.runHTTPServer()
	}
	if !config.DisableBeast {
		app.wg.Add(1)
		go app.runBeastServer()
	}
	if !config.DisableAVR {
		app.wg.Add(1)
		go app.runAVRServer()
	}
	if !config.DisableSBS {
		app.wg.Add(1)
		go app.runSBSServer()
	}

	// Start periodic tasks
	app.wg.Add(1)
	go app.runPeriodicTasks()

	// Run main processing
	var err error
	if config.InputFile != "" {
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

	// Network settings
	flag.IntVar(&config.HTTPPort, "http-port", defaultHTTPPort, "HTTP server port")
	flag.IntVar(&config.BeastPort, "beast-port", defaultBeastPort, "Beast output port")
	flag.IntVar(&config.AVRPort, "avr-port", defaultAVRPort, "AVR output port")
	flag.IntVar(&config.SBSPort, "sbs-port", defaultSBSPort, "SBS output port")
	flag.BoolVar(&config.DisableHTTP, "no-http", false, "Disable HTTP server")
	flag.BoolVar(&config.DisableBeast, "no-beast", false, "Disable Beast output")
	flag.BoolVar(&config.DisableAVR, "no-avr", false, "Disable AVR output")
	flag.BoolVar(&config.DisableSBS, "no-sbs", false, "Disable SBS output")

	// Receiver location
	flag.Float64Var(&config.Latitude, "lat", 0, "Receiver latitude")
	flag.Float64Var(&config.Longitude, "lon", 0, "Receiver longitude")
	flag.Float64Var(&config.MaxRange, "max-range", 300, "Maximum range in nautical miles")

	// Other settings
	flag.BoolVar(&config.Quiet, "quiet", false, "Suppress output")
	flag.BoolVar(&config.FixCRC, "fix", false, "Fix single-bit CRC errors")
	flag.BoolVar(&config.Aggressive, "aggressive", false, "Fix two-bit CRC errors")

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

	// Demodulate
	app.demod.Demodulate2400(app.magBuf)
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

		// Demodulate
		app.demod.Demodulate2400(app.magBuf)
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

		receiver := modes.GenerateReceiverJSON("1.0.0", 1.0, 120, app.config.Latitude, app.config.Longitude)
		data, _ := json.MarshalIndent(receiver, "", "  ")
		w.Write(data)
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

func (app *App) runBeastServer() {
	defer app.wg.Done()
	app.runTCPServer("Beast", app.config.BeastPort, &app.beastClients)
}

func (app *App) runAVRServer() {
	defer app.wg.Done()
	app.runTCPServer("AVR", app.config.AVRPort, &app.avrClients)
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

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-app.ctx.Done():
			return
		case <-ticker.C:
			// Update tracker (remove stale aircraft)
			app.tracker.PeriodicUpdate()
		}
	}
}
