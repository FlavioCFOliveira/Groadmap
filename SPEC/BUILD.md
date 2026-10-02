# Build System Specification

## Overview

This specification defines the build system, cross-compilation targets, and CI/CD workflow for the Groadmap project.

## Go Toolchain

### Minimum Go Version

The required Go version is the one the `go` directive of `go.mod` declares, and
that directive is the only place the version is written; this specification does
not restate its value. This section is the authoritative statement of the rules
that set and move the directive; other specification files point here rather than
restate them. Three constraints bear on the directive, and it MUST satisfy all
three:

1. **Dependency floor, set by the module dependencies.** Go requires a module's
   `go` directive to declare a version no older than the `go` directive of any
   module it requires, so Groadmap cannot declare, or build with, a Go version
   older than the minimum any of its dependencies declares. GoGraph is the dependency this
   floor is recorded for; see `GRAPH.md § Dependency Maturity Risk` for the
   dependency itself.
2. **Security floor, set by a reachable-advisory requirement.** Four Go standard
   library advisories are reachable from Groadmap's own code — the vulnerable
   functions are called, not merely present in the module graph. The `go`
   directive MUST name a stable release that carries the fix for every one of
   them, and the release it names does.
3. **Line currency, and the Unicode data it selects — a deliberate decision.**
   Groadmap builds on the current Go release line. Where the two constraints above
   would admit an older release line, this decision is what sets the floor, and it
   is a decision rather than a consequence. Moving the floor onto a new release
   line can also change the Unicode version the tasks page's search and the graph
   key comparison read, because `golang.org/x/text/unicode/norm` selects its
   character data by toolchain (see `Unicode Data Rules`, Rule 5). Such an adoption
   is chosen and not inherited, and Rule 5 requires it to be treated as a change to
   both.

The four advisories item 2 names:

| Advisory | Package | Defect |
|----------|---------|--------|
| GO-2026-6091 | `html/template` | JavaScript regexp context tracking |
| GO-2026-6090 | `crypto/tls` | Post-handshake handshake messages accepted without limit |
| GO-2026-6089 | `net/http` | `ReadHeaderTimeout` not applied to the unencrypted HTTP/2 check |
| GO-2026-5972 | `encoding/asn1` | Maximum recursion depth not enforced |

Two of the four, `html/template` and `net/http`, sit on the `rmp web` request
path, which serves HTML over HTTP (see `WEB.md`). All four are toolchain
vulnerabilities rather than module vulnerabilities: the toolchain version alone
remediates them, and no dependency change can.

**A reachable advisory raises the patch floor.** The patch component of the
required version is a security floor, not routine version currency. It moves by
this rule:

- An advisory against the Go standard library that is **reachable** from
  Groadmap's own code raises the floor to the earliest **stable** release on the
  current minor line that carries the fix. A release candidate that carries it is
  not that release: Groadmap is neither built nor released from one. The `go`
  directive of `go.mod` is updated to name the stable release, and the table above
  is updated to record the advisory.
- An advisory that is reported but **not** called does not, by itself, raise the
  floor. That distinction is what keeps the rule workable: without it, every
  advisory anywhere in the module graph would move the floor.
- `govulncheck` is what draws the distinction, reporting separately the
  vulnerabilities whose code is called and those merely present. It is a
  diagnostic tool, not one of the six gates in `Validation Gates`, and no gate
  runs it.

The rule is enforced as a required step of the release procedure: `govulncheck`
MUST be run before a `v*` tag is created, and a reachable standard-library
advisory MUST raise the floor before the release is published. That step, and
what the release engineer does with each kind of result, is specified in
`VERSION.md § Pre-Release Vulnerability Check`.

The `go` directive in `go.mod` MUST NOT be lowered below the floor the three
constraints above set, and the CI and release toolchains MUST use the Go version
the directive names (or a later one). The CI and release workflows obtain that
version from `go.mod` via `go-version-file: go.mod`, so they track the directive
automatically and `go.mod` is the only place a pipeline reads it from. No
specification file restates the version.

Because the `go` directive names the patch version, the toolchain enforces the
floor itself: under the default `GOTOOLCHAIN=auto`, a machine whose installed Go
is older downloads and uses the required toolchain instead of building with the
wrong one, and a `GOTOOLCHAIN` pinned to an older release fails with an explicit
error instead of building. The floor therefore needs no manual installation step.

Groadmap MUST NOT be built or released with a toolchain older than the release
the `go` directive of `go.mod` names.

### External Dependencies

Groadmap has exactly **seven** direct module dependencies. Each one is listed
below, and each one is governed by its own set of rules.

The table lists them in the order the first `require` block of `go.mod` lists
them, and it carries **one row per requirement of that block and no other row**.
The two are therefore comparable line by line, which is how this section is kept
correct: a module that block requires and this table does not name is a defect in
this section, and so is a row naming a module that block does not require.

Every module in the table is pinned to an exact version, and that version is
written in `go.mod` alone; `go.sum` records its checksum. The table names no
version, and neither does any other specification file: the version of each
module is the one `go.mod` pins.

| Module | Path | Purpose |
|--------|------|---------|
| GoGraph | `github.com/FlavioCFOliveira/GoGraph` | Labelled property graph, Cypher engine, and durable store backing the `graph` command. See `GRAPH.md`. |
| Syntax highlighting | `github.com/alecthomas/chroma/v2` | The lexer engine and the HTML formatter that highlight a fenced code block of a Markdown field in the web interface by its declared language, the source of the generated lexer registry and style those blocks are highlighted with, and the CSS of the syntax-highlighting stylesheet. Only its root package and `formatters/html` are compiled into the binary. See `WEB.md § Markdown Rendering` and Markdown Rendering Rules below. |
| Regular expressions | `github.com/dlclark/regexp2/v2` | The regular-expression engine chroma requires. Groadmap imports it directly because the generated lexer registry holds a copy of chroma's Raku lexer, whose Go source imports it. See Markdown Rendering Rules below. |
| Markdown | `github.com/yuin/goldmark` | The CommonMark-compliant parser and renderer, with its GitHub Flavored Markdown, footnote, and definition-list extensions, that turns a Markdown field into the HTML the web interface shows. See `WEB.md § Markdown Rendering` and Markdown Rendering Rules below. |
| System calls | `golang.org/x/sys` | The operating-system calls the Go standard library does not publish. Groadmap imports the module at four sites, and each of the four compiles for one platform family only. `golang.org/x/sys/unix` is imported by `internal/terminal/terminal_unix.go`, for the `TIOCGWINSZ` ioctl that decides whether a stream is a terminal, and by `internal/testenv/pty_linux.go`, for the `/dev/ptmx` sequence that opens a pseudo-terminal pair. `golang.org/x/sys/windows` is imported by `internal/terminal/terminal_windows.go`, for the `GetConsoleMode` call that asks the console subsystem that same terminal question, and by `internal/graphlock/graphlock_windows.go`, for the `LockFileEx` and `UnlockFileEx` calls that are the graph store's mutual exclusion on that platform. See `GRAPH.md § Concurrency and Recovery` for the lock the last of those four implements. |
| Unicode data | `golang.org/x/text` | The Unicode character data the roadmap tasks page's search normalises a term and a task's searchable text by, and the knowledge-graph key comparison normalises keys by. `internal/unicodenorm` imports `golang.org/x/text/unicode/norm` — the Go project's own implementation of the normalisation forms UAX #15 defines — and no other package of the module. See `WEB.md § Roadmap Tasks Page` and `GRAPH.md § Node Key Uniqueness` for the rules that normalisation serves, and Unicode Data Rules below. |
| SQLite driver | `modernc.org/sqlite` | Pure-Go SQLite driver backing every roadmap database (`~/.roadmaps/<name>/project.db`). It is the storage engine for all task, sprint, and audit data: `internal/db` registers it under the driver name `sqlite` and opens every database connection through it. Being pure Go, it needs no C toolchain and builds under `CGO_ENABLED=0`. See `DATABASE.md` for the schema it stores, `ARCHITECTURE.md § 3. internal/db/` for the layer that opens it, and `IMPLEMENTATION.md § Database Connections` for the entry point and DSN form that layer must use. |

#### GoGraph Rules

1. GoGraph MUST be pinned to an exact, immutable version in `go.mod`, not a
   floating reference (no branch or moving target), so that builds are
   reproducible and the on-disk graph format is stable.
2. GoGraph is consumed at a pre-1.0 (`v0.y.z`) tag. Because that is a `v0`
   version, it is consumable directly at the bare module path
   `github.com/FlavioCFOliveira/GoGraph`, and `go.mod` pins a clean exact tag at
   that path. This exact-tag pin satisfies Rule 1.
3. The pinned tag is a `0.y.z` release, so GoGraph's public API is not yet stable
   and may change while the module matures toward `1.0.0`. The residual risks
   (pre-1.0 API instability and on-disk format change across pre-1.0 releases) and
   their mitigations are in `GRAPH.md § Dependency Maturity Risk`. Upgrading GoGraph is a
   change that MUST be re-validated against the acceptance criteria in `GRAPH.md`
   before release.
4. `go.sum` MUST record the checksum of the pinned version. The build MUST fail
   if the module checksum does not match.

#### System Call Rules

1. `golang.org/x/sys` MUST be pinned to an exact, immutable version in `go.mod`,
   not a floating reference, so that every build of a given commit issues the same
   system calls with the same constants. `go.sum` MUST record the checksum of the
   pinned version, and the build MUST fail if the checksum does not match.
2. **GoGraph Rule 3 does NOT transfer to this module, and MUST NOT be copied to
   it.** That rule treats an upgrade as a re-validation event against a whole
   acceptance-criteria set, because GoGraph is a `0.y.z` module whose public API is
   still moving and whose on-disk format could move with it. `golang.org/x/sys`
   also carries a `v0.y.z` version, but neither of those risks is the one it
   presents. It stores nothing on disk, so no format can change under a stored
   roadmap. And a change to the **name or the signature** of any of the four
   bindings Groadmap uses is a compilation failure, which the `build` gate catches
   on every target it compiles (see `Validation Gates`). The version number is
   therefore not what makes an upgrade of this module risky; Rule 3 names what
   does.
