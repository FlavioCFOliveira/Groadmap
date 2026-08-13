# Build System Specification

## Overview

This specification defines the build system, cross-compilation targets, and CI/CD workflow for the Groadmap project.

## Go Toolchain

### Minimum Go Version

Groadmap requires **Go 1.26.5** (or later). This section is the authoritative
statement of the required Go version; other specification files point here rather
than restate it. Two independent constraints set this floor:

1. **Minor floor (Go 1.26), set by the GoGraph dependency.** GoGraph declares Go
   1.26 as its minimum, so Groadmap cannot build on an earlier minor version. See
   `GRAPH.md § Dependency Maturity Risk` for the dependency itself.
2. **Patch floor (Go 1.26.5), set by a security requirement.** The Go standard
   library's `crypto/tls` is affected by GO-2026-5856, an Encrypted Client Hello
   privacy leak, in Go 1.26.4 and earlier; the fix ships in Go 1.26.5. This is a
   toolchain vulnerability, not a module vulnerability: it is remediated by the
   toolchain version alone, and no dependency change can remediate it.

The `go` directive in `go.mod` MUST declare `go 1.26.5` (or later), and the CI and
release toolchains MUST use the Go version that matches the `go` directive (Go
1.26.5 or later). The CI and release workflows obtain that version from `go.mod`
via `go-version-file: go.mod`, so they track the directive automatically.

Groadmap MUST NOT be built or released with a toolchain older than Go 1.26.5.

### External Dependencies

Groadmap has exactly two direct module dependencies. Both are listed below, and
each one is governed by its own set of rules.

| Module | Path | Version | Purpose |
|--------|------|---------|---------|
| GoGraph | `github.com/FlavioCFOliveira/GoGraph` | Exact tag **v0.11.0** | Labelled property graph, Cypher engine, and durable store backing the `graph` command. See `GRAPH.md`. |
| SQLite driver | `modernc.org/sqlite` | Exact version **v1.56.0** | Pure-Go SQLite driver backing every roadmap database (`~/.roadmaps/<name>/project.db`). It is the storage engine for all task, sprint, and audit data: `internal/db` registers it under the driver name `sqlite` and opens every database connection through it. Being pure Go, it needs no C toolchain and builds under `CGO_ENABLED=0`. See `DATABASE.md` for the schema it stores and `ARCHITECTURE.md § internal/db/` for the layer that opens it. |

#### GoGraph Rules

1. GoGraph MUST be pinned to an exact, immutable version in `go.mod`, not a
   floating reference (no branch or moving target), so that builds are
   reproducible and the on-disk graph format is stable.
2. GoGraph is consumed at the exact tag **v0.11.0**. Because v0.11.0 is a v0 (pre-1.0)
   version, it is consumable directly at the bare module path
   `github.com/FlavioCFOliveira/GoGraph`, and `go.mod` pins the clean exact tag
   `v0.11.0`. This exact-tag pin satisfies Rule 1.
3. v0.11.0 is a `0.y.z` release, so GoGraph's public API is not yet stable and may
   change while the module matures toward `1.0.0`. The residual risks (pre-1.0 API
   instability and on-disk format change across pre-1.0 releases) and their
   mitigations are in `GRAPH.md § Dependency Maturity Risk`. Upgrading GoGraph is a
   change that MUST be re-validated against the acceptance criteria in `GRAPH.md`
   before release.
4. `go.sum` MUST record the checksum of the pinned version. The build MUST fail
   if the module checksum does not match.

#### SQLite Driver Rules

1. `modernc.org/sqlite` MUST be pinned to an exact, immutable version in `go.mod`,
   not a floating reference, so that builds are reproducible and every build of a
   given commit runs the same storage engine against the same on-disk database
   format. The driver is consumed at the exact version **v1.56.0**. `go.sum` MUST
   record the checksum of that version, and the build MUST fail if the checksum
   does not match.
