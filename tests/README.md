# Groadmap CLI Test Suite

End-to-end test suite for the Groadmap CLI. Every test drives the compiled
binary at `bin/rmp` as a real subprocess and asserts on what the user gets
back: the exit code, the JSON on stdout, the plain text on stderr, and the
state the next command can read.

## Test Modules

`tests/run_tests.py` is the authority on which modules exist: `TEST_MODULES`
holds the standard run and `STRESS_TEST_MODULES` holds the stress run. The
table below is the only place that records what each of those modules covers,
so the runner refuses to start when the two disagree — see
[Registry gates](#registry-gates).

| Module | What it covers |
|--------|----------------|
| `test_01_basic_crud.py` | Roadmap and task CRUD: create/list/remove roadmaps, create/get/edit/remove tasks, tasks created with every optional field, bulk operations and `task list` filters. |
| `test_02_sprint_lifecycle.py` | Sprint lifecycle: create, PENDING -> OPEN -> CLOSED and reopen, rejection of illegal transitions, description update, status-filtered list, removal, and a sprint that holds tasks. |
| `test_03_task_state_machine.py` | Task state machine: every legal transition BACKLOG -> SPRINT -> DOING -> TESTING -> COMPLETED and back, rejection of a manual `stat SPRINT` and of illegal moves, a full workflow and bulk status changes. |
| `test_04_sprint_task_management.py` | Sprint membership: `add-tasks`, `remove-tasks`, `move-tasks`, reassignment between sprints, status-filtered sprint tasks, sprint statistics, and removing a sprint that still holds tasks. |
| `test_05_audit_reporting.py` | Audit log: an entry per task create/update/status change and per sprint operation, the list filters, `audit history`, `audit stats` (including the null-timestamp empty-log regression), date ranges, and priority/severity changes with their filtered listings. |
| `test_06_edge_cases_errors.py` | Error paths and boundaries: missing required parameters, non-numeric and unknown IDs, out-of-range priority/severity, empty and special-character roadmap names, no roadmap selected, duplicate creation, and the exit code (2, 3, 4, 5 or 6) each of them returns. |
| `test_07_concurrency.py` | In-process concurrency: threads sharing one binary create tasks, change status and read while others write, plus a subprocess fan-out, asserting no lost or corrupted row. |
| `test_08_complex_workflow.py` | Realistic multi-sprint delivery: a full cycle over two sprints, carryover of unfinished work, reopen and recovery, execution ordering, sequential sprint management, mixed task types, rejected transitions leaving state intact, bulk transitions, editing an active task, and stats across the lifecycle. |
| `test_09_stress_load.py` | Stress and load: 1000 tasks, 50 sprints, a 50-task sprint reordered and bulk-completed, ten rapid open/close cycles, 100-task bulk priority updates, list timing over a 500-task backlog, concurrent reads, and stats accuracy over 200 tasks. |
| `test_10_task_next.py` | `task next`: ordering by sprint position, the count argument, no open sprint, an empty sprint, non-sprint statuses excluded, explicit roadmap, JSON structure, zero and invalid arguments, and the reaction to status changes. |
| `test_11_sprint_show.py` | `sprint show`: the full status report with progress, severity and criticality distributions, on empty, closed and pending sprints, at severity boundaries, plus the unknown-ID, missing-roadmap and non-numeric-ID error paths. |
| `test_12_sprint_stats.py` | `sprint stats`: status distribution and percentages for empty, single-status and mixed sprints, priority and severity spreads, `task_order` accuracy, counts after tasks are removed, a large sprint, an all-completed sprint, and the exit codes. |
| `test_13_sprint_task_ordering.py` | Sprint task ordering: `reorder`, `move-to`, `swap`, `top` and `bottom` asserted on the resulting positions, persistence across operations and status transitions, independence per sprint, and every rejection path (partial reorder, foreign task, invalid position). |
| `test_14_audit_date_filters.py` | `audit list` date filtering: `--since` alone, `--until` alone, the two combined as a range, and combined with the operation and entity filters. |
| `test_15_roadmap_stats.py` | `rmp stats`: the required roadmap/sprints/tasks fields, per-status task counters that sum to the total, sprint counters for open, closed and never-started sprints, empty roadmaps, a large roadmap, the help flags, and exit codes 3 and 4. |
| `test_16_boundary_unicode.py` | Boundaries and Unicode: priority and severity at 0/9 and one step outside on both create and the dedicated subcommands, titles at 255 bytes and free-text fields at 4096 bytes exactly and one over, CJK/RTL/emoji/diacritic round-trips, and SQL-injection payloads stored verbatim with the schema intact. |
| `test_17_task_type_flag.py` | `--type` on tasks: TASK as the default, every valid value accepted on create and on edit, unknown and lowercase values rejected, and the type reported by `task get` and `task list`. |
| `test_18_cli_validation_data_integrity.py` | Three integrity rules: only one sprint may be OPEN at a time (start and reopen both blocked, error naming the blocking sprint), `task remove` restricted to BACKLOG (batch fails whole), and `task reopen` clearing the lifecycle timestamps, in bulk, with its audit entries. |
| `test_19_completion_summary.py` | `completion_summary`: stored by `--summary`/`-s` on the COMPLETED transition, null without it, present in list output, applied to every ID of a bulk transition, rejected on non-COMPLETED targets, accepted at exactly 4096 characters and rejected one over, and cleared by both a BACKLOG transition and `task reopen`. |
| `test_20_task90_sprint_closed_guard.py` | Closed-sprint guard: `add-tasks` on a CLOSED sprint rejected with exit 6 naming the ID and the status, `sprint close` blocked while tasks are SPRINT/DOING/TESTING, `--force` closing with a stderr warning and no stdout, and COMPLETED tasks never blocking a close. |
| `test_21_task89_move_tasks_closed_guard.py` | Move guard: `sprint move-tasks` to or from a CLOSED sprint rejected with exit 6 naming that sprint, moves between OPEN and PENDING accepted (single and bulk), both-sprints-closed rejected, and a successful move writing nothing to stdout. |
| `test_22_task87_sprint_capacity.py` | Sprint capacity: `--max-tasks` on create and update, `add-tasks` rejected when the limit would be exceeded (including a bulk add rejected as a whole), `current_load` and `capacity_pct` in `sprint show`, sprints without a limit reporting null, and the validation of the flag's own value. |
| `test_23_backlog_management.py` | `backlog list` and `backlog show-next`: BACKLOG-only results, the `--priority` and `--type` filters, every `--sort` ordering, `--limit` bounds, the `ls` alias, the default count of five, and the invalid sort/type/limit/count error paths. |
| `test_24_dependency_workflow.py` | Task dependencies: `add-dep`/`remove-dep`, the `blockers`/`blocking` inverse queries, `depends_on` and `blocks` on `task get`, self-dependency and cycles rejected, COMPLETED dependencies dropped from blockers, the completion guard, and the audit entries for both operations. |
| `test_25_completion_guards.py` | The two completion guards: incomplete subtasks and incomplete dependencies each block COMPLETED naming the blocking IDs, the subtask guard is evaluated first when both would fire, and a rejected transition leaves the task in its original status. |
| `test_26_timing_realism.py` | Time-aware reporting driven by back-dated timestamps written straight into SQLite: a burndown series with one row per completion day, `days_elapsed` reflecting a sprint's real age, and roadmap velocity averaged across recently closed sprints. |
| `test_27_exit_code_extremes.py` | The two ends of the exit-code contract the rest of the suite does not reach: an unknown command or subcommand exits 127 and prints usage, and a SIGINT delivered to a running process collapses to exit 130. |
| `test_28_command_aliases.py` | The documented aliases: the top-level `t`, `s`, `bl`, `aud` and `road`, and the subcommand `ls`, `rm`, `new`, `hist`, `upd`, `stat`, `prio`, `sev`, `add`, `rm-tasks`, `mv-tasks`, `mvto`, `btm` and `order`, each proven equivalent to its long form. |
| `test_29_subprocess_concurrency.py` | Concurrency as the user meets it, between OS processes rather than threads: eight parallel `rmp` processes creating tasks lose no row, and readers never fail while another process bursts writes (the WAL promise). |
| `test_30_aihelp_contract.py` | The `--ai-help` / `ai-help` contract: JSON shape and every required key, a stable `schema_version`, scope filtering, pretty-printing and UTF-8, the six workflows and twelve pitfalls, the help banner, the `AI_AGENT` hint and its deduplication, flag ranges, single-action subcommands as one-element arrays, empty arrays never null, `binary_version` matching main.go, and the loopback default for `web`. |
| `test_31_sprint_description_limit.py` | The 2048-character cap on a sprint description: accepted at exactly the limit and rejected one over with exit 6, on both `sprint create` and `sprint update`, with the limit reported by `--help` and by the `--ai-help` contract. |
| `test_32_layout_migration.py` | The startup sweep from the legacy `~/.roadmaps/<name>.db` layout to `~/.roadmaps/<name>/project.db`: data preserved, 0700/0600 modes applied, the legacy file gone, a second run an idempotent no-op, `roadmap list` reporting the new path, a symlink pointing outside the data directory left untouched, and a name collision never overwriting an existing project.db. |
| `test_33_graph_checkpoint.py` | `rmp graph` baseline and its persistence contract: create/query/update/delete/search round-trips, guard-rail rejection, Cypher from stdin, exit codes 2/3/4/6, a snapshot written and the WAL truncated on every write, recovery from the snapshot in a fresh process, and a checkpoint failure that stays non-fatal and reconciles on the next write. |
| `test_34_graph_realistic_usage.py` | `rmp graph` driven as an agent would over more than a hundred invocations, with a Python model mirroring every mutation so node, label and edge counts are asserted against the operations issued; plus MERGE idempotency, property type round-trips, edge and DETACH DELETE semantics, deletion durable across a reopen, parallel edges, and the guard-rail matrix. |
| `test_35_web_interface.py` | `rmp web` end-to-end on an ephemeral port: flag validation and exit codes, the startup URL object, loopback default and the exposure warning, SIGINT/SIGTERM shutdown, every page (index, sprints, tasks, sprint detail, audit, graph) with its empty states and pagination, read-only enforcement (405, no audit growth), name validation and the traversal guard, the comment surfaces, HTML escaping, assets served only from /static/, and the dark Tabler shell and responsive markup. The single sprint page's member-tasks board is covered as its own three-column Kanban board — the WAITING/DOING/CLOSED grouping and its identity with the sprint status summary line's P/A/C/T, in-column position order and its reordering through `sprint reorder`, the card's six data points in their required order (title, `#id`, `P<n>`/`S<n>` badges, subtask/comment counters), the zero-counter rule, and the empty and single-column sprints — plus the card itself as the sole pointer-and-keyboard trigger (`data-task-id` appearing exactly once), replacing the earlier six-column table. The tasks board is narrowed through its header search and its type, minimum-priority and minimum-severity filters (their conjunction, their URL round trip, and no value an error), and the search term's Unicode case folding and whitespace trimming are asserted code point by code point against the script the server ships, so the server and the browser cannot disagree on which characters a term folds or strips. The graph data endpoint is covered through its query bar: the distinct kind each failure class carries, the node limit injected into the caller's query together with the SHOW and standalone-CALL forms exempt from that injection, and the per-request query time budget that cuts off a Cartesian scan no node limit can bound. |
| `test_36_query_commands_correctness.py` | Every read-only query command checked against an independently computed ground truth on a realistic roadmap, with regression guards for aliased multi-row timestamps, dependency fields missing from views other than `task get`, an inflated sub-day velocity, and a miscounted pending-sprint total. |
| `test_37_write_persistence_fidelity.py` | Persistence fidelity of every state-mutating command: what was requested is read back, across task create/edit/transitions/reopen/priority/severity/assignment/dependencies/removal, sprint create/update/lifecycle/membership/ordering/removal and roadmap create/remove, including the move-tasks status-preservation regression and guards that reject atomically. |
| `test_38_task_list_date_filters.py` | `task list --created-since` / `--created-until`: the RFC3339 and date-only forms, date-only read as start of day UTC, past and future bounds on both sides, a combined range, and a malformed value exiting 6 while naming the flag. |
| `test_39_graph_guardrail_literals.py` | The literal-aware `rmp graph` guard rail: clause keywords appearing only inside a string literal never reclassify the query, while a genuine cross-class clause is still rejected with exit 6. |
| `test_40_graph_notifications.py` | Graph query notifications: each engine advisory surfaces as one stderr diagnostic line carrying severity, code and description, never altering stdout or the exit code, and a query that raises none writes nothing extra. |
| `test_41_graph_concurrency_input.py` | `rmp graph` write concurrency and Cypher input validation: concurrent writers never lose an acknowledged write, `--query` with no value or followed by a flag exits 2 instead of falling back to stdin, a negative numeric query value still reaches the engine, and an unknown flag exits 2. |
| `test_42_security_audit.py` | The reproducible security battery: defense tests that must stay green (SQL injection, ORDER BY whitelist, path traversal, length and range limits, 0700/0600 permissions re-applied on every open, template auto-escaping) and one probe per registered finding (#64-#87) that flips green when the fix lands and stays as its regression guard. |
| `test_43_sprint_order_field.py` | The sprint `order` field: auto-assignment as MAX+1, explicit values stored verbatim, a duplicate exiting 5 and an invalid value exiting 6, updates allowed on PENDING and OPEN and rejected on CLOSED, presence in `sprint get`/`list`, the audit entry after a change, and its documentation in `--help` and `--ai-help`. |
| `test_44_help_and_exitcode_contract.py` | Help structure and the exit codes it declares, measured against the binary: the banner as the first line of every help and absent from `--ai-help`/`--version`, codes 2/4/5/6 produced where help says so, the content each help must carry, no hard tab anywhere, and an exit-code block on every help output. |
| `test_45_audit_stats_keys.py` | `audit stats` emits exactly the five documented top-level keys, no more and no fewer, both for a populated log and for a window that matches nothing (where the empty shape must keep the same keys). |
| `test_46_graph_parallel_edge_predicates.py` | Pattern predicates over parallel edges: positive, negated, incoming, undirected, variable-length and comprehension forms all answer correctly for every relationship type between the same ordered pair, with a genuinely absent type as the negative control and the knowledge-graph gap audit as the production query shape. |
| `test_47_install_script_extraction.py` | `install.sh` archive handling in a hermetic sandbox with stubbed `uname` and `curl`: the Windows ZIP branch installs the archive's content and not the archive itself, a missing `unzip` produces a clean refusal that installs nothing, and the Unix `.tar.gz` path acquires no new dependency. |
| `test_48_graph_clause_surface.py` | The Cypher clause surface the guard rail classifies against: SHOW INDEXES/CONSTRAINTS accepted by the read subcommands and rejected by the write ones, FOREACH classified by the clauses in its body, schema DDL rejected by every subcommand in any spelling or casing, and keywords spoofed with non-ASCII look-alikes rejected before execution. |
| `test_49_install_platform_guards.py` | `install.sh` platform guards in the same sandbox: an unsupported (i386/i686) or unrecognised architecture and an unsupported OS stop at detection with the specified message and exit 1 before anything is downloaded, while every supported architecture still resolves to its documented release asset. |
| `test_50_task_and_sprint_comments.py` | Comments on tasks and sprints: the eight subcommands and their aliases, a body supplied by flag, heredoc, pipe or redirect, both type enums in each direction, body validation at the 4096-character limit and against forbidden control characters, edit semantics including a no-op refusal, ordering and filtering asserted on records, the lifecycle and cascade behaviour, the six audit operations, exit codes 2/3/4/6, the help and `--ai-help` surfaces, concurrent writers, and the 1.9.0 migration. |

`test_09_stress_load.py` is registered in `STRESS_TEST_MODULES`, so it runs
with `--stress` or `--all` and not in the default run. Every other module in
the table belongs to the standard run.

## Running the Suite

Build the binary first: the suite runs the compiled artefact, not the source.

```bash
go build -o ./bin/rmp ./cmd/rmp
```

```bash
python3 tests/run_tests.py            # standard modules (default)
python3 tests/run_tests.py --quick    # explicit spelling of the default
python3 tests/run_tests.py --stress   # stress modules only
python3 tests/run_tests.py --all      # standard modules, then stress modules
```

Each module is also a standalone program, which is the fastest way to work on
one of them:

```bash
python3 tests/test_01_basic_crud.py
python3 tests/test_50_task_and_sprint_comments.py
python3 tests/test_09_stress_load.py
```

The runner executes each module in its own interpreter process and reports a
pass/fail summary, printing the captured output of every module that failed.

`make cover-full` drives this same suite against a coverage-instrumented
binary and merges the result with the Go unit-test coverage, which is how the
project measures how much of the command surface the suite really reaches.

`pytest` is not the entry point. Two modules
(`test_47_install_script_extraction.py`, `test_49_install_platform_guards.py`)
name their test classes `...Tests` rather than `Test...`, which pytest's
default collection skips, so a pytest run silently covers less than
`run_tests.py` does.

## Registry gates

The runner refuses to run at all, before any module executes, when the suite's
own bookkeeping is inconsistent. Both gates print the offending names and exit
non-zero.

- **No dormant module** (`assert_no_dormant_modules`): every `tests/test_*.py`
  on disk must be registered in `TEST_MODULES` or `STRESS_TEST_MODULES`. A
  file that exists but is registered nowhere never runs, and its presence in
  the directory is mistaken for coverage.
- **No undocumented module** (`assert_readme_documents_every_module`): every
  registered module must have exactly one row in the table above, and the
  table must not name a module that is registered nowhere. `run_tests.py`
  holds names; only this table says what a module is for, so a row that goes
  missing takes that meaning with it.

## Test Design

### Isolation
- Every test redirects `HOME` to a fresh temporary directory, so roadmaps are
  created under a throwaway `~/.roadmaps/` and the developer's real one is
  never touched.
- Roadmap names are generated with a random suffix, so parallel or repeated
  runs cannot collide.
- Teardown removes the temporary directory, whatever the outcome of the test.
- The two `install.sh` modules build their own hermetic sandbox instead: a
  `PATH` holding only the tools under test, with `uname` and `curl` stubbed,
  so no network and no real platform detection is involved.

### CLI invocation
- Tests invoke the binary through `subprocess`, never by importing Go code.
- The binary is looked up at `bin/rmp` (and a few sibling locations) and built
  on the spot if it is missing.
- Exit code, stdout and stderr are captured and asserted separately: success
  output is JSON on stdout, errors are plain text on stderr.

### Assertions
- Tests assert the outcome, not just the exit code: after a write, the state
  is read back and compared field by field with what was requested.
- Test data is realistic — production-shaped titles, requirements and sprint
  goals — so failures read like a real roadmap and not like `foo`/`bar`.
- Every bug fixed in this project leaves a regression test behind, so a
  large part of the suite is named after the defect it pins.

## Exit Codes Exercised

`SPEC/ARCHITECTURE.md` § Exit Codes is the authority. The suite asserts these
codes against the binary:

- `0` — success
- `1` — general failure (a busy `--port`, a refused symlinked roadmap home)
- `2` — misuse: bad syntax, missing argument, unknown flag
- `3` — no roadmap selected
- `4` — resource not found
- `5` — resource already exists
- `6` — invalid data: validation and state-machine rejections
- `127` — unknown command or subcommand
- `130` — interrupted by SIGINT

`126` (`EXIT_NOT_EXECUTABLE`) is specified but no end-to-end scenario
reproduces it.

## Requirements

- Python 3.9 or newer. `run_tests.py` annotates with built-in generics
  (`list[str]`), which PEP 585 introduced in 3.9.
- The Go toolchain, to build `bin/rmp` (see `go.mod` for the version).
- A POSIX environment: the suite sends signals, opens FIFOs, and drives
  `bash`, `tar` and `gzip` for the shell-level modules.

## Adding a Test Module

1. Create `tests/test_XX_short_description.py`, continuing the numbering.
2. Build on `GroadmapTestBase` (`tests/base_test.py`) for the isolated `HOME`,
   the CLI helpers and the JSON shape assertions.
3. Give the module a docstring that states what it pins and why that belongs
   in an end-to-end test rather than in a Go unit test.
4. Write test methods named `test_*` inside classes named `Test*`, with
   `setup_method`/`teardown_method`, and end the file with a `__main__` block
   that runs them and exits non-zero on failure.
5. Register the module in `TEST_MODULES` (or `STRESS_TEST_MODULES`) in
   `tests/run_tests.py`.
6. Add its row to the table in this file. The runner will not start until you
   do, and the row must say what the module covers — restating the file name
   is worth nothing to the reader the table exists for.

Never write a test that skips. A scenario that cannot be reproduced
deterministically is left failing and tracked as a task, never faked green.
