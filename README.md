# dump1090-mutability-go

A Go implementation of dump1090-mutability, a Mode S ADS-B decoder for RTL-SDR devices.

## Project Status

**Active Development** - Core decoding algorithms are complete and tested.

### ✅ Completed Modules

#### 1. CRC Module (`internal/modes/crc.go`)
- Mode S CRC-24 checksum calculation
- 1-bit and 2-bit error detection and correction
- Syndrome table generation for fast error diagnosis
- **Performance**: 12.7 ns/op, zero allocations
- **Test Coverage**: 100% golden test alignment with C version

#### 2. CPR Module (`internal/modes/cpr.go`)
- Global airborne position decoding (DecodeCPRAirborne)
- Global surface position decoding (DecodeCPRSurface)
- Relative position decoding (DecodeCPRRelative)
- 59-level latitude zone lookup table (cprNLFunction)
- **Performance**: 44.85 ns/op (airborne), 25.92 ns/op (relative), zero allocations
- **Accuracy**: < 0.000001° float precision error

#### 3. Core Types (`internal/modes/types.go`)
- Message structure (112/56-bit messages)
- Data source and address type enumerations
- ADS-B v2 operational status structures
- Target state & status (TSS) fields

### 🚧 In Progress

#### DSP Demodulator (`internal/dsp/demod.go`)
- 2.4MHz I/Q sample processing
- Preamble detection with phase alignment
- Manchester bit decoding (5 phase correlation functions)
- Signal quality assessment (SNR, power)

### 📋 Planned Modules

1. **Mode S Decoder** (`internal/modes/decoder.go`)
   - DF (Downlink Format) message parsing
   - Extended squitter (DF17/DF18) decoding
   - Surveillance reply (DF4/DF5/DF20/DF21) handling

2. **Aircraft Tracker** (`internal/modes/track.go`)
   - ICAO address deduplication
   - Position and velocity state management
   - CPR frame pairing and timeout handling

3. **Network I/O** (`internal/netio/`)
   - Beast binary protocol output
   - JSON aircraft state export
   - TCP client management

4. **RTL-SDR Interface** (`internal/rtlsdr/`)
   - Minimal cgo wrapper for librtlsdr
   - Batch I/Q sample reading (16-32ms blocks)
   - Zero-copy ring buffer design

5. **Main Programs**
   - `cmd/dump1090go/`: Live decoder
   - `cmd/view1090go/`: Console viewer

## Architecture

```
┌─────────────────┐
│   RTL-SDR       │  (cgo, minimal wrapper)
│   Hardware      │
└────────┬────────┘
         │ I/Q samples ([]byte, 16-32ms chunks)
         ▼
┌─────────────────┐
│ DSP Demodulator │  internal/dsp
│  - Magnitude    │  (batch processing, pre-allocated buffers)
│  - Preamble     │
│  - Manchester   │
└────────┬────────┘
         │ Raw messages ([]Message)
         ▼
┌─────────────────┐
│ Mode S Decoder  │  internal/modes
│  - CRC check    │  (table-driven, zero-alloc hot path)
│  - DF parsing   │
│  - CPR decode   │
└────────┬────────┘
         │ Decoded messages
         ▼
┌─────────────────┐
│ Aircraft Track  │  internal/modes/track.go
│  - State merge  │  (RWMutex, timeout cleanup)
│  - Position est │
└────────┬────────┘
         │ Aircraft states
         ▼
┌─────────────────┐
│   Network I/O   │  internal/netio
│  - Beast TCP    │  (goroutine per client, backpressure)
│  - JSON files   │  (periodic dump)
└─────────────────┘
```

## Build Requirements

- **Go**: 1.21 or later
- **librtlsdr**: For RTL-SDR hardware access
  - macOS: `brew install librtlsdr`
  - Debian/Ubuntu: `apt install librtlsdr-dev`
- **GCC**: For cgo compilation

## Testing

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./internal/modes/

# Run benchmarks
go test -bench=. -benchmem ./internal/modes/

# Cross-validate with C version
cd dump1090-libs
make cprtests && ./cprtests
```

## Performance Targets

All critical path functions must meet these benchmarks:

- **CRC calculation**: < 15 ns/op
- **CPR decoding**: < 50 ns/op
- **Message demodulation**: < 200 ns/op (target)
- **Memory**: Zero allocations in hot path

## Design Principles

1. **Compatibility First**: Bit-exact output alignment with dump1090-mutability
2. **Batch Processing**: Minimize cgo overhead with 16-32ms block sizes
3. **Zero-Copy**: Pre-allocated buffers, sync.Pool for magnitude arrays
4. **Testability**: Golden tests using C version outputs as reference
5. **No Magic**: Direct C-to-Go translation, maintain algorithm clarity

## Testing Strategy

### Unit Tests
- CRC: Known messages with expected checksums
- CPR: Test vectors from cprtests.c (59 cases)
- Error correction: Single/double bit flips

### Integration Tests (Planned)
- I/Q replay: Recorded samples → full decode pipeline
- Statistical validation: Frame count, DF distribution, CRC fail rate

### Cross-validation
All numeric outputs are validated against the C implementation within tolerance:
- Integer fields: Exact match
- Float fields: < 0.000001 error

## Contributing

This project follows the original dump1090-mutability's design closely. When implementing new modules:

1. Read the corresponding C file first
2. Translate algorithm directly (maintain variable names where possible)
3. Add unit tests with C version test data
4. Run benchmarks to ensure performance
5. Commit with clear description of what was implemented

## License

GPL v2+ (matching dump1090-mutability)

Original work:
- Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
- Copyright (C) 2012 Salvatore Sanfilippo <antirez@gmail.com>

Go implementation:
- Copyright (c) 2025

## References

- [dump1090-mutability](https://github.com/mutability/dump1090)
- [ICAO Annex 10, Volume IV](https://www.icao.int/safety/acp/acpwgf/acp-wg-c-15/wp07_att01.pdf) - Mode S specification
- [1090-WP29-07](http://www.anteni.net/adsb/Doc/1090-WP29-07-Draft_CPR101.pdf) - CPR decoding

## Next Steps

1. Complete DSP demodulator (demod_2400.c → demod.go)
2. Implement Mode S message decoder (mode_s.c → decoder.go)
3. Add aircraft tracking (track.c → track.go)
4. Implement network output (net_io.c → beast.go/json.go)
5. Create minimal RTL-SDR cgo wrapper
6. Write main programs and CLI
7. End-to-end testing with recorded I/Q data
