# Build System Specification

## Overview

This specification defines the build system, cross-compilation targets, and CI/CD workflow for the Groadmap project.

## Go Toolchain

### Minimum Go Version

Groadmap requires **Go 1.27.0** (or later). This section is the authoritative
statement of the required Go version; other specification files point here rather
than restate it. Three constraints bear on this floor, and only the third of them
sets it:

1. **Minor floor (Go 1.26), set by the GoGraph dependency.** GoGraph declares Go
   1.26 as its minimum, so Groadmap cannot build on an earlier minor version. See
   `GRAPH.md § Dependency Maturity Risk` for the dependency itself. The required
   version satisfies this floor without being set by it: 1.27 is the later line.
2. **Security floor (Go 1.27.0), set by a reachable-advisory requirement.** Four
   Go standard library advisories are reachable from Groadmap's own code — the
   vulnerable functions are called, not merely present in the module graph. Each
   of the four is fixed on the 1.26 line and on the 1.27 line alike, and
   Go 1.27.0-rc.3 is the release on the 1.27 line that fixes them, so every
   stable release of that line carries all four. On the line item 3 selects, this
   floor therefore sits at Go 1.27.0, the release the line opens with, and it
   raises the required version no further.
3. **Line currency, and the Unicode data it selects — a deliberate decision.**
   Neither constraint above reaches past Go 1.26: GoGraph asks for 1.26, and the
   four advisories are fixed on that line too. The third reason is therefore the
   one that sets the floor, and it is a decision rather than a consequence.
   Groadmap builds on the current Go release line, and moving onto 1.27 is also
   what makes the board search read Unicode 17.0.0 rather than Unicode 15.0.0,
   because `golang.org/x/text/unicode/norm` selects its character data by
   toolchain (see `Unicode Data Rules`, Rule 5). That adoption is chosen and not
   inherited, and Rule 5 requires such a change to be treated as a change to the
   board search.

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
  not that release: Groadmap is neither built nor released from one. This section
  is updated to name the stable release, and `go.mod` with it.
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

The `go` directive in `go.mod` MUST declare `go 1.27.0` (or later), and the CI and
release toolchains MUST use the Go version that matches the `go` directive (Go
1.27.0 or later). The CI and release workflows obtain that version from `go.mod`
via `go-version-file: go.mod`, so they track the directive automatically and
`go.mod` is the only place a pipeline reads it from. This specification and
`go.mod` MUST agree.

Because the `go` directive names the patch version, the toolchain enforces the
floor itself: under the default `GOTOOLCHAIN=auto`, a machine whose installed Go
is older downloads and uses the required toolchain instead of building with the
wrong one, and a `GOTOOLCHAIN` pinned to an older release fails with an explicit
error instead of building. The floor therefore needs no manual installation step.

Groadmap MUST NOT be built or released with a toolchain older than Go 1.27.0.

### External Dependencies

Groadmap has exactly **four** direct module dependencies. Each one is listed
below, and each one is governed by its own set of rules.

The table lists them in the order the first `require` block of `go.mod` lists
them, and it carries **one row per requirement of that block and no other row**.
The two are therefore comparable line by line, which is how this section is kept
correct: a module that block requires and this table does not name is a defect in
this section, and so is a row naming a module that block does not require.

