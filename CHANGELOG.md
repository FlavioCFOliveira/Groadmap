# Changelog

All notable changes to **Groadmap** (`rmp`) are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.15.1] - 2026-08-26

A correctness release, and nothing else. Two sprints — **90 completed tasks across 87
commits** — land together under one exclusive rule: fix the defects Groadmap has today;
do not hunt for features, and do not complete features that were never built. A defect
was defined, and enforced, as a divergence between what the software **does** and what
it is **specified** to do, reproducible against the built binary. Ninety times over,
that test was applied before any code was written. Mid-sprint, eighteen members were
returned to the backlog for failing it; fourteen were later readmitted by explicit
decision, and four are still there.

**The number is `1.15.1` and the release is not backward-compatible.**
`SPEC/VERSION.md` defines `PATCH` as "Bug fixes, backward compatible"; this release
contains a dozen changes that make an invocation which succeeded on `1.15.0` fail on
`1.15.1`. Under a strict reading of Semantic Versioning 2.0.0 that is a `MAJOR` change.
The project publishes it as `1.15.1` by the owner's explicit decision, on the ground
that every change is the correction of a divergence from a contract that was **already
published** — the exit code `127`, the comma-separated id list, the "255 characters"
cap, the date-only filter form, the published error strings — so a caller relying on
the old behaviour was relying on a defect. **That does not soften the incompatibility,
and the version number does not warn anyone.** Read **Changed — BREAKING** below, and
`release-notes/v1.15.1-20260826.md`, before upgrading any automation.

**The CLI now enforces its own argument contract.** A stray positional argument was
**ignored across the whole CLI** — and on `task remove` that silence deleted some of
the tasks you named and reported success. Every command declares its maximum positional
count, and one shared enforcement point refuses the excess with exit `2` before the
database is opened. An unresolved subcommand exits **127**. A failing top-level
invocation writes **zero bytes to stdout**, where an unknown command used to print 2091
bytes of help there.

**Free text is validated the same way everywhere.** Length cap, UTF-8 encoding, control
characters, trim, emptiness — one order, seven write paths, eight fields, by flag and on
standard input. Bytes that are not valid UTF-8 are refused rather than stored; a leading
or trailing vertical tab or form feed no longer slips past the control-character rule; a
whitespace-only value is refused and stored values are trimmed; and the caps count
**Unicode code points instead of bytes**, so 255 four-byte emoji — 1020 bytes — is now
accepted.

**The knowledge graph stops answering the wrong question.** A relationship read bound by
a reverse or undirected pattern returned the **wrong edge** when a node pair carried
edges in both directions; a write bound by such a pattern was silently discarded and
reported success. Both are refused with exit `6` before the store is opened. Graph reads
also take a shared advisory lock, because opening the store is not a read-only operation
on disk.

The database schema advances **two migrations, `1.12.0` → `1.14.0`**, applied
automatically and in order. They make each sprint's task order **total and dense** and
repair gaps already committed on every installation. Neither deletes a row, a column, or
a table. See **Notes** for the migration path and the one-way consequence for sprint
ordering.

### Changed — BREAKING

- **A stray positional argument is refused across the whole CLI.** Twenty paths were
  measured as changing from silently ignored (exit `0`) to refused (exit `2`), across
  the `task`, `sprint`, `audit`, `backlog`, `roadmap` and `stats` families and the six
  global forms. The line is `Error: invalid input: unexpected argument "X"`, with `X`
  echoed as supplied.

  | Invocation | On `1.15.0` | On `1.15.1` |
  |------------|-------------|-------------|
  | `rmp task remove 2 3` | Exit **0**; task `2` deleted, `3` discarded in silence | Exit **2**; **nothing deleted** |
  | `rmp roadmap create alpha beta` | Exit 0; `alpha` created, `beta` ignored | Exit **2**; **neither created** |
  | `rmp backlog show-next 5 10` | Exit 0; trailing token tolerated | Exit **2** |
  | `rmp version check`, `rmp --help sprint` | Exit 0 | Exit **2** |
  | `rmp task get 1 2` | Exit 0 | Exit **2** |

  Enforcement is reached **by construction** from each command's own declaration, in the
  only place in the binary that invokes a subcommand handler, so it covers every command
  that exists and every command that will be added. The refusal precedes the store:
  `rmp task remove -r <missing-roadmap> 3 4` exits `2`, not the `4` a missing roadmap
  gives. The comma-separated form is untouched — `rmp task remove 2,3` still removes
  both — and `SPEC/COMMANDS.md § Remove Task` always said the separator exists precisely
  so that a mistyped list loses no data. The contract was correct and simply unenforced.

  The `graph` family already refused a stray token on `1.15.0` and its line, hint
  included, is byte-identical. The other three families that had their own wording —
  `web`, `ai-help`, and the comment subcommands — keep theirs, exempted by a declaration
  field rather than by a name list inside the dispatcher.

- **An unresolved subcommand exits `127`, not `2`.** `rmp task bogus`, and the same for
  `sprint`, `backlog`, `audit`, `graph`, and `roadmap`. An unresolved **top-level**
  command already exited `127` and is unchanged. The binary moved to the contract, not
  the contract to the binary: two tables in `SPEC/ARCHITECTURE.md` and the
  machine-readable AI contract had published `127` for this case all along. The line
  changes with the exit code, because the classification sentinel that mapped the case
  to `2` went with it: `Error: invalid input: unknown task subcommand: bogus` becomes
  `Error: unknown task subcommand: bogus`. The top-level `Error: unknown command: bogus`
  is unchanged, and a bare family still exits `0` with its help on stdout. One
  carve-out stays at exit `2` and is documented in two places — an unrecognised scope
  selector on `--ai-help`, where the name selects a contract section rather than being
  dispatched. All five family help screens that lacked it now list `127`, as `roadmap`
  already did, and so do the six corresponding `DOCS/commands/*.md` pages.

- **A failing invocation writes nothing to stdout.** `rmp bogus` wrote **2091 bytes** of
  general help to stdout, prefixed by a second copy of the AI-agent hint, so the hint
  appeared once per channel in one run; it now writes **0 bytes** there and 2190 bytes
  to stderr, the growth being the family help now appended after the error. `rmp` and
  `rmp --help` are unchanged at 2090 bytes on stdout. The leak was confined to the
  unresolved top-level command; an unresolved subcommand already wrote nothing there.

  The invoked family's help now follows the error on **stderr**, which settled a
  contradiction inside the specification — `SPEC/COMMANDS.md` said the help is displayed
  after the error and `SPEC/HELP.md` said help is never auto-appended — in favour of
  `COMMANDS.md`. The scope is deliberately narrow: only a dispatch failure appends help.
  A missing parameter, an unknown flag, a bad enum value, a not-found resource, and a
  database error keep the error line and the hint alone.

- **Free text that is not valid UTF-8 is refused.** Eight fields — the four on `task`,
  the two on `sprint`, the comment body, and `completion_summary` — accepted and stored
  the bytes, which JSON then rendered as U+FFFD, so the output disagreed with the store.
  They now exit `6` with
  `Error: validation error: <field>: the value is not valid UTF-8`, on both the flag and
  the standard-input path.

  **Knowledge-graph property values are covered by the same rule, on all five `graph`
  subcommands**, reads included. There the consequence was worse: the engine replaces
  every invalid byte with U+FFFD before its grammar runs, so a write stored a value
  nobody supplied, and a read or a `DELETE` matched a literal that was never supplied and
  reported success having found or removed nothing. The control-character rule
  deliberately does **not** extend to graph reads, because it objects to what is stored
  and refusing a read that names one would make existing data unreadable rather than
  merely unwritable.

- **A leading or trailing VT or FF no longer evades the control-character rule.** Both
  are whitespace to `strings.TrimSpace`, which ran first, so an edge-positioned one was
  stripped before the check saw it and a body made only of a VT was reported as never
  having arrived. An interior VT or FF was already refused, with the identical message,
  so **only the edge positions change**. A 655-probe differential sweep against the
  previous binary returned 599 identical results, 56 changed, and **zero cases that went
  from refused to accepted**.

- **A whitespace-only value in a required free-text field is refused, and stored values
  are trimmed.**

  | Invocation | On `1.15.0` | On `1.15.1` |
  |------------|-------------|-------------|
  | `rmp task create -t "   " ...` | Exit **2**, `Error: required parameter missing: --title` | Exit **6**, `Error: validation error: title cannot be empty` |
  | `rmp sprint create -t "   " -d "   "` | Exit **0**; both stored verbatim | Exit **6** |
  | `rmp sprint create -t "  Padded Title  " ...` | Stored **with** the padding | Stored as `Padded Title` |

  The counter-intuitive fact that made this dangerous: `sprint create` was correct before
  the change **only because it did not trim at all**. The mandatory sequence is content
  rules on the raw value, then the trim, then the emptiness judgement on the trimmed
  value; adding the trim without that ordering would have reproduced the hole
  `task edit` already carried. The observable signature of the correct order is that a
  value made only of VT is refused as a **control character** and never as empty.

- **Reverse and undirected relationship patterns are refused in the graph family.** Two
  defects, one root, both upstream in GoGraph v0.11.0 and both reproduced against the
  engine directly with no Groadmap code in the path.

  On a node pair carrying `gateway-[:CALLS]->authstore` and
  `authstore-[:REPLIES_TO]->gateway`, `1.15.0` answered
  `MATCH (gateway)<-[e]-(b) RETURN type(e), e.weight` with exit `0` and `CALLS, 1` — the
  **forward** edge, where the correct answer is `REPLIES_TO, 2`; the undirected form
  returned two rows, both `CALLS, 1`. On the write side, an undirected
  `SET e.weight = 7` returned `{"ok": true}` at exit `0` having written only the
  outgoing edge, and a purely reverse `SET` wrote nothing while reporting the same
  success.

  Both now exit `6`, raised **before the store is opened**, and the web query bar answers
  HTTP `400` with the new kind `relationship_read_direction`. Correction was investigated
  first and is impossible: the consumer receives a bare scalar with no relationship
  identity, and a `WHERE` over the corrupted value drops the row inside the engine.

  What triggers the refusal is an **expression use** of the relationship variable, which
  is narrower than "any reverse pattern": `graph query` and `graph search` are refused;
  `graph update` and `graph delete` are refused when the variable is read or written; a
  plain `DELETE e` that never reads `e` still succeeds; and `graph create` never reaches
  the guard, its clause-class check refusing a `MATCH` query first. Detection runs over
  the engine's own parsed AST rather than the query text, so an arrow inside a string
  literal cannot trip it and each `UNION` branch is scanned in its own scope.

  **If you write Cypher against `rmp graph`, the undirected cross-reference idiom must be
  rewritten** as the union of two outgoing legs.

- **A schema-introspection query with irregular keyword spacing is refused, and its exit
  code moves from `1` to `6`.** `SHOW  INDEXES` with two spaces passed the guard rail,
  was admitted as an ordinary read, and died in the engine parser with a diagnostic
  listing every clause keyword except `SHOW` — reading as though `SHOW` were unsupported
  when the identical query with one space worked. It is now refused by the guard rail
  with the accepted spelling, rebuilt from the canonical uppercased keyword so no user
  byte reaches stderr. The web endpoint answers HTTP `400` with a kind of its own,
  `invalid_keyword_spacing`, rather than reusing `not_read_only`, which would publish a
  false classification: a `SHOW` is read-only whatever its spacing.

- **An over-long Cypher query is refused by Groadmap rather than by the engine.** The
  limit is unchanged at 1 048 576 bytes and now applies at **both** doors, `--query` and
  standard input. Exit `1` and
  `Error: database error: graph query failed: cypher: parse: ... query too large` become
  exit `6` and `Error: validation error: query exceeds maximum length of 1048576 bytes`,
  and the input is no longer buffered in full first. A `graph` subcommand invoked with no
  query and a terminal on standard input now exits `2` immediately instead of waiting
  forever for input that will never arrive.

- **`sprint update` no longer discards a flag supplied empty.** It treated the empty
  string as its flag-absent sentinel, so it could not tell a flag that was not supplied
  from one supplied empty.

  | Invocation | On `1.15.0` | On `1.15.1` |
  |------------|-------------|-------------|
  | `rmp sprint update 1 -t ""` | Exit **2**, "at least one of ... is required" — false, a flag *was* supplied | Exit **6**, `Error: validation error: title cannot be empty` |
  | `rmp sprint update 1 -t "" -d "text"` | Exit **0**; title silently discarded, description applied | Exit **6**; **nothing mutated** |

  Validation runs before the database is opened, so a rejected update mutates nothing by
  construction rather than by rollback. `sprint update` and `task edit` now agree on both
  exit code and wording for the identical input shape.

- **`sprint list` changes its result order and fills two fields.** The order moves from
  `created_at DESC` to `order_index` ascending — the planned execution order the field
  exists for — so a script reading the first element gets a different sprint. No order
  was previously published, so nothing documented breaks; the ascending order is now a
  published guarantee and is stated on both help surfaces. Separately, `task_count` was
  `0` and `tasks` was `null` for every sprint on every call; both are now populated,
  `task_count` being `len(tasks)` so the two cannot disagree, and an empty sprint
  yielding `[]` rather than `null`. `tasks` carries ids, not objects.

- **Dozens of error messages were rewritten.** If you match `rmp` stderr by string,
  assume every match needs re-checking; at least 35 distinct before/after pairs were
  measured by driving both binaries, and the true figure is higher. Four kinds of change:

  | Kind | On `1.15.0` | On `1.15.1` |
  |------|-------------|-------------|
  | A sentinel printed twice | `validation error: invalid task type: "NOPE": invalid task type` | `validation error: invalid task type: "NOPE"` |
  | A range rule spelled per call site | `validation error: invalid priority: must be 0-9 (got 99)` | `validation error: priority must be between 0 and 9, got 99` |
  | | `validation error: --max-tasks must be between 1 and 10000 (got 0)` | `validation error: max_tasks must be between 1 and 10000, got 0` |
  | | `validation error: --entity-id must be between 1 and 2147483647 (got 0)` | `validation error: entity_id must be between 1 and 2147483647, got 0` |
  | | `validation error: limit must be between 1 and 100` | `validation error: limit must be between 1 and 100, got 0` |
  | | `validation error: Position must be an integer between 0 and 2147483647` | `validation error: position must be an integer between 0 and 2147483647` |
  | | `validation error: invalid task ID: 0 (must be positive)` | `validation error: task_id must be between 1 and 2147483647, got 0` |
  | A field named two ways | `validation error: functional-requirements: control characters ...` | `validation error: functional_requirements: control characters ...` |
  | A class stated twice | `resource not found: from sprint: resource not found: sprint 999` | `resource not found: from sprint 999` |
  | | `Error: validation error: validation error: "CON": ...` (graph only) | `Error: validation error: "CON": ...` |

  A **seventeen-site** sweep found the doubled sentinel, not the one site reported. The
  entity-id rule keeps its deliberate, published exit-code split: the eight comment
  subcommands classify an out-of-range positional id as exit `2` misuse, every other
  surface as exit `6`; the class became a parameter and the wording did not. The
  date-filter refusal now names the flag and both accepted forms. And a value that breaks
  two rules at once resolves differently: a 300-character title carrying a BEL reported
  `title: control characters are not allowed` and now reports
  `field exceeds maximum size: title exceeds maximum length of 255 characters`.

- **Smaller observable changes.** A no-op `sprint move-to`, `top` or `bottom` now writes
  its audit entry, as the specification always required and both siblings already did, so
  `audit list` counts change. Database failures from the comment subcommands now carry
  the `database error:` sentinel the specification publishes in six rows. The retry
  policy runs six attempts rather than five, so `failed after 5 attempts` becomes
  `failed after 6 attempts` and the worst-case wait moves from about 1500 ms to 2500 ms.
  A graph **read** can now wait under that backoff and fail with exit `1` and
  `graph store is busy` while a writer holds the lock. Under `AI_AGENT=1` a suppressed
  AI-agent hint no longer leaves its separator behind, each suppressed path losing exactly
  one byte. `rmp version` is a documented invocation form and all six global forms take
  arity `0`. And `task next` no longer applies a priority tiebreaker that
  `SPEC/COMMANDS.md` already said does not order that listing.

### Changed — widenings

Neither of these can break a caller; both change what the binary accepts.

- **Length caps count Unicode code points, not bytes.** All eight capped fields counted
  `len()` in Go while `SPEC/MODELS.md` said "255 characters" and the SQLite `CHECK`
  counted characters in TEXT semantics: two of the three authorities already agreed and
  the Go layer was the dissenter. 255 accented Latin characters (510 bytes) and 255
  four-byte emoji (1020 bytes) are now accepted; 256 code points is still refused. The
  message needed no rewording — it always read
  `exceeds maximum length of N characters` and has simply become true — and no schema
  change was required. Graphemes are deliberately not the unit and no normalisation is
  introduced.
- **The audit date filters accept the date-only form** that `SPEC/COMMANDS.md`, the AI
  contract and the README all published while the binary exited `6`. Both `audit`
  subcommands now accept the date-only and full RFC 3339 forms through the same entry
  point `task list` uses. **A bare date means the first instant of its day in UTC**, so
  `--until` excludes the day it names: measured against 19 entries all timestamped
  `2026-08-26T07:3x`, `--until 2026-08-26` returns 0 rows and `--until 2026-08-27`
  returns all 19. That reading is load-bearing and is now specified. The shared
  `ParseISO8601` was deliberately **not** widened: its thirteen remaining callers parse
  stored timestamps the code itself wrote.

### Added

- **Two schema migrations, `1.12.0` → `1.14.0`.** `1.12.0` → `1.13.0` makes each
  sprint's task order **total**, renumbering positions to a dense `0..N-1` run and
  replacing `idx_sprint_tasks_order` with its `UNIQUE` form under the same name.
  `1.13.0` → `1.14.0` makes the order **dense**, repairing gaps already committed, and
  drops the unique index for the duration of the repair. Neither adds a column, rebuilds
  a table, or deletes a row. Both are idempotent and both fail closed.
- **The full audit operation catalogue on both help surfaces and in the AI contract.**
  Every one of the 43 operations is published with the entity type it is written against
  and a `legacy` flag marking the four retained for reading historical rows only. The
  classification is **declared and never inferred from the name**: prefix and entity type
  agree on all 39 operations written today, but labelling a group by prefix turns a
  presentation grouping into a factual claim that becomes wrong the day a `TASK_*`
  operation is written against a sprint. `legacy` is a pointer to a boolean so that it
  publishes `false` and omits absent. The six mutating subcommands name the operations
  each writes. Additive: no existing key changed.
- **`commit_hash` and `related_entity_id` on the read-only web audit log page**, so the
  interface no longer shows less than the log holds. The presentation is **bound** to the
  task detail modal's rather than reimplemented: a test extracts the relevant function
  body from the served JavaScript and fails if the placeholder or either class set
  diverges, in both directions. The hash renders verbatim; abbreviation is the
  stylesheet's and never the renderer's.
