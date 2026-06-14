# Compatibility Fixtures

This directory contains small deterministic fixtures used to compare Go runtime behavior with dump1090-mutability behavior.

Fixtures are limited to protocol examples, expected JSON shapes, and minimal binary/text records needed for tests. Browser Web UI assets are intentionally not included.

When adding a fixture, include:

- source file or command used to derive it,
- upstream behavior being represented,
- expected consumer package,
- reason the fixture is small enough to keep in the repository.