| Module | Path | Version | Purpose |
|--------|------|---------|---------|
| GoGraph | `github.com/FlavioCFOliveira/GoGraph` | Exact tag **v0.12.0** | Labelled property graph, Cypher engine, and durable store backing the `graph` command. See `GRAPH.md`. |
| System calls | `golang.org/x/sys` | Exact version **v0.47.0** | The operating-system calls the Go standard library does not publish. Groadmap imports the module at four sites, and each of the four compiles for one platform family only. `golang.org/x/sys/unix` is imported by `internal/terminal/terminal_unix.go`, for the `TIOCGWINSZ` ioctl that decides whether a stream is a terminal, and by `internal/testenv/pty_linux.go`, for the `/dev/ptmx` sequence that opens a pseudo-terminal pair. `golang.org/x/sys/windows` is imported by `internal/terminal/terminal_windows.go`, for the `GetConsoleMode` call that asks the console subsystem that same terminal question, and by `internal/graphlock/graphlock_windows.go`, for the `LockFileEx` and `UnlockFileEx` calls that are the graph store's mutual exclusion on that platform. See `GRAPH.md § Concurrency and Recovery` for the lock the last of those four implements. |
| Unicode data | `golang.org/x/text` | Exact version **v0.41.0** | The Unicode character data the roadmap tasks board's search normalises a term and a task's searchable text by. `internal/unicodenorm` imports `golang.org/x/text/unicode/norm` — the Go project's own implementation of the normalisation forms UAX #15 defines — and no other package of the module. See `WEB.md § Roadmap Tasks Page` for the rule that normalisation serves and for the check that holds the client's copy of it equal to the server's. |
| SQLite driver | `modernc.org/sqlite` | Exact version **v1.57.0** | Pure-Go SQLite driver backing every roadmap database (`~/.roadmaps/<name>/project.db`). It is the storage engine for all task, sprint, and audit data: `internal/db` registers it under the driver name `sqlite` and opens every database connection through it. Being pure Go, it needs no C toolchain and builds under `CGO_ENABLED=0`. See `DATABASE.md` for the schema it stores, `ARCHITECTURE.md § 3. internal/db/` for the layer that opens it, and `IMPLEMENTATION.md § Database Connections` for the entry point and DSN form that layer must use. |

#### GoGraph Rules

1. GoGraph MUST be pinned to an exact, immutable version in `go.mod`, not a
   floating reference (no branch or moving target), so that builds are
   reproducible and the on-disk graph format is stable.
2. GoGraph is consumed at the exact tag **v0.12.0**. Because v0.12.0 is a v0 (pre-1.0)
   version, it is consumable directly at the bare module path
   `github.com/FlavioCFOliveira/GoGraph`, and `go.mod` pins the clean exact tag
   `v0.12.0`. This exact-tag pin satisfies Rule 1.
3. v0.12.0 is a `0.y.z` release, so GoGraph's public API is not yet stable and may
   change while the module matures toward `1.0.0`. The residual risks (pre-1.0 API
   instability and on-disk format change across pre-1.0 releases) and their
   mitigations are in `GRAPH.md § Dependency Maturity Risk`. Upgrading GoGraph is a
   change that MUST be re-validated against the acceptance criteria in `GRAPH.md`
   before release.
4. `go.sum` MUST record the checksum of the pinned version. The build MUST fail
   if the module checksum does not match.

#### System Call Rules

1. `golang.org/x/sys` MUST be pinned to an exact, immutable version in `go.mod`,
   not a floating reference, so that every build of a given commit issues the same
   system calls with the same constants. The module is consumed at the exact
   version **v0.47.0**. `go.sum` MUST record the checksum of that version, and the
   build MUST fail if the checksum does not match.
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
   not a floating reference. The module is consumed at the exact version
   **v0.41.0**. `go.sum` MUST record the checksum of that version, and the build
   MUST fail if the checksum does not match.

   The pin carries more weight here than for any other dependency, and for a
   different reason. This module carries Unicode character data, and that data
   decides **which tasks a search term finds** on the roadmap tasks board (see
   `WEB.md § Roadmap Tasks Page`). A floated version is therefore not merely a
   build that differs from another build: it is a product that answers the same
   user's search differently.
2. **The module is admitted because the standard library cannot do this.** Go's
   `unicode` package publishes case mappings, character categories, and scripts,
   but it publishes no canonical decomposition data and no composition data. There
   is no way to normalise on the server without a module that carries that data,
   and `golang.org/x/text/unicode/norm` is the Go project's own implementation of
   it. Admitting a fourth direct dependency was accepted deliberately on that
   ground, and on no other.