- **Five new Go packages**, taking the module from 9 to 14: `internal/backoff` (the single
  retry loop), `internal/graphlock` (the graph store's advisory lock), `internal/graphkeys`
  (the executable form of the key-uniqueness audit), `internal/terminal` (the one `ioctl`
  that answers whether a stream is a terminal), and `internal/unicodenorm` (the NFC rule).
- **Eleven new end-to-end modules**, taking the registered suite from 51 to 62:
  `test_53_e2e_harness_binary_staleness`, `test_54_audit_enrichment_e2e`,
  `test_55_error_string_parity`, `test_56_graph_read_direction`, `test_57_positional_arity`,
  `test_58_ai_contract_error_parity`, `test_59_graph_property_value_content`,
  `test_60_docs_readme_contract_completeness`, `test_61_family_help_dispatch_exit_code`,
  `test_62_graph_stray_positional_order`, and `test_63_roadmap_name_refusal_parity`.
- **`golang.org/x/text` v0.41.0** as the fourth direct module dependency, pinned to an
  exact version, used only for canonical decomposition and canonical ordering.

### Fixed

- **The end-to-end harness ran against any binary it found**, with no staleness check
  against the source, and two of its four candidate paths were under the current working
  directory — so a run from the wrong directory drove a foreign binary and nothing said
  so. **A green run was not evidence.** The harness now restricts candidates to the
  repository root, compares the binary's mtime against the newest file compiled into it
  (including the directories pulled in by `//go:embed`, which closed a blind spot on the
  web assets), and rebuilds or refuses with the compiler's own error.
- **A test class added to a registered module was silently skipped.** Three modules named
  their suite classes in a fixed tuple. The dangerous part was not the tuple but the
  comment above it, which promised the opposite of what the code did. The runner now
  fails when a registered module defines a suite class it never references; 41 of the
  registered modules are non-exempt and genuinely covered.
- **Five tests had never executed on any run the suite has ever made.**
  `tests/test_49_install_platform_guards.py` had no runner and no main block, so running
  it the way the suite runs it defined the classes, executed nothing, and exited `0`. The
  suite counted it as passed every time.
- **Sprint task positions were neither unique nor dense.** `position` is now `UNIQUE`
  within a sprint, enforced by the schema, and every member holds exactly one of
  `0..N-1`. A plain unique index **breaks three commands** — `reorder`, `swap` and
  `move-to` all assigned sequentially over values still occupied — so they now park into
  a disjoint negative range before assigning, and `MoveTaskToPosition` stopped shifting
  ranges altogether. The four write paths that opened a gap are all removals and now
  compact in the same transaction, and the obligation follows the row rather than the
  command, since three of the four repair a sprint the caller never names.

  The gap was never cosmetic: `sprint move-to`, `top` and `bottom` compare the moved
  task's **stored** position against the **target rank**, so over a sparse run a real
  move was read as no move at all and still reported as a success.

  A live TOCTOU was found and closed in the same cycle: `ReorderSprintTasks` checked list
  completeness in a read **outside** its write transaction. Reproduced under a concurrent
  add, not hypothesised.
- **`sprint add-tasks` re-parents a task that already belongs to another sprint**, and
  `SPEC/COMMANDS.md` now says so. Two specification files contradicted each other and the
  owner ratified what ships, so no behaviour changed.
- **The published validation order for sprint task assignment was the reverse of what the
  binary applies**, and the exit code is contract, so a caller branching on `4` versus `2`
  was branching on which document it had read. Established by measurement: eighteen
  sibling invocations report the lexical fault and suppress the existence one, 18 of 18,
  with no counter-example in the CLI. Correcting the code was declined — it would change a
  shipped exit code. The reported pair was a lower bound; the whole list, now eleven
  steps, was corrected.
- **`audit list` validated its enums a second time, in different words**, leaving the
  model parsers with no caller at all. `invalid operation` was published **nowhere**,
  which is exactly why its divergence survived while its sibling's did not: an unpublished
  string is invisible to the only gate that catches this class of drift.
- **The bounded backoff slept four times where the specification, and the code's own
  comment, promised five.** Four sites stated the policy and a fifth was found in the web
  layer, correct then and due to go stale. `internal/backoff` owns the **loop**, not the
  constants: sharing constants would not have caught this defect, because the constants
  agreed all along and the loop bodies did not. Statements of these numbers in production
  Go drop from four to one, and a structural gate holds that only `internal/backoff` may
  block on a duration in production code.
- **The board search missed a task whose title is stored in a different Unicode
  normalisation form.** A term and a task's searchable text are both normalised to NFC
  before folding, for comparison only — the bytes `rmp` stores and renders are untouched.
  NFC and not NFD, because the search performs substring containment; two passes and not
  one, because one pass leaves the result outside NFC on 70 of 1 321 226 sequences;
  normalisation before the fold, because the two orders differ on 74 of them. The
  behaviour delta is exactly 1117 of 1 112 064 code points, none of them ASCII.
- **Roughly thirty published statements that execution refuted.** All 128 error strings
  `SPEC/COMMANDS.md` publishes were driven against the binary and compared character for
  character, and every one of the 234 rows carrying both a string and an exit code had its
  exit code driven too: nine rows were wrong and are fixed. The whole `Example.Stderr`
  surface of the AI contract — 68 strings — is now driven against the compiled binary;
  eight were stale, two of them publishing a **longer** line than the binary. The
  `Error Code Mapping` table was removed from `SPEC/ARCHITECTURE.md`, all nineteen
  symbolic identifiers it published existing nowhere in the Go source. `SPEC/BUILD.md`
  counted two direct dependencies where `go.mod` carries four, which is why
  `golang.org/x/sys` was subject to no pinning rule and no release-gate check. Six README
  filter examples published a comma-separated value the binary rejects. Six
  `DOCS/commands/*.md` pages omitted exit code `127` and one omitted `2`. Three
  cross-references named a heading that occurs twice in `SPEC/DATABASE.md`.
  `SPEC/ARCHITECTURE.md`'s `SPEC/` tree omitted `GRAPH.md`.

### Security

- **`install.sh` verifies the archive against its published SHA-256 before extracting.**
  The mitigation already existed and was already published by the release pipeline; it was
  simply never used. Two judgements were made rather than defaulted: the script **aborts**
  rather than warning when no hashing tool exists, because the documented invocation is
  `curl` piped into `bash` and whoever benefits from a control that fails open is exactly
  whoever replaced the archive; and there is **no opt-out flag**. `SPEC/DEPLOY.md` states
  what the check does **not** protect against, which was a requirement rather than a
  courtesy.
- **`install.sh` no longer stages in a predictable temporary directory.** Every download
  lands in a directory created by `mktemp -d`, mode `0700`, verified after creation and
  failing closed. The previous `mkdir -p /tmp/rmp_install_$$` succeeds on an existing
  directory, so a local user could pre-create all 32768 PIDs and swap the archive between
  the verification and the extraction — CWE-367 and CWE-377, defeating the checksum gate
  that had just been added. The non-obvious check: the parent is refused when
  world-writable **without** the sticky bit, because there a local user can rename the
  `0700` directory away after creation.
- **Reading the knowledge graph no longer mutates its on-disk store.** Graph reads take a
  **shared** advisory lock across the store open alone, because GoGraph's recovery removes
  a stale staging directory and can promote a backup snapshot — both repairing the very
  directory a concurrent writer publishes into. Measured with a writer holding for four
  seconds and a marker planted in the staging directory: before, the CLI read returned in
  15 ms and the HTTP `GET` in 13 ms and **both destroyed the marker**; after, both wait
  2512 ms and refuse, and the marker survives.
- **`columnExists` no longer interpolates an unguarded table name into SQL.** The safety
  was a property of the call sites, not of the function, and the `#nosec` that documented
  it is the same annotation that would have kept the scanner silent about a future caller
  passing a variable: a suppression is a permanent blindfold, and it does not expire when
  its reason does. The empty string is a correctness argument independent of any attacker:
  `pragma_table_info('')` is valid SQL returning zero rows, so an unguarded empty name
  reports every column absent and the caller then runs an `ALTER TABLE` at a table nobody
  named.
- **The `.gosec.yaml` register of accepted findings is gated, not merely refreshed.** The
  proof that the gate was the point came from the repository's own history: `v1.15.0`
  brought the register up to date and added no check, and within the day five suppression
  sites moved without it. The gate parses with `go/ast`, because the rule that a `#nosec`
  counts only at the head of a comment group is not expressible in `grep`, and because
  `grep` is blind to the `//gosec:disable` syntax.
- **Agent worktrees are gitignored.** The `security` target already excluded
  `.claude/worktrees` from the scanner while git was never told to ignore it, leaving a
  full second checkout one `git add -A` away from the repository.

### Performance

- **The web sprints page no longer reads any sprint's member tasks.** It paid a full
  member-task read per sprint to produce one integer. The new `sprintsSource` interface
  carries `ListSprints` alone, so **the N+1 is not expressible on this path** rather than
  merely absent from it. Member-task reads go from one per sprint to zero at 0, 1, 3, 5
  and 12 sprints, and median page time over 200 sprints went from **38.5 ms, linear in
  sprint count, to 10.9 ms, flat**. The rendered page is byte-identical before and after.
- **`sprint list` resolves the whole listing in one grouped read.** Measured at the driver
  boundary: 2 statements for 1, 2, 3, 4, 5 and 50 sprints alike, against 2+N for the
  per-sprint alternative on the same instrument.

### Internal

- **All fifteen unreached exports of `internal/db` were retired** and the allow-list
  emptied: two promoted onto the command layer's transaction, thirteen deleted. Three of
  the thirteen had already drifted from the shipped path with nothing reporting it — one
  wrote no audit entry and left `completion_summary` behind on a reopening, one reproduced
  a data-corruption defect the shipped path had already fixed, and one wrote a governed
  field underneath every free-text rule. Observable behaviour was proved unchanged by
  comparing 27 command invocations byte for byte — exit codes, stdout, stderr and the
  resulting databases row by row — against a binary built from the previous commit.
- **Eleven error-wrapping sites moved from `%s`/`%v` to `%w`**, so the chain carries both
  the classification sentinel and the specific one. Seventeen residual `%v` sites are named
  and left, because none discards a project-owned sentinel. Proved behaviour-neutral by
  capturing thirteen refusals as exit code plus SHA-256 of stderr before and after: all
  thirteen byte-identical.
- **`TaskUpdate.Validate` had no production caller** and eight assertions were pinned to
  it. The verdict — dead residue rather than missing wiring — was established from git in
  three links before anything was removed, and every deleted assertion was checked against
  live coverage before it went.
- **Structural and specification-parsing gates** were added throughout, each deriving its
  expectation rather than restating it, and each proved non-vacuous by reintroducing the
  defect: the cross-reference resolver (978 references over 683 headings in 15 documents,
  0 unresolved, 0 ambiguous); the example-invocation validator (409 invocations over 24
  files, all 59 subcommands reached); the `SPEC/` directory-listing parser; the
  SCREAMING_SNAKE symbol gate; the positional-arity parity gate; the object-key parity
  gates, which reflect over each struct's JSON tags so the expected set cannot itself go
  stale; the engine-constructor gate; the `.gosec.yaml` register gate; the `.gitignore`
  gate; the backoff singleton gate; and the range-rule caller register.

### Notes

- **On the Semantic Versioning classification.** `SPEC/VERSION.md` defines `PATCH` as
  "Bug fixes, backward compatible", and this release is not backward compatible. Under a
  strict reading of Semantic Versioning 2.0.0 the changes above are `MAJOR` changes. The
  project publishes them as `1.15.1` by the owner's explicit decision, taken after the
  objection was raised and answered, on the ground that every one is the correction of a
  divergence from a contract that was already published, that the surface is the
  roadmap-authoring CLI rather than a linked library API, and that every incompatibility
  fails loudly and immediately rather than silently altering a result — in several cases
  the *old* behaviour being the silent one. **This does not soften the incompatibility.**
  Treat the release as breaking if you drive `rmp` from automation.

- **The two migrations are forward-only, and neither deletes anything.** They run
  automatically and in order on the first command against an existing roadmap. Both rank
  each sprint's rows by `position` ascending with `task_id` ascending as the tie-breaker,
  so **the values change and the sequence never does**: an unambiguous planned order is
  preserved exactly, and an ambiguous one — two members sharing a position — is settled
  deterministically. Both run the repair with no unique index in force, which was measured
  rather than assumed: against the pinned driver, a sprint holding positions `0`, `2` and
  `5` whose rows sat in reverse physical order failed with
  `UNIQUE constraint failed: sprint_tasks.sprint_id, sprint_tasks.position` when the index
  was left in place. Recreating the index is what makes each migration fail closed: if a
  repair ever left two members sharing a position, the statement fails and the whole
  transaction rolls back.

- **Do not run a `1.15.0` binary against a migrated roadmap.** Once a roadmap reaches
  schema `1.14.0` its ordering index is `UNIQUE`, and a `1.15.0` binary's
  `sprint reorder`, `sprint move-to` and `sprint swap` assign positions sequentially over
  values still occupied, because the parking step arrived in this release. Measured on
  both binaries against the same migrated database: exit **1** with
  `Error: updating position for task 4: constraint failed: UNIQUE constraint failed: sprint_tasks.sprint_id, sprint_tasks.position (2067)`.
  Reads are unaffected — no column was added or removed — so it is the three ordering
  commands that break. Replace the binary on `PATH` first.

- **The installer is now hard-fail on integrity.** Every archive must have its
  `<archive>.sha256` published beside it, which the release workflow does, and the
  installing host must have `sha256sum`, `shasum`, or `openssl`, and `mktemp`. Missing any
  of them, the script exits `1` before requesting a single release asset, with a message
  naming the tool. There is no opt-out flag and none will be added.

- **No JSON key changed, and no default moved.** Verified by key-set diff against the
  `1.15.0` binary across the task, sprint, comment, audit-entry, `sprint show`,
  `sprint stats`, `stats`, and `roadmap list` objects, and by running all 52 happy-path
  command invocations on both binaries: identical exit codes throughout.

- **The pre-release vulnerability check was run and is clean.** `govulncheck ./...`
  reports `No vulnerabilities found` at exit `0` against the exact tree being released.
  Nothing is reported, called or otherwise, so nothing needed recording or deciding.

- **Known issues were rebuilt from scratch for this release** and are listed in
  `release-notes/v1.15.1-20260826.md`. Items earlier releases recorded were re-verified by
  execution rather than copied forward: two are fixed by this release and have been
  dropped, one is confirmed still live by reproducing it, one is reframed as the
  environment hazard it really is, and six are new.

- See `SPEC/COMMANDS.md § Positional Arguments`, `§ Positional Arity by Command`,
  `§ Published Error Strings Are Exact`, `§ Published Field Names in Validation Messages`
  and `§ Entity Identifier Range`; `SPEC/GRAPH.md § Relationship Read Direction`,
  `§ Node Key Uniqueness`, `§ Engine Constructor by Path` and `§ Concurrency and
  Recovery`; `SPEC/DATABASE.md § Position Uniqueness Within a Sprint`, `§ Position Density
  Within a Sprint` and `§ Introducing a Uniqueness Constraint over Existing Rows`;
  `SPEC/VERSION.md § Migration 1.12.0 → 1.13.0` and `§ Migration 1.13.0 → 1.14.0`;
  `SPEC/DEPLOY.md § Checksum Verification` and `§ Staging Directory`; and
  `SPEC/BUILD.md § External Dependencies`.

## [1.15.0] - 2026-08-21

The release in which a task stops being a status and starts being a record. Three
sprints land together. Groadmap gains **comments** — a typed, timestamped log
attached to a task or a sprint, so that work can answer, months later and to
someone who was not there, what was tried, what was found, and why it went the way
it did. It gains **commit tracking**: entering `DOING` and reaching `COMPLETED` now
require the git commit that brackets the work, so a task points at the code history
that produced it. Its **audit log stops being generic**: `TASK_STATUS_CHANGE`
gives way to five destination-named operations, `TASK_UPDATE` and `SPRINT_UPDATE`
give way to one entry per changed field, relational operations name **both**
entities instead of one, and every entry can carry the commit that bracketed it.
The operation catalogue grows from 23 to **43**. The read-only **web interface** is
rebuilt around how work is actually read: the roadmap tasks page becomes a Kanban
board with header search and three filters, the sprint page's member-task table
becomes a three-column board that cannot disagree with its own summary line, and
the server's absorbed errors — previously discarded — are now written to the
console through `log/slog`.

Two things are **removed or made mandatory** and will break callers. The task
`specialists` field is gone entirely, taking `task assign` and `task unassign` with
it. And `task stat <ids> DOING` and `task stat <ids> COMPLETED` now **refuse to run**
without `--commit-open` and `--commit-close` respectively. Both are described in
full in the two BREAKING sections below; read them before upgrading.

Under a strict reading of Semantic Versioning 2.0.0, the two backward-incompatible
changes above are `MAJOR` changes: existing invocations that succeeded on `1.14.0`
now fail on `1.15.0`. The project has deliberately chosen to publish them as
**`1.15.0`**, on the ground that the affected surface is the roadmap-authoring CLI
rather than a consumed library API, and that both failures are loud, immediate, and
exit non-zero rather than silently changing a result. That choice does not soften
the incompatibility, and the two sections below state plainly what stops working.

The database schema advances **four migrations in one release, `1.8.0` → `1.12.0`**,
applied automatically and in order on the first command run against an existing
roadmap. See **Notes** for the migration path and what is not reversible.

### Removed — BREAKING

- **The task `specialists` field is gone, end to end.** The field recorded which
  specialists a task was assigned to. It is removed from the model, from storage,
  from the command surface, from the web interface, and from the audit catalogue.
  Nothing replaces it.

  **What stops working:**

  | Invocation | Result on `1.15.0` |
  |------------|--------------------|
  | `rmp task assign <ids> <specialists>` | Exit **2** — unknown subcommand |
  | `rmp task unassign <ids> <specialists>` | Exit **2** — unknown subcommand |
  | `rmp task create ... -sp/--specialists <v>` | Exit **2** — unknown flag |
  | `rmp task edit <id> -sp/--specialists <v>` | Exit **2** — unknown flag |
  | `rmp task list -sp/--specialists <v>` | Exit **2** — unknown flag |

  **What changes in output.** The `Task` JSON object no longer carries a
  `specialists` key, on any command that returns a task (`list`, `get`, `next`,
  `subtasks`, `blockers`, `blocking`) and on the web interface's task detail modal.
  A consumer that reads `task["specialists"]` must stop doing so; a consumer that
  reads it defensively sees the key simply absent.

  **What is deleted, and is not recoverable.** Migration `1.9.0 → 1.10.0` drops the
  `tasks.specialists` column. The stored values go with it and cannot be recovered
  from the database afterwards. A caller who needs them must export them **before**
  running a `1.15.0` binary against the roadmap; see **Notes**.

  **The audit catalogue loses two operations.** `TASK_ASSIGN` and `TASK_UNASSIGN`
  are removed from `ValidAuditOperations` and are no longer accepted by
  `rmp audit list --operation`. Historical rows carrying those values are **not**
  deleted — the migration touches no audit row — but they can no longer be selected
  by operation.

### Changed — BREAKING

- **A task can no longer start or finish without recording where in the code
  history it did so.** `rmp task stat <ids> DOING` now requires `--commit-open`
  (short `-co`) and `rmp task stat <ids> COMPLETED` now requires `--commit-close`
  (short `-cc`). Both invocations previously succeeded with no flag at all; both
  now **exit 6** and change nothing.

  ```bash
  # 1.14.0 — succeeded
  rmp task stat -r myproject 42 DOING
  rmp task stat -r myproject 42 COMPLETED --summary "Shipped"

  # 1.15.0 — the same invocations exit 6 and change nothing
  # Error: --commit-open is required when transitioning to DOING
  # Error: --commit-close is required when transitioning to COMPLETED

  # 1.15.0 — what to write instead
  rmp task stat -r myproject 42 DOING --commit-open $(git rev-parse HEAD)
  rmp task stat -r myproject 42 COMPLETED --commit-close $(git rev-parse HEAD) --summary "Shipped"
  ```

  **Every route into those two states is affected**, including the re-entry into
  `DOING` from `TESTING`. On a re-entry the supplied hash **replaces** the stored
  one; no history of earlier values is kept.

  **Each flag is rejected on any other target state** (exit 6), mirroring the rule
  that already governs `--summary`. `--commit-open` on a target other than `DOING`
  and `--commit-close` on a target other than `COMPLETED` are refused.

  **One hash applies to the whole batch.** Every task named in a comma-separated
  `<task-ids>` receives the same value, exactly as every task receives the same
  `--summary`. A caller needing different hashes issues separate commands.

  **Validation runs before anything is resolved or written.** The commit flags are
  checked at step 4 of the `stat` validation order — before ID resolution and before
  any mutation — so a batch in which any check fails leaves every task in the batch
  untouched.

  **Format, and what Groadmap does not do.** A commit hash is 7 to 64 hexadecimal
  characters, stored lowercase, and enforced by a `CHECK` constraint as well as by
  the application. Groadmap **derives nothing**: it invokes no git command, reads no
  working directory, inspects no repository, and does not check that the hash names
  a commit that exists anywhere. The caller supplies the value.

  **Neither field is editable.** `task create` accepts neither flag, because a task
  is created in `BACKLOG`, and `task edit` cannot change either value. A wrong hash
  is corrected by performing the transition again where the state machine allows it.

  **Practical impact on automation.** Any script, agent, or CI step that drives
  `task stat ... DOING` or `task stat ... COMPLETED` must be updated to pass a hash.
  There is no environment variable, no configuration key, and no opt-out.

### Added

- **Comments on tasks and sprints: a durable record of findings and decisions.**
  Groadmap recorded what work was planned and what state it was in, but nothing of
  what was learned while doing it. A comment is a typed, timestamped log entry
  attached to a task or a sprint; a task or sprint holds as many as the work needs,
  and the log is read oldest first, because the order is the story it tells.

  Eight new subcommands, four in each family, in the flat form
  `<family> comment-<verb>`. There is no separate `rmp comment` family and no
  three-level `rmp task comment add` form.

  | Subcommand | Alias | Takes | Purpose |
  |------------|-------|-------|---------|
  | `task comment-add` / `sprint comment-add` | `c-add` | the TASK's or SPRINT's id | Add one typed comment |
  | `task comment-list` / `sprint comment-list` | `c-ls` | the TASK's or SPRINT's id | List the log, oldest first, optionally filtered by `--type` |
  | `task comment-edit` / `sprint comment-edit` | `c-edit` | the COMMENT's own id | Change the type and/or the body |
  | `task comment-remove` / `sprint comment-remove` | `c-rm` | the COMMENT's own id | Delete one comment, irreversibly |

  `comment-add` emits `{"id": <int>}`, `comment-list` emits an array, and the two
  mutating subcommands emit empty stdout on success, following the conventions the
  rest of the CLI already uses.

- **A per-entity comment type, mandatory and without a default.** A task comment
  accepts `FINDING`, `HYPOTHESIS`, `TEST`, `DECISION`, `PROGRESS`, `UPDATE`, and
  `NOTE`; a sprint comment accepts only `FINDING`, `DECISION`, `PROGRESS`, and
  `UPDATE`. The asymmetry is deliberate: a sprint records how the sprint went, not
  the execution diary of its individual tasks, so the three task-only values are
  rejected on a sprint with exit code 6 and a message naming the valid set. The
  database enforces the same two subsets independently, through a `CHECK`
  constraint on each comment table.

  `-y, --type` therefore carries two unrelated enums in the `task` family: a task
  type on `list`, `create`, and `edit`, and a comment type on the comment
  subcommands. A value from one set is rejected by the other. Each family's help
  lists only its own set, and the AI Agent Contract exposes them as two separate
  enum keys, `TaskCommentType` and `SprintCommentType`.

- **A comment body from standard input.** The body is supplied through `--body` or,
  when that flag is absent, read from standard input — the same mechanism the
  `graph` subcommands already use for `--query`, so a multi-line finding can be
  piped or redirected in without shell quoting. There is no `--body-file` flag and
  no path argument; the commands open no file. On `comment-edit` standard input is
  read only when `--type` is absent too, so a type-only edit never blocks waiting
  for input. `--type` is validated before the body is resolved, so a missing or
  invalid type fails immediately rather than leaving the command waiting on input it
  would reject anyway. Bodies are trimmed at the edges, preserve interior line
  breaks, are capped at 4096 characters, and reject control characters like every
  other free-text field. The read is **bounded**: it stops the moment the body
  cannot fit, so an oversized body is refused without ever being buffered.

- **Comments on the read-only web interface.** The task detail modal renders the
  task's comments as a chronological timeline placed last in the modal body, and
  the dedicated sprint page gains a Comments card, after the member-tasks card,
  holding the sprint's own comments. Both show, per entry, the type as a neutral
  badge, the creation timestamp, an edited marker when the comment has been
  changed, and the body with the author's line breaks preserved; both show a clear
  empty state when there is nothing recorded yet. The comments of every task
  rendered on a page are loaded in one grouped query, never one query per task. The
  interface displays comments and never writes them: the CLI remains the sole write
  path.

- **`commit_open` and `commit_close` on the task model.** Two nullable fields
  recording the commit the work started from and the commit it was concluded at.
  Both are returned on every `Task` JSON object, `null` until the task reaches
  `DOING` and `COMPLETED` respectively, and both are shown on the web interface's
  task detail modal. See the BREAKING section above for how they are written.

- **Five destination-named audit operations replace one generic status
  operation.** `TASK_STATUS_BACKLOG`, `TASK_STATUS_SPRINT`, `TASK_STATUS_DOING`,
  `TASK_STATUS_TESTING`, and `TASK_STATUS_COMPLETED`. The audit log previously
  recorded that a task's status changed but never to what; the destination was
  recoverable only by replaying the whole history. `TASK_STATUS_CHANGE` is retained
  as a **LEGACY** value — still accepted by `audit list --operation`, never written
  again.

- **Nine per-field audit operations replace two generic update operations.**
  `TASK_TITLE_CHANGE`, `TASK_TYPE_CHANGE`, `TASK_FUNCTIONAL_REQUIREMENTS_CHANGE`,
  `TASK_TECHNICAL_REQUIREMENTS_CHANGE`, `TASK_ACCEPTANCE_CRITERIA_CHANGE`,
  `SPRINT_TITLE_CHANGE`, `SPRINT_DESCRIPTION_CHANGE`, `SPRINT_MAX_TASKS_CHANGE`, and
  `SPRINT_ORDER_CHANGE`. An edit now writes **one entry per field supplied**, in the
  deterministic field order the `UPDATE` statement is built with, all sharing one
  `performed_at`. A row is written per flag **supplied**, not per value that
  differs: supplying a value equal to the stored one still writes its row, exactly
  as `task prio` already did. `TASK_UPDATE` and `SPRINT_UPDATE` join the LEGACY
  group.

  This also settles an inconsistency: `task prio <id> 5` wrote
  `TASK_PRIORITY_CHANGE` while `task edit <id> -p 5`, performing the identical
  mutation, wrote `TASK_UPDATE`. Both now write `TASK_PRIORITY_CHANGE`.

- **A directional pair replaces the single move operation.**
  `SPRINT_MOVE_TASK_OUT` and `SPRINT_MOVE_TASK_IN` are written against the source
  and the destination sprint respectively, so a sprint's history records losing
  tasks as well as gaining them. `SPRINT_MOVE_TASK` joins the LEGACY group.

- **Six comment audit operations.** `TASK_COMMENT_CREATE`, `TASK_COMMENT_UPDATE`,
  `TASK_COMMENT_DELETE`, `SPRINT_COMMENT_CREATE`, `SPRINT_COMMENT_UPDATE`, and
  `SPRINT_COMMENT_DELETE`. Each is written in the same transaction as the change it
  records, and each is logged against the **parent** entity, never against the
  comment: the entry names the task or the sprint, so a deleted comment still
  leaves a trace that it existed and was removed. Listing comments is a read and
  writes no audit entry.

- **Two new audit columns, and two new keys on every audit entry.**
  `related_entity_id` names the counterpart of a relational operation, and
  `commit_hash` carries the commit that bracketed the work. Both are returned on
  **every** entry returned by `rmp audit list` and `rmp audit history`, `null` where
  unused, taking the entry from five keys to **seven**.

- **A structural guarantee on both new columns.** `LogAuditTx`, the package's only
  audit writer, refuses a commit hash on any operation outside
  `TASK_STATUS_DOING` and `TASK_STATUS_COMPLETED`, and refuses a counterpart on any
  operation outside the eight relational ones. The invariant is enforced by the
  writer rather than restated at each call site: the wrong call cannot compile a row
  into existence.

- **The roadmap tasks page is a Kanban board.** `/roadmaps/{name}/tasks` presents
  every task of the roadmap as a read-only board of five fixed status columns, each
  header carrying a count badge. The page reads every task of the roadmap, so its
  per-column counts state facts about the roadmap rather than about a page of it.
  Each card is a `<button>`, reachable by pointer, touch, and keyboard, and opens
  the read-only task detail modal. The board is the page's only task presentation:
  there is no table view.

- **Header search and three filters on the tasks board.** A search box in the page
  header plus `type`, `priority`, and `severity` filters, carried in the URL as the
  `q`, `type`, `priority`, and `severity` query parameters. On a cold load they are
  applied in memory over the rows already read; narrowing in the browser issues no
  request at all, because every card is already in the document. The search term is
  trimmed and case-folded with the **server's** whitespace set and mapping, not the
  browser's, so the same term selects the same cards on both paths.

- **The task detail modal is filled on demand from a JSON endpoint.** Opening a task
  fetches that task's fields and comments, so the page's own read does not grow with
  the number of tasks rendered.

- **The sprint page's member tasks are a three-column board.** `WAITING`
  (`BACKLOG` or `SPRINT`), `DOING` (`DOING` or `TESTING`), and `CLOSED`
  (`COMPLETED`) — the same grouping the sprint's status summary line already uses,
  so the column counts and the line agree by construction rather than by
  coincidence. Each column is ordered by the question it answers: `WAITING` by
  planned in-sprint execution order, because it is a queue; `DOING` and `CLOSED`
  most-recent-first, because they are records of what has happened.

- **A structured console log for `rmp web`.** The server absorbs every per-request
  failure into an HTTP status and keeps serving, and its response body is
  deliberately opaque so no internal detail reaches the browser. That detail was not
  merely withheld, it was **discarded**: every `500` path dropped the error value and
  no package imported `log/slog`, so an operator facing a failing page had a silent
  terminal. The console is now the counterpart of the opaque response: one
  `key=value` record per failure on **stderr**, carrying the method, the path, the
  roadmap, the subject, the status, and the error, with UTC timestamps in the
  project's canonical ISO 8601 form. A rejected query-bar query is a `WARN` (the
  user's query failed); a server failure is an `ERROR`. The three ad-hoc
  `warning: ` lines at startup — non-loopback bind, unreadable roadmap list,
  per-roadmap migration skip — become `WARN` records, so stderr speaks with one
  voice.

  Deliberate exclusions, specified rather than omitted: `404` and `405` are not
  logged, there is no access log, and the client address is not recorded. Logging
  ordinary navigation would bury genuine failures under every mistyped URL. Stdout
  still carries only the startup URL object, and the log adds no flag, no
  configuration surface, and no dependency.

- **A per-request time budget on the graph data endpoint.** The endpoint executes
  the caller's query under a **5-second deadline** covering both the run against the
  engine's read path and the walk over the result. The deadline is derived from the
  request's own context, so a client that disconnects still cancels immediately and a
  client that stays connected can no longer hold a query running indefinitely. The
  budget bounds the **work**; the injected node `LIMIT` bounds only the **result**,
  and neither substitutes for the other. Exhausting the budget is an ordinary query
  execution failure: HTTP `400` with `kind` `execution`, no new status, no new error
  class, and the server keeps serving.

### Changed

- **Database schema `1.8.0` → `1.12.0`, in four migrations.** They are applied
  automatically, in order, on the first command run against an existing roadmap.

  | Migration | What it does |
  |-----------|--------------|
  | `1.8.0` → `1.9.0` | Adds the `task_comments` and `sprint_comments` tables and one index each, `idx_task_comments_task_created` and `idx_sprint_comments_sprint_created`, both on `(parent_id, created_at ASC)` to serve the oldest-first read directly |
  | `1.9.0` → `1.10.0` | **Drops** `tasks.specialists`. No table rebuild: the column is plain nullable `TEXT`, not indexed, in no `CHECK`, no foreign key, no view, and no trigger |
  | `1.10.0` → `1.11.0` | Adds `tasks.commit_open` and `tasks.commit_close`, each nullable `TEXT` carrying its own `CHECK` (`NULL`, or 7 to 64 lowercase hexadecimal characters). No backfill and no index |
  | `1.11.0` → `1.12.0` | Adds `audit.related_entity_id` and `audit.commit_hash`, then **reclassifies** historical `TASK_STATUS_CHANGE` rows onto the five destination-named operations |

  The two comment tables are independent, so comment ids are **per-family**:
  `rmp task comment-edit 7` and `rmp sprint comment-edit 7` address two unrelated
  comments, and an id that exists only in the other family is a not-found condition
  (exit code 4).

- **Historical audit entries are reclassified, by exact equality and nothing else.**
  A `TASK_STATUS_CHANGE` row becomes `TASK_STATUS_DOING`, `TASK_STATUS_TESTING`, or
  `TASK_STATUS_COMPLETED` only when its `performed_at` equals **exactly one** of the
  owning task's three lifecycle timestamps. It keeps `TASK_STATUS_CHANGE` when it
  matches none (a transition to `BACKLOG` stamps no timestamp, and a reopening clears
  the ones that would have matched), when it matches more than one, and when the task
  no longer exists. The `audit` table is never rebuilt: no `DELETE`, no id rewrite, no
  compaction, no new index, no backfill of the two new columns, and no audit entry of
  the migration's own. Ids, `entity_type`, `entity_id`, and `performed_at` are
  untouched on every row.

- **The audit operation catalogue grows from 23 to 43 operations**, four of them in a
  documented **LEGACY** group — `TASK_STATUS_CHANGE`, `TASK_UPDATE`, `SPRINT_UPDATE`,
  and `SPRINT_MOVE_TASK`. LEGACY values are published last in
  `ValidAuditOperations` and remain accepted by `audit list --operation`, so a query
  still reaches the rows already stored, while no code path writes them again.

- **Relational audit operations name both entities.** Adding three tasks to a sprint
  previously wrote three rows identical in every column but `id`, all naming the
  sprint; which tasks were added was recorded nowhere, and `audit history TASK <id>`
  showed nothing even though the task's status had changed. `sprint add-tasks` and
  `sprint remove-tasks` now write a mirrored pair — the sprint-scoped row naming the
  task in `related_entity_id`, and the matching task-scoped status row — and both
  dependency writers name the counterpart task.

- **`task reopen` and the other three routes back to `BACKLOG` clear
  `commit_close` and preserve `commit_open`.** The four routes are
  `task stat <ids> BACKLOG`, `task reopen`, `sprint remove-tasks`, and
  `sprint remove`. The asymmetry is deliberate and differs from the lifecycle
  timestamps, which are all cleared: reopening invalidates where the work **ended**,
  not where it began.

- **The web interface's page chrome is rebuilt.** The read-only footer is removed
  from every page, the selected roadmap is named in the top navbar, and every page
  header is rendered through one shared partial. The sprints page tabs are coloured
  by the status each one groups, rather than all three carrying a fixed neutral
  badge.

- **Two full-height regions have their height computed by the layout** — the tasks
  board and the knowledge-graph card — instead of being sized by subtracting from
  the viewport.

- **The vendored Tabler Icons webfont advances `3.44.0` → `3.46.0`.** Every other
  vendored web asset was audited against the registry and the release feed and was
  already current. No rendering change; the licence is unchanged.

### Fixed

- **A guard-rail bypass let DDL through on an interface documented as read-only.**
  A non-ASCII homoglyph smuggled a schema-DDL statement past the Cypher guard rail:
  the engine's own classifier folds `U+0131` to `I` and `U+017F` to `S` through
  `strings.ToUpper`, while the guard rail's discriminator used Go's `(?i)`, which is
  Unicode simple case folding and does **not** contain the `I`/dotless-i pair. The
  guard rail therefore saw an ordinary read where the engine saw DDL, and an
  unauthenticated `GET` on the read-only web interface reached the index and
  constraint executors. The classifier now evaluates each discriminator twice — over
  the masked text and over its `strings.ToUpper` copy, the exact transformation the
  engine applies — which can only add detections, so no legitimate read became
  rejected.

- **A node-limit bypass and an unbounded standard-input read** were closed in the
  same adversarial pass. Seven further findings from that audit were recorded as
  tracked tasks rather than left undocumented.

- **The `0600` guarantee on `project.db` is now enforced on every open, not only at
  creation.** Opening an existing database neither re-applied nor verified the mode,
  while the surrounding directories were re-hardened to `0700` and re-verified every
  time. A database arrives mis-permissioned in ordinary ways — restored from an
  archive that carried no modes, copied off a FAT or NTFS mount, `rsync`'d without
  `-p`, produced under a permissive `umask`. Groadmap now hardens and verifies on
  every open and **fails** when the file cannot be hardened.

- **A roadmap directory that is a symbolic link now fails with exit code 1, not 2.**
  The specification classified the refusal as a database error; the code raised a
  misuse error. A symlink is a condition of the filesystem's state, not a syntax or
  flag error, and exit 2 is documented as misuse. Two call sites were already
  contradicting the old classification, `rmp web` among them.

- **`rmp --ai-help` published stale wording for 16 of the 43 audit operations.**
  Four were actively wrong rather than merely dated: `TASK_STATUS_CHANGE`,
  `TASK_UPDATE`, `SPRINT_UPDATE`, and `SPRINT_MOVE_TASK` are LEGACY values that the
  per-destination and per-field operations replaced, yet the contract still
  described them as the live way to record a status change, an edit, or a move. An
  agent reading only the contract was told to expect operations no command writes.
  The contract is regenerated verbatim from the canonical catalogue and gated
  byte-for-byte at test time, so it cannot drift again in either direction.

- **`DOCS/commands/audit.md` listed 21 of the 43 operations and denied the commit
  feature's existence.** Both commit-carrying operations were absent, the full
  per-destination and per-field families were missing, the four LEGACY names were
  presented as live, and both `Output:` lines said an entry has five keys where the
  contract requires seven. The command help under-documented the same shape in two
  places, so `commit_hash` was undiscoverable from `rmp audit list --help`. A new
  enum-coverage gate now requires the document to name every operation the code can
  write, and to name none it cannot.

- **`DOCS/commands/task.md` documented a `stat` command that no longer runs.** Its
  usage line, flag table, rules, and examples showed `task stat ... DOING` and
  `task stat ... COMPLETED` with no commit flag — invocations that now exit 6. The
  section now documents both flags with their mandatory states, format, batch
  semantics, and error table; `reopen` states that it clears `commit_close` and
  preserves `commit_open`; the Task object key list gains `commit_open` and
  `commit_close`; and the output-format line no longer names the removed `assign`
  and `unassign` subcommands. `DOCS/commands/sprint.md` states what
  `remove-tasks` and `remove` clear when a member task falls back to `BACKLOG`.

- **The sprint board's own defects**, settled during the rebuild: three review
  findings on the sprint board, the tasks board columns widened and the card body
  tightened, the audit page's tree experiment reverted at the owner's request, and a
  search term folded with the browser's mapping instead of the server's.

- **Six stale specification defects settled against the binary rather than against
  each other.** `DATABASE.md`'s reopen SQL did not clear `completion_summary` while
  two other files said it did — observation settled it, and `DATABASE.md` was wrong
  rather than outvoted. Six `DATABASE.md` sections documented a `SELECT` shape no
  query in the repository has ever used, which is what hid `subtask_count`.
  `COMMANDS.md` claimed an ordering flag sorts by severity where the code and the
  binary's own help sort by sprint position, carried a malformed table row, and gave
  fifteen invalid-id messages that differed from the bytes the binary prints.
  `STATE_MACHINE.md` omitted the manual `SPRINT` → `BACKLOG` transition, making a
  reachable state look impossible. The sprint summary line's own example was
  arithmetically impossible.

- **A test class that never ran.** `tests/test_22_task87_sprint_capacity.py`
  declared `TestAC6CapacityPctZero` twice; Python binds the name to whichever
  definition it evaluates last, so the first was never collected. The two bodies
  were diffed before anything was touched and were functionally identical, so no
  coverage was at risk — the defect is that anyone correcting the assertions had an
  even chance of editing the definition that does not run.

### Internal

- **Three unreachable `internal/db` helpers were deleted, and the class that
  produced them is now gated.** `DeleteSprint` duplicated the transaction in
  `commands.sprintRemove` and had already drifted from it, so a published atomicity
  guarantee was being asserted against a copy the binary does not run.
  `LogAuditEntry` and `LogAuditEntriesBatch` opened their own connection, which the
  architecture forbids for an audit row: it must be written inside the transaction
  of the modification it records, or it can outlive a rollback. `LogAuditTx` is now
  the package's only audit writer. A new AST sweep fails on any exported identifier
  in `internal/db` that no non-test file names, and fails in both directions, so the
  allow-list may only shrink.

- **The Go test suite is hermetic** and no longer writes to the developer's home
  directory.

- **A fourth enum-coverage gate** joins the help gate, the specification gate, and
  the contract gate, this one over `DOCS/commands/audit.md`.

- **196 end-to-end call sites across 26 modules** were updated to pass a commit
  hash, through a `commit_flags_for(status)` helper for the sites that build the
  target status dynamically. No assertion was weakened; several were strengthened,
  because tests asserting a transition guard would otherwise have started passing
  for the wrong reason — a missing flag rather than the guard.

- **The end-to-end module index is complete and gated against the registry**, so a
  module cannot be added without being registered.

- **The `.gosec.yaml` suppression register is reconciled with the tree.** The
  register declares that a suppression not listed in it is a review failure, and one
  had become unlisted — `#nosec G306` in `internal/db/permissions_test.go`. Three
  per-class counts had drifted as code moved, and the register's own stated total was
  one short of the grep it cites. It now lists all eight classes and 53 suppressions
  with locations and reasons. Nothing about what is scanned or suppressed changed:
  the file is comments only, and the `security` gate reports 0 issues before and
  after.

### Notes

- **On the Semantic Versioning classification.** This release contains two
  backward-incompatible changes, either of which would justify a `MAJOR` bump under
  a strict reading of Semantic Versioning 2.0.0. The project has deliberately
  published them under `1.15.0`. Both incompatibilities fail loudly and
  immediately — exit 2 for the removed surface, exit 6 for the missing commit
  flags — and neither silently changes a result. Read **Removed — BREAKING** and
  **Changed — BREAKING** above before upgrading any automation.

- **Export `specialists` before you upgrade, if you need it.** Migration
  `1.9.0 → 1.10.0` drops the column, and the values are not recoverable from the
  database afterwards. Run the export with a `1.14.0` binary or read the column
  directly:

  ```bash
  # Before running any 1.15.0 command against the roadmap
  sqlite3 ~/.roadmaps/<name>/project.db \
    "SELECT id, title, specialists FROM tasks WHERE specialists IS NOT NULL;" \
    > specialists-backup.txt
  ```

- **The four migrations are forward-only.** They run automatically and in order on
  the first command against an existing roadmap, and there is no downgrade path: a
  `1.14.0` binary opening a roadmap migrated to schema `1.12.0` will not read it.
  Back up `~/.roadmaps/<name>/` before the first `1.15.0` command if you may need to
  return.

- **No audit history is lost.** The `1.11.0 → 1.12.0` migration appends columns and
  rewrites the `operation` value of matching rows only. It deletes nothing,
  renumbers nothing, and compacts nothing; ids, `entity_type`, `entity_id`, and
  `performed_at` are identical before and after. Rows the migration cannot determine
  keep `TASK_STATUS_CHANGE`, which remains queryable.

- **Comments are strictly additive.** No existing command contract, exit code, or
  state transition changes on their account. Comments are accepted in every task and
  sprint status, including `COMPLETED` and `CLOSED`; no comment subcommand checks or
  changes an entity's status, no comment ever gates a transition, and `task reopen`
  does not touch comments. The `Task` and `Sprint` JSON objects carry no `comments`
  array and no comment count: comments are read only through `comment-list` and the
  web interface.

- **An edit is a replacement.** `comment-edit` replaces the stored body in place and
  stamps `updated_at`; the previous text is not retained anywhere and cannot be
  recovered. The audit log records that an edit happened, not what it replaced.

- **The web interface remains read-only throughout.** `GET` and `HEAD` only. The
  board moves nothing, the modal edits nothing, and no route added in this release
  writes. No new exit code and no new schema requirement come from the web work.

- **Known issues were rebuilt from scratch for this release** and are listed in
  `release-notes/v1.15.0-20260821.md`. Items earlier releases recorded were
  re-verified by execution rather than copied forward; one — the sprints page's
  non-semantic tab badges — is fixed by this release and has been dropped.

- See `SPEC/COMMANDS.md § Set Task Status`, `§ Task Comments` and
  `§ Sprint Comments`, `SPEC/MODELS.md § Task` and `§ Comment Type`,
  `SPEC/DATABASE.md § Comments` and `§ audit Table`, `SPEC/DATA_FORMATS.md
  § Task Comment`, `SPEC/STATE_MACHINE.md § Commit Tracking Fields`,
  `SPEC/WEB.md § Server Logging`, `§ Task Detail Modal` and `§ The Tasks Board`,
  and `SPEC/VERSION.md § Migration 1.8.0 → 1.9.0` through
  `§ Migration 1.11.0 → 1.12.0`.

## [1.14.0] - 2026-08-17

A release that widens what the project can ship and tightens how it ships it. The
build matrix grows from nine targets to **eleven**: `openbsd/amd64` and
`openbsd/arm64` join it, and the two Windows targets — listed as Primary Platforms
for several releases — now actually compile, having never done so. Schema
introspection (`SHOW INDEXES` / `SHOW CONSTRAINTS`) becomes a specified,
deliberately accepted read-only operation class on the `rmp graph` read
subcommands and the read-only web query bar; it already executed, but only because
the guard rail failed to recognise it. Underneath, the embedded GoGraph
knowledge-graph engine moves three minor releases forward, from `v0.8.1` to
`v0.11.0`, and the SQLite driver moves from `v1.53.0` to `v1.56.0`. Three database
correctness and security fixes land, the most important of which stops a database
**path** redirecting an open to a different file or switching off foreign-key
enforcement. The **minimum Go version rises to 1.26.6**, remediating four standard
library advisories reachable from Groadmap's own code, two of them on the `rmp web`
request path. A release-pipeline hardening pass closes two defects affecting every
artefact the project has published: archives shipped **without the project's
licence file**, and a `v*` tag could publish binaries with neither the linter nor
the security scanner having run anywhere in the pipeline.

Under Semantic Versioning 2.0.0 this is a **MINOR** release: it adds
backward-compatible functionality — two new build targets, two specified targets
that now build and ship, and a newly specified accepted operation class. No `rmp`
command, subcommand, flag, exit code, or JSON success schema is added, removed, or
renamed, and no exit code changes meaning. The database schema version remains
`1.8.0`, so there is **no SQLite migration**. The GoGraph upgrade, however, does
carry a **one-way** on-disk rewrite of the graph store's `labels.bin` from snapshot
format version 1 to version 2; see Upgrade Notes below. The Go floor rise is a
build-time requirement, not a runtime contract change: it constrains who can
compile the project, not what the binary does.

### Added

- **Two OpenBSD build targets; the matrix reaches eleven.** `openbsd/amd64` and
  `openbsd/arm64` are now built and published as
  `rmp-{version}-openbsd-{arch}.tar.gz`. `modernc.org/sqlite` `v1.56.0` added them
  to its own supported-platform table, and the storage engine was the only
  component that could have held them back — the binary is pure Go and links no C
  library. Both are **build-verified, not runtime-verified**: the binaries are
  cross-compiled and checked with `file(1)`, never executed, because no OpenBSD
  host is available. A new Architecture Verification criterion pins that check (the
  ELF note must read `version 1 (OpenBSD)`), and the same honest statement now
  covers `freebsd/amd64`, which was in the same undocumented position. Found and
  fixed in the same cycle: `install.sh` had no OpenBSD case in `detect_os`, so a
  released OpenBSD archive could never have been installed by the documented
  one-line installer. See `SPEC/BUILD.md § Supported Build Targets` and
  `SPEC/DEPLOY.md § Platform Detection`.
- **The two Windows targets compile and ship.** `SPEC/BUILD.md` listed
  `windows/amd64` and `windows/arm64` among the Primary Platforms and required
  every matrix target to build, but neither compiled: `acquireGraphWriteLock`
  called `syscall.Flock` from a file with no build tag and no platform
  counterpart, and that symbol does not exist on Windows. Seven of the nine targets
  built; the break was invisible because no gate compiled the matrix. The system
  calls now sit behind `lockGraphWriteFile` / `unlockGraphWriteFile`, implemented
  with `flock(2)` (`LOCK_EX|LOCK_NB`, under `//go:build unix` rather than
  `!windows`, which would also admit `plan9`, `js`, and `wasip1`) and with
  `LockFileEx` (`LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY` — without the
  second flag `LockFileEx` blocks, turning the immediate failure
  `SPEC/GRAPH.md § Concurrency and Recovery` requires into a hang). The exclusion
  contract is identical on both platforms. Guarded by
  `cmd/rmp/build_targets_test.go`, which builds every target on every `go test` and
  parses the Primary Platforms table out of the specification, and by
  `internal/commands/graph_lock_test.go`.
- **Schema introspection as a specified read-only operation class.**
  `SHOW INDEX` / `SHOW INDEXES` / `SHOW CONSTRAINT` / `SHOW CONSTRAINTS`, in any
  case and with an optional `YIELD` / `WHERE` / `RETURN` projection tail, are
  accepted by `rmp graph query`, `rmp graph search`, and the read-only `rmp web`
  query bar, and rejected by `rmp graph create`, `update`, and `delete`. The class
  is published on every contract surface: the command help, the machine-readable AI
  contract, `internal/aihelp/pitfalls.go`, `README.md`, and
  `DOCS/commands/graph.md`. See `SPEC/GRAPH.md § Schema Introspection` and
  acceptance criteria 23–27.

### Fixed

- **Graph guard rail — schema introspection is recognised, not admitted by
  omission.** The engine gained the `SHOW` commands and the `FOREACH` clause, and
  extended its own `cypher/ir.IsDDL` to report the former as DDL. Because
  `cypher.QueryHasWritingClause` returns false for whatever `IsDDL` accepts, and
  Groadmap's DDL discriminator matches only `(CREATE|DROP)\s+(INDEX|CONSTRAINT)`, a
  `SHOW` query reached the guard rail as **neither a write nor DDL** and passed the
  read-only check — a verdict reached because nothing matched, not because the
  class had been judged read-only. Such a verdict cannot be reviewed, cannot be
  tested for intent, and silently absorbs whatever clause family the engine gains
  next. `Classes` now carries `Introspect`, set from a statement-anchored (`\A`),
  case-insensitive matcher evaluated on the masked query, and `IsReadOnly` decides
  through an explicit switch. The verdict is unchanged for every input that existed
  before. Anchoring is what stops a label, variable, or property named `show` —
  `CREATE (n:Panel {show:'indexes'})` — being read as an introspection command.
  Groadmap's DDL class stays deliberately narrower than the engine's: `CREATE
  INDEX` changes the schema, `SHOW INDEXES` only reports it. `FOREACH` carries no
  discriminator of its own and is classified by the writing clauses its body
  contains — sound only because a `FOREACH` body may hold nothing else, a
  containment property now pinned by tests.
- **Database — a database path can no longer redirect the open or inject
  connection parameters.** `internal/db` built every DSN as
  `fmt.Sprintf("%s?_pragma=...", dbPath)`. The driver splits a DSN at the first
  `?` and, unless the string starts with `file:`, keeps only what precedes it as
  the filename, so a database path containing `?` **opened a different file from
  the one intended** and everything after the `?` was parsed as driver query
  parameters. The blast radius grew with the driver upgrade: before `v1.55.0` the
  shorthand keys did not exist, so a path tail spelling `_foreign_keys=0` or
  `_synchronous=off` was ignored and the failure was a silent wrong-file open;
  since `v1.55.0` those keys are recognised, so a path could **switch off
  referential integrity or downgrade durability** on a connection the application
  believed it had configured itself. The roadmap name is validated; the home
  directory the path is rooted in is not. `dsnWithPragmas` gives way to
  `dsnFor(dbPath, readOnly)` over a new `uriPath` helper following the SQLite URI
  specification: convert `\` to `/`, prepend `/` to a leading drive letter,
  percent-encode, and emit `file:///path` (absolute) or `file:path` (relative). The
  drive-letter test is on the string rather than on `runtime.GOOS`, so it cannot
  turn a relative path into an absolute one. Nothing changes for an ordinary path.
  Guarded by `internal/db/dsn_test.go`, which reads the opened file back from
  `pragma_database_list` rather than inferring it, under six hostile home
  directories. See `SPEC/IMPLEMENTATION.md § Database Connections` and
  `SPEC/ARCHITECTURE.md § Robustness and Reliability`.
- **Database — connection PRAGMAs travel in the driver's validated DSN keys.**
  `foreign_keys`, `busy_timeout`, and `query_only` travelled as
  `_pragma=name(value)`, a form executed exactly as written and never validated,
  and the one DSN parameter class that can fail partway through a DSN: a bad value
  is rejected by SQLite only as it runs, **after every earlier `_pragma` has
  already taken effect**, leaving the connection half-configured. Driver `v1.55.0`
  added first-class keys for exactly these three settings, checked against a fixed
  accepted set before any parameter is applied, so a malformed value fails the
  connection outright. Only the primary key names are used: each has an alias
  (`_fk`, `_timeout`) which wins when both are supplied, so carrying a key and its
  alias together is a trap, avoided by construction. `journal_mode` stays out of
  the DSN — it is database-level, recorded in the file header, and set once by
  `configureConnection`. Measured on `v1.56.0`, both forms produce the same end
  state, so this changes failure behaviour only, not steady-state behaviour.
- **Database — every database opens through the driver's connector.**
  `sql.Open` is documented as not connecting, and `modernc.org/sqlite` deliberately
  does not implement `driver.DriverContext`, so the DSN was not examined there at
  all: a defect in it surfaced not on opening the database but on **whichever query
  first forced the pool to dial**, mid-command and attributed to that query.
  `v1.56.0` added `NewConnector`, which validates as much of the DSN as can be
  checked without touching the filesystem; `sql.OpenDB` builds the `*sql.DB` from
  it and registers nothing process-globally. Connections are otherwise identical.
  See `SPEC/IMPLEMENTATION.md § Entry Point`.
- **Installer — unsupported architectures are rejected at detection, not at
  download.** `install.sh` mapped `i386` and `i686` to `arch="386"` while the guard
  that followed rejected only `"unknown"`, so a 32-bit x86 host passed straight
  through platform detection and failed much later on an HTTP download for
  `rmp-{version}-linux-386.tar.gz` — **an asset no release has ever produced**. The
  user saw a download error instead of being told the platform is unsupported.
  `detect_arch` now returns `"unsupported"` for an architecture it recognises but
  the build does not serve, keeping `"unknown"` for one it does not recognise at
  all, and the guard rejects both — rejecting only one is exactly how this defect
  arose. Both guards report the raw `uname` output rather than the mapped name,
  because the mapping is what failed, and the OS guard was given the same shape, so
  the message for an unsupported operating system changes too. No working
  installation is lost: no published binary has ever existed for 32-bit x86.
  Guarded by `tests/test_49_install_platform_guards.py`, which asserts that nothing
  is requested once a guard fires.
- **Installer — the Windows branch extracts the archive.** It previously moved the
  downloaded archive into place as `rmp.exe`, harmless only while no Windows asset
  existed. Publishing `.zip` archives would have turned a clean download failure
  into silent corruption — measured against the pre-fix script, it exited 0,
  printed `SUCCESS`, and installed a file reported as Zip archive data. The branch
  now requires `unzip` and extracts from the archive root; when `unzip` is missing
  it installs nothing and exits 1, naming the tool and the manual download URL. The
  Linux and macOS paths are unchanged and gain no new tool requirement. Guarded by
  `tests/test_47_install_script_extraction.py`.

### Security

- **The minimum Go version rises from 1.26.5 to 1.26.6, remediating four reachable
  standard-library advisories.** `govulncheck ./...` reported four Go standard
  library vulnerabilities whose vulnerable functions are **called** by Groadmap's
  own code, not merely present in the module graph:

  | Advisory | Package | Defect | Reached via |
  |----------|---------|--------|-------------|
  | GO-2026-6091 | `html/template` | JavaScript regexp context tracking | `internal/web/pages.go` → `template.Template.ExecuteTemplate` |
  | GO-2026-6090 | `crypto/tls` | Post-handshake messages accepted without limit | `internal/web/server.go` → `http.Server.Serve` |
  | GO-2026-6089 | `net/http` | `ReadHeaderTimeout` not applied to the unencrypted HTTP/2 check | `internal/web/server.go` → `http.Server.Serve` |
  | GO-2026-5972 | `encoding/asn1` | Maximum recursion depth not enforced | `internal/aihelp/hint.go` → `asn1.Unmarshal` |

  **Two of the four — `html/template` and `net/http` — sit on the `rmp web` request
  path**, which serves HTML over HTTP, so they are reachable by whoever can reach
  the server. These are **toolchain** vulnerabilities rather than module
  vulnerabilities: the toolchain version alone remediates them, and no dependency
  change can. Go 1.26.6 is the release on the 1.26 line that fixes all four, so the
  `go` directive in `go.mod` moves to `go 1.26.6` and
  `SPEC/BUILD.md § Go Toolchain` moves with it. After the change,
  `govulncheck ./...` reports **`No vulnerabilities found`, exit 0** — the four
  reported-but-not-called advisories of the previous toolchain
  (GO-2026-6218, GO-2026-5942, GO-2026-5026, GO-2026-6088) are fixed by the same
  release and are gone too. Because the `go` directive names the patch version, the
  floor enforces itself: under the default `GOTOOLCHAIN=auto` an older machine
  fetches the required toolchain rather than building with the wrong one, and a
  `GOTOOLCHAIN` pinned to an older release fails with an explicit error instead of
  building. No manual installation step is added.
- **`SPEC/BUILD.md § Go Toolchain` now states the rule that moves the floor.**
  Previously the patch floor was a bare fact — `v1.13.2` raised it to `1.26.5` for
  GO-2026-5856 — with nothing saying when it moves again, so each occurrence was
  re-argued from scratch. The rule is now written down: **a standard-library
  advisory reachable from Groadmap's own code raises the floor to the release that
  fixes it; one that is reported but not called does not.** That distinction is what
  keeps the rule workable — without it, every advisory anywhere in the module graph
  would move the floor. `govulncheck` is what draws the distinction, and the
  specification states plainly that it is a diagnostic, **not** one of the six
  gates.
- **A pre-release vulnerability check is now a required release step.**
  `SPEC/VERSION.md § Release Process` gains a new **step 1, Pre-Release
  Vulnerability Check**: `govulncheck ./...` MUST be run against the exact tree
  being released, before the version bump, and its result acted on. It is
  deliberately step 1 because a finding changes `go.mod` and `SPEC/BUILD.md`, and
  those changes belong in the release commit rather than in a follow-up. An outcome
  table tells the release engineer what to do with each kind of result: a called
  standard-library vulnerability stops the release until the floor is raised; a
  called vulnerability outside the standard library is referred to the project
  owner, because the pins are governed by `SPEC/BUILD.md § External Dependencies`;
  a reported-but-not-called vulnerability does not block, but MUST be recorded in
  the release notes so the judgement is visible. The remaining steps are renumbered,
  and `SPEC/DEPLOY.md § Release Checklist` gains the matching items.

  **It is deliberately not a seventh gate.** Making it one would turn a pipeline red
  because an advisory was published between two commits, with nothing in the
  repository having changed; the specification therefore forbids adding it to
  `make check`, `ci.yml`, or `release.yml`, and the gate set stays at six. For the
  same reason `govulncheck` is **not pinned** to a version, unlike `golangci-lint`
  and `gosec`: those are pinned so a gate returns the same verdict everywhere, while
  this check exists precisely to reflect the vulnerability database as it stands at
  release time. This step exists because of this release: it passed all six gates
  and the full end-to-end suite, and would have shipped binaries carrying four
  reachable advisories. Every gate was green, because no gate inspects published
  advisories, and nothing in the procedure required anyone to look.
- **Every published archive now carries the licence.**
  `SPEC/BUILD.md § Artifact Structure` has always specified three entries per
  archive — the binary, `LICENSE`, and `README.md` — while `release.yml` packed the
  binary alone. **Every published archive of every target therefore shipped without
  the project's licence file.** Both branches now ship all three (`tar.gz` with
  `rmp`, `zip` with `rmp.exe`), and the rolling `dev` pre-release built by `ci.yml`
  does the same. Verified by execution rather than by reading the YAML: `tar -tzf`
  and `unzip -Z1` list exactly those entries, both shipped files are byte-identical
  to the repository copies, the extracted binary runs, and the `install.sh`
  extraction path still finds the binary in the enlarged archive. Two specification
  gaps were closed first: the `zip`'s contents were never specified — the string
  `.exe` appeared nowhere in `SPEC/` — and the `dev` pre-release sat outside the
  block's scope by accident.