3. **Two of this module's four import sites are runtime-verified, and the other
   two are build-verified only.** Every job of both workflows runs on Linux
   (`runs-on: ubuntu-latest`), so the `test` gate executes
   `internal/terminal/terminal_unix.go` and `internal/testenv/pty_linux.go`, and
   executes them on Linux alone — the first of those two is the file every Unix
   target compiles, so its macOS, FreeBSD, and OpenBSD builds are verified no
   further than the OpenBSD targets of `Supported Build Targets` are. The
   two files that import `golang.org/x/sys/windows` —
   `internal/terminal/terminal_windows.go` and
   `internal/graphlock/graphlock_windows.go` — are compiled by the `build` gate for
   the two Windows targets and are never run by any gate. An upgrade that changed
   what `GetConsoleMode`, `LockFileEx`, or `UnlockFileEx` **does**, rather than what
   it is called, would therefore pass every gate and land unobserved. What is at
   stake in the second of those files is the graph store's mutual exclusion, whose
   contract is `GRAPH.md § Concurrency and Recovery`.

   This module is consequently upgraded **deliberately** — as its own change, with
   its own stated reason — and never as a side effect of a blanket refresh such as
   `go get -u ./...`. It is held on the same terms as the targets
   `Supported Build Targets` marks build-verified rather than runtime-verified: the
   limit of the verification is stated here rather than covered over by a gate that
   does not reach it.

#### Unicode Data Rules

1. `golang.org/x/text` MUST be pinned to an exact, immutable version in `go.mod`,
   not a floating reference. `go.sum` MUST record the checksum of the pinned
   version, and the build MUST fail if the checksum does not match.

   The pin carries more weight here than for any other dependency, and for a
   different reason. This module carries Unicode character data, and that data
   decides **which tasks a search term finds** on the roadmap tasks page (see
   `WEB.md § Roadmap Tasks Page`) and which knowledge-graph node keys count as the
   same key (see `GRAPH.md § Node Key Uniqueness`). A floated version is therefore
   not merely a build that differs from another build: it is a product that answers
   the same user's search differently.
2. **The module is admitted because the standard library cannot do this.** Go's
   `unicode` package publishes case mappings, character categories, and scripts,
   but it publishes no canonical decomposition data and no composition data. There
   is no way to normalise on the server without a module that carries that data,
   and `golang.org/x/text/unicode/norm` is the Go project's own implementation of
   it. Admitting this direct dependency was accepted deliberately on that
   ground, and on no other.
3. **The server normalises with this module, and nothing else normalises.**
   Groadmap takes Normalization Form C from `golang.org/x/text/unicode/norm` —
   `norm.NFC` — through one function in `internal/unicodenorm`, and both the
   roadmap tasks page's search and the key comparison of
   `GRAPH.md § Node Key Uniqueness` normalise through that function. Groadmap
   derives no normalisation data of its own from the module, ships no normalisation
   data or algorithm to the browser, and keeps no second statement of the
   algorithm: the search is applied on the server alone (see
   `WEB.md § Roadmap Tasks Page`).
4. **The import adds exactly one module to the graph.**
   `golang.org/x/text/unicode/norm` imports the standard library and
   `golang.org/x/text/transform`, which is a package of the same module.
   `golang.org/x/text`'s own `go.mod` requires `golang.org/x/tools`,
   `golang.org/x/mod`, and `golang.org/x/sync`, but those serve packages Groadmap
   does not import, so none of them enters the build or the indirect requirements
   of `go.mod`.

   No third module's version is constrained by this one either: the coupling
   between `modernc.org/sqlite` and `modernc.org/libc` that `SQLite Driver Rules`,
   Rule 3 records has **no analogue here**, and MUST NOT be invented for it.
5. **Neither this module's version nor the `go` directive fixes the Unicode
   version. The toolchain that runs the build does.** Inside `golang.org/x/text`,
   `unicode/norm` carries its character data as one set of tables per Unicode
   version, and selects among them with `//go:build` constraints on the Go release
   that compiles the package. Which Unicode version the server normalises against
   is therefore decided by the toolchain, and nothing on the `golang.org/x/text`
   line of `go.mod`, nor any other signal of its own, names it.

   **No line of `go.mod` pins that version, and the `go` directive MUST NOT be
   read as pinning it.** The directive is a floor, and `toolchain` is a floor too;
   neither is a ceiling. A machine whose installed Go is newer than the floor
   builds with the newer release, and the constraint above resolves against that
   release rather than against the directive. Measured: a build whose directive
   named an older Go release line than the toolchain that ran it normalised
   against the Unicode version the running toolchain selects, not the one the
   directive's line would select. The Unicode version of the server's rule is
   therefore a property of the **toolchain that ran** — which `Go Toolchain`
   constrains from below and nothing constrains from above — and not of any pin.

   The other half of that rule is already in the same position: the case fold
   reads the standard library's own tables, so its Unicode version comes from the
   toolchain alone. **Raising the Go floor in `Go Toolchain` is consequently also a
   change to the tasks page's search and to the key comparison of
   `GRAPH.md § Node Key Uniqueness`, and MUST be treated as one.** So is a build
   made with a toolchain newer than that floor, which no pin can prevent. Groadmap
   ships no copy of either rule to the browser, so there is no second copy that a
   change of Unicode version could leave behind: the server's rule is the only
   one, and it moves as a whole.
#### SQLite Driver Rules

1. `modernc.org/sqlite` MUST be pinned to an exact, immutable version in `go.mod`,
   not a floating reference, so that builds are reproducible and every build of a
   given commit runs the same storage engine against the same on-disk database
   format. `go.sum` MUST record the checksum of the pinned version, and the build
   MUST fail if the checksum does not match.
2. **`modernc.org/libc` and `modernc.org/memory` MUST be pinned to exact,
   immutable versions in `go.mod`, and they follow their own latest releases. They
   are NOT held to the versions that `modernc.org/sqlite`'s own `go.mod`
   requires.** Both modules are indirect dependencies of Groadmap, and `go.mod` is
   the only place their versions are written. Each pin is either the version the
   pinned driver's own `go.mod` requires or a later release of the same module;
   which of the two it is, at a given commit, is read by comparing Groadmap's
   `go.mod` with the driver's, not from this specification.

   When a dependency refresh moves either module to a later release, that release
   is kept; the pin is not reset to the version the driver's `go.mod` names. Go's
   minimal version selection never selects a module version older than one that
   another module in the build requires, so the driver's requirement remains a
   floor that the build enforces by itself, and the only departure from it that
   this rule admits is a later release.
3. **A later `modernc.org/libc` or `modernc.org/memory` than the driver requires
   is an accepted runtime risk.** `modernc.org/sqlite` does not link the C SQLite
   library. It ships the SQLite amalgamation transpiled into Go, and that
   transpiled code executes inside `modernc.org/libc`, a Go implementation of the
   C runtime (`modernc.org/memory` is the allocator that `modernc.org/libc` in turn
   requires). The driver's transpiled sources are generated against one specific
   `modernc.org/libc` version. Its author states, in the driver's package
   documentation and release notes, that a downstream module must pin the same
   `modernc.org/libc` version as the driver's own `go.mod`, and tracks that
   coupling upstream as GitLab issue #177. Upstream has also retracted a release of
   its own that attempted to resolve issue #177, because that release broke client
   modules.

   A later `modernc.org/libc` is therefore not guaranteed to be a drop-in
   replacement. A defect caused by the mismatch would be a runtime defect inside
   the storage engine — the component that owns the project's durable data — and
   not a compilation error. Rule 2 departs from the author's instruction
   deliberately, and this risk is accepted with it.
4. **No validation gate detects the mismatch.** No gate that `make check` runs —
   format, vet, unit tests, host build, `golangci-lint`, and the `gosec` security
   scan — and no test of the E2E suite compares the `modernc.org/libc` and
   `modernc.org/memory` versions with the ones the driver requires. A build that
   carries a later release therefore passes every gate unless the mismatch happens
   to break a behaviour one of those tests exercises. A green build is NOT evidence
   that the driver and `modernc.org/libc` operate correctly together, and neither
   is a clean security scan: a defect the mismatch introduces would surface only
   at runtime, inside the storage engine.

#### Markdown Rendering Rules

1. `github.com/yuin/goldmark`, `github.com/alecthomas/chroma/v2`, and
   `github.com/dlclark/regexp2/v2` MUST each be pinned to an exact, immutable
   version in `go.mod`, not a floating reference, so that every build of a given
   commit renders the same stored Markdown into the same HTML. `go.sum` MUST record
   the checksum of each pinned version, and the build MUST fail if a checksum does
   not match.
2. **chroma's regular-expression module is a direct dependency.** chroma requires
   `dlclark/regexp2`, under the module path the pinned chroma's own `go.mod` names,
   and the generated lexer registry imports it too, through its copy of chroma's
   Raku lexer (rule 5). It is therefore a row of the table above, pinned in the
   first `require` block of `go.mod`, and `go.sum` records its checksum.
3. **All three modules are pure Go and compiled in.** None needs a C toolchain, so
   the build stays under `CGO_ENABLED=0`, and neither they nor the generated lexer
   registry load a lexer, a style, or any other file at runtime or fetch anything
   from the network: the renderer is part of the binary (see
   `WEB.md § Self-Contained Deliverable`).
4. **An upgrade of any of the three modules changes rendered output, and is
   re-validated as such.** The HTML these modules produce is what the web interface
   inserts without escaping, so an upgrade MUST be re-validated against the
   Markdown acceptance criteria of `WEB.md § Acceptance Criteria`, including those
   that prove raw HTML is not emitted and dangerous links are not active. An
   upgrade of chroma also changes the CSS of the syntax-highlighting
   stylesheet and the generated lexer registry, which the test gate holds equal to
   the pinned chroma's output (see `WEB.md § Markdown Rendering`, rules 6 and 7).
