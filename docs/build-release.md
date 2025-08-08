# Build & Release

## Build
```bash
make deps
make build
```

## Tests & lint
```bash
make test
make lint
```

## Cross-platform builds
```bash
make build-all
```

## Releases
- Tag a version (e.g., `v1.0.0`)
- CI runs GoReleaser to produce multi-platform binaries, checksums, and signed artifacts
- Binaries are published to GitHub Releases
- Homebrew/Docker publishing handled in CI