2. **`modernc.org/libc` and `modernc.org/memory` MUST be pinned to exactly the
   versions that `modernc.org/sqlite`'s own `go.mod` requires, and never to a later
   release.** For the pinned driver version, those versions are
   `modernc.org/libc v1.74.4` and `modernc.org/memory v1.11.0`. Both modules are
   indirect dependencies of Groadmap, but their versions are not free to float:
   pinning them to the driver's own required versions is a standing instruction
   from the driver's author, restated in its release notes and tracked upstream as
   GitLab issue #177.

   The mechanism is that `modernc.org/sqlite` does not link the C SQLite library.
   It ships the SQLite amalgamation transpiled into Go, and that transpiled code
   executes inside `modernc.org/libc`, a Go implementation of the C runtime
   (`modernc.org/memory` is the allocator that `modernc.org/libc` in turn
   requires). The driver's transpiled sources are generated against one specific
   `modernc.org/libc` version, and its release notes state that correct operation
   requires that matching version. A newer `modernc.org/libc` is therefore not a
   drop-in replacement: the mismatch is a runtime risk inside the storage engine —
   the component that owns the project's durable data — and not a compilation
   error. The coupling MUST be respected as stated rather than worked around:
   upstream retracted its own release `modernc.org/sqlite v1.33.0`, which was an
   attempt to resolve issue #177, because it broke client modules.
3. **No validation gate can detect a violation of Rule 2.** A build carrying a
   mismatched `modernc.org/libc` or `modernc.org/memory` version compiles cleanly,
   passes `go vet`, passes the unit tests, passes `golangci-lint`, passes the
   `gosec` security scan, and passes the full E2E suite. Every gate that
   `make check` runs therefore reports success, because not one of them compares
   the pinned versions against the driver's requirements. A green build is NOT
   evidence that the pins are correct, and neither is a clean security scan.
   Checking the two pins is a required manual step, and it MUST be performed
   whenever a dependency version changes and before any release.
4. **The pins MUST be re-derived after any `go get -u`.** A dependency refresh
   such as `go get -u ./...` floats `modernc.org/libc` and `modernc.org/memory` to
   their own latest releases and silently breaks Rule 2. After any such refresh,
   the required versions MUST be re-read from the driver's own `go.mod` inside the
   module cache — `$(go env GOMODCACHE)/modernc.org/sqlite@<version>/go.mod`, which
   is the authoritative statement of what the consumed driver requires — and, if
   the pins have drifted, reset to those exact versions with
   `go get modernc.org/libc@<version> modernc.org/memory@<version>`.
5. **The permanent "update available" report for these two modules is the expected
   state.** Because `modernc.org/libc` and `modernc.org/memory` release more often
   than the driver that pins them, `go list -m -u all` normally reports an
   available update for both while Rule 2 is satisfied. That report MUST be left
   alone: acting on it is exactly what breaks Rule 2. These two pins change only as
   part of a `modernc.org/sqlite` upgrade, and they then change to whatever the new
   driver version's `go.mod` requires — not to the newest available release.

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
| openbsd | amd64 | - | openbsd-amd64 | OpenBSD x86_64 |
| openbsd | arm64 | - | openbsd-arm64 | OpenBSD ARM64 |

Eleven targets in total. The two OpenBSD targets became available with
`modernc.org/sqlite` v1.56.0, which added `openbsd/amd64` and `openbsd/arm64` to
its own supported-platform table; the storage engine was the only component that
could have held them back, since the binary is pure Go and links no C library.

**OpenBSD is build-verified, not runtime-verified.** Both targets cross-compile
under `CGO_ENABLED=0` and their architecture is confirmed with `file`, but no
OpenBSD host is used to execute the resulting binaries. They are released on the
same terms as every other target the project cannot run locally.

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
   - Use Go 1.26.5 or later (see `Go Toolchain`)
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

## Static Analysis

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

### Security Scan: gosec