3. **Only this module's DECOMPOSITION is used. Its COMPOSITION is not, and MUST
   NOT be.** Groadmap takes canonical decomposition and canonical ordering — that
   is, NFD — from `golang.org/x/text/unicode/norm`, and performs the composition
   step itself, from a table it generates and ships to the browser (see
   `WEB.md § Roadmap Tasks Page`). **`norm.NFC.String`, `norm.NFC.Bytes`, and every
   part of `norm.NFKC` MUST NOT be called anywhere in Groadmap's own code.** Those
   are the entry points that compose, and composing through them is what this rule
   forbids.

   **The tests of `internal/unicodenorm` are the single exception, and only as a
   measuring standard.** A test in that package MAY call `norm.NFC.String` as the
   reference a result of Groadmap's own is compared against — never as a value any
   caller receives, and in no other package and no non-test file. The exception is
   required rather than tolerated: this rule asserts that Groadmap's composition
   agrees with the module over every single code point and departs from it only
   where the module is wrong, and an assertion no test is allowed to measure is one
   no reader can falsify.

   **Exactly one use of `norm.NFC` is admitted in the rule itself: the
   Full_Composition_Exclusion lookup that derives the composition exclusions.**
   `norm.NFC.IsNormalString` reports whether a string is already in Normalization
   Form C. For a single code point carrying a canonical decomposition, that is
   false exactly when Full_Composition_Exclusion is true of it, so the call is how
   Groadmap reads which code points Unicode excludes from composition. It returns
   a property of its argument and never a transformed string, so the composition
   defect described below cannot reach a value any caller receives; and it runs in
   the one-time derivation of the composition table — once per process on the
   server, and once per run of the generator — never on a search.

   **`norm.NFC.QuickSpanString` is NOT that lookup, and MUST NOT be used as one.**
   It reports a boundary up to which a string is *quick-checked* to be in
   Normalization Form C, and its own documentation states that the boundary is not
   guaranteed to be the largest such. For a single code point the boundary is
   therefore the whole of it or none of it, and none of it means NFC_QC **is not
   Yes** — `No` **or** `Maybe` — where the property this lookup needs is `No`
   alone. NFC_QC=Maybe is carried by every code point that can be the second
   element of a primary composite, and such a code point is not excluded from
   composition; it is the reason the composition table has any entries at all.

   The two questions had the same answer under Unicode 15.0.0, because no code
   point then carried both a canonical decomposition and NFC_QC=Maybe, and the
   distinction was invisible for exactly that reason. Unicode 16.0.0 introduced
   twelve that do — `U+113C5`, `U+113C7` and `U+113C8`, `U+16121` through
   `U+16128`, and `U+16D68` — and the quick-check form reported all twelve as
   excluded, dropping their composites from the table and leaving Groadmap's NFC
   returning the decomposition of a code point that composes, which is not
   Normalization Form C. The two forms disagree on **132** code points in all; the
   other 120 carry no canonical decomposition, and the derivation never asks the
   question of a code point that carries none, so those never reached the table.
   Twelve was the symptom; the predicate was the fault.

   **The exclusions are not derivable from the decomposition data, which is what
   the admission rests on.** A script exclusion such as `U+0958`, and a
   post-composition-version exclusion such as `U+2ADC`, decompose exactly as an
   ordinary composite does, so no inspection of the decompositions can separate
   them; the exclusions have to be read from somewhere. Reading the property from
   the same module that supplies the decompositions makes the two move together
   when the Unicode version moves. The alternative is to write the exclusions into
   this specification, or into the code, as a list, and that is precisely the
   stored copy of expected results that `WEB.md § Roadmap Tasks Page`, **What keeps
   the shipped rule equal to the server's**, refuses: such a list would go stale in
   silence the day the Unicode version changed, in the one document a reader
   trusts.

   **A test MAY hold the derived exclusions to a transcribed copy of the property,
   and the transcription is deliberate.** The reference the test in
   `internal/unicodenorm` compares against is copied from
   `DerivedNormalizationProps.txt` of the Unicode Character Database. It is
   neither derived nor fetched, and three reasons rule out the alternatives:

   - **The property has four sources, and two of them cannot be derived at all.**
     UAX #15, under *Composition Exclusion Types*, names four: script-specific
     exclusions, post composition version exclusions, singleton decompositions,
     and non-starter decompositions. Of the first two it states that the list
     "cannot be computed from the decomposition mappings in the Unicode Character
     Database, and must instead be explicitly listed".
   - **The last two are not derivable from this module either.**
     `norm.NFD.Properties(b).Decomposition()` returns the FULL, recursive
     canonical decomposition, so a singleton such as `U+212B` — whose one-step
     mapping is `U+00C5` — is indistinguishable from an ordinary two-character
     composite. Deriving them would mean transcribing `UnicodeData.txt` instead,
     which is larger and no more authoritative.
   - **Asking the module is what the test exists to check.** A reference has to
     come from outside the thing measured, so the module cannot be it.

   Admitting a stored copy here does not contradict what the paragraph above
   refuses for this specification and for the code. That refusal is of a stored
   copy the rule is READ from; this is a reference the rule is HELD to, and when
   the Unicode version moves it fails the test and names the code points rather
   than going stale in silence. Fetching the file at test time is forbidden for
   the same reason: a test that reached the network would fail offline, and would
   follow a property that had moved instead of reporting it.

   Groadmap declines to cross that boundary even where crossing would pay, and
   declining is what keeps the boundary meaningful rather than nominal. Gating the
   server's own normalisation on a `norm.NFC` check, so that text already in
   Normalization Form C skips the work, is a correct and tempting optimisation;
   **it is declined**, because it would put a `norm.NFC` call on the search path
   itself, where the next reader would find a precedent instead of a boundary.

   The reason no composition at all is taken from the module is a defect in it at
   the pinned version: it composes a supplementary starter as though the starter
   were its **low 16 bits**. Three witness values, each of which the platform's own
   normalisation and Groadmap's leave unchanged:

   | Input | `norm.NFC` returns | Correct result | Why |
   |-------|--------------------|----------------|-----|
   | `U+1003C` `U+0338` | `U+226E` | unchanged | `U+1003C` masked to 16 bits is `U+003C` |
   | `U+10041` `U+0301` | `U+00C1` | unchanged | `U+10041` masked to 16 bits is `U+0041` |
   | `U+1042B` `U+0308` | `U+04F8` | unchanged | `U+1042B` masked to 16 bits is `U+042B` |

   Measured over every supplementary starter against each of the 72 code points a
   composition can consume, the defect spans **15,342** pairs over **6,232**
   distinct leading code points. The decomposition Groadmap does use is unaffected
   by it. Groadmap's own composition agrees with the module on all **1,112,064**
   single code points, which is the claim the test the exception above admits
   measures directly, and it still composes the 33 legitimate supplementary
   composites: `U+11935` followed by `U+11930` gives `U+11938`. It is therefore
   NFC where the module is right and NFC where the module is wrong, not a private
   variant of it.

   **A later simplification that replaces the composition step with a call to
   `norm.NFC.String` would reintroduce the defect silently**, because the shipped
   table and the server would then disagree and the guard test in Rule 6 would fail
   with the client correct and the server wrong. This rule exists so that the reason
   is found here rather than rediscovered.