5. **The lexer registry is generated from the pinned chroma and committed.** The
   lexers and the `github-dark` style that highlight a fenced code block come from
   a registry generated from the module source of the chroma version `go.mod`
   pins: a copy of every lexer that version ships, both those defined in its
   embedded XML definitions and those defined in Go, and of that style's
   definition. The generator runs through `go generate`, beside the generator of
   the syntax-highlighting stylesheet, and its output is committed, so `go build`
   runs no generation step. A test regenerates the registry in memory and fails
   when the committed files differ from it. The generated files carry chroma's
   copyright and permission notice. The registry's contents, lookup, and
   construction are specified in `WEB.md § Markdown Rendering`, rule 6.
6. **chroma's registry packages are not compiled into the binary.**
   `github.com/alecthomas/chroma/v2/lexers` and
   `github.com/alecthomas/chroma/v2/styles` each build their whole registry in a
   package-level initialiser, which every `rmp` invocation would pay whatever it
   does. No package compiled into the `rmp` binary imports either; tests and the
   generators, which the binary does not contain, may.

## Vendored Web Assets

The `rmp web` command serves a read-only web interface from assets embedded into
the binary at build time (see `WEB.md` and `ARCHITECTURE.md § 7. internal/web/ and
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
     framework — the syntax-highlighting stylesheet of rendered Markdown, and
     any further vendored CSS);
   - all client JavaScript, including the Tabler JavaScript and the D3.js
     knowledge-graph visualisation library (and the d3-sankey plugin) and any of
     their dependencies;
   - web fonts, including the Inter font, in its upright and its italic face, and
     the Tabler Icons webfont;
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
   `/static/...` (see `WEB.md § UI Framework`). Inter is committed as two
   variable-weight faces from the one `@fontsource-variable/inter` source, the
   upright `inter-latin-wght-normal.woff2` and the italic
   `inter-latin-wght-italic.woff2`, both under `internal/web/static/vendor/inter/files/`
   and both declared in `internal/web/static/vendor/inter/inter.css`
   (`WEB.md § UI Framework`, rule 4). `internal/web/static/vendor/LICENSES.md`
   records, for every vendored web asset, its location, its upstream project, and
   its licence; the Inter entry names both faces under the SIL Open Font License
   1.1. The vendored Tabler CSS and the vendored Tabler JavaScript are always taken
   from the same Tabler release, and they are upgraded together, in one change; the
   Tabler Icons webfont and the Inter font are separate projects and are not bound
   to that release. Upgrading or replacing any of these
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
| darwin | amd64 | - | darwin-amd64 | Intel macOS |
| darwin | arm64 | - | darwin-arm64 | Apple Silicon macOS |
| windows | amd64 | - | windows-amd64 | Windows x86_64 |
| windows | arm64 | - | windows-arm64 | Windows ARM64 |
| freebsd | amd64 | - | freebsd-amd64 | FreeBSD x86_64 |
| openbsd | amd64 | - | openbsd-amd64 | OpenBSD x86_64 |
| openbsd | arm64 | - | openbsd-arm64 | OpenBSD ARM64 |

Nine targets in total. The two OpenBSD targets depend on the storage engine:
`modernc.org/sqlite` lists `openbsd/amd64` and `openbsd/arm64` in its own
supported-platform table, and it is the only component that could hold them back,
since the binary is pure Go and links no C library.

**OpenBSD is build-verified, not runtime-verified.** Both targets cross-compile
under `CGO_ENABLED=0` and their architecture is confirmed with `file`, but no
OpenBSD host is used to execute the resulting binaries. They are released on the
same terms as every other target the project cannot run locally.

**Every supported target is 64-bit, and the project produces no 32-bit target.**
The `GOARM` column is therefore `-` in every row of the table above. The column
stays because the triple it completes is what both the target name and the build
environment are derived from, so a target with no ARM version states that rather
than leaving it implied. Three independent facts put 32-bit out of reach, and
each one alone would be sufficient:

1. **It does not compile.** `internal/graphserve`, the package behind
   `rmp graph serve`, imports GoGraph's `bolt/server` package. That package fixes
   the size of its transaction registry at 128 bytes and asserts that size at
   compile time from both directions. On a 32-bit platform the structure does not
   reach 128 bytes, one of the two assertions resolves to an array of negative
   length, and the package fails to compile before any Groadmap code is reached.
   No build tag or compiler flag avoids it, and `rmp` cannot be linked without
   that package.
2. **It was never a verified configuration.** Compiled for a 32-bit architecture
   and run natively, GoGraph's own test suite fails: several structure-size
   assertions in its labelled-property-graph package report sizes below the ones
   those tests require, and its PackStream tests do not compile at all, because a
   constant they use exceeds the range of a 32-bit `int`. The same tests pass on a
   64-bit host at the same commit, so the cause is the word size and not a flaky
   run. GoGraph publishes release binaries for 64-bit platforms only and runs no
   32-bit continuous-integration job.
3. **The stored graph would not be portable.** GoGraph's durable file format
   persists the platform-width integer types — `int`, `uint`, and `uintptr` — as
   64-bit values. A file written by a 64-bit build is misread by a 32-bit one, so
   a roadmap's graph could not be carried between the two even if the first two
   obstacles were removed.

`DEPLOY.md § Architecture Detection` states the consequence for installation: the
installation script recognises a 32-bit architecture and refuses it at detection,
rather than requesting a release asset that does not exist.

### Raspberry Pi Support

Raspberry Pi support is 64-bit support. One target serves it, `linux-arm64`, and
the board qualifies by running a 64-bit operating system.

| Model | Architecture | GOARM | Target Name |
|-------|--------------|-------|-------------|
| Raspberry Pi Zero 2 W (64-bit OS) | ARMv8 | N/A (arm64) | linux-arm64 |
| Raspberry Pi 3 (64-bit OS) | ARMv8 | N/A (arm64) | linux-arm64 |
| Raspberry Pi 4 (64-bit OS) | ARMv8 | N/A (arm64) | linux-arm64 |
| Raspberry Pi 5 (64-bit OS) | ARMv8 | N/A (arm64) | linux-arm64 |

**Compatibility Notes:**
- ARMv8 (arm64) is the only ARM architecture the project builds for, and the
  64-bit Raspberry Pi OS is what runs it. Every board in the table above has a
  64-bit build of that operating system available for it.
- **A board is not dropped by being absent from the table; an operating system
  is.** A Raspberry Pi 3, a Pi 4, a Pi 5, or a Pi Zero 2 W running a 32-bit
  operating system is not served by any target, although the hardware is 64-bit
  capable. Reinstalling it with the 64-bit operating system brings it back under
  `linux-arm64`; nothing else about the board has to change.
- **Hardware that can only ever run 32-bit is not supported at all.** That is the
  Raspberry Pi Zero, the Pi Zero W, the Pi 1, and the Pi 2. No target serves them,
  for the reasons given in Primary Platforms, and no 64-bit operating system can
  bring them back.

## GitHub Actions Workflow

Two workflows run in GitHub Actions, and both enforce the complete validation
gate set and run the end-to-end suite as a required job. `Validation Gates` — not
this section — is the authoritative statement of which gates exist, what each one
runs, and where each one is enforced, and
`The End-to-End Suite Is a Required Pipeline Job` is the authoritative statement
of the end-to-end job. This
section describes only the shape of each workflow: what triggers it, which jobs
it declares, and the order those jobs run in.

The release workflow is the only workflow that publishes a release, and the only
one that publishes anything. The CI workflow publishes nothing: no release, no
pre-release, and no build artefact.

Both workflows take the Go toolchain from `go.mod` (`go-version-file: go.mod`),
so both track the version required by `Go Toolchain`.

Both workflows build `rmp` with `-buildvcs=true`, and neither passes a `-X` linker
flag. The flag makes a build that finds the repository but cannot record its
version-control stamp fail, instead of producing a binary that reports
`(commit unknown)`. The flag cannot catch a build that finds no repository at all, so
each build job also runs a stamp check: after the build, and in the release
workflow before the job uploads its artefact, it runs `go version -m` on the built
binary and fails when the binary carries no
`vcs.revision` build setting. The stamp, the flag, and the check are specified in
`VERSION.md § Build Identification` and
`DEPLOY.md § How a Released Binary Carries Its Commit`.

### Release Workflow

**File:** `.github/workflows/release.yml`

**Trigger:** Push of tags matching `v*`

**Jobs:**

1. **test** (job name "Pre-release Tests")
   - Runs every validation gate except `build`: `fmt`, `vet`, `lint`, `test`, and
     `security`
   - Installs the tools those gates need, `golangci-lint` and `gosec`; a tool
     that is absent fails the job (see `Validation Gates`)
   - Every gate MUST pass before the build job starts

2. **e2e** (job name "End-to-End Tests")
   - Builds `rmp` and runs the end-to-end suite against it, as
     `The End-to-End Suite Is a Required Pipeline Job` specifies. It is not a
     gate, and it runs beside the test job rather than after it
   - The suite MUST pass before the build job starts

3. **build** — declares `needs: [test, e2e]`
   - The `build` gate: builds the binary for all nine Primary Platforms listed
     in `Supported Build Targets`, in the same order
   - Builds with `-buildvcs=true` and passes no `-X` linker flag, so a build that
     finds the repository but cannot stamp the binary with its commit fails (see
     `DEPLOY.md § How a Released Binary Carries Its Commit`)
   - Before uploading its artefact, runs `go version -m` on the built binary and
     fails the job when the binary carries no `vcs.revision` build setting (see
     `DEPLOY.md § How a Released Binary Carries Its Commit`)
   - Upload artifacts with naming: `release-{target}`
   - Archive naming: `rmp-{version}-{target}.tar.gz` (or `.zip` for Windows)
   - Generates a SHA256 checksum file for each archive

   `{target}` above is the Target Name from `Supported Build Targets`, and it is
   `{goos}-{goarch}` for every one of the nine targets, with no exception. The
   artifact for the 64-bit ARM Linux target is therefore `release-linux-arm64`
   and its archive `rmp-{version}-linux-arm64.tar.gz`, which is both what the
   workflow produces and what the installation script asks for (see
   `DEPLOY.md § Architecture Detection`).

4. **release** — declares `needs: build`
   - Downloads every build artifact and creates the GitHub release, attaching the
     archives and their checksums

**Permissions:**
```yaml
permissions:
  contents: read
```

The workflow grants `contents: read`. Only the `release` job, which creates the
GitHub release, raises its own permission to `contents: write`; no other job
writes to the repository, the `e2e` job included.

**Build Configuration:**
```yaml
env:
  GOOS: ${{ matrix.goos }}
  GOARCH: ${{ matrix.goarch }}
  GOARM: ${{ matrix.goarm }}
  CGO_ENABLED: 0
```

### CI Workflow

**File:** `.github/workflows/ci.yml`

**Trigger:** Push to the `main` branch, and pull requests targeting `main`

**Jobs:**

1. **test**
   - Runs the same gates as the release workflow's gate job: `fmt`, `vet`,
     `lint`, `test`, and `security`, installing `golangci-lint` and `gosec` in
     the job
   - Collects a coverage profile while running the `test` gate and uploads it.
     The upload reports coverage; it is not a gate, and its failure does not fail
     the job
   - Every gate MUST pass before the build job starts

2. **e2e** (job name "End-to-End Tests")
   - The same job as the release workflow's `e2e` job, run on every push to
     `main` and on every pull request targeting `main`
   - The suite MUST pass before the build job starts

3. **build** — declares `needs: [test, e2e]`
   - The `build` gate: a compile check of the four-target fast-feedback subset
     defined in `Validation Gates`. For each target it runs two steps: the
     `go build` of `rmp`, and the stamp check of the binary that build wrote
   - Builds with `-buildvcs=true` and passes no `-X` linker flag, and runs the same
     stamp check as the release workflow's build job, placed after the `go build`
     step
   - Packs no archive, generates no checksum, and uploads no artefact: the binary
     it builds does not leave the job

The CI workflow declares no other job, and it publishes nothing (see
`GitHub Actions Workflow`).

**Permissions:**
```yaml
permissions:
  contents: read
```

The CI workflow grants `contents: read` at workflow level, and no job raises it.
The gate job, the `e2e` job, and the build job read; none of them may write.

## Static Analysis

Two tools implement two of the validation gates: `golangci-lint` implements
`lint`, and `gosec` implements `security`. Each has its own section below, and
`Local Tool Resolution` specifies how the local gates find and check both. The
three rules in this preamble govern both of them.

**Both tools are pinned to an exact version.** The authoritative pins are two
`Makefile` variables: `GOLANGCI_LINT_VERSION` holds the `golangci-lint` pin, and
`GOSEC_VERSION` holds the `gosec` pin. This specification does not restate their
values. A tool's version is part of its gate's meaning, so the pin is what makes the gate mean the same thing in the
three places that enforce it (see `Validation Gates`). Three reasons set this
rule:

1. Rules are added, changed, and retired between releases of either tool, so two
   versions can disagree about the same source. Pinning keeps the finding set,
   and therefore each gate's verdict, the same everywhere.
2. It keeps the whole gate identical, not just the command. Scanned scope,
   accepted suppressions, and the active rule set all follow from the version.
3. An unpinned tool can fail a pipeline with no change in the repository. A
   release that adds a rule would break a build that no commit touched. Every
   input that decides whether a gate passes is pinned to an exact version in this
   project — these two tools, and the modules the build compiles against
   (GoGraph, the SQLite driver, and the two modules that driver requires) — and
   the workflows likewise pin every GitHub Action they use to an exact version.

**The pins bind local installations too, and the local gates enforce them.** A
machine can hold more than one copy of either tool, and the copy `PATH` finds
first need not be the pinned one: a packaged linter that tracks the latest
release, such as a snap in `/snap/bin`, can precede the directory `go install`
writes to. `make lint` and `make security` therefore prefer the copy in that
directory to any copy `PATH` finds, and consult `PATH` only when that directory
holds none. Whichever binary they resolve, as `Local Tool Resolution` specifies,
they run it only after checking its version against the pin, so a machine whose
tools are not the pinned versions fails the gate instead of passing it on a
different tool. Install the pinned version of both tools.

**Where the pins live, and how they change.** Each tool's pin has one
authoritative value and exactly two copies, and a pipeline reads it from nowhere
else:

1. **The `Makefile`, which holds the pin.** Each variable MUST be assigned
   exactly once, as `override <VAR> := <version>`, so that a value given on the
   make command line or in the environment cannot replace it. That value is the one the
   local version check compares against (see `Local Tool Resolution`).
2. **`.github/workflows/ci.yml`, which holds a copy.** The linter's copy is the
   `version` input the workflow passes to the `golangci-lint` action, and the
   scanner's copy is the version in the command that installs `gosec`.
3. **`.github/workflows/release.yml`, which holds a copy** in the same two forms.

Each workflow's copy of a pin MUST equal the `Makefile`'s value for that tool, and
the `test` gate MUST fail when a workflow's copy differs from the `Makefile`'s, as
it MUST when the `Makefile` does not assign a pin variable exactly once.

**How the workflows use the pins.** The workflows do not call `make lint` or
`make security`. Each runs the command the gate defines in `Validation Gates`,
through the tool it installs in the same job at its copy of the pin: the linter
through the `golangci-lint` action, whose `version` input is the copy, and the
scanner through the `gosec` that the copied install command writes. Neither
workflow therefore holds a version of its own. The value each one installs is
the `Makefile`'s, held equal to it by the `test` gate, so a pin raised in the
`Makefile` alone fails the next run of either workflow rather than letting a
workflow and the local gate run different versions. On a developer's machine,
which may hold several copies of either tool, the version check of
`Local Tool Resolution` does the same work: it refuses to run any copy that is
not the pinned version.

The pin on the `golangci-lint` action itself
(`golangci/golangci-lint-action@<version>`) is a separate pin, and the `Makefile`
does not hold it: it is written only in the two workflows. Both workflows MUST name
the same action pin, and the `test` gate MUST fail when they differ.

Raising any of these pins is a deliberate change, never an incidental one: it
updates every place that names the pin in the same commit, and the new version's
findings MUST be reviewed before the change lands, because a tool upgrade can fail
its gate on source that no commit modified.

### Local Tool Resolution

The `lint` and `security` targets of the `Makefile` never run their tool by its
bare name. Each resolves the tool to one binary through a make variable,
preferring the copy `go install` wrote to any copy `PATH` finds, then reads that
binary's version, and runs the tool only when the version matches the pin:
prefer, then assert. One rule governs both tools:

| Tool | Variable in the `Makefile` | How the gate reads the binary's version |
|------|----------------------------|-----------------------------------------|
| `golangci-lint` | `$(GOLANGCI_LINT)` | The linter's own report: `$(GOLANGCI_LINT) version --short` |
| `gosec` | `$(GOSEC)` | The version field of the `mod` line that `go version -m $(GOSEC)` prints |

The rule changes which binary a gate runs and nothing else. The command each gate
executes and the scope it covers are the ones `Validation Gates` defines, and the
version is the one `Static Analysis` pins. The rule is therefore not a difference
between the three places that enforce the gates: it is what makes the local gate
run the version the two workflows install on a fresh runner.

**Resolution.** When the caller does not set the variable, it resolves to one
binary, the first of these that exists:

1. **The copy `go install` wrote.** This is the tool's executable name inside the
   directory `go install` writes executables to, which is where the install
   command in the tool's own section puts the tool. That directory is the one
   `go env GOBIN` reports, when that value is not empty, and otherwise the `bin`
   directory of the first entry of the list `go env GOPATH` reports. The copy
   exists when that path names an existing file. A file that exists there but
   cannot be executed, or whose version cannot be read, is still the resolved
   binary: it fails the version check, and does not give way to a copy on
   `PATH`.
2. **The copy `PATH` finds first**, when the first step finds none: the tool's
   name joined to the first directory of `PATH` that holds an executable file of
   that name, which is the entry `which -a` lists first for the tool.
3. **No copy.** When neither step finds one, the variable holds the path of step
   1 — the path the install command would write — and the gate fails with the
   no-readable-version line below. When `go env` reports neither a `GOBIN` nor a
   `GOPATH`, that path, and therefore the variable, is empty.

`GOBIN` and `GOPATH` are read from `go env` rather than from the shell's
environment, because `go env` also reports a setting written with `go env -w`.
Only the first entry of `GOPATH` counts: `go install` writes to that entry's
`bin` directory and to no other, so appending `/bin` to a `GOPATH` that lists
several directories would name no directory at all.

The resolved binary is run by its path, never by its bare name. `PATH` decides
which binary a gate runs only when the directory `go install` writes to holds no
copy, and it never decides whether the gate passes: the version check applies to
a copy `PATH` found exactly as to the preferred one. A copy of another version
that comes first on `PATH` therefore cannot pass a gate. It is not run at all
when that directory holds a copy, and it fails the version check when that
directory holds none.

**Override.** A caller names a different binary by setting the variable, on the
make command line or in the environment:

```bash
make lint GOLANGCI_LINT=/opt/golangci-lint/bin/golangci-lint
GOSEC=/opt/gosec/bin/gosec make security
```

A command-line assignment takes precedence over the environment, and either takes
precedence over the default. An override changes where the binary is found and
nothing else: the version check applies to an overriding binary exactly as it
applies to the default one, and nothing disables the check. An override is never
subject to the fallback to `PATH`: the binary it names is the binary the gate
checks, present or not. A variable set to the empty string names no binary, and
fails the check rather than falling back to the default.

**Version check.** Before the tool runs, the gate reads the resolved binary's
version as the table above gives, and compares it with the pin: the value of the
tool's pin variable in the `Makefile`, which `Static Analysis` names. No single
method reads both tools. `gosec --version` prints `dev` for a
build made by `go install`, so the scanner's version comes from the Go build
information the toolchain embedded, the reading `Security Scan: gosec` already
prescribes. The linter's version comes from its own report, because
`Linter: golangci-lint` admits an installation by a package manager, and a
packaged build need not carry the linter's module version in its build
information. A snap is the measured case: its command is a launcher, and
`go version -m` on it reports the build information of the snap tool itself, not
of the linter.

A version is **readable** when, after one optional leading `v` is removed, it
begins with a decimal digit. An empty report is not readable, and neither is
`(devel)`, the value the toolchain records when it knows no module version. Two
readable versions **match** when they are equal after one leading `v` is removed
from each, so a report of `1.2.3` matches a pin of `v1.2.3`. Nothing looser
matches: there is no prefix match, no range, and no rule that accepts a newer
release.

**Failure.** A failed check writes a report of three parts to standard error: the
line that states the failure, the copies of the tool on `PATH`, and the command
that installs the pinned version. When the version is readable and does not
match, the first line is, according to the tool:

```
golangci-lint at {path} is version {found}, but the Makefile pins {pin}. Install golangci-lint {pin}, or name a binary of it with GOLANGCI_LINT=<path>.
gosec at {path} is version {found}, but the Makefile pins {pin}. Install gosec {pin}, or name a binary of it with GOSEC=<path>.
```

When no readable version can be obtained — the path names no file, the file
cannot be executed, `go version -m` finds no Go build information in it, or what
the reading yields is not readable — the first line is this one instead:

```
golangci-lint at {path} has no readable version, but the Makefile pins {pin}. Install golangci-lint {pin}, or name a binary of it with GOLANGCI_LINT=<path>.
gosec at {path} has no readable version, but the Makefile pins {pin}. Install gosec {pin}, or name a binary of it with GOSEC=<path>.
```

The lines that follow the first one are the same for both failures. When `PATH`
holds at least one copy of the tool, they are:

```
PATH holds these copies of {tool}, in PATH order:
  {entry}
Install the pinned version with: {install}
```

When `PATH` holds no copy of the tool, they are:

```
PATH holds no copy of {tool}.
Install the pinned version with: {install}
```

| Placeholder | Value |
|-------------|-------|
| `{path}` | The resolved path, exactly as the variable holds it; empty when the variable is empty |
| `{found}` | The version read from the binary, with a leading `v` added when it has none |
| `{pin}` | The value the `Makefile` assigns to the tool's pin variable, `GOLANGCI_LINT_VERSION` or `GOSEC_VERSION`, with its leading `v` |
| `{tool}` | The tool's executable name: `golangci-lint` or `gosec` |
| `{entry}` | A list, one line per copy, each line indented by two spaces: the path of every executable file named `{tool}` in a directory of `PATH`, in `PATH` order, which is the list `which -a {tool}` prints. Every such copy is listed, including a copy of the pinned version, and including the copy step 1 of the resolution found when a directory of `PATH` holds it |
| `{install}` | The install command of the tool's own section (`Linter: golangci-lint` or `Security Scan: gosec`), with its placeholder replaced by `{pin}` |

`<path>` is literal text, not a placeholder: it shows the reader the form of the
override. The report is the whole of what the check writes. It names the version
found when one is readable, the pin, every copy of the tool that `PATH` reaches,
and how to install the pinned one, so the reader can tell which copy was checked
and which copies shadow it or are shadowed by it. The gate then exits with a
non-zero status without running the tool, and `make check` fails with it. What `make` itself prints about the failed target
follows the report; that is `make`'s own text and is not specified here.

### Linter: golangci-lint

The project uses [golangci-lint](https://golangci-lint.run) for static analysis.
Configuration is in `.golangci.yml`, which declares `version: "2"` and therefore
requires a golangci-lint v2 release. The pinned version is the value of
`GOLANGCI_LINT_VERSION` in the `Makefile` (see `Static Analysis`).

**Install:**
```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<GOLANGCI_LINT_VERSION>
```

`<GOLANGCI_LINT_VERSION>` is a placeholder, not literal text: it stands for the
value the `Makefile` assigns to that variable, leading `v` included.

The module path MUST include the `/v2` suffix. The v1 path
(`github.com/golangci/golangci-lint/cmd/golangci-lint`) is still published and
still installs, but it resolves to the final v1 release, and a v1 binary cannot
read this project's `version: "2"` configuration.

A package manager may be used instead, provided it installs exactly the pinned
version.

**Verifying the installed version.** `golangci-lint --version` reports the
version the binary was built from, but it answers for whichever binary `PATH`
resolves first, which is not necessarily the one `go install` wrote. A packaged
linter earlier on `PATH` — a snap in `/snap/bin`, for example — shadows that
one, and because such packages track the latest release, the shadow can report
the pinned version itself. The check then passes while a bare `golangci-lint`
command runs a binary the pin never installed. Run `which -a golangci-lint` first:
it lists every match in `PATH` order, so it reveals a shadow that `--version` alone
cannot. Read the version of the entry it lists first, because that is the one a
bare `golangci-lint` command runs. `make lint` depends on that order only when the
directory `go install` writes to holds no copy, and in no case runs a copy that
fails its version check (see `Local Tool Resolution`).

In the workflows, the pinned version is the `version` input passed to the
`golangci-lint` GitHub Action, `version: <GOLANGCI_LINT_VERSION>`, with the
placeholder standing for the `Makefile`'s value as above. This is separate from
the pin on the action itself (`golangci/golangci-lint-action@<version>`), which
selects the action's code rather than the linter's; that pin is written only in
the two workflows, and both name the same one (see `Static Analysis`). Both pins
are exact, and neither substitutes for the other.

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
| `unused` | Dead code | Every declared symbol must have a reachable caller; a package is analysed together with its tests, so a helper used only by a test is live and one used by nothing is not |

**The set is closed, and nothing is enabled implicitly.** `.golangci.yml` opens
with `linters: default: none`, so a linter absent from the table above is not
running and the analyses this gate performs are exactly the ones enumerated
there. `unused` is in the set because no other member of it looks for a
declaration nothing reaches — `ineffassign` reports an assignment whose value is
never read, which is a different finding about live code — and without it a
symbol with no caller compiles, passes `go vet`, passes the tests, and passes
this gate. Dead code is not a style question here: a reader who finds a function
reasonably assumes something calls it, and the specification it appears to
implement is the one nothing implements.

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
optional check: it runs alongside the other five gates everywhere the gate set is
enforced (see `Validation Gates`). The pinned version is the value of
`GOSEC_VERSION` in the `Makefile` (see `Static Analysis`).

**Install:**
```bash
go install github.com/securego/gosec/v2/cmd/gosec@<GOSEC_VERSION>
```

`<GOSEC_VERSION>` is a placeholder, not literal text: it stands for the value the
`Makefile` assigns to that variable, leading `v` included. Each workflow installs
`gosec` with exactly this command, the placeholder replaced by that value.

**Verifying the installed version.** Unlike `golangci-lint`, `gosec` does not
report a usable version when it is built by `go install`: `gosec --version`
prints `dev`, because the release version is stamped by the project's own release
build. It therefore cannot confirm the pin. Read the module version the binary
was built from instead, with `go version -m "$(which gosec)"`, whose `mod` line
names the version. `which` resolves through `PATH`, so that command reads the copy
a bare `gosec` command runs; `make security` reads, and checks, the binary
`Local Tool Resolution` names instead.

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
`gosec` applies a configuration file only when `-conf` names one, and its
configuration reader is a JSON decoder, so a commented YAML document could not
serve as one even if the flag were passed.

**The register is gated.** A record no gate reads drifts away from the code, and
this one did. `internal/testenv/nosec_register_gate_test.go` parses `.gosec.yaml`
and sweeps the module for suppressions with `go/ast`, reading comment groups the
way `gosec` reads them, and fails when the two disagree in either direction: a
suppression the register does not account for, and a register line naming a count
the source no longer carries. It also refuses a suppression that names no rule —
which would suppress every rule on its node — and one that carries no
`-- justification`. The gate runs under `go test ./...`, so it is enforced in all
three places the validation gates run, exactly like the scan it describes.

**Two counts, both true.** The register accounts for every suppression in the
module. `gosec`'s own summary line reports a smaller number, because it does not
scan `_test.go` files unless `-tests` is passed, and smaller still on a platform
where a file carrying one is not built. The register states the module-wide count
and the rule that derives the scanner's from it; a published figure quoting the
scanner's summary is therefore consistent with the register rather than in
conflict with it.

**Test files are not scanned, and that gap is measured rather than forgotten.**
`gosec` skips `_test.go` files unless `-tests` is passed, and the invocation above
does not pass it. That is a deliberate decision, not an oversight, and it was
taken against a measurement rather than an assumption: the tree HAS been scanned
with `-tests`, and the result is recorded here so a later reader can weigh the
decision instead of rediscovering it.

Scanned with `-tests`, the scan reports 103 issues. Ninety-eight of them are in
test code. The remaining five are in production files — three in
`internal/commands/flags.go` and two in `internal/commands/graph.go`, all G602
(slice index out of range) — and all five were verified to be false positives:
each indexing site is preceded by an `i+1 >= len(args)` check that returns before
any index is taken, and the SSA analyzer that raises G602 does not model that
short-circuit.

Turning `-tests` on would therefore find no defect and cost two things. First, the
ninety-eight test-code findings would each have to be suppressed individually, and
because of the register gate above, every one of those suppressions would also
have to be argued for and recorded. That is a large amount of noise bought with no
defect found. Second, and worse, the five G602 would have to be suppressed
permanently at their sites, which would silence a genuine G602 appearing at those
same nodes later. Suppressing a real future finding to close a gap that currently
hides nothing is a worse position than the gap itself.

The gap that remains is therefore precisely this: findings that exist only in
`_test.go` files are not reported by the `security` gate. It is accepted, and it
is reviewed by re-running the scan with `-tests` when the question is reopened —
not by trusting this paragraph, which records a measurement taken at a point in
time.

**Where the gate runs.** The scan is not local-only. It runs in all three places
that enforce the validation gates — the local `make check`, the CI workflow
(`.github/workflows/ci.yml`), and the release workflow
(`.github/workflows/release.yml`) — under the same invocation in each. A green CI
run and a green release run are therefore evidence that the security gate passed.
Each workflow installs `gosec` in the job that runs the scan: a host without
`gosec` fails the job, and never skips the gate (see `Validation Gates`).

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
# 64-bit ARM Linux, including a Raspberry Pi Zero 2 W, 3, 4 or 5
# running a 64-bit operating system
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o ./bin/rmp-linux-arm64 ./cmd/rmp
```

## Validation Gates

Groadmap has exactly one set of validation gates, and this section is its single
authoritative definition. The gate set is the six gates in the table below.
Everything that enforces gates — the local pre-commit check, the CI workflow, and
the release workflow — enforces this set, whole and unchanged.

The end-to-end suite is not one of the six gates. Both workflows run it as a
separate required job, outside the gate set and beside it, and `make check` does
not run it; `The End-to-End Suite Is a Required Pipeline Job` specifies that job.
Every rule in this section that speaks of the gates, the gate set, or a subset of
it speaks of the six gates in the table and of nothing else.

`make check` is the aggregate command that runs the six gates locally, in the
order the target declares them, and every one of them MUST pass before a commit.

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

The `lint` and `security` rows give the command each gate runs. The `Makefile` runs
both commands through the binary `Local Tool Resolution` names, and only once that
binary's version matches the pin; the command and its scope are the ones in the
table.

Each gate is also available on its own, for example `make lint` or
`make security`. Running the gates individually is a convenience during
development; it does not replace `make check` before a commit.

What the `test` gate may contain is bounded by
`No Benchmarks and No Performance-Measurement Tests` below: the suite proves
behaviour, and it measures nothing.

### Where the Gate Set Is Enforced

The same six gates run in three places, and they mean the same thing in each:

1. **Locally, before every commit** — `make check`.
2. **In the CI workflow** (`.github/workflows/ci.yml`), on every push to `main`
   and on every pull request targeting `main`.
3. **In the release workflow** (`.github/workflows/release.yml`), on every push
   of a `v*` tag.

There is no per-pipeline exception. No pipeline runs a subset of the six gates,
and no gate belongs to one place only. A green CI run and a green release run are
therefore each evidence that all six gates passed — and, because each workflow
also runs the end-to-end job, that the end-to-end suite passed — and a `v*` tag
cannot publish a release unless the linter and the security scan both ran and
reported nothing. The end-to-end job runs in the two workflows and not in
`make check`; since it is not a gate, that is not a subset of the gate set and
not a difference between the three places the gates run.

In both workflows, the gates other than `build` run in the workflow's gate job,
and the `build` gate is the workflow's build job. The build job MUST declare
`needs:` on the gate job and on the end-to-end job, and the job of the release
workflow that publishes the release MUST declare `needs:` on the build job. No job may build or publish an
artefact in parallel with the gates or the end-to-end job, or independently of
them.

### A Missing Tool Is a Failure, Never a Skip

Two gates need a tool that the Go toolchain does not provide: `lint` needs
`golangci-lint` and `security` needs `gosec`. The install command for each one is
in its own section above. Both tools are pinned to an exact version, and every
place that runs those gates — the two workflows and a developer's machine alike —
installs the pinned version; see `Static Analysis`.

Each workflow MUST install both tools in the job that runs those gates. All of
the following are forbidden:

- Testing whether a tool is present and continuing when it is absent.
- Reporting a gate as passed, waived, or not applicable because its tool is
  missing from the host.
- Allowing a gate step to fail without failing its job, for example with
  `continue-on-error`.

If a tool cannot be installed, the job fails. No project policy permits skipping
a gate, and none may be invented: a host that lacks `gosec` is a host that fails
the run, not a host that is exempt from the security gate. The same rule governs
a local run — whoever lacks either tool has not run `make check`. Locally the rule
is enforced as well as stated: a missing tool, or a tool of another version, fails
its gate with the report `Local Tool Resolution` publishes.

**No release may report a gate as skipped.** Every gate MUST have run and passed
in the release workflow before a release is published. Release notes, release
records, and every other release artefact MUST NOT report a gate as skipped,
waived, not installed, or not applicable. If a gate did not run and pass, the
release MUST NOT be published.

### Permitted Differences Between the Three Pipelines

The gate set does not change from one place to another, but three gates run with
a wider scope in the workflows than they do locally. These are the only permitted
differences: each one adds to the local gate, and none of them replaces or
narrows it.

1. **`fmt` fails instead of rewriting.** Locally, `go fmt ./...` rewrites a badly
   formatted file in place. Both workflows run `go fmt ./...` and then
   `git diff --exit-code`, so unformatted source fails the job instead of being
   silently corrected inside the runner.
2. **`test` runs with the race detector and an explicit timeout, and CI also
   measures coverage.** Locally the gate is `go test ./...`. Both workflows run
   the suite verbosely, with the race detector, and under an explicit timeout
   (`go test -v -race -timeout=30m ./...`); the CI workflow additionally writes
   a coverage profile (`-coverprofile=coverage.out`) and uploads it. The
   coverage upload is reporting, not a gate, and its failure does not fail the
   job. As with the linter's timeout below, the timeout a workflow passes to
   `go test` is an execution limit, not a change of scope: the suite it runs is
   the same `./...` the local gate runs, with the same tests in it.

   **The timeout is explicit because the default one is easy to exceed without
   noticing.** `go test` applies its timeout to each package separately, never
   to the run as a whole, and the default is ten minutes per package. A package
   that reaches the limit does not report a failing test: it panics with
   `test timed out` and takes the whole job down. The tests under
   `internal/commands/` already run for close to 400 seconds under the race
   detector on a development machine, and a slower CI runner crosses the
   ten-minute ceiling outright. `-timeout=30m` is per package in the same way,
   and it makes that margin a decision this specification records rather than
   one inherited silently from the toolchain: a suite that grows into the limit
   has to raise this number deliberately, instead of failing a run that has
   nothing wrong with it.
3. **`build` is a host build locally and a matrix build in the workflows.**
   `make check` builds the binary for the host platform only. The CI workflow
   builds a four-target fast-feedback subset — `linux/amd64`, `linux/arm64`,
   `darwin/amd64`, and `darwin/arm64` — as a compile check, and publishes none
   of the binaries. The release workflow builds all nine Primary Platforms and ships them. That
   subset is a statement about feedback speed, not about portability: the `test`
   gate compiles every Primary Platform wherever it runs, because the unit-test
   suite cross-compiles the whole target table (see `Supported Build Targets`).
   No supported target can therefore break unnoticed in any of the three places.
   Both workflows also build with `-buildvcs=true`, where a local build keeps the
   default `-buildvcs=auto`, and each workflow build job checks the binary's stamp
   with `go version -m` after building it, which a local build does not; the
   release workflow does so before uploading it. A workflow therefore never publishes a binary that carries no commit (see
   `GitHub Actions Workflow`).

Nothing else may differ. In particular, `vet`, `lint`, and `security` run the
same command over the same scope in all three places:

- `vet` runs `go vet ./...`.
- `lint` runs `golangci-lint run ./...` at the pinned linter version, governed by
  `.golangci.yml`. A workflow may run it through the official `golangci-lint`
  GitHub Action, which installs the pinned version and runs that command; the
  timeout a workflow passes to the action is an execution limit, not a change of
  scope.
- `security` runs `gosec -exclude-dir=.claude/worktrees ./...`, at the pinned
  scanner version, so the scanned scope, the accepted `#nosec` suppressions, and
  the rule set are identical everywhere.

### The End-to-End Suite Is a Required Pipeline Job

The end-to-end suite, run by `tests/run_tests.py`, drives the compiled `rmp`
binary through its command-line surface. It is not a validation gate: it adds no
row to the table above, `make check` does not run it, and the rules of
`Validation Gates` that speak of the gate set do not count it. It is nevertheless
required in both workflows. A workflow run whose end-to-end job did not pass
builds no artefact and publishes none.

**Where it runs.** Each workflow declares a job with the ID `e2e` and the name
"End-to-End Tests":

1. **In the CI workflow**, on every push to `main` and on every pull request
   targeting `main`.
2. **In the release workflow**, on every push of a `v*` tag.

The job runs on the same Linux runner image as the gate job. It declares no
`needs:`, so it runs beside the gate job rather than after it, and the build job
declares `needs:` on it (see `Where the Gate Set Is Enforced`). No artefact is
therefore built, and none published, from a commit whose suite did not pass. The
job reads the repository and never writes to it: it raises no permission above
the workflow-level `contents: read`.

**What it runs.** The job takes these steps, in this order:

1. It checks out the repository.
2. It sets up the Go toolchain from `go.mod` (`go-version-file: go.mod`), as the
   gate job and the build job do.
3. It sets up Python, as the paragraph on Python below specifies.
4. It builds the binary it tests, with `go build -o ./bin/rmp ./cmd/rmp`, in a
   step of its own that runs before the suite. The job tests only a binary built
   by that step in the same run: nothing restores `./bin/rmp` from a cache or
   from an artefact of another job. A compilation error therefore fails the build
   step and is reported as a build failure, not as a failing test.
5. It runs the suite with `python3 -u tests/run_tests.py`. That is the default
   module set, the same one a local `python3 tests/run_tests.py` runs; the stress
   modules, which only `--stress` and `--all` select, are not part of the job.

The harness keeps its own staleness guard. Before any module runs, it compares
the binary with the newest source compiled into it and rebuilds a binary older
than that source; `tests/test_53_e2e_harness_binary_staleness.py` proves the
guard. The build step and the guard do not replace each other. The step makes
the tested binary a product of the run; the guard stops a stale binary from
reaching the suite by any path the step does not cover, and it is the only
protection a local run has.

**A failure names the failing module.** The harness runs each module as a
separate process and exits with a non-zero status when any module fails, which
fails the job. Before it exits, it writes the name of every failing module,
followed by that module's own output, so a failed job states which modules
failed and why. A check the harness runs before any module — a test file that no
registry lists, a suite class a module never runs, a module the index of
`tests/README.md` does not document — fails the job the same way, naming the
module concerned. The `-u` flag makes the interpreter's output unbuffered. The
harness announces each module as it starts it, and without `-u` those lines stay
in a buffer whenever standard output is not a terminal, which in a runner it
never is. With `-u`, a job cut off by its timeout still shows, as the last
module it announced, the module that was running.

**Timeout.** The job declares `timeout-minutes: 60`. The harness gives a module
no time limit of its own, so a module that hangs — a server that never exits, a
socket nobody closes — would otherwise hold the job until the platform's default
job limit, six hours. The suite runs for minutes on a development machine, on the
order of ten, and a hosted runner is slower. Sixty minutes is several times that
duration, so the suite's growth does not reach the limit unnoticed, while a hang
still ends within the hour. Like the `test` gate's `-timeout=30m` (see
`Permitted Differences Between the Three Pipelines`), the value is an execution
limit and not a change of scope; a suite that grows toward it raises it
deliberately.

