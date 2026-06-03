# Build System Specification

## Overview

This specification defines the build system, cross-compilation targets, and CI/CD workflow for the Groadmap project.

## Go Toolchain

### Minimum Go Version

Groadmap requires **Go 1.26.4** (or later). This is raised from the
previous minimum of Go 1.25 to satisfy the GoGraph dependency, which sets the
1.26 minor floor. The `go` directive in `go.mod` MUST declare `go 1.26.4` (or
later), and the CI and release toolchains MUST use the Go version that matches
the `go` directive (Go 1.26.4 or later). The CI and release workflows obtain
that version from `go.mod` via `go-version-file: go.mod`, so they track the
directive automatically.

### External Dependencies

| Module | Path | Version | Purpose |
|--------|------|---------|---------|
| GoGraph | `github.com/FlavioCFOliveira/GoGraph` | Exact tag **v0.1.0** | Labelled property graph, Cypher engine, and durable store backing the `graph` command. See `GRAPH.md`. |

Rules:

1. GoGraph MUST be pinned to an exact, immutable version in `go.mod`, not a
   floating reference (no branch or moving target), so that builds are
   reproducible and the on-disk graph format is stable.
2. GoGraph is consumed at the exact tag **v0.1.0**. Because v0.1.0 is a v0 (pre-1.0)
   version, it is consumable directly at the bare module path
   `github.com/FlavioCFOliveira/GoGraph`, and `go.mod` pins the clean exact tag
   `v0.1.0`. This exact-tag pin satisfies Rule 1.
3. v0.1.0 is a `0.y.z` release, so GoGraph's public API is not yet stable and may
   change while the module matures toward `1.0.0`. The residual risks (pre-1.0 API
   instability and on-disk format change across pre-1.0 releases) and their
   mitigations are in `GRAPH.md § Dependency Maturity Risk`. Upgrading GoGraph is a
   change that MUST be re-validated against the acceptance criteria in `GRAPH.md`
   before release.
4. `go.sum` MUST record the checksum of the pinned version. The build MUST fail
   if the module checksum does not match.

## Vendored Web Assets

The `rmp web` command serves a read-only web interface from assets embedded into
the binary at build time (see `WEB.md` and `ARCHITECTURE.md § internal/web/ and
the embedded HTTP server`). These assets are part of the Go build; they are not a
separate runtime artefact.

Rules:

1. **Self-contained binary: everything embedded via `go:embed`.** The shipped
   `rmp` binary MUST embed every component required to render and operate the web
   interface, with zero external runtime dependency. Every asset category lives
   under `internal/web/` (in `templates/` and `static/`) and is embedded with
   `go:embed`, so each becomes part of the compiled binary. The complete set of
   embedded asset categories is:
   - HTML templates;
   - the stylesheet (all CSS, including the vendored Tabler CSS framework — the UI
     framework — and any further vendored CSS);
   - all client JavaScript, including the Tabler JavaScript and the D3.js
     knowledge-graph visualisation library (and the d3-sankey plugin) and any of
     their dependencies;
   - web fonts, including the Inter font and the Tabler Icons webfont;
   - icons and images, including the Tabler Icons set;
   - the favicon;
   - any other static asset the interface requires.

   No web asset is read from the host filesystem at runtime, and the binary
   remains a single self-contained file. There is no sidecar file and no separate
   assets directory shipped alongside the binary (see
   `WEB.md § Self-Contained Deliverable` and
   `WEB.md § Embedded Asset Categories`).
2. **No JavaScript build toolchain.** The build uses the Go toolchain only. There
   is no Node.js, no `npm`/`yarn`, no `node_modules`, and no bundler step in the
   build or CI pipeline. Any JavaScript dependency is committed to the repository
   in already-built (vendored) form.
