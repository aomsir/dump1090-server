# Repository Guidelines

## Project Structure & Module Organization
- Root Go module `github.com/aomsir/dump1090-go`; core decoder lives in `dump1090-libs/`.
- `dump1090-libs/*.c`/`*.h` implement the ADS-B decoder (`dump1090`), console viewer, and FlightAware feeder; front-end assets and config JS sit in `public_html/`.
- `compat/` contains POSIX/macOS shims; `tools/` holds helper scripts and test pages; `debian/` contains packaging specs; README files document usage and JSON outputs.
- Tests are colocated with sources (e.g., `cprtests.c`), built via Make targets.

## Build, Test, and Development Commands
- `cd dump1090-libs`
- `make`: build `dump1090` and `view1090` with `gcc`, `pkg-config`, `librtlsdr`, and `libusb-1.0`.
- `make faup1090`: build the TSV feeder.
- `make test`: build and run `cprtests`; `make crctests` for CRC harness; `make clean` to remove objects/binaries.
- Debian packaging: install `librtlsdr-dev libusb-1.0-0-dev pkg-config debhelper`, then `dpkg-buildpackage -b` from `dump1090-libs/`.

## Coding Style & Naming Conventions
- Target C11; prefer 4-space indentation and avoid tabs; all code must pass `-Wall -Werror`.
- Keep functions small and testable; maintain existing module prefixes (`modes_*`, `net_*`, `cpr_*`); limit new globals and mark file-local helpers `static`.
- Use uppercase snake case for macros/constants and lowercase snake case for functions; keep config paths and defaults aligned with current behavior.

## Testing Guidelines
- Run `make test` before submitting; add focused test binaries beside the module (pattern `<feature>tests.c`) using the existing harness style.
- For data or output changes, replay captures through `dump1090`/`view1090` and verify JSON under `/run/dump1090-mutability/` renders via `public_html`.
- Keep logging quiet by default; guard verbose output behind existing flags.

## Commit & Pull Request Guidelines
- Use concise, imperative subjects (e.g., `Fix CPR edge cases`); separate unrelated changes into distinct commits.
- PRs should explain motivation, risks, and testing (`make test`, manual replay notes); call out packaging/compatibility changes and any new dependencies.
- Include screenshots only when modifying `public_html` UI; note when behavior, default ports, or security posture (binding addresses) changes.
