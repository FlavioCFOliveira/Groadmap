# Build System Specification

## Overview

This specification defines the build system, cross-compilation targets, and CI/CD workflow for the Groadmap project.

## Supported Build Targets

### Primary Platforms

| GOOS | GOARCH | GOARM | Target Name | Notes |
|------|--------|-------|-------------|-------|
| linux | amd64 | - | linux-amd64 | Standard x86_64 Linux |
| linux | arm64 | - | linux-arm64 | ARM 64-bit Linux |
| linux | arm | 6 | linux-armv6 | ARMv6 (Raspberry Pi Zero/1) |
| linux | arm | 7 | linux-armv7 | ARMv7 (Raspberry Pi 2/3/4 32-bit) |
| darwin | amd64 | - | darwin-amd64 | Intel macOS |
| darwin | arm64 | - | darwin-arm64 | Apple Silicon macOS |
| windows | amd64 | - | windows-amd64 | Windows x86_64 |
| windows | arm64 | - | windows-arm64 | Windows ARM64 |
| freebsd | amd64 | - | freebsd-amd64 | FreeBSD x86_64 |

### Raspberry Pi Support

| Model | Architecture | GOARM | Target Name |
|-------|--------------|-------|-------------|
| Raspberry Pi Zero / Zero W | ARMv6 | 6 | linux-armv6 |
| Raspberry Pi 1 | ARMv6 | 6 | linux-armv6 |
| Raspberry Pi 2 | ARMv7 | 7 | linux-armv7 |
| Raspberry Pi 3 (32-bit OS) | ARMv7 | 7 | linux-armv7 |
| Raspberry Pi 3 (64-bit OS) | ARMv8 | N/A (arm64) | linux-arm64 |
| Raspberry Pi 4 (32-bit OS) | ARMv7 | 7 | linux-armv7 |
| Raspberry Pi 4 (64-bit OS) | ARMv8 | N/A (arm64) | linux-arm64 |
| Raspberry Pi 5 (64-bit OS) | ARMv8 | N/A (arm64) | linux-arm64 |

**Compatibility Notes:**
- ARMv6 binaries are compatible with all Raspberry Pi models (backward compatible)
- ARMv7 binaries offer better performance on Pi 2/3/4 but won't run on Pi Zero/1
- ARMv8 (arm64) is already supported and should be used for 64-bit Raspberry Pi OS

## GitHub Actions Workflow

### Release Workflow

**Trigger:** Push of tags matching `v*`

**Jobs:**

1. **test**
   - Run `go fmt`, `go vet`
   - Run `go test ./...`
   - Validate code quality before build

2. **build**
   - Build binaries for all matrix targets
   - Upload artifacts with naming: `release-{goos}-{goarch}{goarm}`
   - Archive naming: `rmp-${version}-{target}.tar.gz` (or `.zip` for Windows)

**Permissions:**
```yaml
permissions:
  contents: read
```

**Build Configuration:**
```yaml
env:
  GOOS: ${{ matrix.goos }}
  GOARCH: ${{ matrix.goarch }}
  GOARM: ${{ matrix.goarm }}
  CGO_ENABLED: 0
```

### CI Workflow

**Trigger:** Pull requests to main branch

**Jobs:**
- Run tests
- Validate formatting
- Static analysis with `go vet`

## Static Analysis (Lint)

### Linter: golangci-lint

The project uses [golangci-lint](https://golangci-lint.run) for static analysis. Configuration is in `.golangci.yml`.

**Install:**
```bash
# macOS
brew install golangci-lint

# Any platform
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

**Run:**
```bash
golangci-lint run ./...
# or via Makefile:
make lint
```

### Enabled Linters

| Linter | Purpose | Policy enforced |
|--------|---------|----------------|
| `goerr113` | Error wrapping | No `errors.New` inside functions; all `fmt.Errorf` must use `%w` |
| `errcheck` | Error checking | All returned errors must be handled or explicitly discarded |

### Error Policy Rules (goerr113)

These patterns are **forbidden** and caught by the linter:

```go
// FORBIDDEN: bare errors.New inside a function (use package-level sentinels in utils/errors.go)
func doSomething() error {
    return errors.New("something failed")
}

// FORBIDDEN: fmt.Errorf without %w (loses error chain for errors.Is inspection)
return fmt.Errorf("opening roadmap %q: failed", name)

// CORRECT: wrap with %w to preserve chain
return fmt.Errorf("opening roadmap %q: %w", name, utils.ErrNotFound)
```

### Known Exclusions

Intentional deviations are documented in `.golangci.yml`:

| Location | Reason |
|----------|--------|
| `internal/commands/roadmap.go` WAL cleanup | `os.Remove` on `-shm`/`-wal` files is best-effort; missing files are expected |
| `internal/commands/sprint.go` sprint-stats fallback | Preserves E2E exit-code contract (see `test_12_sprint_stats.py:528`) |
| `internal/utils/time.go` package-level sentinels | Package-level `fmt.Errorf` declarations are permitted sentinel definitions |
| `*_test.go` files | Test helpers and deferred cleanups use idiomatic error-ignoring patterns |

## Build Commands

### Local Build

```bash
# Build for current platform
go build -o ./bin/rmp ./cmd/rmp

# Build for specific target
GOOS=linux GOARCH=amd64 go build -o ./bin/rmp-linux-amd64 ./cmd/rmp
```

### Cross-Compilation

```bash
# Raspberry Pi Zero / 1 (ARMv6)
GOOS=linux GOARCH=arm GOARM=6 CGO_ENABLED=0 go build -o ./bin/rmp-linux-armv6 ./cmd/rmp

# Raspberry Pi 2/3/4 32-bit (ARMv7)
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -o ./bin/rmp-linux-armv7 ./cmd/rmp

# Raspberry Pi 3/4/5 64-bit
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o ./bin/rmp-linux-arm64 ./cmd/rmp
```

## Artifact Structure

```
rmp-{version}-{target}.tar.gz
├── rmp                    # Binary
├── LICENSE               # License file
└── README.md             # Quick start guide
```

## Change History

| Date | Change | Description |
|------|--------|-------------|
| 2026-03-20 | Initial | Build system with all supported platforms including Raspberry Pi |

## Acceptance Criteria

### Build Verification
- [ ] All matrix targets build successfully
- [ ] Binaries are statically linked (`CGO_ENABLED=0`)
- [ ] Archive naming follows convention: `rmp-{version}-{target}.{ext}`

### Architecture Verification
- [ ] Use `file` command to verify binary architecture matches target
- [ ] ARM binaries show correct ARM version (ARMv6, ARMv7)

### CI/CD Verification
- [ ] Workflow triggers on tag push
- [ ] Test job passes before build
- [ ] Artifacts uploaded successfully
- [ ] Permissions set to minimum required (`contents: read`)