**Python.** The suite needs Python 3.12 or later and nothing beyond the standard
library. Its sources use the replacement-field syntax that Python 3.12
introduced — a backslash inside the expression of an f-string — which an earlier
interpreter rejects at compile time; Python 3.11 refuses to compile
`tests/test_35_web_interface.py`. Each workflow installs Python in the job with
the `actions/setup-python` action, pinned to an exact version like every action
the workflows use (see `Static Analysis`), and passes it a `python-version` input
that names Python 3.12 or a later release. That value is written only in the two
workflows, and both MUST name the same one, so the suite runs under the same
interpreter in CI and at release. The job does not use the `python3` the runner
image happens to carry, because that one changes whenever the image does. Beyond
the Go toolchain and Python, the suite uses only tools the runner image provides
(`bash`, `sh`, `git`, `tar`, `unzip`, `sha256sum`, and `mktemp`), and the job
installs nothing else.

**Nothing skips the suite.** The rules of
`A Missing Tool Is a Failure, Never a Skip` bind this job as they bind a gate
job. No step tests whether Python or a tool is present and continues without it,
no step of the job carries `continue-on-error`, and a module that cannot run
fails the job. A release whose
end-to-end job did not run and pass MUST NOT be published.

### No Benchmarks and No Performance-Measurement Tests