The project scans its Go source for security defects with
[gosec](https://github.com/securego/gosec). The scan is a validation gate, not an
optional check: `make check` runs it alongside the other five gates (see
`Validation Gates`).

**Install:**
```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

**Run:**
```bash
gosec -exclude-dir=.claude/worktrees ./...
# or via Makefile:
make security
```

`gosec` exits with status 1 when it reports at least one issue and with status 0
when it reports none, so any finding that is not suppressed fails the gate.

**Scope exclusion (`-exclude-dir=.claude/worktrees`).** `gosec` does not expand
`./...` the way the Go toolchain does. `go build ./...` and `go list ./...` skip
directories whose names begin with a dot, whereas `gosec` walks the tree and
analyses the Go files it finds beneath them. `.claude/worktrees` is a scratch
location for temporary git worktrees and holds no committed project source.
Without the exclusion, a Go file placed there would be analysed as though it were
project source and its findings would fail the gate. The flag keeps the scan
scoped to the repository's own packages. A directory that carries its own
`go.mod` belongs to a different module and is outside the scan either way.

**Accepted findings.** A finding that the project has reviewed and accepted is
annotated with a `#nosec` comment at the site in the Go source, which suppresses
that finding. The repository also carries `.gosec.yaml`, a commented record of
accepted findings and the reason each one is accepted. That file is a record for
reviewers, not scan configuration: the invocation above passes no `-conf` flag,
and `gosec` applies a configuration file only when `-conf` names one.

**The gate is local-only.** No pipeline runs `gosec`. Neither
`.github/workflows/ci.yml` nor `.github/workflows/release.yml` invokes it, so the
scan runs only where a developer or a release engineer runs `make check` or
`make security` on a local machine. A green CI run is therefore not evidence that
the security gate passed.

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

### Validation Gates

`make check` is the aggregate validation command. It runs the six gates below, in
the order the target declares them, and every one of them MUST pass before a
commit.

```bash
make check
```

| Target | Command | Purpose |
|--------|---------|---------|
| `fmt` | `go fmt ./...` | Format the source |
| `vet` | `go vet ./...` | Standard static analysis |
| `test` | `go test ./...` | Unit tests |
| `build` | `go build -o ./bin/rmp ./cmd/rmp` | Build the binary for the host platform |
| `lint` | `golangci-lint run ./...` | Lint (see `Linter: golangci-lint`) |
| `security` | `gosec -exclude-dir=.claude/worktrees ./...` | Security scan (see `Security Scan: gosec`) |

Each gate is also available on its own, for example `make lint` or
`make security`. Running the gates individually is a convenience during
development; it does not replace `make check` before a commit.

Two gates need a tool that the Go toolchain does not provide: `lint` needs
`golangci-lint` and `security` needs `gosec`. The install command for each one is
in its own section above.

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
- [ ] `make check` passes: format, vet, unit tests, host build, `golangci-lint`, and the `gosec` security scan all succeed. The security scan reports no unsuppressed finding (see Validation Gates and Security Scan: gosec)
- [ ] `go.mod` pins both direct dependencies to exact versions, and the `modernc.org/libc` and `modernc.org/memory` versions match exactly the versions required by the pinned `modernc.org/sqlite`. This is verified by reading the driver's own `go.mod` in the module cache, because no gate detects a mismatch — neither any gate run by `make check` (format, vet, test, build, `golangci-lint`, `gosec`) nor the E2E suite (see External Dependencies, SQLite Driver Rules 2 and 3)
- [ ] Archive naming follows convention: `rmp-{version}-{target}.{ext}`
- [ ] Every web asset category (HTML templates, the stylesheet including the vendored Tabler CSS framework, all client JS including the vendored Tabler JavaScript and D3.js with the d3-sankey plugin and their dependencies, web fonts including the Inter font and the Tabler Icons webfont, icons and images, and the favicon) is embedded via `go:embed`; the build uses the Go toolchain only, with no Node.js or `node_modules` step (see Vendored Web Assets)
- [ ] The web interface is fully self-contained: with networking disabled and with only the `rmp` binary present on disk (no sidecar files and no separate assets directory), `rmp web` serves the full UI — every page and the knowledge-graph visualisation render and function with no network egress (see Vendored Web Assets and `WEB.md § Self-Contained Deliverable`)

### Architecture Verification
- [ ] Use `file` command to verify binary architecture matches target
- [ ] ARM binaries show correct ARM version (ARMv6, ARMv7)
- [ ] BSD binaries report the expected OS in the ELF note: `file` shows `version 1 (FreeBSD)` for the FreeBSD target and `version 1 (OpenBSD)` for both OpenBSD targets. This is the only verification these targets receive, since none of them is executed (see Supported Build Targets)

### CI/CD Verification
- [ ] Workflow triggers on tag push
- [ ] Test job passes before build
- [ ] Artifacts uploaded successfully
- [ ] Permissions set to minimum required (`contents: read`)
