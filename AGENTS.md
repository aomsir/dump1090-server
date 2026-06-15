# Repository Guidelines

## Project Structure & Module Organization

- Root Go module `github.com/aomsir/dump1090-server`; all source in pure Go.
- `cmd/dump1090-server/` - Main ADS-B decoder binary.
- `cmd/view1090/` - Interactive aircraft display client (Beast TCP).
- `cmd/faup1090/` - FlightAware uploader proxy (Beast TCP, FATSV stdout).
- `modes/` - Core decoder, CPR, tracking, output, protocol, and DSP logic.
- `rtlsdr/` - CGO wrapper for librtlsdr (optional, requires C toolchain).
- `ui/` - Terminal UI rendering for interactive display.
- `testdata/` - Test fixtures and sample data.
- `docs/` - Documentation (compatibility matrix, etc.).
- `bin/` - Build output (gitignored).

## Build, Test, and Development Commands

```bash
# Build main binary (network-only, no CGO required)
CGO_ENABLED=0 go build -o bin/dump1090-server ./cmd/dump1090-server

# Build with RTL-SDR hardware support (requires librtlsdr)
go build -o bin/dump1090-server ./cmd/dump1090-server

# Build companion utilities
CGO_ENABLED=0 go build -o bin/view1090 ./cmd/view1090
CGO_ENABLED=0 go build -o bin/faup1090 ./cmd/faup1090

# Run all tests
CGO_ENABLED=0 go test ./...

# Run tests with coverage
CGO_ENABLED=0 go test -cover ./modes/

# Run benchmarks
CGO_ENABLED=0 go test -bench=. -benchmem ./modes/

# Vet
CGO_ENABLED=0 go vet ./...

# Format check
gofmt -w . && git diff --exit-code
```

## Coding Style & Naming Conventions

- Go 1.21+; use standard `gofmt` formatting.
- Keep functions small and testable; prefer table-driven tests.
- Minimize allocations in hot paths (decoder, CPR, CRC).
- Use `sync.RWMutex` for shared state; avoid global mutable state.
- Export only what needs to be public; keep helpers unexported.
- Follow existing module patterns (`modes/`, `cmd/`).

## Testing Guidelines

- Run `CGO_ENABLED=0 go test ./...` before submitting.
- Add table-driven tests beside source files (`*_test.go`).
- For data or output changes, verify JSON output format matches dump1090-mutability.
- Keep logging quiet by default; guard verbose output behind flags.
- Use `testdata/` for fixture files; do not commit large binaries.

## Commit & Pull Request Guidelines

- Use concise, imperative subjects (e.g., `Fix CPR edge cases`).
- Separate unrelated changes into distinct commits.
- PRs should explain motivation, risks, and testing (`go test`, manual replay notes).
- Call out packaging/compatibility changes and any new dependencies.
- Note when behavior, default ports, or security posture (binding addresses) changes.

## Scope Limitations

- **No WebUI**: This project does not include browser-based map UI. Use external tools (e.g., tar1090) for visualization.
- **No CGO in CI**: Tests and vet run with `CGO_ENABLED=0` only. Hardware-dependent code is excluded from automated testing.
- **Network-only default**: Docker image and default builds assume network-only mode. Hardware support requires explicit CGO build.