**This project keeps no benchmarks and no performance-measurement tests, and no
statement in this specification may require one.** The `test` gate runs the suite
that proves behaviour. Nothing in that suite establishes how fast the product is,
how much memory it uses, how many allocations it makes, or what throughput it
sustains, and nothing anywhere in the repository does.

The following are forbidden, in every package and every pipeline:

1. A `Benchmark` function, whether or not any gate runs it. The gate set runs
   `go test ./...`, which does not execute benchmarks, so an unrun benchmark
   would be dead weight that no gate could ever defend.
2. A test that asserts a duration, an allocation count, a resident-memory figure,
   or a rate, or that compares two such quantities against each other.
3. A requirement in this specification whose only stated proof is a benchmark or
   a measurement of any of those quantities, and a performance target expressed
   as a figure to reach.

**Specified time-bounded behaviour is kept, and is proven without measuring.**
Several behaviours this specification fixes are defined in terms of time: the
graph statement budget (`GRAPH.md § Statement Time Budget`), the delay ladder and
the jitter ceiling of the single retry policy (`IMPLEMENTATION.md § Retry Logic`),
the graph store lock's bounded wait (`GRAPH.md § Lock Contention`), and the
signal-ownership window of a long-lived server (`GRAPH.md § Server Startup`).
Those requirements stand and the durations in them are contractual. What may not
stand is a proof that reads the clock. A test establishes such a behaviour by an
**observable other than elapsed time**, and the admissible observables are:

- an injected clock or an injected delay source, which the test advances itself;
- a count of attempts, of waits, or of steps taken;
- a count of statements issued, which is how a claim about what a page or a
  command costs the database is already settled (`WEB.md § Acceptance Criteria`,
  criterion 92, and `DATABASE.md § Resolve the Sprint of Many Tasks (Grouped)`);
- a state change that outlives the operation — a row, a file, a byte-for-byte
  comparison of the graph store before and after;
- the identity of the published error line, which distinguishes one failure
  cause from another;
- the exit code.

A test MUST NOT sleep and then assert on what the clock says, MUST NOT assert
that an operation finished within or after a given duration, and MUST NOT assert
that one operation was quicker than another. Where a duration is itself the
contract, the test asserts the **declared value** — that the constant the code
reads is the one this specification fixes, and that it is read from one
declaration rather than restated — and proves the behaviour around it by the
observables above.

**Requiring termination is not a timing assertion.** A test may require that an
invocation exits rather than blocks; what it asserts is that the process ended,
which is an outcome and not a duration. A run that hangs is ended by the `test`
gate's own timeout (see `Permitted Differences Between the Three Pipelines`),
which is the harness stopping a stuck run rather than a test measuring one.

**This is not a gate.** It adds no target to the table above, changes no command
any pipeline runs, and is enforced by review of what is written rather than by a
check that runs. Adding a static check for it would be a change to the gate set
and is not specified here.