- **One validation gate set, enforced locally, in CI, and at release.**
  `release.yml` ran `go fmt`, `go vet`, and the tests, and nothing else; `ci.yml`
  ran `golangci-lint` but never `gosec`. Only the local `make check` ran all six
  gates. **A `v*` tag could therefore be pushed and binaries published with neither
  the linter nor the security scan having run anywhere in the pipeline**, resting
  entirely on the release engineer having run `make check` on their own machine.
  Ten release-notes files between `v1.6.0` and `v1.13.0` record the security gate
  as skipped because `gosec` was not installed on the release host, nine of them
  citing "per project policy" — **no such policy was ever specified**.
  `SPEC/BUILD.md` now promotes Validation Gates to a top-level section as the
  single authoritative definition, states that a missing tool is a failure and
  never a skip (probing for a tool and continuing without it, reporting a gate as
  waived, and `continue-on-error` are each forbidden, and no release may report a
  gate as skipped), and enumerates the only three permitted differences between the
  three pipelines. `golangci-lint` is pinned to **v2.12.2** and `gosec` to
  **v2.28.0**, and the pins bind local installations too, because `make lint` and
  `make security` run whatever is on `PATH`. `ci.yml` also moves to least
  privilege: `contents: read` at workflow level, with `contents: write` raised only
  on the one job that writes to the repository. Each of the five gates was made to
  fail on an injected violation, one at a time, running the exact commands the
  steps run. Guarded by `cmd/rmp/workflow_gates_test.go`, which parses every
  expected value out of `SPEC/BUILD.md` rather than restating it, so raising a pin
  in the specification alone fails the test.