4. **The import adds exactly one module to the graph.**
   `golang.org/x/text/unicode/norm` imports the standard library and
   `golang.org/x/text/transform`, which is a package of the same module.
   `golang.org/x/text`'s own `go.mod` requires `golang.org/x/tools`,
   `golang.org/x/mod`, and `golang.org/x/sync`, but those serve packages Groadmap
   does not import, so none of them enters the build or the indirect requirements
   of `go.mod`.

   No third module's version is constrained by this one either: the coupling
   `SQLite Driver Rules`, Rule 2 imposes on `modernc.org/libc` and
   `modernc.org/memory` has **no analogue here**, and MUST NOT be invented for it.
5. **Neither this module's version nor the `go` directive fixes the Unicode
   version. The toolchain that runs the build does.** Inside `golang.org/x/text`
   v0.41.0, `unicode/norm` selects its character data with a build constraint on
   the toolchain: `tables15.0.0.go` is compiled under `//go:build !go1.27` and
   `tables17.0.0.go` under `//go:build go1.27`. Built with the Go version that
   `Go Toolchain` requires, the server normalises against Unicode 17.0.0, with no
   mention of a Unicode version on the `golang.org/x/text` line of `go.mod` and no
   other signal of its own.

   **No line of `go.mod` pins that version, and the `go` directive MUST NOT be
   read as pinning it.** The directive is a floor, and `toolchain` is a floor too;
   neither is a ceiling. A machine whose installed Go is newer than the floor
   builds with the newer release, and the constraint above resolves against that
   release rather than against the directive. Measured: a build whose directive
   read `go 1.26.6`, made on a machine running Go 1.27.0, already normalised
   against Unicode 17.0.0. The Unicode version of the server's rule is therefore a
   property of the **toolchain that ran** — which `Go Toolchain` constrains from
   below and nothing constrains from above — and not of any pin.

   The other half of that rule is already in the same position: the case fold
   reads the standard library's own tables, so its Unicode version comes from the
   toolchain alone. **Raising the Go floor in `Go Toolchain` is consequently also a
   change to the board search, and MUST be treated as one.** So is a build made
   with a toolchain newer than that floor, which no pin can prevent and which
   Rule 6 is what catches.
