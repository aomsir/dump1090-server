# dump1090-server

A pure Go implementation of dump1090, a Mode S ADS-B decoder for RTL-SDR devices.

## Features

- **Full Mode S decoding** - DF0-21 message types with CRC error correction
- **CPR position decoding** - Global, relative, and surface position calculations
- **Aircraft tracking** - State management with CPR frame pairing and timeout handling
- **Multiple output formats** - Beast, AVR, SBS, FATSV, JSON
- **Network I/O** - TCP servers for data output and input
- **Interactive display** - Real-time TTY aircraft list
- **JSON file output** - Compatible with dump1090-mutability web interface
- **Docker support** - Minimal container image

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/aomsir/dump1090-server.git
cd dump1090-server

# Build (requires librtlsdr for hardware support)
go build -o bin/dump1090-server ./cmd/dump1090-server

# Or build without CGO (network-only mode)
CGO_ENABLED=0 go build -o bin/dump1090-server ./cmd/dump1090-server
```

### Dependencies

For RTL-SDR hardware support:
- **macOS**: `brew install librtlsdr`
- **Debian/Ubuntu**: `apt install librtlsdr-dev`
- **Fedora**: `dnf install rtl-sdr-devel`

### Docker

The default Docker image runs in **network-only mode** and does **not** access USB RTL-SDR hardware.

```bash
docker build -t dump1090-server .

# Run with data ports exposed (default: --net-only)
docker run -p 30002:30002 -p 30003:30003 -p 30005:30005 dump1090-server

# Run with all default ports (including HTTP API, FATSV, and Beast input)
docker run -p 30001:30001 -p 30002:30002 -p 30003:30003 -p 30004:30004 -p 30005:30005 -p 30104:30104 -p 8080:8080 -p 10001:10001 dump1090-server
```

To use RTL-SDR hardware, build with CGO and run with `--privileged` or `--device` flags outside Docker.

## Usage

### Basic Usage

```bash
# Live decoding from RTL-SDR device
./dump1090-server

# Network-only mode (receive data from other sources)
./dump1090-server --net-only

# Read from recorded I/Q file
./dump1090-server --infile samples.bin

# Interactive mode with statistics
./dump1090-server --interactive --stats
```

### Network Ports

| Port  | Protocol | Description |
|-------|----------|-------------|
| 30001 | Raw In   | Receive AVR/Raw format messages |
| 30002 | AVR Out  | Output in AVR text format |
| 30003 | SBS Out  | Output in SBS (BaseStation) format |
| 30004 | Beast In | Receive Beast binary format |
| 30005 | Beast Out| Output in Beast binary format |
| 8080  | HTTP     | JSON API endpoints |
| 10001 | FATSV    | FlightAware TSV format |

### HTTP Endpoints

- `/data/aircraft.json` - Current aircraft list
- `/data/receiver.json` - Receiver information
- `/data/stats.json` - Decoding statistics
- `/data/history_N.json` - Historical snapshots

### Command Line Options

#### Device Configuration
```
--device <N>          RTL-SDR device index (default: 0)
--freq <Hz>           Center frequency (default: 1090000000)
--gain <N>            Gain in 0.1dB units (-1 for auto)
--ppm <N>             Frequency correction in PPM
--enable-agc          Enable automatic gain control
--bias-tee            Enable bias-T power
```

#### Input Sources
```
--infile <file>       Read samples from file
--net                 Enable network services (HTTP, Beast, AVR, SBS, FATSV)
--net-only            Network only mode, no RTL-SDR/file input (implies --net)
```

#### Network Output
```
--net-bind-address <ip>   Bind address for all servers
--beast-out-port <N>      Beast output port (default: 30005)
--avr-out-port <N>        AVR output port (default: 30002)
--sbs-port <N>            SBS output port (default: 30003)
--fatsv-port <N>          FATSV output port (default: 10001)
--http-port <N>           HTTP server port (default: 8080)
--no-beast-out            Disable Beast output
--no-avr-out              Disable AVR output
--no-sbs                  Disable SBS output
```

#### Network Input
```
--raw-in-port <N>         Raw/AVR input port (default: 30001)
--beast-in-port <ports>   Beast input port(s) (default: 30004,30104)
                          Comma or whitespace separated list; 0 to disable