### Changed

- **Dependency refresh.**

  | Module | From | To | Kind |
  |--------|------|----|------|
  | `github.com/FlavioCFOliveira/GoGraph` | `v0.8.1` | `v0.11.0` | Direct |
  | `modernc.org/sqlite` | `v1.53.0` | `v1.56.0` | Direct |
  | `golang.org/x/sys` | `v0.47.0` (indirect) | `v0.47.0` (direct) | Promoted |
  | `github.com/RoaringBitmap/roaring/v2` | `v2.21.0` | `v2.24.0` | Indirect |
  | `github.com/bits-and-blooms/bitset` | `v1.24.6` | `v1.25.0` | Indirect |
  | `github.com/mattn/go-isatty` | `v0.0.22` | `v0.0.24` | Indirect |
  | `golang.org/x/exp` | `20260709` snapshot | `20260813` snapshot | Indirect |
  | `modernc.org/libc` | `v1.74.1` | `v1.74.4` | Indirect (pinned) |

  The `go` directive moves from `1.26.5` to `1.26.6` for the security reason given
  under Security above; **no `toolchain` directive is introduced**, so the `go`
  directive remains the single place any pipeline reads the version from — the CI
  and release workflows obtain it via `go-version-file: go.mod`.
  `golang.org/x/sys` is promoted to direct because the Windows lock primitives call
  it.

  **GoGraph `v0.8.1` → `v0.11.0`.** No Go source file changed; the upgrade is
  absorbed entirely by the existing integration layer. Across the eight consumed
  packages, 12 exported symbols moved — `Graph.View`, `SetActiveConstraintCount`,
  and `SetActiveIndexCount` were removed, while the six `snapshot.Write*`
  functions, `txn.Tx.CommitWALOnly`, and `wal.Writer.SyncGroup` were re-signed —
  and **Groadmap calls none of the twelve**. Of the 53 symbols it does consume, all
  are present and call-compatible. The release's semantic changes are likewise
  unreachable from the CLI, which calls neither `BeginReadTx`, `BeginTx`,
  `MVCCStats`, nor `AllocateCommitTS`, and issues no `MERGE` of its own.

  **The graph store's `labels.bin` migrates one-way.** On-disk compatibility was
  demonstrated, not assumed: against a replica of the project's own graph directory
  written under `v0.8.1`, the new binary read back 293 nodes and 802 relationships
  with an unchanged relationship-type distribution; a write then succeeded and the
  synchronous checkpoint **rewrote `labels.bin` from snapshot format version 1 to
  version 2, in place**, after which the same counts and distribution still read
  back. Reading version 1 is the retained upgrade path, and **the rewrite is
  one-way** — once a `v1.14.0` binary has written to a graph store, an older
  Groadmap cannot be expected to read it. This is not the SQLite schema, which is
  unchanged at `1.8.0`. `SPEC/GRAPH.md § Dependency Maturity Risk` records the
  format change under residual risk 2, with a new mitigation 4 requiring an
  existing graph directory to be proven readable across a GoGraph upgrade
  empirically, before release.

  **The single-writer semaphore is retired.** `v0.11.0` removes the engine-level
  semaphore that `SPEC/GRAPH.md`, `SPEC/IMPLEMENTATION.md`, and the `README.md`
  index cited as the reason two concurrent writers serialise. Every promised
  behaviour still holds, including failing fast with `utils.ErrDatabase` rather
  than hanging or corrupting the store, but the reason is corrected to the real
  mechanism: Groadmap's own exclusive non-blocking `flock` on `write.lock`, taken
  before the store is opened and held across the whole open, commit, checkpoint,
  and WAL-truncate sequence — a span wider than a transaction, which an
  engine-level writer exclusion would never have covered.

  **`modernc.org/sqlite` `v1.56.0`** patches the transpiled amalgamation for an
  upstream SQLite 3.53.3 **data-corruption bug in journal rollback**: a crash while
  committing a multi-database `ATTACH` transaction can leave the super-journal name
  zeroed while its length and trailing magic survive; the checksum is a plain byte
  sum, so an all-zero name still validates, and `pager_playback` then deletes the
  hot journal without replaying it. Groadmap issues no `ATTACH`, so the exposure
  was low. Caught and fixed while upgrading: `go get -u ./...` floated
  `modernc.org/libc` to `v1.75.3` and `modernc.org/memory` to `v1.12.0`, silently
  breaking the standing upstream requirement that downstream modules pin the exact
  `libc` version the driver itself pins. `libc` is the transpiled C runtime the
  SQLite amalgamation executes inside, so a mismatch is a runtime risk in the
  storage engine rather than a compile error. The violation **passed every gate**
  and surfaced only from the driver's CHANGELOG. `SPEC/BUILD.md § External
  Dependencies` now governs the driver with its own rules, including the explicit
  statement that no gate run by `make check` detects a violation, so a green build
  is not evidence.

- **The inert version `ldflag` is removed from both workflows.** Both built with
  `-X main.version=...`, which **the linker silently ignores**: `cmd/rmp/main.go`
  declares `version` inside a `const` block, and `-X` writes only to a
  package-level `var`. Measured rather than reasoned — a build with
  `-X main.version=9.9.9-PROOF` still reported `1.13.3`. The constant in
  `cmd/rmp/main.go` has always been the real source of the version, and still is,
  which is what `SPEC/VERSION.md` specifies. No binary changes behaviour, because
  the flag was already inert; what is gone is a promise in the YAML that the next
  reader would have trusted. The regression gate parses `main.go` with `go/ast`,
  collects the package-level `const` names, and fails any `-X` naming one — first
  asserting that `version` *is* a `const`, so flipping it to a `var` fails loudly
  instead of quietly re-enabling the flag.
- **Documented `golangci-lint` install command corrected.** It used the v1 module
  path, which installs `v1.64.8` and cannot read this project's `version: "2"`
  configuration, so anyone following the specification got a linter unable to lint
  the repository. The path now carries the `/v2` suffix and the pinned version.
- **Release artefact suffix notation corrected.** `SPEC/BUILD.md` wrote it as
  `{goarch}{goarm}`, naming the ARM targets `linux-arm6` and `linux-arm7`, while
  the workflow produces `linux-armv6` and `linux-armv7` — which is also what
  `install.sh` asks for.

### Internal

- **The engine's Cypher clause surface is pinned per subcommand.** The
  upgrade-validation method the project uses — a symbol diff plus a re-run of the
  `GRAPH.md` acceptance criteria — is **structurally blind to a clause family being
  added**: no symbol disappears, and no existing criterion names a clause that did
  not previously exist. `SHOW` and `FOREACH` both entered that way.
  `SPEC/GRAPH.md` gains residual risk 3 and mitigation 5, requiring the clause
  surface to be re-verified and every named class pinned by a regression test
  before an upgrade ships. New coverage: 24 cases across `TestClassify` and
  `TestIsReadOnly`; 30 subtests in
  `internal/commands/graph_clause_surface_test.go` pinning each subcommand's exact
  guard-rail message; and 17 end-to-end tests in
  `tests/test_48_graph_clause_surface.py` that assert outcomes rather than exit
  codes — that a projection tail really projects, that a rejected `FOREACH` wrote
  nothing, that an accepted `FOREACH` body ran on every matched row, and that a
  rejected `CREATE INDEX` left the schema empty.
- **A matcher bug caught before it shipped.** The first draft wrote `INDEXES?`,
  which parses as `INDEXE` plus an optional `S` — matching `INDEXE` and `INDEXES`
  but not the singular `SHOW INDEX`. It is now `INDEX(?:ES)?`.
- **A stale rationale corrected.** The comment justifying the Groadmap-local DDL
  regex claimed `cypher/ir.IsDDL` is "case- and whitespace-sensitive". The upgrade
  falsified half of it: `IsDDL` now folds ASCII case and skips leading comments.
  Only the whitespace claim survives, and every claim in the rewritten rationale
  was verified against the pinned engine rather than read from release notes.
- **Two specification gaps closed.** `gosec` was mandatory under `CLAUDE.md`
  Rule 2 but documented nowhere, so the specification of the build system omitted
  one of its own required gates; and the target count contradicted itself, with
  `BUILD.md` listing nine Primary Platforms including `freebsd/amd64` while
  `DEPLOY.md` listed eight and stated "8 total". `BUILD.md` matched reality — the
  release workflow had been shipping `freebsd/amd64` all along — so `DEPLOY.md`
  changed. `SPEC/DEPLOY.md` also gained a Diagnostic Output section tabulating the
  five output helpers and ruling that no error path may bypass the error helper.
- **`internal/cypherguard` keeps its leaf-dependency property**: standard library
  plus the GoGraph `cypher` package only, so the CLI guard rail and the read-only
  web endpoint cannot drift apart.

### Known Issues

Every item below was verified against the code at this tag by running it, not by
carrying forward what a previous release recorded. Items that earlier entries
listed and that are no longer true have been dropped rather than repeated; the
historical entries themselves are left untouched, because they are the record of
what was known at the time. In particular, the two SPEC-versus-code divergences
recorded under `[1.3.0]` — the `ErrInvalidInput` exit-code mapping and the
`audit stats` JSON keys — **are both resolved and are not carried forward**:
`cmd/rmp/main.go` now maps `ErrInvalidInput` and `ErrRequired` to `ExitMisuse` (2)
and `ErrValidation` and `ErrFieldTooLarge` to `ExitInvalidData` (6), matching
`SPEC/ARCHITECTURE.md § Sentinel Error Catalogue`, and `SPEC/COMMANDS.md`
documents exactly the five `audit stats` keys the binary emits.

- **A stranded `snapshot.bak` in the graph directory stops every later checkpoint,
  and the write still exits 0.** This is upstream GoGraph behaviour and the one
  item here that can affect a user. The snapshot publish is a crash-atomic
  three-step swap: archive the live snapshot to `snapshot.bak`, rename the staging
  directory into place, then drop the backup. The pre-swap cleanup of a stale
  backup is best-effort (`_ = fsys.RemoveAll(bak)`), so if the residue cannot be
  removed — for example because of directory permissions — the archive rename then
  fails against the non-empty `snapshot.bak` and the checkpoint returns an error.
  Groadmap surfaces that error as `Warning: graph checkpoint failed: ...` on
  **stderr** and returns exit code 0, which is required by
  `SPEC/GRAPH.md` FR7: the commit is already durable, so a checkpoint failure MUST
  NOT fail the write. The consequences are that automation checking only the exit
  code or only stdout will not notice, and that the WAL is truncated only by a
  successful checkpoint, so **it grows without bound** while the condition lasts.
  No data is lost: the WAL is intact and recovery still works. It **self-heals** as
  soon as the residue is cleared — remove `snapshot.bak` from
  `~/.roadmaps/<roadmap>/graph/` and the next write checkpoints and truncates
  normally. Watch stderr on `rmp graph create`, `update`, and `delete`.
- **The graph read path builds an in-memory engine where `SPEC/GRAPH.md` specifies
  a store-backed one.** `SPEC/GRAPH.md § Engine Construction and Lifecycle`
  requires a persistent engine via `cypher.NewEngineWithStore`, stating that the
  in-memory `NewEngine`, `NewEngineWithOptions`, and `NewEngineWithRegistry`
  constructors are not used for persisted graphs. The write path
  (`internal/commands/graph.go:840`) complies; the two read paths —
  `internal/commands/graph.go:693` and `internal/web/data.go:652` — call
  `cypher.NewEngine(res.Graph)`. **No user-visible difference has been
  demonstrated**, and none is expected on the evidence available: both read paths
  build the engine over a graph that `recovery.Open` has already loaded from the
  on-disk snapshot and WAL, so a read still observes committed state, which the
  end-to-end graph suites exercise across process exits. It is recorded as a
  specification-conformance divergence, not as a data-correctness defect, and the
  claim is deliberately limited to what the code supports. Tracked as `rmp` task
  #149.
- **The sprints web page renders plain count badges where `SPEC/WEB.md` requires
  semantic ones.** The page's tab badges show counts without the status-derived
  Tabler colour variant that `SPEC/WEB.md § Status, Priority, and Severity Badge
  Colours` makes the single authoritative mapping. Presentation only: the counts
  themselves are correct, and no data or `rmp` command output is affected. Tracked
  as `rmp` task #127.
- **`.gosec.yaml` documents three accepted finding classes while the code carries
  six.** The file records G104, G201, and G304 with locations and justifications;
  the sources also carry `#nosec` suppressions for **G204, G302, and G703**, which
  are undocumented. The `security` gate is green and every suppression was
  individually verified as safe when written, so this is a hygiene and auditability
  gap rather than an unreviewed suppression: the record of *why* three classes are
  accepted is missing, and nothing detects the drift. Internal; no user-facing
  behaviour depends on it. Tracked as `rmp` task #141.