6. **Unlike the driver's coupling, a drift here IS caught, and by an ordinary
   test.** `SQLite Driver Rules`, Rule 3 records that no gate can detect a
   mismatched `modernc.org/libc`. The opposite holds for this module. The copy of
   the rule the binary ships to the browser is generated from the server's own
   normalisation, and a guard test compares the two over the whole of Unicode, so a
   change of Unicode version — whether the module version or the toolchain that ran
   produced it — fails the `test` gate until that shipped copy is regenerated from
   the new data. A server whose rule moved is **caught**, never silently followed.
   The check itself is specified in `WEB.md § Roadmap Tasks Page`.

   What that gate does not do is decide whether the new Unicode version is wanted.
   It reports that the rule moved; regenerating is a deliberate act, taken with the
   change that caused it — a new module version, or a new toolchain — and never as
   a way of making a failing test pass.

   A second test, in `internal/unicodenorm`, covers the other direction. It holds
   the composition exclusions this package derives to a transcribed copy of
   Full_Composition_Exclusion over the whole of Unicode, in both directions, and
   holds this package's Normalization Form C equal to the module's over every
   single code point. The first guard catches a client and a server that have
   drifted apart; this one catches a server that has drifted away from Unicode
   while the client faithfully follows it.

#### SQLite Driver Rules

1. `modernc.org/sqlite` MUST be pinned to an exact, immutable version in `go.mod`,
   not a floating reference, so that builds are reproducible and every build of a
   given commit runs the same storage engine against the same on-disk database
   format. The driver is consumed at the exact version **v1.57.0**. `go.sum` MUST
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

Two workflows run in GitHub Actions, and both enforce the complete validation
gate set. `Validation Gates` — not this section — is the authoritative statement
of which gates exist, what each one runs, and where each one is enforced. This
section describes only the shape of each workflow: what triggers it, which jobs
it declares, and the order those jobs run in.

Both workflows take the Go toolchain from `go.mod` (`go-version-file: go.mod`),
so both track the version required by `Go Toolchain`.

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

2. **build** — declares `needs: test`
   - The `build` gate: builds the binary for all eleven Primary Platforms listed
     in `Supported Build Targets`, in the same order
   - Upload artifacts with naming: `release-{target}`
   - Archive naming: `rmp-{version}-{target}.tar.gz` (or `.zip` for Windows)
   - Generates a SHA256 checksum file for each archive

   `{target}` above is the Target Name from `Supported Build Targets`. It is
   `{goos}-{goarch}` for nine of the eleven targets, and for the two ARM targets
   the ARM version follows a literal `v`: `linux-armv6` and `linux-armv7`. The
   artifact for the ARMv6 target is therefore `release-linux-armv6` and its
   archive `rmp-{version}-linux-armv6.tar.gz`. Writing that suffix as
   `{goarch}{goarm}` would name them `linux-arm6` and `linux-arm7`, which is
   neither what the workflow produces nor what the installation script asks for
   (see `DEPLOY.md § Architecture Detection`).

3. **release** — declares `needs: build`
   - Downloads every build artifact and creates the GitHub release, attaching the
     archives and their checksums

**Permissions:**
```yaml
permissions:
  contents: read
```

The workflow grants `contents: read`. Only the `release` job, which creates the
GitHub release, raises its own permission to `contents: write`; no other job
writes to the repository.

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

2. **build** — declares `needs: test`
   - The `build` gate: builds the four-target fast-feedback subset defined in
     `Validation Gates`, for the rolling `dev` pre-release

3. **dev-release** — declares `needs: build`
   - Publishes the rolling `dev` pre-release. It runs only for a push to `main`,
     never for a pull request

**Permissions:**
```yaml
permissions:
  contents: read
```

The CI workflow follows the same least-privilege pattern as the release
workflow. It grants `contents: read` at workflow level, and only the
`dev-release` job — the one job that writes to the repository, because it
replaces the rolling `dev` release and its tag — raises its own permission to
`contents: write`. The gate job and the build job read; neither may write.

## Static Analysis