## Artifact Structure

Every archive the project publishes carries the same three entries: the compiled
binary, the licence, and the quick-start guide. The licence is not optional
packaging — the project's licence travels with every binary the project
distributes — so this structure governs every published archive without
exception. The release workflow is the only workflow that publishes archives (see
`GitHub Actions Workflow`).

```
rmp-{version}-{target}.tar.gz
├── rmp                    # Binary
├── LICENSE                # License file
└── README.md              # Quick start guide
```

The three entries sit at the archive root. No entry is wrapped in a leading
directory, so extracting an archive puts the binary in the current directory and
the documented install step — extract, then move `rmp` onto the `PATH` (see
`DEPLOY.md § Manual Installation`) — works exactly as written.

**The binary entry's name and the archive's format both follow the target
operating system.** The drawing above shows the `.tar.gz` form, which every
target uses except Windows:

| Target OS | Binary entry | Archive format |
|-----------|--------------|----------------|
| `windows` | `rmp.exe` | `.zip` |
| Every other target OS | `rmp` | `.tar.gz` |

The Windows executable MUST carry the `.exe` extension: Windows does not run it
otherwise, and the installation script expects that name inside the archive. The
other two entries are identical in both forms, under exactly the names `LICENSE`
and `README.md`.

**Every published archive is covered.** The release workflow publishes one
archive per Primary Platform, named `rmp-{version}-{target}.{ext}`. `{version}` is
the `v*` tag being released, `{target}` is the Target Name from
`Supported Build Targets`, and `{ext}` is the format the table above gives for the
target's operating system.

Each archive is published alongside a `.sha256` checksum file. That file is a
separate published asset, not a fourth entry inside the archive.

## Acceptance Criteria