- **Two knowledge-graph nodes share the key `README.md`.** `knowledge-model.md`
  states that every node's `key` is globally unique across the graph, so that
  `MATCH (n {key:'...'})` without a label is unambiguous. A `Doc` node and a `Spec`
  node both carry the key `README.md`, so that query is ambiguous for this one
  value. The model contradicts itself rather than merely being violated by the
  data: it keys `Spec` nodes by bare file name and `Doc` nodes by repository-
  relative path, and for `README.md` those two rules produce the same string. This
  affects the project's own internal knowledge graph, not any user's roadmap data
  and no `rmp` command output. Tracked as `rmp` task #155.

## [1.13.3] - 2026-07-14

A correctness release. The embedded GoGraph knowledge-graph engine moves from
`v0.7.0` through `v0.8.0` to `v0.8.1`, which fixes a Cypher pattern-evaluator bug
that made relationship-type matching unreliable over **parallel edges** — two or
more relationships of different types between the same ordered pair of nodes.
That bug silently corrupted the results of the canonical "find the gaps" query
shape, `WHERE NOT (n)-[:TYPE]->()`, so completeness and coverage audits run
through `rmp graph query`, `rmp graph search`, and the `rmp web` query bar could
report relationships as missing when they were present. This release also
corrects a SQLite error-classification bug that could report a non-uniqueness
constraint failure as an "already in use" collision, and it restores the
integrity of two test suites that were not testing what they claimed to test.

Under Semantic Versioning 2.0.0 this is a **PATCH** release: it contains
backward-compatible bug fixes, test-integrity work, and documentation only. No
`rmp` command, flag, or JSON success schema is added, removed, or renamed. No
exit code changes meaning. The GoGraph upgrade is API-additive and confined to
the read path — no `rmp` call site changed, no write, commit, or recovery path is
touched, and there is **no on-disk graph store migration**. The database schema
version remains `1.8.0`, so there is no SQLite migration either.

### Fixed

- **Graph — relationship-type matching over parallel edges.** Delivered by the
  GoGraph `v0.8.1` upgrade. Up to `v0.8.0`, pattern-predicate and
  pattern-comprehension relationship-type matching inspected only the **first**
  stored relationship type between an ordered pair of nodes. Over parallel edges,
  every non-first type was therefore reported as absent, and a bound relationship
  variable's `type(r)` could report the wrong type. Concretely, over
  `(a)-[:FIRST]->(b)` and `(a)-[:SECOND]->(b)`, the predicate
  `WHERE NOT (a)-[:SECOND]->()` wrongly returned `a` — claiming an edge that
  exists is missing — while the positive form `WHERE (a)-[:SECOND]->()` wrongly
  returned nothing. The first-stored type always answered correctly, which is
  precisely why the defect stayed hidden. The engine now tests **every**
  relationship type of the endpoint pair, across the outgoing, incoming,
  undirected, variable-length, and comprehension paths. This reaches users
  through `rmp graph query`, `rmp graph search`, and the `rmp web`
  knowledge-graph query bar, which all run the same engine. Guarded by the new
  end-to-end suite `tests/test_46_graph_parallel_edge_predicates.py`.
- **Database — only genuine uniqueness violations are reported as collisions.**
  `IsUniqueConstraintErr` masked the SQLite result code down to the **primary**
  code `SQLITE_CONSTRAINT` (19), which is shared by every constraint kind. A
  `CHECK`, `NOT NULL`, or `FOREIGN KEY` violation was therefore indistinguishable
  from a uniqueness collision, and on the sprint insert and update paths it was
  reported to the user as `sprint order N is already in use` with exit code 5
  (`EXIT_EXISTS`). The check is now made on the **extended** result codes —
  `SQLITE_CONSTRAINT_UNIQUE` (2067) and `SQLITE_CONSTRAINT_PRIMARYKEY` (1555) —
  so a non-uniqueness constraint failure now surfaces as the failure it actually
  is. Exit code 5 keeps its meaning; it is simply no longer emitted for failures
  that are not collisions. Guarded by
  `TestIsUniqueConstraintErr_OnlyUniquenessViolations`.

### Changed

- **Dependency refresh.**

  | Module | From | To | Kind |
  |--------|------|----|------|
  | `github.com/FlavioCFOliveira/GoGraph` | `v0.7.0` | `v0.8.1` | Direct |

  No other module changed. Beyond the parallel-edge fix, GoGraph `v0.8.0` widened
  the Cypher DDL surface, accepting the modern
  `CREATE CONSTRAINT ... FOR ... REQUIRE ...` grammar alongside the legacy
  `ON ... ASSERT` form. Groadmap continues to **reject all DDL** in every
  `rmp graph` subcommand; the guard-rail contract is now pinned by regression
  tests in `internal/cypherguard` covering both grammars.

### Internal

- **Database index tests now verify the real schema and the real queries.** The
  index tests built their own local schema and their own lookalike SQL, so they
  could pass while the production schema or the production query drifted away
  from the index they claimed to exercise. Query assembly is now separated from
  query execution (`buildListTasksQuery`, `buildAuditEntriesQuery`, and the named
  `sprintTasksLookupQuery` constant), so the tests take the query plan of the
  exact SQL production runs, against the real schema. This is a
  behaviour-preserving refactor. See `SPEC/DATABASE.md`, "Performance
  Optimization".
- **Model memory layout is asserted, not logged.** The struct-size benchmark
  logged the layout with `b.Logf` and asserted nothing, so a regression in the
  memory layout pinned by `SPEC/MODELS.md` could not fail the build. It is now a
  real test (`TestSpecifiedStructSizes`) that asserts the specified sizes, scoped
  to 64-bit targets as the specification states.
- **Knowledge-graph model documented.** `knowledge-model.md` is added as the
  authoritative description of the project knowledge graph's shape: the
  normalized node schema, the structural edges re-derived from the code, and the
  traceability and provenance contract.

## [1.13.2] - 2026-07-13

A maintenance release that refreshes every module dependency, raises the Go
floor to **1.26.5** to remediate a toolchain security advisory, and specifies the
sprint `description` field as the sprint's macro goal. The headline dependency
change is the embedded GoGraph knowledge-graph engine, which moves from `v0.6.0`
to `v0.7.0` and brings two write-correctness fixes to the `rmp graph` command
family, along with a broad denial-of-service hardening pass across the graph
store. No `rmp` source behaviour changes in this release.

Under Semantic Versioning 2.0.0 this is a **PATCH** release: it contains
backward-compatible fixes, dependency maintenance, and documentation only. No
`rmp` command, flag, or JSON success schema is added, removed, or renamed; no
exit code changes; the GoGraph upgrade is additive with no exported API break and
no on-disk store migration; and the database schema version remains `1.8.0`. The
Go floor moves only in its *patch* component (1.26.4 to 1.26.5) within the Go
1.26 minor floor that has been required since v1.7.0, so it changes no language
version and breaks no consumer: Groadmap exposes no importable Go API (every
package other than `cmd/rmp` lives under `internal/`), and released binaries are
distributed prebuilt.

### Security

- **Go floor raised to 1.26.5 (GO-2026-5856).** The Go standard library's
  `crypto/tls` is affected by GO-2026-5856, an Encrypted Client Hello privacy
  leak, in Go 1.26.4 and earlier; the fix ships in Go 1.26.5. This is a
  *toolchain* vulnerability, not a module vulnerability: it is remediated by the
  toolchain version alone, and no dependency change can remediate it. The `go`
  directive in `go.mod` now declares `go 1.26.5`, and Groadmap MUST NOT be built
  or released with an older toolchain. `govulncheck ./...` reports no
  vulnerabilities. See `SPEC/BUILD.md`, "Go Toolchain".

### Fixed

- **Graph — `MERGE` now applies whole-pattern match-or-create semantics.**
  Delivered by the GoGraph `v0.7.0` upgrade. A `MERGE` of a pattern with a fresh
  endpoint was previously silently incomplete; the most common Cypher
  graph-building idiom now either fully applies or is rejected with a typed
  error. This affects every `rmp graph update` and `rmp graph create` query that
  uses `MERGE`.
- **Graph — parallel edges are rejected instead of silently dropped.** Delivered
  by the GoGraph `v0.7.0` upgrade. On a non-multigraph engine (the documented
  default), writing a parallel edge previously discarded the write silently; it
  is now rejected with a typed error, closing a write-loss gap.
- **AI Agent Contract — `sprint update` summary now lists `--order`.** The
  machine-readable contract (`rmp --ai-help`) described `sprint update` as
  editing "title, description or capacity cap", omitting the execution-order
  field that the subcommand has accepted since v1.10.0. The summary and
  description now state all four editable fields.

### Changed

- **Sprint `description` is specified as the sprint's macro goal.** The
  `description` field of a sprint is now specified, on every surface that states
  it, as the high-level (macro) goal of the development effort the sprint
  delivers: a new development, a fix, a refactoring, or another kind of change.
  Together with the title, it must give a human reader or an AI agent a clear
  macro idea of what the sprint's tasks are aimed at. The field states the macro
  goal only: detailed scope, technical detail, and acceptance conditions belong
  to the sprint's tasks, which carry them in their functional requirements,
  technical requirements, and acceptance criteria. A label such as `"Sprint 3"`
  no longer satisfies the documented contract. The semantics are stated in the
  plain-text help for `sprint create` and `sprint update`, in the AI Agent
  Contract, in `SPEC/MODELS.md`, `SPEC/COMMANDS.md`, `SPEC/HELP.md`, and in the
  command documentation and README examples. This is a documentation and
  specification change: the field was already mandatory on `sprint create`, and
  no validation behaviour changes.
- **Dependencies — GoGraph upgraded to `v0.7.0`.** The embedded GoGraph
  knowledge-graph engine moves from `v0.6.0` to `v0.7.0`. Beyond the two write
  fixes above, the upgrade lands a security and denial-of-service hardening pass
  across the graph store: a size-bounded manifest, symlink rejection on every
  file the engine opens by name (including the write-ahead log and snapshots),
  bounded allocation of frame payloads, and memory ceilings in the Cypher engine.
  It also adds eight openCypher builtin functions (`elementId`, `timestamp`,
  `randomUUID`, `isNaN`, and the `toStringList` / `toIntegerList` / `toFloatList`
  / `toBooleanList` family), which become available to `rmp graph query`. The
  upgrade is a pre-1.0 additive change: no exported Go API is broken and the
  on-disk graph store requires no migration.
- **Dependencies — transitive modules refreshed.** `github.com/RoaringBitmap/roaring/v2`
  moves from `v2.18.2` to `v2.21.0`, `github.com/bits-and-blooms/bitset` from
  `v1.24.5` to `v1.24.6`, `golang.org/x/sys` from `v0.46.0` to `v0.47.0`,
  `golang.org/x/exp` to its `2026-07-09` snapshot, and `modernc.org/libc` from
  `v1.73.5` to `v1.74.1`. `modernc.org/sqlite` remains at `v1.53.0`.
- **Specification — version state removed from `SPEC/VERSION.md`.** The
  `Current Version` table and the Version History it implied are removed, and the
  documented release process is aligned with the process the project actually
  follows. Git tags and `git log` are now the single source of truth for the
  history of the specification, in line with the project's versioning policy.
  `SPEC/BUILD.md` becomes the authoritative statement of the required Go version,
  and the other specification files point to it rather than restate it.

### Internal

- **Help-content unit tests.** New unit tests cover the AI Agent Contract
  generator (`internal/aihelp`) and the command help content
  (`internal/commands`), pinning the sprint `description` semantics in both the
  generated contract and the plain-text help.
- **End-to-end contract coverage extended.** The help and exit-code contract
  suite gains two cases asserting that the sprint `description` macro-goal
  semantics are stated in the plain-text help and in `rmp --ai-help`.