```

#### Receiver Location
```
--lat <degrees>       Receiver latitude
--lon <degrees>       Receiver longitude
--max-range <nm>      Maximum range in nautical miles
```

#### Output Control
```
--quiet               Suppress all output
--raw                 Output raw messages only
--onlyaddr            Output ICAO addresses only
--mlat                Include MLAT timestamps
--show-only <icao>    Only show specific ICAO address
```

#### Error Correction
```
--fix                 Enable 1-bit error correction
--aggressive          Enable 2-bit error correction
```

#### Interactive Mode
```
--interactive             Enable interactive TTY display
--interactive-rows <N>    Number of rows (default: 22)
--interactive-ttl <N>     Display timeout in seconds (default: 60)
--metric                  Use metric units
```

#### JSON Output
```
--write-json <dir>        JSON output directory
--write-json-every <s>    Update interval in seconds (default: 1.0)
--history-size <N>        Number of history files (default: 120)
--history-interval <N>    History snapshot interval (default: 30)
```

## Architecture

```
┌─────────────────┐
│   RTL-SDR       │  (cgo wrapper, optional)
│   Hardware      │
└────────┬────────┘
         │ I/Q samples ([]byte, 16-32ms chunks)
         ▼
┌─────────────────┐
│ DSP Demodulator │  modes/demod.go
│  - Magnitude    │  (2.4MHz Manchester decoding)
│  - Preamble     │  (5-phase correlation)
│  - Manchester   │
└────────┬────────┘
         │ Raw messages
         ▼
┌─────────────────┐
│ Mode S Decoder  │  modes/decoder.go
│  - CRC check    │  (table-driven, zero-alloc)
│  - DF parsing   │  (DF0-21 support)
│  - CPR decode   │
└────────┬────────┘
         │ Decoded messages
         ▼
┌─────────────────┐
│ Aircraft Track  │  modes/track.go
│  - State merge  │  (RWMutex, thread-safe)
│  - Position     │  (CPR frame pairing)
│  - Timeout      │  (5min TTL)
└────────┬────────┘
         │ Aircraft states
         ▼
┌─────────────────┐
│   Network I/O   │  modes/output.go
│  - Beast TCP    │  (binary protocol)
│  - AVR TCP      │  (text format)
│  - SBS TCP      │  (BaseStation format)
│  - FATSV TCP    │  (FlightAware TSV)
│  - JSON files   │  (periodic dump)
│  - HTTP API     │  (REST endpoints)
└─────────────────┘
```

## Performance

All critical path functions are optimized for minimal latency:

| Operation | Performance | Allocations |
|-----------|-------------|-------------|
| CRC calculation | 12.7 ns/op | 0 |
| CPR airborne decode | 44.85 ns/op | 0 |
| CPR relative decode | 25.92 ns/op | 0 |

## Testing

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./modes/

# Run benchmarks
go test -bench=. -benchmem ./modes/
```

## Compatibility

This implementation maintains bit-exact compatibility with dump1090-mutability:
- Integer fields: Exact match
- Float fields: < 0.000001 error tolerance
- Output formats: Protocol-compatible with existing tools

## License

GPL v2+ (matching dump1090-mutability)

**Original work:**
- Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
- Copyright (C) 2012 Salvatore Sanfilippo <antirez@gmail.com>

**Go implementation:**
- Copyright (c) 2025

## References

- [dump1090-mutability](https://github.com/mutability/dump1090) - Original C implementation
- [ICAO Annex 10, Volume IV](https://www.icao.int/safety/acp/acpwgf/acp-wg-c-15/wp07_att01.pdf) - Mode S specification
- [1090-WP29-07](http://www.anteni.net/adsb/Doc/1090-WP29-07-Draft_CPR101.pdf) - CPR decoding specification
- [SBS BaseStation Format](http://woodair.net/sbs/article/barebones42_socket_data.htm) - SBS protocol documentation
