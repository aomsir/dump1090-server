# Compatibility Matrix

Feature parity status with [dump1090-mutability](https://github.com/mutability/dump1090).

## Core Decoding

| Upstream feature | Current support | Status | Notes |
|---|---|---|---|
| DF0/DF4/DF11 (short air-to-air, all-call reply) | Full | Stable | CRC, ICAO extraction |
| DF16 (long air-to-air) | Full | Stable | |
| DF17/DF18 (ADS-B extended squitter) | Full | Stable | TC, ME parsing, ICAO |
| DF20/DF21 (comm-B, extended squitter) | Full | Stable | MB field extraction |
| CRC error correction (1-bit fix) | Full | Stable | `--fix` flag |
| CRC error correction (2-bit aggressive) | Full | Stable | `--aggressive` flag |
| Mode A/C decoding | Full | Stable | `--modeac` flag |

## CPR Position Decoding

| Upstream feature | Current support | Status | Notes |
|---|---|---|---|
| Airborne CPR global decode | Full | Stable | Zero-alloc, ~45 ns/op |
| Airborne CPR relative decode | Full | Stable | Zero-alloc, ~26 ns/op |
| Surface CPR decode | Full | Stable | |
| Receiver location (`--lat`, `--lon`) | Full | Stable | Required for relative decode |

## Aircraft Tracking

| Upstream feature | Current support | Status | Notes |
|---|---|---|---|
| State tracking with CPR frame pairing | Full | Stable | RWMutex, thread-safe |
| Aircraft timeout (5 min TTL) | Full | Stable | |
| ICAO filter (`--show-only`) | Full | Stable | |

## Input Sources

| Upstream feature | Current support | Status | Notes |
|---|---|---|---|
| RTL-SDR USB hardware | Full | Stable | CGO build, requires librtlsdr |
| I/Q file input (`--infile`) | Full | Stable | |
| Network raw/AVR input (port 30001) | Full | Stable | |
| Network Beast input (ports 30004/30104) | Full | Stable | Multi-port comma-separated |
| Network-only mode (`--net-only`) | Full | Stable | No hardware/file input |

## Output Formats

| Upstream feature | Current support | Status | Notes |
|---|---|---|---|
| Beast binary TCP output (port 30005) | Full | Stable | |
| AVR text TCP output (port 30002) | Full | Stable | |
| SBS/BaseStation TCP output (port 30003) | Full | Stable | |
| FATSV output (port 10001) | Full | Stable | FlightAware TSV format |
| JSON file output (`--write-json`) | Full | Stable | dump1090-mutability compatible |
| HTTP JSON API (port 8080) | Full | Stable | `/data/aircraft.json` etc. |

## Companion Commands

| Upstream feature | Current support | Status | Notes |
|---|---|---|---|
| `view1090` interactive display | Full | Stable | Beast TCP client, TTY table |
| `faup1090` FlightAware uploader | Full | Stable | Beast TCP client, FATSV stdout |

## Device Configuration

| Upstream feature | Current support | Status | Notes |
|---|---|---|---|
| Device index (`--device`) | Full | Stable | |
| Center frequency (`--freq`) | Full | Stable | Default 1090 MHz |
| Gain control (`--gain`, `--enable-agc`) | Full | Stable | |
| PPM correction (`--ppm`) | Full | Stable | |
| Bias-T (`--bias-tee`) | Full | Stable | |

## Docker

| Upstream feature | Current support | Status | Notes |
|---|---|---|---|
| Docker image | Full | Stable | `scratch` base, network-only default |
| Multi-arch build | Full | Stable | `--platform` via buildx |

## Intentionally Unsupported

| Feature | Status | Reason |
|---|---|---|
| WebUI / browser map | Not supported | Pure CLI/server tool; no `public_html/` assets; use external visualizer (e.g., tar1090) |
| Integrated `--interactive` mode in main binary | Not supported | Upstream removed; use separate `view1090` command instead |