### Build Verification
- [ ] All matrix targets build successfully
- [ ] Binaries are statically linked (`CGO_ENABLED=0`)
- [ ] `make check` passes: format, vet, unit tests, host build, `golangci-lint`, and the `gosec` security scan all succeed. The security scan reports no unsuppressed finding (see Validation Gates and Security Scan: gosec)
- [ ] `make check` exits 0 on a machine where both tools are installed at their pinned versions by the documented install commands, including a machine whose `PATH` holds a different version of either tool ahead of them (see Local Tool Resolution)
- [ ] `make lint` runs the pinned linter even when another `golangci-lint` comes first on `PATH`: with the pinned version installed by the documented command, placing first on `PATH` a `golangci-lint` that fails whenever it is run leaves `make lint` passing. The same holds for `make security` with such a `gosec` first on `PATH`
- [ ] Neither the `lint` target nor the `security` target of the `Makefile` runs its tool by its bare name. Each runs the binary its variable resolves to. Unset, each variable resolves to the tool's name in the directory `go env GOBIN` reports, or, when that value is empty, in the `bin` directory of the first entry of `go env GOPATH`, when that path names an existing file, and otherwise to the first copy of the tool on `PATH` (see Local Tool Resolution)
- [ ] The preferred copy is never bypassed: with a `golangci-lint` of another release installed in the directory `go install` writes to and the pinned version first on `PATH`, `make lint` exits non-zero with the mismatch report naming the preferred copy's path, and does not fall back to the copy on `PATH`. The same holds for `make security` with a `gosec` of another release
- [ ] The fallback to `PATH` is checked like the preferred copy: with no copy in the directory `go install` writes to, a pinned `golangci-lint` that is the first copy on `PATH` passes `make lint` and is the binary it runs; a `golangci-lint` of another release that is the first copy on `PATH` fails `make lint`, even when the pinned version comes later on `PATH`. The same holds for `make security` with `gosec`
- [ ] A shadowing binary of the wrong version cannot produce a passing `make check`: on a machine where a `golangci-lint` or a `gosec` of another release is the first copy of that tool on `PATH`, `make check` either runs the pinned copy in the directory `go install` writes to, or exits non-zero with the mismatch report; in no case does it run the shadowing copy as its `lint` or `security` gate
- [ ] A version mismatch fails the gate before the tool runs and reports what it found: `make lint GOLANGCI_LINT=<path>`, with `<path>` a linter of another release, exits non-zero, writes to standard error the report Local Tool Resolution publishes — the mismatch line with that release as `{found}` and the pin as `{pin}`, then every copy of `golangci-lint` on `PATH` in `PATH` order (or the line stating that `PATH` holds none), then the install command with the pin in place of its placeholder — and does not run the linter. `make security GOSEC=<path>` behaves the same way for a `gosec` of another release
- [ ] A path that names no file, and a file from which no readable version can be obtained, each fail their gate with the report Local Tool Resolution publishes, whose first line is the no-readable-version line, for both tools. With no copy of a tool in the directory `go install` writes to and none on `PATH`, the gate fails with that report, `{path}` is the path the install command would write, and the second line states that `PATH` holds no copy of the tool
- [ ] The `security` gate reads `gosec`'s version from the `mod` line of `go version -m`, not from `gosec --version`: a `gosec` of the pinned version built by `go install`, whose `--version` prints `dev`, passes the version check
- [ ] An override set in the environment is honoured and checked exactly as one set on the make command line, and a command-line assignment takes precedence over the environment
- [ ] The `Makefile` assigns each tool's pin exactly once, as `override GOLANGCI_LINT_VERSION := <version>` and `override GOSEC_VERSION := <version>`, and the `test` gate fails when either variable is assigned any other number of times (see Static Analysis)
- [ ] The `test` gate fails when a workflow's copy of a tool pin differs from the `Makefile`'s value: the `version` input of the `golangci-lint` action in `.github/workflows/ci.yml` or `.github/workflows/release.yml` differing from `GOLANGCI_LINT_VERSION`, or the version in either workflow's `gosec` install command differing from `GOSEC_VERSION` (see Static Analysis)
- [ ] The `test` gate fails when `.github/workflows/ci.yml` and `.github/workflows/release.yml` name different `golangci/golangci-lint-action` pins (see Static Analysis)
- [ ] No specification file names the version of a Go module dependency, of the Go toolchain, or of `golangci-lint`, `gosec`, or the `golangci-lint` action: those versions are written in `go.mod`, the `Makefile`, and the two workflows (see Go Toolchain, External Dependencies, and Static Analysis)
- [ ] `go.mod` pins **every** direct dependency the External Dependencies table names — `github.com/FlavioCFOliveira/GoGraph`, `golang.org/x/sys`, `golang.org/x/text`, and `modernc.org/sqlite` — to an exact version, and the first `require` block of `go.mod` requires those four modules and no others, so the table and the block still agree row for row (see External Dependencies)
- [ ] `go.mod` pins `modernc.org/libc` and `modernc.org/memory` to exact versions, as SQLite Driver Rules, Rule 2 requires. Those versions are not checked against the ones the pinned `modernc.org/sqlite` requires: a later release is the risk Rule 3 accepts, and no gate compares them — neither any gate run by `make check` (format, vet, test, build, `golangci-lint`, `gosec`) nor the E2E suite (see External Dependencies, SQLite Driver Rules 2 to 4)
- [ ] Any change to the pinned `golang.org/x/text` version, and any raise of the `go` directive in `go.mod` (see Go Toolchain), has been treated as a change to the roadmap tasks page's search and to the graph key comparison: the tests of both pass on the new Unicode character data (see External Dependencies, Unicode Data Rules 5, `WEB.md § Roadmap Tasks Page`, and `GRAPH.md § Node Key Uniqueness`)
- [ ] Archive naming follows convention: `rmp-{version}-{target}.{ext}`
- [ ] Every published archive holds exactly the three entries Artifact Structure lists, and nothing else. Listing a `.tar.gz` (`tar -tzf`) shows `rmp`, `LICENSE`, and `README.md`; listing a Windows `.zip` (`unzip -l`) shows `rmp.exe`, `LICENSE`, and `README.md`. Every entry is at the archive root, with no leading directory component
- [ ] The `.sha256` file for each archive is published as a separate asset and is not an entry inside the archive
- [ ] Every web asset category (HTML templates, the stylesheet including the vendored Tabler CSS framework, all client JS including the vendored Tabler JavaScript and D3.js with the d3-sankey plugin and their dependencies, web fonts including the Inter font and the Tabler Icons webfont, icons and images, and the favicon) is embedded via `go:embed`; the build uses the Go toolchain only, with no Node.js or `node_modules` step (see Vendored Web Assets)
- [ ] The vendored Tabler CSS and the vendored Tabler JavaScript come from the same Tabler release: the release named in the licence banner at the head of the committed CSS file equals the release named in the banner at the head of the committed JavaScript file (see Vendored Web Assets)
- [ ] The web interface is fully self-contained: with networking disabled and with only the `rmp` binary present on disk (no sidecar files and no separate assets directory), `rmp web` serves the full UI — every page and the knowledge-graph visualisation render and function with no network egress (see Vendored Web Assets and `WEB.md § Self-Contained Deliverable`)

### Architecture Verification
- [ ] Use `file` command to verify binary architecture matches target
- [ ] No released binary is 32-bit. `file` reports `ELF 64-bit` for every Linux target, and `aarch64` for `linux-arm64`; no published archive name carries an `armv6` or `armv7` suffix (see Supported Build Targets)
- [ ] BSD binaries report the expected OS in the ELF note: `file` shows `version 1 (FreeBSD)` for the FreeBSD target and `version 1 (OpenBSD)` for both OpenBSD targets. This is the only verification these targets receive, since none of them is executed (see Supported Build Targets)

### CI/CD Verification
- [ ] The release workflow triggers on the push of a `v*` tag, and the CI workflow triggers on a push to `main` and on a pull request targeting `main`
- [ ] Each workflow file runs the complete gate set: reading `.github/workflows/ci.yml` and `.github/workflows/release.yml` shows a step for `fmt` (`go fmt ./...` followed by `git diff --exit-code`), `vet`, `lint`, `test`, and `security` in the gate job, plus a build job that is the `build` gate (see Validation Gates)
- [ ] The gate set in each workflow file matches the `check` target of the `Makefile` gate for gate: no gate is present in one and absent from the other
- [ ] Each workflow installs `golangci-lint` and `gosec` in the job that runs those gates. No step tests whether a tool is present and continues without it, and no gate step carries `continue-on-error`
- [ ] Both workflows pin `gosec` to the version the `Makefile` assigns to `GOSEC_VERSION`, installing it with the command Security Scan: gosec gives (`go install github.com/securego/gosec/v2/cmd/gosec@<GOSEC_VERSION>`, the placeholder replaced by that value), so that version is one and the same in `.github/workflows/ci.yml`, in `.github/workflows/release.yml`, and in the `Makefile`. Confirm an installed scanner with `go version -m "$(which gosec)"`, whose `mod` line names the version; `gosec --version` prints `dev` for a `go install` build and proves nothing
- [ ] Both workflows pin `golangci-lint` to the version the `Makefile` assigns to `GOLANGCI_LINT_VERSION`, passing that value as the `version` input of the `golangci-lint` action, so that version is one and the same in `.github/workflows/ci.yml`, in `.github/workflows/release.yml`, and in the `Makefile`. The action itself stays pinned to its own exact version, which is a separate pin, written only in the two workflows and the same in both. Confirm an installed linter with `which -a golangci-lint` and then `golangci-lint --version`; `--version` alone answers for whichever binary `PATH` resolves first, and a shadowing package that tracks the latest release can report the pinned version itself
- [ ] The documented local install command for each tool installs the pinned version, and the linter it installs can actually run this project: the golangci-lint module path carries the `/v2` suffix, so `golangci-lint run ./...` reads `.golangci.yml` (`version: "2"`) instead of rejecting it
- [ ] `gosec` runs in both workflows with the invocation the `security` gate defines (`gosec -exclude-dir=.claude/worktrees ./...`), so the scanned scope and the accepted `#nosec` suppressions are the same everywhere
- [ ] Every gate fails its job when it fails: introducing one violation at a time — an unformatted file, a `go vet` finding, a failing test, a `golangci-lint` violation, and an unsuppressed `gosec` finding — fails the workflow run in each case, in both workflows
- [ ] No artefact is built or published on a run whose gates or end-to-end suite did not pass: the build job declares `needs:` on the gate job and on the `e2e` job, and the release workflow's publishing job declares `needs:` on the build job
- [ ] The CI workflow publishes nothing: reading `.github/workflows/ci.yml` shows the jobs `test`, `e2e`, and `build` and no other, and no step of it packs an archive, generates a checksum, uploads a build artefact, or creates or deletes a release or a tag. The release workflow is the only workflow that publishes a release
- [ ] Each workflow declares a job with the ID `e2e`, which runs on a Linux runner, declares `timeout-minutes: 60`, holds no permission above `contents: read`, sets up Go from `go.mod`, sets up Python with `actions/setup-python` pinned to an exact version, builds the binary with `go build -o ./bin/rmp ./cmd/rmp` in a step before the suite, restores `./bin/rmp` from no cache and no artefact, and runs `python3 -u tests/run_tests.py`. No step of the job carries `continue-on-error` or tests whether a tool is present (see The End-to-End Suite Is a Required Pipeline Job)
- [ ] The `python-version` input of the `e2e` job is the same in `.github/workflows/ci.yml` and in `.github/workflows/release.yml`, and names Python 3.12 or a later release
- [ ] The end-to-end suite fails its workflow run when it fails: making one module fail fails the `e2e` job in both workflows, the job's log names that module, and neither workflow builds or publishes an artefact on that run
- [ ] The end-to-end suite stays outside the gate set: the gate table of Validation Gates still lists exactly the six gates, and the `check` target of the `Makefile` still runs exactly those six and not the suite
- [ ] The release workflow builds all nine Primary Platforms, and the CI build job builds the four-target fast-feedback subset (see Validation Gates, Permitted Differences Between the Three Pipelines)
- [ ] Both workflows build `rmp` with `-buildvcs=true` and pass no `-X` linker flag: reading the `go build` command of the build job in `.github/workflows/ci.yml` and in `.github/workflows/release.yml` shows the flag (see GitHub Actions Workflow)
- [ ] Every build job of both workflows runs the stamp check: reading `.github/workflows/ci.yml` and `.github/workflows/release.yml` shows, in each build job, a step placed after the `go build` step — and, in the release workflow, before the upload step — that runs `go version -m` on the built binary and fails the job when its output carries no `vcs.revision` build setting (see GitHub Actions Workflow and `DEPLOY.md § How a Released Binary Carries Its Commit`)
- [ ] Artifacts uploaded successfully
- [ ] Permissions set to minimum required in BOTH workflows: each grants `contents: read` at workflow level; exactly one job of the release workflow, `release`, raises that to `contents: write`, and no job of the CI workflow raises it. No gate job and no build job holds write permission
- [ ] No release reports any gate as skipped, waived, not installed, or not applicable