## [1.13.1] - 2026-06-29

A maintenance release that refreshes third-party dependencies and remediates
every finding from the 2026-06-29 release-readiness audit. The headline fix
restores correct parsing of Cypher queries supplied through the `--query` flag
when the value begins with a negative numeric literal (for example `-1` or
`-0.5`), which were previously rejected as a missing flag value. The release also
upgrades the embedded GoGraph engine and the SQLite driver, and reconciles the
GoGraph pin across the specification. Internal hardening completes the cycle: the
`gosec` security gate is now green, the `internal/cypherguard` package reaches
full unit-test coverage, and a new end-to-end test pins the `rmp audit stats`
JSON key set against regressions.

Under Semantic Versioning 2.0.0 this is a **PATCH** release: it contains a
backward-compatible bug fix and dependency maintenance only. No `rmp` command,
flag, or JSON success schema is added, removed, or renamed; the GoGraph upgrade
is additive with no exported API break and no on-disk store migration; and the
database schema version remains `1.8.0`.

### Changed

- **Dependencies — GoGraph upgraded to `v0.6.0`.** The embedded GoGraph
  knowledge-graph engine moves from `v0.4.0` to `v0.6.0`. The upgrade is a
  pre-1.0 additive change: no exported Go API is broken and the on-disk graph
  store requires no migration.
- **Dependencies — SQLite driver refreshed.** `modernc.org/sqlite` moves from
  `v1.52.0` to `v1.53.0` and its `modernc.org/libc` runtime from `v1.73.4` to
  `v1.73.5`.
- **Specification — GoGraph pin reconciled.** The GoGraph version pin recorded in
  `SPEC/BUILD.md`, `SPEC/GRAPH.md`, and `SPEC/ARCHITECTURE.md` is reconciled from
  `v0.3.2` to `v0.6.0`, so the specification matches the module the project
  actually builds against.

### Fixed

- **Graph — `--query` values beginning with a negative number are accepted.**
  The `rmp graph query` / `search` family of commands now accepts a `--query`
  value that starts with a negative numeric literal (for example `-1` or
  `-0.5`). Such values were previously misclassified as a missing flag value and
  rejected; only genuine flag forms (`--x`, `-<letter>`) are treated as flags.
  The refined contract is documented in `SPEC/GRAPH.md`, "Cypher Input Source and
  Precedence", rule 4.

### Internal

- **Security gate green.** The `gosec` security scan now reports zero issues; the
  eight verified-safe findings raised during the audit are annotated with
  `#nosec` and justified in place. `make check` runs the security gate as part of
  the standard validation battery.
- **`internal/cypherguard` fully covered.** New unit tests raise the package from
  0 % to 100 % statement coverage.
- **Audit-stats key regression test.** A new end-to-end test pins the JSON key
  set returned by `rmp audit stats`, guarding the command's output contract
  against accidental change.

## [1.13.0] - 2026-06-19

A web-focused release that adds a read-only Roadmap Audit Log page to the
`rmp web` interface and completes a Tabler-fidelity pass across all served
templates. The new Audit Log page exposes the roadmap's audit trail in the
browser with numbered Tabler pagination, while the fidelity pass aligns the web
UI with the Tabler design system: semantic colour-coded badges for status,
priority, and severity; sprint tabs migrated to the Tabler `card-header-tabs`
pattern; all inline styles removed in favour of Tabler and project CSS; and a
set of minor markup corrections (grid gaps, heading hierarchy, footer). The web
specification (`SPEC/WEB.md`) is extended with explicit Tabler-fidelity rules
for templates.

Under Semantic Versioning 2.0.0 this is a **MINOR** release: it adds
backward-compatible functionality (a new read-only page) and refines the
existing read-only presentation. No `rmp` command, flag, or JSON success schema
is removed or renamed, the on-disk graph format is unchanged, and the database
schema version remains `1.8.0`.

### Added

- **Web — read-only Roadmap Audit Log page.** The `rmp web` interface gains a
  new read-only page that surfaces the roadmap's audit trail in the browser. The
  page is served from the embedded templates and reads through the same strictly
  read-only data path as the rest of the web UI. Specified in `SPEC/WEB.md`.
- **Web — numbered Tabler pagination on the Audit Log page.** The Audit Log page
  paginates its entries with a numbered Tabler pagination control, so large
  audit trails are navigable page by page.

### Changed

- **Web — semantic Tabler badge colours for status, priority, and severity.**
  Status, priority, and severity values are now rendered as semantically
  colour-coded Tabler badges, making state and importance visually
  distinguishable across the served pages.
- **Web — sprint tabs migrated to the Tabler `card-header-tabs` pattern.** The
  sprint tabs now use Tabler's `card-header-tabs` markup, aligning the sprint
  views with the Tabler design system.
- **Web — inline styles removed from templates.** All inline `style` attributes
  were removed from the templates; presentation is now driven exclusively by
  Tabler and the project's `style.css`.
- **Web — minor Tabler markup fidelity.** Several small markup corrections bring
  the templates closer to Tabler conventions, including grid gap utilities
  (`g-2`), an `h1` brand heading, and footer markup.
- **SPEC — Tabler-fidelity rules for web templates.** `SPEC/WEB.md` is extended
  with explicit rules governing Tabler fidelity for the web templates, and the
  web documentation notes the semantic colour-coded status/priority/severity
  badges.

### Internal

- New web unit suites covering the Audit Log page (`internal/web/audit_test.go`),
  semantic badges (`internal/web/badge_test.go`), pagination
  (`internal/web/pagination_test.go`), the no-inline-style invariant
  (`internal/web/inline_style_test.go`), Tabler-fidelity markup
  (`internal/web/tabler_fidelity_test.go`), and sprint rendering
  (`internal/web/sprint_test.go`).
- New audit-log query support in `internal/db/queries.go` with accompanying
  unit tests (`internal/db/queries_test.go`).
- Extended web end-to-end coverage in `tests/test_35_web_interface.py`.

## [1.12.0] - 2026-06-17

A combined release that pairs read-only `rmp web` interface enhancements with a
full review of the command-line help surface and the machine-readable AI Agent
Contract. The web interface gains a graph query bar, a clickable labels sidebar,
neighbour focus, unified sprint cards, and now surfaces each sprint's title and
execution order. In parallel, every plain-text help printer was revised for
correctness and completeness, the `rmp --ai-help` contract was normalised, and a
set of documented-but-divergent exit codes was corrected so that the runtime now
matches the published contract. The `GoGraph` dependency is bumped to `v0.4.0`.

Under Semantic Versioning 2.0.0 this is a **MINOR** release: it adds
backward-compatible functionality (web features, richer help, contract
completeness) and corrects exit codes to match the already-documented contract.
No JSON success schema is removed or renamed, and the database schema version is
unchanged. The corrected exit codes are aligned to the values the contract and
help already promised, so they restore — rather than break — the documented
behaviour.

### Added

- **Web — graph query bar, labels sidebar, neighbour focus.** The read-only
  `rmp web` graph view gains an interactive query bar, a labels sidebar with
  click-to-highlight, and neighbour-focus navigation, plus unified sprint cards
  for a consistent layout. Specified in `SPEC/WEB.md`.
- **Web — sprint title and execution order surfaced.** The read-only UI now
  displays each sprint's title and its execution order, so the served pages
  reflect the same ordering the CLI uses.
- **`sprint tasks` — `-s` short alias for `--status`.** The `sprint tasks`
  command now accepts `-s` as a short alias for `--status`, matching the
  documented contract.

### Changed

- **Plain-text help revised across all commands.** Every plain-text help printer
  was reviewed and corrected for accuracy, formatting, and completeness, so the
  on-screen help now faithfully describes the runtime behaviour.
- **AI Agent Contract normalised and completed.** `rmp --ai-help` was reworked
  for internal consistency: nested single-action commands are represented
  uniformly; empty arrays are emitted as `[]` rather than `null`; min-only ranges
  no longer carry a misleading `max: 0`; `--max-tasks` advertises its `1-10000`
  range; the `roadmap_flag` web exemption is documented; `sprint tasks` exposes
  `-s`/`--status`; and failure examples are included for commands with non-zero
  exit codes.
- **SPEC reconciled with verified runtime behaviour.** The help-related
  specifications (`SPEC/HELP.md`, `SPEC/COMMANDS.md`, and related files) were
  reconciled with the behaviour confirmed empirically against the binary.

### Fixed

- **Invalid `--type` on list commands now exits 6.** Passing an invalid
  `--type` to `backlog list` and `task list` now exits with code 6 (invalid
  data) instead of 1, matching the documented exit-code contract.
- **Invalid `--status` on list commands now exits 6.** Passing an invalid
  `--status` to `task list`, `sprint list`, `sprint tasks`, and `task stat` now
  exits with code 6 instead of 1.
- **`audit stats` emits `null` for empty-set timestamps.** On an empty result
  set, the first/last timestamps are now emitted as JSON `null` rather than an
  empty string `""`.
- **`sprint bottom` on a missing sprint now exits 4.** Targeting a non-existent
  sprint with `sprint bottom` now exits with code 4 (not found) instead of 6.

### Dependencies

- **`GoGraph` bumped to `v0.4.0`** and module dependencies refreshed
  (`go.mod`, `go.sum`).

### Internal

- **New and extended test coverage.** Adds the help-content unit suite
  (`internal/commands/help_content_test.go`) and the help/exit-code end-to-end
  contract suite (`tests/test_44_help_and_exitcode_contract.py`), and extends the
  AI-contract E2E suite (`tests/test_30_aihelp_contract.py`) to lock in the
  revised help text and contract invariants.