3. **Vendored UI framework: Tabler.** The web interface is built on the Tabler
   admin-dashboard framework (Bootstrap-based). Its already-built distribution —
   the compiled Tabler CSS and JavaScript — is committed under
   `internal/web/static/` and embedded with `go:embed`. It is served locally from
   the `/static/...` route and is never fetched from a content delivery network or
   any remote origin. The fonts and icons the Tabler shell depends on are likewise
   vendored: the Inter font and the Tabler Icons webfont are committed font files
   under `internal/web/static/`, embedded with `go:embed`, and served only from
   `/static/...` (see `WEB.md § UI Framework`). Upgrading or replacing any of these
   vendored Tabler assets — the framework CSS or JavaScript, the Inter font, or the
   Tabler Icons webfont — is a change to the committed asset and to this section,
   recorded in git.
4. **Vendored graph library: D3.js.** The interactive knowledge-graph
   visualisation uses D3.js together with the d3-sankey plugin (used for the Sankey
   layout). Their already-built distribution files are committed under
   `internal/web/static/` and embedded with `go:embed`. They are served locally
   from the `/static/...` route and are never fetched from a content delivery
   network or any remote origin (see `WEB.md § Knowledge-Graph Visualisation
   Library`). Upgrading or replacing the vendored library or its plugin is a change
   to the committed asset and to this section, recorded in git.
5. **No CDN and no outbound network at build or run time.** The build does not
   download web assets, and the running server makes no outbound request to load
   them; every asset is in the binary. No page references a content delivery
   network, a remote font host such as Google Fonts, or any other remote origin
   for a script, stylesheet, font, icon, image, or API. This covers the vendored
   Tabler CSS framework, the Tabler JavaScript, the Tabler Icons webfont, the
   Inter font, and the D3.js library with the d3-sankey plugin: all are embedded,
   locally-served assets with no remote origin. The interface renders and functions
   fully offline (see `WEB.md § Self-Contained Deliverable`).
6. **Embedding does not change the build targets.** Embedded assets are part of
   the Go package, so every target in Supported Build Targets builds the web
   interface in without any per-target asset handling. `CGO_ENABLED=0` static
   linking is unaffected.

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
   - Use Go 1.26.4 or later (see `Go Toolchain`)
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
| `err113` | Error wrapping | No `errors.New` inside functions; all `fmt.Errorf` must use `%w` |
| `errcheck` | Error checking | All returned errors must be handled or explicitly discarded |
| `bodyclose` | HTTP body close | Response bodies must be closed to avoid leaks |
| `gocritic` | Performance idioms | Performance preset + `sloppyReassign`; flags hot-path inefficiencies |
| `govet` | Static analysis (incl. `fieldalignment`) | Struct fields ordered to minimise padding; standard `vet` checks |
| `ineffassign` | Dead assignments | Detects assignments whose values are never read |
| `perfsprint` | Sprintf hotspots | Replaces `fmt.Sprintf("%s", s)` with cheaper alternatives |
| `prealloc` | Slice preallocation | Loops with known iteration count must preallocate slice capacity |

### Error Policy Rules (err113)

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

## Acceptance Criteria

### Build Verification
- [ ] All matrix targets build successfully
- [ ] Binaries are statically linked (`CGO_ENABLED=0`)
- [ ] Archive naming follows convention: `rmp-{version}-{target}.{ext}`
- [ ] Every web asset category (HTML templates, the stylesheet including the vendored Tabler CSS framework, all client JS including the vendored Tabler JavaScript and D3.js with the d3-sankey plugin and their dependencies, web fonts including the Inter font and the Tabler Icons webfont, icons and images, and the favicon) is embedded via `go:embed`; the build uses the Go toolchain only, with no Node.js or `node_modules` step (see Vendored Web Assets)
- [ ] The web interface is fully self-contained: with networking disabled and with only the `rmp` binary present on disk (no sidecar files and no separate assets directory), `rmp web` serves the full UI — every page and the knowledge-graph visualisation render and function with no network egress (see Vendored Web Assets and `WEB.md § Self-Contained Deliverable`)

### Architecture Verification
- [ ] Use `file` command to verify binary architecture matches target
- [ ] ARM binaries show correct ARM version (ARMv6, ARMv7)

### CI/CD Verification
- [ ] Workflow triggers on tag push
- [ ] Test job passes before build
- [ ] Artifacts uploaded successfully
- [ ] Permissions set to minimum required (`contents: read`)