Two tools implement two of the validation gates: `golangci-lint` implements
`lint`, and `gosec` implements `security`. Each has its own section below. The
three rules in this preamble govern both of them.

**Both tools are pinned to an exact version.** The pinned versions are
`golangci-lint v2.13.1` and `gosec v2.28.0`. A tool's version is part of its
gate's meaning, so the pin is what makes the gate mean the same thing in the
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

**The pins bind local installations too.** `make lint` and `make security` run
whichever `golangci-lint` and `gosec` the shell finds on `PATH`; neither target
installs or verifies a version. A developer whose `PATH` resolves either tool to
a different version is therefore not running the gate this specification
defines, and `make check` on that machine does not mean what a green pipeline
means. Install the pinned version of both tools, and re-check after any change
to `PATH` or to how either tool was installed.

**Where the pins live, and how they change.** Each tool's version appears in
exactly three places: the tool's section below, `.github/workflows/ci.yml`, and
`.github/workflows/release.yml`. All three MUST name the same version for a
given tool. Raising either pin is a deliberate change, never an incidental one:
it updates all three in the same commit, and the new version's findings MUST be
reviewed before the change lands, because a tool upgrade can fail its gate on
source that no commit modified.

### Linter: golangci-lint

The project uses [golangci-lint](https://golangci-lint.run) for static analysis.
Configuration is in `.golangci.yml`, which declares `version: "2"` and therefore
requires a golangci-lint v2 release. The pinned version is **v2.13.1**.

**Install:**
```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1
```

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
the pinned version itself. The check then passes while `make lint` runs a binary
the pin never installed. Run `which -a golangci-lint` first: it lists every
match in `PATH` order, so it reveals a shadow that `--version` alone cannot.
Read the version of the entry it lists first, because that is the one the gate
runs.

In the workflows, the pinned version is the `version` input passed to the
`golangci-lint` GitHub Action: `version: v2.13.1`. This is separate from the pin
on the action itself (`golangci/golangci-lint-action@v9.3.0`), which selects the
action's code rather than the linter's. Both are exact, and neither substitutes
for the other.

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
optional check: it runs alongside the other five gates everywhere the gate set is
enforced (see `Validation Gates`). The pinned version is **v2.28.0**.

**Install:**
```bash
go install github.com/securego/gosec/v2/cmd/gosec@v2.28.0
```

**Verifying the installed version.** Unlike `golangci-lint`, `gosec` does not
report a usable version when it is built by `go install`: `gosec --version`
prints `dev`, because the release version is stamped by the project's own release
build. It therefore cannot confirm the pin. Read the module version the binary
was built from instead, with `go version -m "$(which gosec)"`, whose `mod` line
names the version.

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
# Raspberry Pi Zero / 1 (ARMv6)
GOOS=linux GOARCH=arm GOARM=6 CGO_ENABLED=0 go build -o ./bin/rmp-linux-armv6 ./cmd/rmp

# Raspberry Pi 2/3/4 32-bit (ARMv7)
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -o ./bin/rmp-linux-armv7 ./cmd/rmp

# Raspberry Pi 3/4/5 64-bit
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o ./bin/rmp-linux-arm64 ./cmd/rmp
```

## Validation Gates

Groadmap has exactly one set of validation gates, and this section is its single
authoritative definition. The gate set is the six gates in the table below.
Everything that enforces gates — the local pre-commit check, the CI workflow, and
the release workflow — enforces this set, whole and unchanged.

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

Each gate is also available on its own, for example `make lint` or
`make security`. Running the gates individually is a convenience during
development; it does not replace `make check` before a commit.

### Where the Gate Set Is Enforced

The same six gates run in three places, and they mean the same thing in each:

1. **Locally, before every commit** — `make check`.
2. **In the CI workflow** (`.github/workflows/ci.yml`), on every push to `main`
   and on every pull request targeting `main`.
3. **In the release workflow** (`.github/workflows/release.yml`), on every push
   of a `v*` tag.

There is no per-pipeline exception. No pipeline runs a subset of the gates, and
no gate belongs to one place only. A green CI run and a green release run are
therefore each evidence that all six gates passed, and a `v*` tag cannot publish
a release unless the linter and the security scan both ran and reported nothing.

In both workflows, the gates other than `build` run in the workflow's gate job,
and the `build` gate is the workflow's build job. The build job MUST declare
`needs:` on the gate job, and the job that publishes artefacts MUST declare
`needs:` on the build job. No job may build or publish an artefact in parallel
with the gates, or independently of them.

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
a local run — whoever lacks either tool has not run `make check`.

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
   `darwin/amd64`, and `darwin/arm64` — for the rolling `dev` pre-release. The
   release workflow builds all eleven Primary Platforms and ships them. That
   subset is a statement about feedback speed, not about portability: the `test`
   gate compiles every Primary Platform wherever it runs, because the unit-test
   suite cross-compiles the whole target table (see `Supported Build Targets`).
   No supported target can therefore break unnoticed in any of the three places.

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

## Artifact Structure

Every archive the project publishes carries the same three entries: the compiled
binary, the licence, and the quick-start guide. The licence is not optional
packaging — the project's licence travels with every binary the project
distributes — so this structure governs every published archive without
exception, the release archives and the rolling `dev` pre-release archive alike.

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

**Every published archive is covered.** Two workflows publish archives, and this
structure governs both:

| Archive | Name | Published by |
|---------|------|--------------|
| Release archive | `rmp-{version}-{target}.{ext}` | The release workflow, for all eleven Primary Platforms |
| Dev pre-release archive | `rmp-dev-{sha}-{target}.tar.gz` | The CI workflow, for the four-target fast-feedback subset |

`{version}` is the `v*` tag being released, `{target}` is the Target Name from
`Supported Build Targets`, `{ext}` is the format the table above gives for the
target's operating system, and `{sha}` is the first seven characters of the
commit the pre-release was built from. Every dev archive is a `.tar.gz` holding
`rmp`, because the fast-feedback subset contains no Windows target; were one ever
added to that subset, the format and binary-name rule above would apply to it
exactly as it does to a release archive.

Each archive is published alongside a `.sha256` checksum file. That file is a
separate published asset, not a fourth entry inside the archive.

## Acceptance Criteria

### Build Verification
- [ ] All matrix targets build successfully
- [ ] Binaries are statically linked (`CGO_ENABLED=0`)
- [ ] `make check` passes: format, vet, unit tests, host build, `golangci-lint`, and the `gosec` security scan all succeed. The security scan reports no unsuppressed finding (see Validation Gates and Security Scan: gosec)
- [ ] `go.mod` pins **every** direct dependency the External Dependencies table names — `github.com/FlavioCFOliveira/GoGraph`, `golang.org/x/sys`, `golang.org/x/text`, and `modernc.org/sqlite` — to an exact version, and the first `require` block of `go.mod` requires those four modules and no others, so the table and the block still agree row for row (see External Dependencies)
- [ ] The `modernc.org/libc` and `modernc.org/memory` versions match exactly the versions required by the pinned `modernc.org/sqlite`. This is verified by reading the driver's own `go.mod` in the module cache, because no gate detects a mismatch — neither any gate run by `make check` (format, vet, test, build, `golangci-lint`, `gosec`) nor the E2E suite (see External Dependencies, SQLite Driver Rules 2 and 3)
- [ ] Any change to the pinned `golang.org/x/text` version, and any raise of the Go floor in Go Toolchain, has been treated as a change to the roadmap tasks board's search: the copy of the search rule the binary ships to the browser was regenerated from the new Unicode character data, and the guard test that holds it equal to the server's own rule passes (see External Dependencies, Unicode Data Rules 5 and 6, and `WEB.md § Roadmap Tasks Page`)
- [ ] Archive naming follows convention: `rmp-{version}-{target}.{ext}`
- [ ] Every published archive holds exactly the three entries Artifact Structure lists, and nothing else. Listing a `.tar.gz` (`tar -tzf`) shows `rmp`, `LICENSE`, and `README.md`; listing a Windows `.zip` (`unzip -l`) shows `rmp.exe`, `LICENSE`, and `README.md`. Every entry is at the archive root, with no leading directory component
- [ ] The dev pre-release archive holds the same three entries as a release archive. This is checked on a published `dev` asset, not only on a release asset, because both workflows pack archives and only one of them builds release tags
- [ ] The `.sha256` file for each archive is published as a separate asset and is not an entry inside the archive
- [ ] Every web asset category (HTML templates, the stylesheet including the vendored Tabler CSS framework, all client JS including the vendored Tabler JavaScript and D3.js with the d3-sankey plugin and their dependencies, web fonts including the Inter font and the Tabler Icons webfont, icons and images, and the favicon) is embedded via `go:embed`; the build uses the Go toolchain only, with no Node.js or `node_modules` step (see Vendored Web Assets)
- [ ] The web interface is fully self-contained: with networking disabled and with only the `rmp` binary present on disk (no sidecar files and no separate assets directory), `rmp web` serves the full UI — every page and the knowledge-graph visualisation render and function with no network egress (see Vendored Web Assets and `WEB.md § Self-Contained Deliverable`)

### Architecture Verification
- [ ] Use `file` command to verify binary architecture matches target
- [ ] ARM binaries show correct ARM version (ARMv6, ARMv7)
- [ ] BSD binaries report the expected OS in the ELF note: `file` shows `version 1 (FreeBSD)` for the FreeBSD target and `version 1 (OpenBSD)` for both OpenBSD targets. This is the only verification these targets receive, since none of them is executed (see Supported Build Targets)

### CI/CD Verification
- [ ] The release workflow triggers on the push of a `v*` tag, and the CI workflow triggers on a push to `main` and on a pull request targeting `main`
- [ ] Each workflow file runs the complete gate set: reading `.github/workflows/ci.yml` and `.github/workflows/release.yml` shows a step for `fmt` (`go fmt ./...` followed by `git diff --exit-code`), `vet`, `lint`, `test`, and `security` in the gate job, plus a build job that is the `build` gate (see Validation Gates)
- [ ] The gate set in each workflow file matches the `check` target of the `Makefile` gate for gate: no gate is present in one and absent from the other
- [ ] Each workflow installs `golangci-lint` and `gosec` in the job that runs those gates. No step tests whether a tool is present and continues without it, and no gate step carries `continue-on-error`
- [ ] Both workflows pin `gosec` to the exact version this specification names, installing it with the command the specification gives (`go install github.com/securego/gosec/v2/cmd/gosec@v2.28.0`), and that version is one and the same in `.github/workflows/ci.yml`, in `.github/workflows/release.yml`, and in Security Scan: gosec. Confirm an installed scanner with `go version -m "$(which gosec)"`, whose `mod` line names the version; `gosec --version` prints `dev` for a `go install` build and proves nothing
- [ ] Both workflows pin `golangci-lint` to the exact version this specification names, passing `version: v2.13.1` to the `golangci-lint` action, and that version is one and the same in `.github/workflows/ci.yml`, in `.github/workflows/release.yml`, and in Linter: golangci-lint. The action itself stays pinned to its own exact version, which is a separate pin. Confirm an installed linter with `which -a golangci-lint` and then `golangci-lint --version`; `--version` alone answers for whichever binary `PATH` resolves first, and a shadowing package that tracks the latest release can report the pinned version itself
- [ ] The documented local install command for each tool installs the pinned version, and the linter it installs can actually run this project: the golangci-lint module path carries the `/v2` suffix, so `golangci-lint run ./...` reads `.golangci.yml` (`version: "2"`) instead of rejecting it
- [ ] `gosec` runs in both workflows with the invocation the `security` gate defines (`gosec -exclude-dir=.claude/worktrees ./...`), so the scanned scope and the accepted `#nosec` suppressions are the same everywhere
- [ ] Every gate fails its job when it fails: introducing one violation at a time — an unformatted file, a `go vet` finding, a failing test, a `golangci-lint` violation, and an unsuppressed `gosec` finding — fails the workflow run in each case, in both workflows
- [ ] No artefact is built or published on a run whose gates did not pass: the build job declares `needs:` on the gate job, and the publishing job declares `needs:` on the build job
- [ ] The release workflow builds all eleven Primary Platforms, and the CI build job builds the four-target fast-feedback subset (see Validation Gates, Permitted Differences Between the Three Pipelines)
- [ ] Artifacts uploaded successfully
- [ ] Permissions set to minimum required in BOTH workflows: each grants `contents: read` at workflow level, and exactly one job in each raises that to `contents: write` — `release` in the release workflow, `dev-release` in the CI workflow. No gate job and no build job holds write permission
- [ ] No release reports any gate as skipped, waived, not installed, or not applicable