[1.15.1]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.15.0...v1.15.1
[1.15.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.14.0...v1.15.0
[1.14.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.13.3...v1.14.0
[1.13.3]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.13.2...v1.13.3
[1.13.2]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.13.1...v1.13.2
[1.13.1]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.13.0...v1.13.1
[1.13.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.12.0...v1.13.0
[1.12.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.11.0...v1.12.0

## [1.11.0] - 2026-06-16

A reliability release for the read-only `rmp web` server. It makes `rmp web`
**auto-migrate every served roadmap's SQLite schema once at startup**, before the
HTTP listener binds, so that the web interface can no longer return HTTP 500 when
it is the first command run against a roadmap whose on-disk schema predates a
binary schema bump. The migration runs through the writable `db.Open` path
(idempotent `RunMigrations`, a no-op when the schema is already current) and never
touches the per-request data path, which remains strictly read-only
(`OpenReadOnly` with `query_only`), preserving the read-only invariant (finding
#43). Under Semantic Versioning 2.0.0 this is a **MINOR** release: the change is
additive and fully backward compatible with `v1.10.0`. No `rmp` command, flag,
JSON output, exit code, or on-disk format is altered. The database schema version
is unchanged at `1.8.0`; this release adds no migration of its own and only
applies the already-defined migrations earlier in the `rmp web` lifecycle.

### Added

- **Startup schema migration for `rmp web`** — `serve()` now runs a new
  `migrateRoadmapsAtStartup()` step after `EnsureDataDir` and before the listener
  binds. It enumerates every roadmap via `utils.ListRoadmaps()`, opens each one
  through the writable `db.Open` path (which runs the idempotent
  `RunMigrations`, a no-op when the schema is already current), then closes it.
  A per-roadmap list, open, or migration failure is logged to stderr and is
  **non-fatal**: the server still starts and serves the remaining roadmaps.
  Specified in `SPEC/WEB.md` (§ Startup Schema Migration, Server Lifecycle step 2,
  Acceptance Criteria 41/42) and `SPEC/ARCHITECTURE.md` (§ internal/web).

### Fixed

- **`rmp web` sprints page returned HTTP 500 on a stale schema** — when `rmp web`
  was the first command run after a binary schema bump on a roadmap whose
  `project.db` predated schema `1.7.0`/`1.8.0`, the sprints page
  (`GET /roadmaps/{name}`) failed with HTTP 500 because the read-only query
  referenced columns absent from the stale file (`sprints.title`,
  `sprints.order_index`). The per-request loaders open the database read-only
  (`OpenReadOnly`, `query_only`) and therefore cannot migrate it. Migrating once
  at startup, before any read-only connection is opened, closes this gap and makes
  migration automatic and input-free while keeping every per-request connection
  strictly read-only — no write, no audit row, and no schema change occurs on a
  read (finding #43 preserved).

### Tests

- New regression suite `internal/web/startup_migration_test.go` (6 tests) covering:
  stale-to-current schema migration at startup, the sprints page recovering from
  HTTP 500 to 200, the non-fatal behaviour when one roadmap is broken,
  multi-roadmap migration, idempotency against an already-current schema, and the
  read-only invariant (no audit row and no `schema_version` change across `GET`
  requests).
- The full battery is green this release cycle: `go test -count=1 ./...`
  (7 packages PASS), `gofmt -l .` clean, `go vet ./...` 0 issues,
  `golangci-lint run ./...` 0 issues, `go build` succeeds (`rmp --version` reports
  `1.11.0`), and `python3 tests/run_tests.py` reports **42/42** (100 %).

### Known Issues

One SPEC-vs-code divergence remains open and is unaffected by this release. It does
not affect runtime behaviour and is tracked as a `spec` / `tech-debt` follow-up for
a future `specification-manager` pass:

- `SPEC/COMMANDS.md` documents the `audit stats` JSON keys `operations_count`,
  `entity_type_count`, `first_entry`, `last_entry` and a `period.{since,until}`
  block; the implementation emits `by_operation`, `by_entity_type`,
  `first_entry_at`, `last_entry_at`, `total_entries` with no `period` object.

[1.11.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.10.0...v1.11.0

## [1.10.0] - 2026-06-16

A feature and reliability release. It introduces a required, human-readable
**sprint title** and a unique **sprint execution-order** field; sharpens the
read-only `rmp web` interface (never-stale responses, a shared sprint
presentation, preserved free-text line breaks, and a hardened HTTP server);
surfaces GoGraph query notifications as diagnostics; and lands a 32-finding
reliability/spec-conformance audit and a 23-finding security audit, each with a
permanent regression gate. The knowledge-graph engine GoGraph is upgraded
`v0.2.0` → `v0.3.2`. Under Semantic Versioning 2.0.0 this is a **MINOR** release:
all changes are additive or corrective and remain backward compatible with
`v1.9.1`, with the single migration-bearing exception that `sprint create` now
requires `--title` (see Changed and Upgrade Notes). The database schema advances
to `1.8.0` through two automatic, idempotent migrations; existing installations
upgrade transparently on first run.

### Added

- **Required sprint title** — sprints now carry a human-readable title in
  addition to their description, so a sprint is identifiable at a glance in
  listings and stand-ups. `sprint create` requires `-t/--title` (max 255
  characters); `sprint update` accepts an optional `-t/--title`. The schema adds
  a `title TEXT NOT NULL CHECK(length(title) <= 255)` column (migration to schema
  `1.7.0`, backfilling existing sprints as `Sprint <id>`). `sprint show` output
  gains a `sprint_title` field. Specified across `SPEC/MODELS.md`,
  `SPEC/DATABASE.md`, `SPEC/COMMANDS.md`, `SPEC/DATA_FORMATS.md`, and
  `SPEC/VERSION.md`.
- **Unique sprint execution-order field** — every sprint carries a unique,
  positive execution order. `sprint create` accepts an optional `--order` (auto-
  assigned to `MAX+1` when omitted); `sprint update` may edit it while the sprint
  is `PENDING` or `OPEN` and rejects edits on a `CLOSED` sprint (exit 6). The
  schema adds `order_index INTEGER NOT NULL CHECK(order_index > 0)` plus a unique
  index (migration to schema `1.8.0`, deterministically backfilling `1..N` by
  creation order). Order collisions map to exit 5. `sprint get`/`list` expose the
  `order` field. Specified across `SPEC/MODELS.md`, `SPEC/DATABASE.md`,
  `SPEC/STATE_MACHINE.md`, `SPEC/COMMANDS.md`, `SPEC/HELP.md`,
  `SPEC/DATA_FORMATS.md`, `SPEC/WEB.md`, and `SPEC/VERSION.md`.
- **Graph query notifications as diagnostics** — `rmp graph query`/`search` now
  print each GoGraph query notification (for example the Neo4j-compatible
  `CartesianProductWarning` for a disconnected multi-pattern `MATCH`) as a
  plain-text line to stderr in the form `<Severity> <Code>: <Description>`. The
  stdout success JSON and exit codes are unchanged, so JSON-parsing consumers are
  unaffected. Specified in `SPEC/GRAPH.md`.

### Changed

- **`sprint create` now requires `--title`** — a previously non-existent flag is
  now mandatory on `sprint create`. Scripts that create sprints must pass
  `-t/--title`; calls without it fail with exit 6
  (`required parameter missing: --title`). This is the only backward-incompatible
  command-line change in the release; see Upgrade Notes.
- **Unified sprint presentation in `rmp web`** — a shared `{{sprintDetail}}`
  sub-template renders the full sprint block (completion summary, metadata
  datagrid, and member-task table) identically on the sprint page and the Actual
  tab of the sprints page, so the two views can no longer diverge. The CLI
  `sprint show` report and the web summary now share
  `CategorizeTaskStatus`/`CalculateSprintSummary`/`CompletionPercentage`, so their
  figures are guaranteed consistent. Specified in `SPEC/WEB.md`.
- **GoGraph upgraded `v0.2.0` → `v0.3.2`** — the engine behind `rmp graph` and the
  read-only graph page of `rmp web` adopts upstream robustness, security, Cypher,
  and durability hardening. The release line is API-additive over `v0.2.0`; no
  consumed exported identifier was removed or renamed. v0.3.2 specifically fixes a
  recovery panic present in v0.3.0/v0.3.1 when reopening a `v0.2.0`-written store,
  which is why the pin targets v0.3.2. The indirect `golang.org/x/sys` hash is
  refreshed and the toolchain directive is `go1.26.4`. Specified across
  `SPEC/BUILD.md`, `SPEC/GRAPH.md`, and `SPEC/ARCHITECTURE.md`.

### Fixed

#### Reliability and SPEC conformance (audit findings #39–#63)

- **Concurrent graph-write data loss (#39, CRITICAL)** — `rmp graph` writes now
  take an exclusive, non-blocking file lock for the whole write (open → commit →
  checkpoint → WAL-truncate). Two concurrent writers could otherwise interleave so
  one writer's snapshot checkpoint overwrote the other's committed-but-unseen
  write and then truncated the WAL that still held it, silently losing an
  acknowledged write. Contention now surfaces as exit 1.
- **Per-connection PRAGMAs (#41) and version comparison (#42)** — `foreign_keys`
  and `busy_timeout` are now carried in the SQLite DSN so every pooled connection
  applies them (a one-shot `Exec` had left the second pooled connection with
  `foreign_keys=OFF`, silently disabling `ON DELETE CASCADE`). Migration gating now
  compares versions numerically, so `1.9.0` versus `1.10.0` orders correctly and
  migrations are no longer skipped once a component reaches two digits.
- **Read-only web database access (#43)** — `rmp web` opens roadmap databases with
  `query_only` and without running migrations, so a mere page view can never
  rewrite a stale-schema `project.db`.
- **Task-command correctness (#44–#46, #48)** — `task get`, `task priority`, and
  `task severity` fail fast with exit 4 on unknown IDs (no phantom audit rows,
  no partial mutation in a mixed batch); out-of-range priority/severity returns
  exit 6; a no-field `task edit` is a successful no-op.
- **Sprint task management (#40, #47, #49, #50)** — `sprint remove-tasks` is scoped
  to the named sprint (membership-checked, exit 6 otherwise), resets reverted tasks
  to `BACKLOG` clearing their lifecycle timestamps and completion summary, and
  compacts remaining positions to a contiguous sequence; `move-to` clamps an
  out-of-range position to the end.
- **State-machine and empty-list contracts (#53, #55)** — the `DOING → SPRINT`
  manual transition is forbidden (SPRINT is set exclusively by
  `sprint add-tasks`); empty sprint and audit lists marshal to `[]`, never `null`.
- **SPEC-verbatim messages and exit codes (#54, #58, #59, #60)** — required-
  parameter and roadmap-name error messages match the SPEC canonical text exactly;
  an int64-overflowing all-digit ID is a range failure (exit 6); the data
  directory's `0700` permission is re-applied and verified on every layout
  migration.
- **Global help generated from the registry (#51)** — `rmp --help` builds its
  command list from the single command registry instead of a hardcoded block that
  had silently dropped the `web` command.
- **Atomic sprint add/move-task audit (#65–#67)** — `AddTasksToSprint` and
  `MoveTasksBetweenSprints` write their audit rows inside the same transaction as
  the membership change; sprint capacity is enforced inside the transaction,
  closing a TOCTOU window. `DeleteSprint` and `RemoveTasksFromSprint` run their
  multi-statement mutations in a single transaction so task status and membership
  can never diverge.
- **Idempotent migrations (#68)** — `ALTER TABLE ADD COLUMN` migrations are guarded
  by a `pragma_table_info` column-existence check.
- **Audit catalogue and help cleanup** — the unused `TASK_TYPE_CHANGE` audit
  operation is removed (a task type change is recorded as `TASK_UPDATE`); the
  `audit list --operation` help, SPEC, and code now agree.

### Security (audit findings #64–#87)

- **`rmp web` server hardening (#69–#71, #73, #76)** — added `WriteTimeout` (30 s)
  and `IdleTimeout` (120 s) to mitigate slowloris-style denial of service; `/static/`
  directory requests return 404 (no embedded-tree disclosure); a strict security-
  header set (Content-Security-Policy, `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY`, `Referrer-Policy: same-origin`) is applied to every
  response; `/graph/data` emits HTML-safe JSON; the default bind host is now
  `127.0.0.1`, and a non-loopback bind prints a network-exposure warning to stderr.
- **Never serve stale data** — `Cache-Control: no-store` is set on every data-
  derived response (all dynamic pages, the `/graph/data` endpoint, and data-state-
  dependent error responses) at the single outermost middleware choke point, so no
  client or intermediary cache can re-present a database/store state that has since
  changed. Immutable `/static/` assets stay cacheable.
- **Symlink following refused (#72, #74, #75)** — the data directory, roadmap
  directories, and the legacy-layout migration are guarded by an `Lstat` symlink
  check, so a pre-placed symlink can no longer redirect a `project.db` write or a
  `0700` `chmod` outside `~/.roadmaps`.
- **Bounded results and secure file permissions (#64, #77, #78)** — `GetAuditEntries`
  hard-caps its result set; a new `project.db` is created with
  `O_CREATE|O_EXCL` mode `0600` before `sql.Open`, eliminating the umask-derived
  world-readable window, and the `-wal`/`-shm` sidecars are `chmod`-ed to `0600`.
- **Input validation hardening (#82–#87)** — CLI free-text inputs reject ASCII
  control characters (except TAB/LF/CR), DEL, and Unicode bidirectional/format code
  points, blocking terminal-escape injection and Trojan Source (CVE-2021-42574);
  specialist names containing the list-separator comma are rejected; audit IDs,
  `--entity-id`, `--limit`, sprint `--max-tasks`, and sprint `move-to` positions are
  bounded.
- **Read-only graph guard-rail rejects DDL (#79, #80)** — the `query`/`search`
  guard-rail rejects `CREATE`/`DROP INDEX|CONSTRAINT` using a case- and whitespace-
  insensitive matcher on the literal-masked query string (GoGraph's own check was
  case/whitespace-sensitive and bypassable). DDL keywords inside string literals are
  not misclassified, so legitimate read queries and the knowledge-graph memory store
  still pass.

### Documentation

- `CLAUDE.md` gained "Separation of Responsibilities", prioritization, and
  "Knowledge Graph as Memory" working principles (internal, contributor-facing).
- `SPEC/IMPLEMENTATION.md` removed an unimplemented "Connection Caching" section
  so the specification reflects the real code (#63); four further SPEC-vs-code
  contradictions were reconciled (#56, #61, #62).
- `DOCS/commands/sprint.md` and `DOCS/commands/audit.md` synced with the new
  `--title`/`--order` flags and the trimmed audit-operation catalogue.

### Tests

- New E2E suites: `tests/test_40_graph_notifications.py`,
  `tests/test_41_graph_concurrency_input.py`,
  `tests/test_42_security_audit.py` (8 standing defense assertions plus 15 finding
  regression probes for #64–#87), and `tests/test_43_sprint_order_field.py`.
- The full battery is green: `go test -count=1 ./...` (7 packages PASS),
  `gofmt -l .` clean, `go vet ./...` 0 issues, `golangci-lint run ./...` 0 issues,
  `go build` succeeds, and `python3 tests/run_tests.py` reports **42/42** (100 %).

### Known Issues

One SPEC-vs-code divergence remains open and is unaffected by this release. It does
not affect runtime behaviour and is tracked as a `spec` / `tech-debt` follow-up for
a future `specification-manager` pass:

- `SPEC/COMMANDS.md` documents the `audit stats` JSON keys `operations_count`,
  `entity_type_count`, `first_entry`, `last_entry` and a `period.{since,until}`
  block; the implementation emits `by_operation`, `by_entity_type`,
  `first_entry_at`, `last_entry_at`, `total_entries` with no `period` object.

[1.10.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.9.1...v1.10.0

## [1.9.1] - 2026-06-05

Hardens the `rmp graph` knowledge-graph store by upgrading its backing engine,
GoGraph, from `v0.1.0` to `v0.2.0`. The upgrade is a drop-in dependency change:
no `rmp` command, flag, JSON output, exit code, on-disk graph format, or database
schema is altered, and no `rmp` source change is required. A v0.2.0 usage
evaluation confirmed that the existing consumers (`internal/commands/graph.go`
and `internal/web/data.go`) are source-compatible, and the only behavioural
change that reaches Groadmap strengthens existing error handling. Under Semantic
Versioning 2.0.0 this is a **PATCH** release, fully backward compatible with
`v1.9.0`. The database schema version is unchanged at `1.6.0`, so existing
installations require no migration.

### Changed

- **`rmp graph` store hardened via GoGraph `v0.1.0` → `v0.2.0`** — the
  knowledge-graph store that backs the `graph` command family is upgraded to
  GoGraph `v0.2.0`, a reliability, ACID, and durability hardening release. The
  consumed surface (`store/recovery`, `store/wal`, `store/txn`, `store/snapshot`,
  `cypher`, `graph/lpg`, `graph/csr`) is unchanged at the API level; the
  exact-tag pin in `go.mod` moves to `v0.2.0` and the indirect
  `golang.org/x/exp` hash is refreshed. Specified across `SPEC/BUILD.md`,
  `SPEC/GRAPH.md`, and `SPEC/ARCHITECTURE.md`, all reconciled to the `v0.2.0`
  pin.

### Fixed

- **Fail-stop on genuine graph-store corruption** — `recovery.Open`, used by
  both `rmp graph` and the read-only `rmp web` graph page, now returns a clean
  error on genuine write-ahead-log corruption (CRC mismatch or unsupported
  record version) instead of the `v0.1.0` behaviour of swallowing it and risking
  further appends onto a damaged store. A benign crash-truncated WAL tail still
  recovers cleanly, so the change only tightens the corruption path and leaves
  the normal open path unaffected. Inherited crash-durability ordering fixes from
  GoGraph also apply: the snapshot writer `fsync`s its staging directory before
  the publish rename, autocommit writes are made durable before they become
  visible, and the snapshot manifest now records the directed/multigraph shape so
  a simple graph cannot silently become a multigraph after a reopen.

### Security

- **Two Go standard-library vulnerabilities resolved** — the GoGraph `v0.2.0`
  upgrade pulls in the `go1.26.4` toolchain, which resolves **GO-2026-5039**
  (`net/textproto`) and **GO-2026-5037** (`crypto/x509`), both reachable through
  the dependency. `govulncheck ./...` was run against the upgraded module and
  reports **"No vulnerabilities found."**

### Documentation

- **Regression Prevention principle** — `CLAUDE.md` gains a "Regression
  Prevention" working principle. This is an internal, contributor-facing
  governance change only, with no code, CLI, or runtime impact.

### Known Issues

One SPEC-vs-code divergence remains open and is unaffected by this release. It
does not affect runtime behaviour and is tracked as a `spec` / `tech-debt`
follow-up for a future `specification-manager` pass:

- `SPEC/COMMANDS.md` documents the `audit stats` JSON keys `operations_count`,
  `entity_type_count`, `first_entry`, `last_entry` and a `period.{since,until}`
  block; the implementation emits `by_operation`, `by_entity_type`,
  `first_entry_at`, `last_entry_at`, `total_entries` with no `period` object.

[1.9.1]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.9.0...v1.9.1

## [1.9.0] - 2026-06-03

Redesigns the read-only `rmp web` interface and restructures its navigation. The
web UI is rebuilt on a shared Tabler-based layout with a D3.js knowledge-graph
viewer (replacing the previous Cytoscape.js renderer), and the roadmap view is
split into a dedicated Sprints landing page and a separate Tasks page served from
a new `/roadmaps/{name}/tasks` endpoint. Authored line breaks in sprint and task
free-text are now preserved verbatim. Every change is additive or
presentation-only: the `rmp` CLI remains the sole write path, the read-only
contract is unchanged, and there are no CLI, JSON output, exit-code, or database
schema changes. Under Semantic Versioning 2.0.0 this is a **MINOR** release, fully
backward compatible with `v1.8.2`. The database schema version is unchanged at
`1.6.0`.

### Added

- **Dedicated sprint page** — a new read-only page at
  `/roadmaps/{name}/sprints/{id}` presents a single sprint and its member tasks.
  The id is validated in the handler; an unknown or non-integer id returns HTTP
  `404`. Specified in `SPEC/WEB.md` (Routes and Pages).
- **Separate Tasks endpoint** — the roadmap view is split into two pages. The
  landing page (`/roadmaps/{name}`) is now the Sprints page (three sprint tabs,
  the current/`OPEN` sprint expanded by default), and a new
  `/roadmaps/{name}/tasks` endpoint serves the full task list with a per-row
  read-only detail modal. The sidebar links Sprints to `/roadmaps/{name}` and
  Tasks to `/roadmaps/{name}/tasks`. Both endpoints answer `GET`/`HEAD` only and
  keep the existing roadmap-name 404 path guard.

### Changed

- **Read-only web UI redesigned on a shared Tabler layout** — `index`, the
  roadmap pages, and the graph page now extend a single shared base layout
  (HTML skeleton, Tabler-based navbar and shell, favicon, vendored CSS and
  fonts), removing the per-page duplicated boilerplate. The vendored asset set is
  updated accordingly: the Cytoscape.js bundle is removed and a new vendor bundle
  (D3 with d3-sankey, the Inter web font, the Tabler UI framework and Tabler
  Icons, and the favicon) is embedded with `go:embed`, each bundle's licence
  recorded in `vendor/LICENSES.md`. The interface remains fully self-contained
  and renders offline; the server makes no outbound request.
- **Knowledge-graph viewer rebuilt on D3.js** — the graph renderer is
  reimplemented in D3.js, replacing Cytoscape.js. A force-directed layout is the
  default, with the D3 "Networks" gallery layouts (including the d3-sankey flow
  layout) selectable from a dropdown. Graph data is fetched once and re-rendered
  in memory on layout change, with no re-fetch (`SPEC/WEB.md` FR7, AC10).
- **Authored line breaks preserved in sprint and task free-text** — sprint
  descriptions and task long-text now retain their authored newlines instead of
  collapsing them under HTML's default whitespace handling. A shared `pre-wrap`
  stylesheet rule (`white-space: pre-wrap; word-break: break-word;
  overflow-wrap: anywhere`) covers both the task detail modal and sprint
  descriptions across the Sprints page and the sprint detail page. Output remains
  `html/template`-escaped; no raw HTML is introduced.
- **Project governance** — `CLAUDE.md` gains a "Core Working Principles" section
  (Ask-Never-Assume, Never-Guess, Measure-to-Decide, Production-Grade-by-Default,
  Self-Contained Development, and the Specify -> Implement -> Test -> Document
  workflow), reinforced across the Decision Matrix and Anti-Patterns sections.
  Documentation and process only; no code or SPEC impact.

### Tests

- Web E2E suite extended for the redesigned UI: the shared layout chrome, the new
  sprint page (valid id and 404 paths), the D3 graph assets, the Sprints/Tasks
  split, the Tasks page with HTML escaping, the new sidebar links, and the
  verbatim survival of multi-line sprint and task free-text in the served HTML.
- New Go unit tests cover the shared layout rendering and the sprint page
  (`internal/web/layout_test.go`, `internal/web/sprint_test.go`); `web_test.go`
  is updated for the D3 asset set and the new routes.
- All validation gates pass against the freshly built binary (fmt / vet / test
  under the race detector / build / lint clean).

[1.9.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.8.2...v1.9.0

## [1.8.2] - 2026-06-03

Corrects a silent result-truncation defect in large multi-task fetches and
activates a query-caching optimisation that was specified but dormant. Both
changes are backward-compatible bug fixes: existing commands now return the
complete data they were always meant to return, and a mandated internal
optimisation finally runs. No new commands, flags, JSON output fields, exit
codes, or database schema changes, so this is a **PATCH** release fully
compatible with `v1.8.1`. The database schema version is unchanged at `1.6.0`.

### Fixed

- **Large task fetches and batch updates no longer truncate to the first 1000
  ids** — multi-id task operations built a single `WHERE id IN (...)` clause and
  passed the id count through `normalizeSize`, which caps at 1000. Result sets
  with more than 1000 matching tasks (for example `task list` over a large
  roadmap) were silently truncated to the first 1000 rows, and batch status,
  priority, and sprint-membership updates could miss tasks beyond the cap. Every
  multi-id path now sorts a copy of the id set and chunks it through the
  `BatchProcessor` (`ProcessChunks` / `ProcessChunksWithResult`), so results are
  complete and each generated statement stays within SQLite's per-statement
  variable limit. The caller's slice is never mutated, and chunks are processed
  in deterministic id order.
- **Query cache reconciled with the real schema and activated** — the
  `QueryCache` templates referenced a fictional schema (columns that never
  existed in the `tasks` table), so the cached query plans could not be used and
  the optimisation mandated by `SPEC/IMPLEMENTATION.md` (§ Query Caching) was
  effectively dead. The templates are now generated from a single
  `buildTemplates` source of truth shared by the pre-generation and on-demand
  paths, byte-identical in semantics to the real production queries (full task
  projection with dependency columns, subtask count, and the `ORDER BY t.id`
  tail). The cache and batch path are now genuinely active, so repeated batch
  operations reuse prepared query plans instead of rebuilding them.

### Performance

- **Repeated batch task operations reuse cached query plans** — with the query
  cache reconciled and activated (see Fixed), `GetTasks` and the batch
  status/priority/sprint-membership updates now fetch a cached, chunk-sized
  template per operation instead of formatting a fresh SQL string on every call.
  The optimisation is internal and changes no observable output; it reduces
  per-call query construction on hot batch paths.

### Removed

- **Dead code in `internal/db` and `internal/commands`** — removed ten unused
  `internal/db` functions (parent/subtask, task-dependency, and max-position
  helpers superseded by current code paths) and the unused `HandleBacklog` and
  `HandleGraph` command wrappers (command dispatch already routes through the
  central registry). No behaviour change; these paths were unreachable.

### Tests

- **Nine dormant E2E suites revived and a dormancy guard added** — nine
  `tests/test_*.py` files existed on disk but were never registered in
  `tests/run_tests.py`, so they never ran and gave a false sense of coverage.
  All nine are now registered, and a new `assert_no_dormant_modules()` guard
  fails the run fast if any `tests/test_*.py` is left unregistered, preventing
  the gap from silently returning. The registered E2E suite grows from 27 to 37
  modules; all 37 pass (100 %) against the freshly built binary.
- **New `task list --created-since` / `--until` coverage** — added
  `tests/test_38_task_list_date_filters.py` exercising the date-range filters on
  `task list`. Two stale tests targeting non-existent features were removed.
- **Web-server coverage is now measurable** — the web server now tears down
  gracefully on `SIGTERM`, so coverage counters flush on shutdown. New
  `internal/web` unit tests (`data_test.go`, `handlers_test.go`,
  `server_test.go`) and new `internal/db` tests (`batch_test.go`,
  `query_cache_test.go`) accompany the fixes above.

### Tooling

- **New coverage targets** — `make cover` reports unit-test coverage, and
  `make cover-full` builds an instrumented binary, drives it through the E2E
  suite, and merges the result with unit coverage to report the real exercised
  command surface. Merged coverage for this release is **83.9 %**.

[1.8.2]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.8.1...v1.8.2

## [1.8.1] - 2026-06-03

Fixes a rendering defect in the `rmp web` knowledge-graph page introduced with
the web interface in `v1.8.0`. The page rendered the empty-state overlay on top
of a populated knowledge graph, hiding the graph from view. This is a
backward-compatible bug fix that restores the behaviour already specified in
`SPEC/WEB.md` (§ Empty graph). No new features, and no API, exit-code, or schema
changes, so this is a PATCH release fully compatible with `v1.8.0`.

### Fixed

- **`rmp web` graph page now renders the knowledge graph** — the
  `/roadmaps/{name}/graph` page no longer paints the empty-state overlay over a
  populated graph. Root cause: the `.graph-empty { display: flex }` class rule
  outranked the user-agent `[hidden] { display: none }` rule on specificity, so
  the `hidden` empty-state overlay (`position: absolute; inset: 0;` with an
  opaque background) was always painted on top of the Cytoscape canvas, hiding
  the graph that had in fact initialised correctly underneath. The fix adds a
  global `[hidden] { display: none !important; }` reset to
  `internal/web/static/style.css`, so the `hidden` attribute always wins over
  component `display` rules. The empty-graph state now appears only when the
  graph is genuinely empty, as specified in `SPEC/WEB.md` (§ Empty graph).

### Tests

- Added the regression test `TestEmbeddedCSS_HiddenAttributeWins` in
  `internal/web/web_test.go`, which asserts the embedded `style.css` carries the
  global `[hidden] { display: none !important }` rule so the defect cannot
  silently return (no browser required).
- E2E: 24/24 pass (100 % success rate) against the freshly built binary.

[1.8.1]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.8.0...v1.8.1

## [1.8.0] - 2026-06-03

Adds the read-only `rmp web` interface and aligns the CLI exit-code mapping with
the canonical `SPEC/ARCHITECTURE.md` contract. Both changes are additive or
corrective: the web interface is a new command and the exit-code change only
affects error paths the SPEC already defined. No existing JSON output schema
changes, so this remains backward compatible with `v1.7.0`.

### Added

- **`rmp web` command** — a read-only, self-contained, mobile-first web
  interface for browsing every roadmap under `~/.roadmaps/`, its tasks and
  sprints, and an interactive knowledge-graph visualisation. Specified in
  `SPEC/WEB.md`.
  - Serves server-rendered HTML and a JSON graph-data endpoint over the Go
    standard-library `net/http`; routes answer `GET`/`HEAD` only (any other
    method returns HTTP `405`).
  - Routes: roadmap index (`/`), roadmap detail (`/roadmaps/{name}`),
    knowledge-graph page (`/roadmaps/{name}/graph`), graph data
    (`/roadmaps/{name}/graph/data`), and embedded static assets (`/static/...`).
    Roadmap names from the URL are validated before any path is built; an
    invalid or unknown name returns HTTP `404`.
  - **Self-contained** — every asset (HTML templates, stylesheet, all client
    JavaScript including the vendored Cytoscape.js graph library, favicon) is
    embedded with `go:embed`; the interface renders fully offline and the
    server makes no outbound request.
  - **Read-only** — exposes no route that creates, edits, or deletes data; the
    graph store is opened read-only and a web read never triggers a checkpoint
    or write-ahead-log truncation. The `rmp` CLI remains the sole write path.
  - **Loopback by default** — binds `127.0.0.1:8787`; a non-loopback bind
    (`--host 0.0.0.0`) is an explicit opt-in. When `--port` is omitted and the
    default port is in use, the server falls back to an OS-chosen ephemeral
    port so it still starts.
  - Flags: `--host`, `--port`, `--no-open`, and `-h`/`--help`. The process is
    long-lived: `SIGINT`/`SIGTERM` shut it down gracefully (exit 0). It is the
    one command exempt from the always-required-roadmap rule and accepts no
    `-r`/`--roadmap` flag and no subcommands.

### Fixed

- **CLI exit-code mapping aligned with `SPEC/ARCHITECTURE.md`** —
  `ErrInvalidInput` now maps to exit `2` (misuse: unknown flags and
  subcommands, malformed or non-numeric IDs), while value, range, enum, date,
  state-transition and business-rule validations are reclassified to
  `ErrValidation` so they remain exit `6` (invalid data). This resolves the
  first item under the `v1.7.0` "Known Issues" (the `ErrInvalidInput`
  exit-code divergence).

### Tests

- E2E: 24/24 pass (100 % success rate) against the freshly built binary,
  including the new `tests/test_35_web_interface.py` suite covering the server,
  every route and method, the read-only guarantee, path-traversal validation,
  and graceful shutdown.
- Go unit tests green across all packages (fmt / vet / test / build / lint
  clean), including the new `internal/web` package tests.

[1.8.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.7.0...v1.8.0

## [1.7.0] - 2026-06-02

Feature release. Introduces the `rmp graph` command family: a per-roadmap
knowledge graph backed by the external GoGraph engine, accessed through Cypher
and exposed via five guard-railed subcommands (`create`, `query`, `update`,
`delete`, `search`). Each write is made durable through a synchronous
checkpoint that snapshots the committed state and truncates the write-ahead log
before the process exits. The graph is an additive capability: the existing CLI
surface, exit codes, and all JSON output schemas remain fully backward
compatible with `v1.6.0`, so this is a MINOR release under SemVer.

### Added

- **`rmp graph` command family** — a per-roadmap knowledge graph for recording
  and querying the project's elements and their relationships. Specified in
  `SPEC/GRAPH.md`. Five subcommands, each accepting Cypher via `--query` or
  standard input:
  - `graph create` — execute write Cypher that adds nodes and edges.
  - `graph query` — execute read Cypher and return `columns`/`rows` as JSON.
  - `graph update` — execute write Cypher that modifies existing elements.
  - `graph delete` — execute write Cypher that removes elements; deletions are
    durable tombstones that survive store reopen.
  - `graph search` — execute read Cypher tailored to lookup/traversal queries.
- **Guard-rail validation** — every subcommand validates that the supplied
  Cypher matches its operation class (read vs. write) before execution, so a
  read subcommand cannot mutate the graph and a write subcommand cannot be used
  to bypass the intended access pattern.
- **Cypher input precedence** — each subcommand reads its query from the
  `--query` flag when present, otherwise from standard input, enabling both
  inline invocation and piped/heredoc usage.
- **Synchronous checkpoint on write** — after a write subcommand commits its
  transaction durably, and before the process exits, the implementation
  produces a self-sufficient on-disk snapshot of the committed graph state and
  truncates the write-ahead log within the same invocation. This bounds WAL
  growth and keeps recovery cost proportional to the live graph size rather
  than to the total history of writes. Read subcommands never checkpoint.
- **Per-roadmap graph store** — each roadmap owns one graph, persisted under
  `~/.roadmaps/<name>/graph/`, independent of the roadmap's `project.db`. Graph
  operations never read or write the SQLite database.
- **Multigraph support** — parallel edges between the same pair of nodes are
  supported, allowing multiple distinct relationships to coexist.

### Changed

- **Go toolchain directive**: `go.mod` raised from `go 1.26.2` to `go 1.26.4`.
  CI and release workflows derive the Go version from `go.mod` via
  `go-version-file`, so no workflow edit was required.
- **`SPEC/VERSION.md` `Current Version` table**: corrected the stale
  Application entry (was `v1.2.1`) to reflect the real state
  (Application `v1.7.0`, Database Schema `v1.6.0`), and updated the illustrative
  `const version` snippet to match.

### Dependencies

- **GoGraph** added as a direct dependency, pinned at the exact tag `v0.1.0`
  (a pre-1.0 release consumed directly via `go get`, with no pseudo-version).
  GoGraph provides the labelled property graph, the Cypher engine, the durable
  on-disk store, durable node tombstones (deletes survive reopen), and
  multigraph parallel edges that back the `rmp graph` command.

### Tests

- Go unit tests: 6 packages, all green (fmt / vet / test / build / lint clean).
- E2E: 23/23 pass (100 % success rate) against the freshly built `v1.7.0`
  binary on the Go 1.26.4 toolchain.
- Two new E2E suites added for the graph command:
  - `tests/test_33_graph_checkpoint.py` — verifies the synchronous
    snapshot-and-WAL-truncate checkpoint contract on every write.
  - `tests/test_34_graph_realistic_usage.py` — exercises 219 graph calls in a
    realistic modelling scenario, including multigraph parallel edges.

### Known Issues

The two SPEC-vs-code divergences carried forward from prior releases remain
open and are unaffected by this release:

1. **Exit-code mapping for `ErrInvalidInput`** — `SPEC/ARCHITECTURE.md`
   documents exit code `2`; the implementation uses `ExitInvalidData = 6`.
2. **`audit stats` JSON keys** — `SPEC/COMMANDS.md` documents one set of keys;
   the implementation emits a different (stable) set.

Both are tracked as `spec` / `tech-debt` GitHub issues and will be resolved by
a `specification-manager` pass in a future release.

[1.7.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.6.0...v1.7.0

## [1.6.0] - 2026-06-01

Maintenance release. Updates the Go toolchain directive to `1.26.2`, upgrades
all GitHub Actions to their latest patch releases, and bumps transitive Go
module dependencies. Removes the stale Windows binary references from the
release workflow body (Windows builds were dropped in v1.5.0 when GoGraph's
`syscall.Kill` dependency made cross-compilation to Windows infeasible). No
new features, no breaking changes; the public CLI surface, exit codes, and
all JSON output schemas remain fully backward compatible with `v1.5.0`.

### Changed

- **Go toolchain directive**: `go.mod` raised from `go 1.26` to `go 1.26.2`.
- **GitHub Actions — `actions/checkout`**: v6 → v6.0.2.
- **GitHub Actions — `actions/setup-go`**: v6.3.0 → v6.4.0.
- **GitHub Actions — `actions/upload-artifact`**: v7 → v7.0.1.
- **GitHub Actions — `actions/download-artifact`**: v7/v8 → v8.0.1
  (unified to a single version across `ci.yml` and `release.yml`).
- **GitHub Actions — `golangci/golangci-lint-action`**: v8 → v9.2.1.
- **GitHub Actions — `codecov/codecov-action`**: v5.5.3 → v6.0.1.
- **GitHub Actions — `softprops/action-gh-release`**: v2.6.1 → v3.0.0.
- **Go module `google/go-cmp`**: v0.6.0 → v0.7.0 (indirect).
- **Go module `golang.org/x/text`**: v0.22.0 → v0.37.0 (indirect).
- **Go module `modernc.org/cc/v4`**: v4.28.2 → v4.28.4 (indirect).
- **Go module `modernc.org/ccgo/v4`**: v4.34.2 → v4.34.4 (indirect).
- **Release workflow body**: stale Windows binary download links removed
  from the `dev-release` step; Windows targets were already dropped in
  a prior commit due to GoGraph's `syscall.Kill` dependency.

### Tests

- Go unit tests: 6 packages, all green (fmt / vet / test / build / lint clean).
- E2E: 21/21 pass (100 % success rate).
- `gosec` not installed on the release host; security gate skipped and noted
  per project policy. No security-relevant code changes in this release.

### Known Issues

The two SPEC-vs-code divergences flagged in earlier releases remain open and
are unchanged by this release:

- `SPEC/ARCHITECTURE.md` documents `ErrInvalidInput` mapping to exit code `2`;
  the implementation maps `ErrInvalidInput`, `ErrValidation` and
  `ErrFieldTooLarge` to `ExitInvalidData = 6`.
- `SPEC/COMMANDS.md` `audit stats` JSON keys differ from the implementation
  (`by_operation` / `by_entity_type` / `first_entry_at` / `last_entry_at` /
  `total_entries`, no `period` object). Implementation behaviour is stable.

No E2E tests cover `rmp graph` subcommands. Coverage will be added in a
follow-up release.

[1.6.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.5.0...v1.6.0

## [1.5.0] - 2026-06-01

Minor release. Introduces the **`rmp graph` command**: a Cypher-queryable
knowledge graph per roadmap, backed by the GoGraph engine and persisted under
`~/.roadmaps/<name>/graph/`. Adds the **per-roadmap directory layout** with
automatic legacy migration, moving each roadmap's SQLite database from a flat
`~/.roadmaps/<name>.db` file to `~/.roadmaps/<name>/project.db` inside its own
home directory. The AI Agent Contract gains two new registry fields
(`stdin_fallback`, `reads_stdin`), a new `build_knowledge_graph` workflow, and
two new pitfall entries. No breaking changes; the public CLI surface, exit codes
and existing JSON output schemas remain backward compatible with `v1.4.0`.

### Added

- **`rmp graph` command** (`internal/commands/graph.go`,
  `internal/commands/registry_graph.go`): five subcommands backed by the
  GoGraph Cypher engine:
  - `graph create` — executes `CREATE`/`MERGE` queries to add nodes or edges.
  - `graph query` — executes read-only `MATCH ... RETURN` queries.
  - `graph update` — executes `SET`/`REMOVE` queries to mutate existing elements.
  - `graph delete` — executes `DELETE`/`DETACH DELETE` queries to remove elements.
  - `graph search` — executes read-only traversal queries (variable-length paths).
  Each subcommand is a guard rail: it rejects a Cypher query whose operation class
  does not match the subcommand, exiting with code `6` before touching the graph.
  The `--query` flag falls back to reading from standard input when absent.
  The graph store is rooted at `~/.roadmaps/<name>/graph/` (mode `0700`),
  created on first use, and is durable via GoGraph's WAL.
- **Per-roadmap directory layout** (`internal/utils/path.go`): each roadmap is
  now stored under `~/.roadmaps/<name>/` (mode `0700`) with the SQLite database
  at `~/.roadmaps/<name>/project.db` (mode `0600`).
- **Automatic legacy migration** (`internal/utils/migrate.go`):
  `MigrateLegacyLayout` runs at startup and atomically renames any flat
  `~/.roadmaps/<name>.db` file into `~/.roadmaps/<name>/project.db`. The
  migration is idempotent, skips symbolic links and invalid names, and handles
  WAL/SHM sidecars best-effort. An existing `project.db` is never overwritten.
- E2E test `test_32_layout_migration.py`: end-to-end coverage of the migration
  (data preservation, permissions, idempotent re-run, conflict resolution,
  symlink security guard).
- Go unit tests in `internal/utils/migrate_test.go`: happy-path, idempotent
  no-op, conflict, empty home directory recovery, invalid-name skip, and symlink
  guard scenarios.
- AI contract field `stdin_fallback` on `FlagEntry`: projected from registry's
  `Flag.StdinFallback`; omitted when false.
- AI contract field `reads_stdin` on `SubcommandEntry`: projected from registry's
  `Subcommand.ReadsStdin`; omitted when false.
- AI contract workflow `build_knowledge_graph`: a four-step guide for populating
  and querying a roadmap's knowledge graph with Cypher.
- AI contract pitfall `graph_guard_rail_mismatch`: documents the exit-6 guard
  rail and shows the correct subcommand for each operation class.
- AI contract pitfall `graph_missing_query`: documents the stdin-fallback
  behaviour and the failure mode when neither `--query` nor stdin is supplied.
- `SPEC/GRAPH.md` (new): complete specification for the graph command —
  persistence layout, guard-rail rules, Cypher input source precedence, output
  schemas, exit codes, and security model.

### Changed

- Storage layout: roadmap databases moved from `~/.roadmaps/<name>.db` to
  `~/.roadmaps/<name>/project.db`. Existing databases are migrated automatically
  on the first run of any command (other than `--help`, `--version`,
  `--ai-help`). No manual action is required.
- `go.mod`: GoGraph (`github.com/FlavioCFOliveira/GoGraph`) promoted from
  indirect to direct dependency at `v0.0.0-20260601121207-03162239610a`; `go`
  directive raised to `1.26`.
- AI contract tool description updated to mention the Cypher-queryable knowledge
  graph capability (`cmd/rmp/aihelp_wiring.go`).
- `internal/commands/registry.go`: `Flag` gains `StdinFallback bool`;
  `Subcommand` gains `ReadsStdin bool`.
- `internal/commands/registry_data.go`: `graph` family registered in the
  declarative command registry.
- `internal/commands/roadmap.go`: `list` output now reports
  `<name>/project.db` paths; `remove` deletes the whole `<name>/` home
  directory.
- `internal/db/connection.go`: `Open` ensures the roadmap home directory before
  opening `project.db`.
- SPEC updated: `ARCHITECTURE.md`, `COMMANDS.md`, `DATA_FORMATS.md`, `BUILD.md`,
  `HELP.md`, `IMPLEMENTATION.md`, `README.md`, `DATABASE.md`, `DEPLOY.md`,
  `VERSION.md`.

### Tests

- E2E: 21/21 pass (`test_32_layout_migration.py` added; no E2E coverage for
  `rmp graph` subcommands yet — tracked as a follow-up).
- Go unit tests: 6 packages, all green (fmt/vet/test/build/lint clean).

### Known Issues

The two SPEC-vs-code divergences flagged in earlier releases remain open and are
unchanged by this release:

- `SPEC/ARCHITECTURE.md` documents `ErrInvalidInput` mapping to exit code `2`;
  the implementation maps `ErrInvalidInput`, `ErrValidation` and
  `ErrFieldTooLarge` to `ExitInvalidData = 6`.
- `SPEC/COMMANDS.md` `audit stats` JSON keys differ from the implementation
  (`by_operation` / `by_entity_type` / `first_entry_at` / `last_entry_at` /
  `total_entries`, no `period` object). Implementation behaviour is stable.

No E2E tests cover `rmp graph` subcommands in this release. Coverage will be
added in a follow-up release.

[1.5.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.4.0...v1.5.0

## [1.4.0] - 2026-05-25

Minor release. Introduces the **AI Agent Contract**: a machine-readable JSON
description of every command, flag, exit code, JSON output shape, common
workflow and known pitfall, exposed via `rmp --ai-help` and `rmp ai-help`.
The release adds proactive discovery hints (a banner on every `--help` page,
a stderr hint on every error, and an opt-in `AI_AGENT=1` environment-variable
hint) so that AI agents that invoke `rmp` always find the contract entry
point. Internally, command dispatch is now driven by a declarative command
registry, eliminating the long-standing handcrafted switch chains. The
`sprint` description length cap is raised from 500 to 2048 characters. No
breaking changes; the public CLI surface, exit codes and existing JSON
output schemas remain backward compatible with `v1.3.0`.

### Added

- **AI Agent Contract** (`internal/aihelp`): a complete machine-readable
  contract describing every command family, subcommand, flag, alias,
  enum, exit code, JSON output shape, plus canonical `common_workflows`
  and `pitfalls`. The contract is generated from the same registry that
  drives runtime dispatch, so it cannot drift from the binary.
- `rmp --ai-help` global flag and `rmp ai-help` command emit the contract
  to stdout. The flag takes precedence over `--help`, `--version`, `-r`
  and every action flag. Scoping is supported: `rmp task --ai-help`,
  `rmp sprint create --ai-help` and equivalents return the relevant
  contract slice.
- AI-agent discovery banner prepended to every `--help` page (main help
  and every family/subcommand help), pointing agents at `rmp --ai-help`.
- AI-agent stderr hint emitted on the error path
  (`Error: ...` followed by a hint pointing at `rmp --ai-help`).
- Opt-in `AI_AGENT=1` environment-variable mode: when active, the
  discovery hint is the first line written to stderr for the entire
  invocation, with a `sync.Once`-guarded dedup so it appears exactly
  once even when both the env-var path and the error path are involved.
  The hint is intentionally suppressed when the invocation itself is
  serving the contract.
- Declarative **command registry** (`internal/commands/registry*.go`):
  command families, subcommands, aliases, and handlers are now declared
  as data. `cmd/rmp/main.go` dispatches through the registry and the
  AI Agent Contract is generated from the same source.
- E2E test `test_30_aihelp_contract.py`: exhaustive coverage of the
  contract surface (579 lines) including precedence, scoping,
  exit codes, JSON schema invariants and discoverability.
- E2E test `test_31_sprint_description_limit.py`: exhaustive coverage
  of the new sprint description length boundary (178 lines).
- Go unit-test suites for the AI Agent Contract generator, registry,
  banner, hint emission and the `--ai-help` wiring layer.
- `DOCS/commands/ai-help.md`: complete reference page for the AI Agent
  Contract feature.
- README section surfacing `--ai-help` for human discovery.

### Changed

- `sprint` description maximum length raised from **500** to **2048**
  characters (`internal/models/consts.go`, SPEC/DATABASE.md,
  SPEC/MODELS.md). Existing rows are unaffected; only the validator
  upper bound changes. No schema migration required.
- Command family dispatch (`task`, `sprint`, `roadmap`, `audit`,
  `backlog`) now flows through the declarative registry rather than
  per-family switch statements. Public CLI behaviour is unchanged.
- The AI-agent stderr hint replaces the previous silent error path: every
  error message is now followed by `AI agents: run `+"`rmp --ai-help`"+`
  for a machine-readable command contract.` (suppressed when the
  contract itself is being emitted).

### Documentation

- New SPEC pages: SPEC/COMMANDS.md (AI Help section), SPEC/HELP.md
  (banner, error hint, AI_AGENT env var), SPEC/ARCHITECTURE.md
  (contract subsystem), SPEC/DATA_FORMATS.md (contract JSON schema).
- README and `DOCS/commands/` updated to surface the new feature.

### Known Issues

The two SPEC-vs-code divergences flagged in v1.3.0 remain open and are
unchanged by this release:

- `SPEC/ARCHITECTURE.md` documents `ErrInvalidInput` mapping to exit
  code `2`; the implementation maps `ErrInvalidInput`, `ErrValidation`
  and `ErrFieldTooLarge` to `ExitInvalidData = 6`.
- `SPEC/COMMANDS.md` `audit stats` JSON keys differ from the
  implementation (`by_operation` / `by_entity_type` /
  `first_entry_at` / `last_entry_at` / `total_entries`, no `period`
  object). Implementation behaviour is stable and adopted by tooling.

[1.4.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.3.0...v1.4.0

## [1.3.0] - 2026-05-13

Minor release. Adds GNU-style `--flag=value` parsing across every command and
introduces per-subcommand `--help` for the `roadmap`, `task`, `sprint`, `audit`
and `backlog` families. Several internal performance and refactoring passes
reduce SQL round-trips and standardise data-access patterns. Test coverage
grows substantially (E2E and Go unit tests) and the SPEC has been reorganised
for single-responsibility files. No breaking changes; the public CLI surface,
exit codes and JSON output schemas remain backward compatible with `v1.2.1`.

### Added

- GNU-style `--flag=value` syntax is now accepted across all commands, in
  addition to the existing space-separated form.
- Per-subcommand `--help` for every command in the `roadmap`, `task`,
  `sprint`, `audit` and `backlog` families, backed by a shared `hasHelpFlag`
  helper.
- Family help texts now enumerate valid enum values (status, priority, type),
  document previously-omitted flags, distinguish similar listing commands,
  describe their JSON outputs, document conditional flags and workflow rules,
  and list exit codes per command family.
- Go unit tests for `sprint open-tasks`.
- E2E coverage for backlog `list` and `show-next`, task dependency workflows,
  subtask and dependency completion guards, command-alias surface, the
  `4096`-byte field-length boundary, exit codes `127` (unknown command) and
  `130` (SIGINT), `reopen` lifecycle-field clearing, subprocess-level
  parallel `rmp` invocations, JSON schema shape assertions, and
  timing-realistic burndown and velocity scenarios.

### Changed

- The Go source uses the `any` alias instead of `interface{}` across the
  whole project for readability.
- `GetAuditEntries` now accepts an `AuditFilter` struct rather than a long
  positional argument list.
- Position and swap mutations are now executed through `WithTransaction`,
  unifying transactional boundaries for ordering operations.
- Inline status string literals in SQL fragments have been replaced with
  model-backed constants.
- Audit-row inserts have been consolidated into a single `LogAuditTx` helper.
- The `release-notes/` directory and this `CHANGELOG.md` are introduced for
  the first time; future releases will follow the same layout.

### Fixed

- Help texts no longer contain factual errors; wording is standardised across
  families and aligned with the actual runtime behaviour (exit codes,
  required vs optional flags, conditional flags).
- `isLockedError` now uses a structured `errors.As` check instead of
  string matching.
- Update statements emit fields in a deterministic sorted order to make
  generated SQL stable.
- Roadmap-name validation errors are now wrapped in `ErrValidation`.
- Code and SPEC are aligned across the twelve audit findings raised during
  the SPEC v2.0.0 consolidation.
- Lint: replaced the deprecated `reflect.Ptr` with `reflect.Pointer` in
  `flags.go`.

### Performance

- `GetAuditStats` collapsed into a single `GROUP BY` query.
- `GetAuditEntries` query construction uses `strings.Builder` to avoid
  intermediate string allocations.
- Task dependencies are now resolved with `group_concat` to eliminate the
  per-task N+1 query.
- `hasTransitiveDependency` uses a recursive Common Table Expression instead
  of an application-side breadth-first search.
- Subtask and dependency completion guards run as bulk queries inside
  `task_mutate`.
- `AddTasksToSprint` uses a single multi-row `INSERT`.
- `roadmapList` uses `entry.Info()` to skip a per-file `stat` syscall.
- `ValidateIDString` uses `strconv.Atoi` for ID parsing.

### Refactored

- `db.ConnectionCache` and the related `atexit` hooks were removed; nothing
  consumed them and the lifecycle was a source of confusion.
- The retry wrapper around `sql.Open` was dropped (the driver already
  retries lazy connections on first use).
- `ValidateNumericRange` helper added in `internal/utils` for bounded
  integer parsing.
- `ParseCommaSeparatedIDs` helper extracted in `internal/utils`.
- The unused string-match fallback in `handleError` was removed.
- `task_mutate` now reuses the cached `db.Placeholders` lookup.

### Tests

- E2E weak assertions of the form `!= 0` have been replaced with explicit
  exit-code and error-message checks (ten call-sites).
- Python test artefacts (`__pycache__`, `*.pyc`) are now git-ignored.
- New `perfsprint` lint compliance for the `sprint open-tasks` Go tests.

### Documentation

- The `SPEC/` directory has been reorganised into single-responsibility
  files optimised for LLM navigation, with versioning and change-history
  removed in favour of git as the source of truth.
- `SPEC/HELP.md` was refreshed to match the new per-subcommand help
  structure.
- The `.claude/` agent and skill references were updated after the
  project-local skill cleanup.

### Known Issues

Two SPEC-vs-code divergences remain open and are tracked as follow-up
issues so a future `specification-manager` pass can decide how to reconcile
them. Neither divergence affects runtime behaviour:

- `SPEC/ARCHITECTURE.md` documents `ErrInvalidInput` mapping to exit code
  `2`; the implementation in `cmd/rmp/main.go` maps `ErrInvalidInput`,
  `ErrValidation` and `ErrFieldTooLarge` to `ExitInvalidData = 6` and the
  help texts reflect the implementation.
- `SPEC/COMMANDS.md` documents the `audit stats` JSON keys
  `operations_count`, `entity_type_count`, `first_entry`, `last_entry`
  and a `period.{since,until}` block; the implementation emits
  `by_operation`, `by_entity_type`, `first_entry_at`, `last_entry_at`,
  `total_entries` with no `period` object.

[1.3.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.2.1...v1.3.0
