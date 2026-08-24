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
| `test_14_audit_date_filters.py` | `audit list` and `audit stats` date filtering: `--since` alone, `--until` alone, the two combined as a range, and combined with the operation and entity filters. Also the two accepted input forms (#324): a bare `YYYY-MM-DD` selects exactly what its RFC3339 midnight twin selects, every pre-#324 timestamp form still works, and a value that is not a date is still refused with exit 6 on the published line. |
| `test_15_roadmap_stats.py` | `rmp stats`: the required roadmap/sprints/tasks fields, per-status task counters that sum to the total, sprint counters for open, closed and never-started sprints, empty roadmaps, a large roadmap, the help flags, and exit codes 3 and 4. |
| `test_16_boundary_unicode.py` | Boundaries and Unicode: priority and severity at 0/9 and one step outside on both create and the dedicated subcommands; the length caps measured in CHARACTERS, with all eight free-text fields driven from every command that writes one, at the cap and one code point over it, in ASCII, accented Latin, CJK and four-byte emoji, including a title of 255 CJK characters accepted and 256 refused; the ASCII cases that pin the numbers 255, 4096 and 2048; CJK/RTL/emoji/diacritic round-trips; malformed UTF-8 refused on every free-text flag; and SQL-injection payloads stored verbatim with the schema intact. |
| `test_17_task_type_flag.py` | `--type` on tasks: TASK as the default, every valid value accepted on create and on edit, unknown and lowercase values rejected, and the type reported by `task get` and `task list`. |
| `test_18_cli_validation_data_integrity.py` | Three integrity rules: only one sprint may be OPEN at a time (start and reopen both blocked, error naming the blocking sprint), `task remove` restricted to BACKLOG (batch fails whole), and `task reopen` clearing the lifecycle timestamps, in bulk, with its audit entries. Also the free-text emptiness and trimming constraint: a value that is empty once trimmed is refused naming the field, the literal empty string keeps its own missing-flag refusal, the stored value and the length cap are both the trimmed one, and a leading or trailing VT or FF is refused as a control character rather than silently trimmed away. |
| `test_19_completion_summary.py` | `completion_summary`: stored by `--summary`/`-s` on the COMPLETED transition, null without it, present in list output, applied to every ID of a bulk transition, rejected on non-COMPLETED targets, accepted at exactly 4096 characters and rejected one over, and cleared by both a BACKLOG transition and `task reopen`. |
| `test_20_task90_sprint_closed_guard.py` | Closed-sprint guard: `add-tasks` on a CLOSED sprint rejected with exit 6 naming the ID and the status, `sprint close` blocked while tasks are SPRINT/DOING/TESTING, `--force` closing with a stderr warning and no stdout, and COMPLETED tasks never blocking a close. |
| `test_21_task89_move_tasks_closed_guard.py` | Move guard: `sprint move-tasks` to or from a CLOSED sprint rejected with exit 6 naming that sprint, moves between OPEN and PENDING accepted (single and bulk), both-sprints-closed rejected, and a successful move writing nothing to stdout. |
| `test_22_task87_sprint_capacity.py` | Sprint capacity: `--max-tasks` on create and update, `add-tasks` rejected when the limit would be exceeded (including a bulk add rejected as a whole), `current_load` and `capacity_pct` in `sprint show`, sprints without a limit reporting null, and the validation of the flag's own value. |
| `test_23_backlog_management.py` | `backlog list` and `backlog show-next`: BACKLOG-only results, the `--priority` and `--type` filters, every `--sort` ordering, `--limit` bounds, the `ls` alias, the default count of five, and the invalid sort/type/limit/count error paths. |
| `test_24_dependency_workflow.py` | Task dependencies: `add-dep`/`remove-dep`, the `blockers`/`blocking` inverse queries, `depends_on` and `blocks` on `task get`, self-dependency and cycles rejected, COMPLETED dependencies dropped from blockers, the completion guard, and the audit entries for both operations. |
| `test_25_completion_guards.py` | The two completion guards: incomplete subtasks and incomplete dependencies each block COMPLETED naming the blocking IDs, the subtask guard is evaluated first when both would fire, and a rejected transition leaves the task in its original status. |
| `test_26_timing_realism.py` | Time-aware reporting driven by back-dated timestamps written straight into SQLite: a burndown series with one row per completion day, `days_elapsed` reflecting a sprint's real age, and roadmap velocity averaged across recently closed sprints. |
| `test_27_exit_code_extremes.py` | The two ends of the exit-code contract the rest of the suite does not reach: an unresolved command or subcommand name exits 127, writing the error and the recovery help to stderr and nothing to stdout, and a SIGINT delivered to a running process collapses to exit 130. |
| `test_28_command_aliases.py` | The documented aliases: the top-level `t`, `s`, `bl`, `aud` and `road`, and the subcommand `ls`, `rm`, `new`, `hist`, `upd`, `stat`, `prio`, `sev`, `add`, `rm-tasks`, `mv-tasks`, `mvto`, `btm` and `order`, each proven equivalent to its long form. |
| `test_29_subprocess_concurrency.py` | Concurrency as the user meets it, between OS processes rather than threads: eight parallel `rmp` processes creating tasks lose no row, and readers never fail while another process bursts writes (the WAL promise). |
| `test_30_aihelp_contract.py` | The `--ai-help` / `ai-help` contract: JSON shape and every required key, a stable `schema_version`, scope filtering, pretty-printing and UTF-8, the six workflows and twelve pitfalls, the help banner, the `AI_AGENT` hint and its deduplication, flag ranges, single-action subcommands as one-element arrays, empty arrays never null, `binary_version` matching main.go, and the loopback default for `web`. |
| `test_31_sprint_description_limit.py` | The 2048-character cap on a sprint description: accepted at exactly the limit and rejected one over with exit 6, on both `sprint create` and `sprint update`, with the limit reported by `--help` and by the `--ai-help` contract. |
| `test_32_layout_migration.py` | The startup sweep from the legacy `~/.roadmaps/<name>.db` layout to `~/.roadmaps/<name>/project.db`: data preserved, 0700/0600 modes applied, the legacy file gone, a second run an idempotent no-op, `roadmap list` reporting the new path, a symlink pointing outside the data directory left untouched, and a name collision never overwriting an existing project.db. |
| `test_33_graph_checkpoint.py` | `rmp graph` baseline and its persistence contract: create/query/update/delete/search round-trips, guard-rail rejection, Cypher from stdin, exit codes 2/3/4/6, a snapshot written and the WAL truncated on every write, recovery from the snapshot in a fresh process, and a checkpoint failure that stays non-fatal and reconciles on the next write. |
| `test_34_graph_realistic_usage.py` | `rmp graph` driven as an agent would over more than a hundred invocations, with a Python model mirroring every mutation so node, label and edge counts are asserted against the operations issued; plus MERGE idempotency, property type round-trips, edge and DETACH DELETE semantics, deletion durable across a reopen, parallel edges, and the guard-rail matrix. |
| `test_35_web_interface.py` | `rmp web` end-to-end on an ephemeral port: flag validation and exit codes, the startup URL object, loopback default and the exposure warning, SIGINT/SIGTERM shutdown, every page (index, sprints, tasks, sprint detail, audit, graph) with its empty states and pagination, read-only enforcement (405, no audit growth), name validation and the traversal guard, the comment surfaces, HTML escaping, assets served only from /static/, and the dark Tabler shell and responsive markup. Each sprint card's footer task count on the roadmap sprints landing page is checked against real, independently-fixtured membership rather than a mere substring: distinct counts across sprints so a constant or transposed value cannot pass, an empty sprint whose footer must read zero, coverage across all three tabs, a BACKLOG-status member still counted per its status-independent membership rule, and cross-checked against `rmp sprint get`'s own task_count for the identical sprint. The single sprint page's member-tasks board is covered as its own three-column Kanban board — the WAITING/DOING/CLOSED grouping and its identity with the sprint status summary line's P/A/C/T, in-column position order and its reordering through `sprint reorder`, the card's six data points in their required order (title, `#id`, `P<n>`/`S<n>` badges, subtask/comment counters), the zero-counter rule, and the empty and single-column sprints — plus the card itself as the sole pointer-and-keyboard trigger (`data-task-id` appearing exactly once), replacing the earlier six-column table. The tasks board is narrowed through its header search and its type, minimum-priority and minimum-severity filters (their conjunction, their URL round trip, and no value an error), and the search term's Unicode case folding and whitespace trimming are asserted code point by code point against the script the server ships, so the server and the browser cannot disagree on which characters a term folds or strips. The graph data endpoint is covered through its query bar: the distinct kind each failure class carries, the node limit injected into the caller's query together with the SHOW and standalone-CALL forms exempt from that injection, and the per-request query time budget that cuts off a Cartesian scan no node limit can bound. The console log is covered against the running process's own stderr: every HTTP 500 carries exactly one `slog` ERROR record naming the request and the underlying error while the response body stays opaque, the query bar's 400 carries exactly one WARN record whose `kind` matches the JSON body, successful requests and every 404 and 405 leave stderr empty, the startup network-exposure warning is a WARN record rather than an ad-hoc `warning: ` line while stdout still holds only the URL object, and the timestamps are the real UTC instant in `YYYY-MM-DDTHH:mm:ss.sssZ` even when the server runs under `TZ=Asia/Tokyo`. |
| `test_36_query_commands_correctness.py` | Every read-only query command checked against an independently computed ground truth on a realistic roadmap, with regression guards for aliased multi-row timestamps, dependency fields missing from views other than `task get`, an inflated sub-day velocity, a miscounted pending-sprint total, and `sprint list` leaving `task_count`/`tasks` unresolved (rmp task #233): a roadmap holding sprints of different sizes (0/2/4/6 members) proves `sprint list`, `sprint get` and `sprint tasks` agree on membership, the empty sprint reports `0`/`[]` rather than `null` in both list and get, a member deliberately reordered off ascending-id order shows `tasks` in ascending id order while `sprint tasks` returns the scrambled planned position order, a member parked in `BACKLOG` status stays counted and listed, and `--status` narrows which sprints are returned without altering any returned sprint's membership. |
| `test_37_write_persistence_fidelity.py` | Persistence fidelity of every state-mutating command: what was requested is read back, across task create/edit/transitions/reopen/priority/severity/assignment/dependencies/removal, sprint create/update/lifecycle/membership/ordering/removal and roadmap create/remove, including the move-tasks status-preservation regression and guards that reject atomically. The `sprint update` flag-presence sentinel (rmp task #270) is its own regression block: `-t ""`/`-d ""` and every flag spelling (`-t`/`--title`/`--title=`) rejected with exit 6 and nothing mutated, the empty-title-beside-a-valid-description case writing zero audit entries, the flagless invocation still exiting 2, `sprint update` and `task edit` agreeing on the identical input, the one-entry-per-field / shared-`performed_at` audit contract on success, and the empty value on a nonexistent sprint id exiting 6 rather than 4 to prove validation runs before the sprint is looked up. |
| `test_38_task_list_date_filters.py` | `task list --created-since` / `--created-until`: the RFC3339 and date-only forms, date-only read as start of day UTC, past and future bounds on both sides, a combined range, and a malformed value exiting 6 while naming the flag. |
| `test_39_graph_guardrail_literals.py` | The literal-aware `rmp graph` guard rail: clause keywords appearing only inside a string literal never reclassify the query, while a genuine cross-class clause is still rejected with exit 6. |
| `test_40_graph_notifications.py` | Graph query notifications: each engine advisory surfaces as one stderr diagnostic line carrying severity, code and description, never altering stdout or the exit code, and a query that raises none writes nothing extra. |
| `test_41_graph_concurrency_input.py` | `rmp graph` write concurrency and Cypher input validation: concurrent writers never lose an acknowledged write, `--query` with no value or followed by a flag exits 2 instead of falling back to stdin, a negative numeric query value still reaches the engine, and an unknown flag exits 2. |
| `test_42_security_audit.py` | The reproducible security battery: defense tests that must stay green (SQL injection, ORDER BY whitelist, path traversal, length and range limits, 0700/0600 permissions re-applied on every open, template auto-escaping) and one probe per registered finding (#64-#87) that flips green when the fix lands and stays as its regression guard. |
| `test_43_sprint_order_field.py` | The sprint `order` field: auto-assignment as MAX+1, explicit values stored verbatim, a duplicate exiting 5 and an invalid value exiting 6, updates allowed on PENDING and OPEN and rejected on CLOSED, presence in `sprint get`/`list`, the audit entry after a change, and its documentation in `--help` and `--ai-help`. Also `sprint list`'s Result Ordering guarantee (rmp task #281): the array is ordered by `order` ascending even when creation order disagrees with it, `--status` narrows it as a subsequence without reordering it, two consecutive reads over unchanged data are identical, and both `sprint list --help` and its `--ai-help` contract entry state the guarantee. |
| `test_44_help_and_exitcode_contract.py` | Help structure and the exit codes it declares, measured against the binary: the banner as the first line of every help and absent from `--ai-help`/`--version`, codes 2/4/5/6 produced where help says so, the content each help must carry, no hard tab anywhere, and an exit-code block on every help output. |
| `test_45_audit_stats_keys.py` | `audit stats` emits exactly the five documented top-level keys, no more and no fewer, both for a populated log and for a window that matches nothing (where the empty shape must keep the same keys). |
| `test_46_graph_parallel_edge_predicates.py` | Pattern predicates over parallel edges: positive, negated, incoming, undirected, variable-length and comprehension forms all answer correctly for every relationship type between the same ordered pair, with a genuinely absent type as the negative control and the knowledge-graph gap audit as the production query shape. |
| `test_47_install_script_extraction.py` | `install.sh` archive handling in a hermetic sandbox with stubbed `uname` and `curl`: the Windows ZIP branch installs the archive's content and not the archive itself, a missing `unzip` produces a clean refusal that installs nothing, and the Unix `.tar.gz` path acquires no new dependency. Also the integrity gate that runs immediately before that extraction (rmp task #185): the archive is verified against the `<archive>.sha256` the release publishes, on both the `.tar.gz` and the `.zip` branch, and a substituted archive, a truncated download, an absent checksum, a checksum naming a different archive, a malformed checksum and an empty one are each refused with nothing installed, nothing left in the staging path, no temporary directory left behind and -- proved by wrapping `tar` and `unzip` in recording shims -- no extraction attempted at all; a host with none of `sha256sum`, `shasum` or `openssl` is refused before a single asset is requested. And the directory both of those steps happen in (rmp task #309): the staging directory was `/tmp/rmp_install_$$` created with `mkdir -p`, a name drawn from at most 32768 PIDs and a call that succeeds on a directory that already exists, so another local user could pre-create it and swap the archive between the checksum read and the extraction read. With `TMPDIR` pointed at a directory the test owns and `mktemp` replaced by a recording shim, the module pins that the directory is created by `mktemp -d` from an unpredictable template and differs between runs, that `ls -ld` reports `drwx------` at the instant the verified archive sits in it, that a directory the script did not create is refused and left untouched -- one that already exists and is world-writable, one that belongs to another user, and one that is a symlink to somewhere else -- that a staging directory which cannot be created and a host without `mktemp` both install nothing, that a world-writable non-sticky `TMPDIR` is refused, that files already sitting at the old fixed paths `/tmp/rmp` and `/tmp/rmp.exe` come out of a successful install byte-identical, and that nine abort paths plus a SIGTERM delivered mid-extraction each leave the temporary directory empty. |
| `test_48_graph_clause_surface.py` | The Cypher clause surface the guard rail classifies against: SHOW INDEXES/CONSTRAINTS accepted by the read subcommands and rejected by the write ones, FOREACH classified by the clauses in its body, schema DDL rejected by every subcommand in any spelling or casing, and keywords spoofed with non-ASCII look-alikes rejected before execution. Also the relationship-write-direction refusal (rmp task #193): `graph update` SET/REMOVE on a relationship bound by an incoming or undirected pattern rejected with exit 6 from every anchor and writing nothing, `graph delete` through the same undirected pattern still succeeding, the outgoing form still writing and reading back from either endpoint, a node write and a relationship read reached through a reverse traversal still accepted, and the refusal message naming the variable, the direction and the outgoing rewrite. |
| `test_49_install_platform_guards.py` | `install.sh` platform guards in the same sandbox: an unsupported (i386/i686) or unrecognised architecture and an unsupported OS stop at detection with the specified message and exit 1 before anything is downloaded, while every supported architecture still resolves to its documented release asset. The module shipped with no runner and no `__main__` block, so it exited 0 having executed nothing and the suite counted it as passed; it now runs the five tests it always contained. |
| `test_50_task_and_sprint_comments.py` | Comments on tasks and sprints: the eight subcommands and their aliases, a body supplied by flag, heredoc, pipe or redirect, both type enums in each direction, body validation at the 4096-character limit and against forbidden control characters, edit semantics including a no-op refusal, ordering and filtering asserted on records, the lifecycle and cascade behaviour, the six audit operations, exit codes 2/3/4/6, the help and `--ai-help` surfaces, concurrent writers, and the 1.9.0 migration. |
| `test_51_specialists_field_removal.py` | Removal of the task `specialists` field (rmp task #246): `task assign`/`task unassign` and the `-sp`/`--specialists` flags and filter rejected with their exact documented exit codes (2 or 6 for `audit list -o TASK_ASSIGN`/`TASK_UNASSIGN`), no state or audit change on a rejected attempt, the field absent from `task get`/`list`/`next` (exactly 20 keys) and from every help surface and `--ai-help`, and the 1.9.0 -> 1.10.0 schema migration built from the verbatim historical DDL: the column dropped, other data and CHECK constraints intact, AUTOINCREMENT continuing, pre-existing `TASK_ASSIGN`/`TASK_UNASSIGN` audit rows retained and visible in an unfiltered listing, and a second open idempotent. |
| `test_52_commit_tracking.py` | Commit tracking on `task stat` (rmp task #254): `--commit-open`/`-co` mandatory on every transition into DOING and `--commit-close`/`-cc` on the transition into COMPLETED, each refused on every other target state, a malformed hash refused at exit 6 with the 7..64 bounds proved inclusive, a flag written with no value after it refused at exit 2, the exact stderr line asserted in every rejection and the task re-read to prove nothing moved, the stored hash lowercased, `TESTING -> DOING` replacing `commit_open`, one hash applied across a batch, the asymmetric clearing on all four routes back to BACKLOG (`task stat BACKLOG`, `task reopen`, `sprint remove-tasks`, `sprint remove`) which clear `commit_close` and preserve `commit_open`, a partly-failing batch leaving every task unchanged, and neither flag accepted by `task create` or `task edit`. Also covers the three hash shapes git actually emits (abbreviated, full 40-character SHA-1, full 64-character SHA-256) round-tripping unchanged, and the 1.10.0 to 1.11.0 schema migration: a database built to the verbatim 1.10.0 `tasks` shape gains both columns null on the next open, keeps every row, reaches the same schema version a fresh roadmap is stamped with, and ends up enforcing the same commit rule at BOTH the command layer and the column CHECK — the storage half is probed by writing straight to SQLite, because a command-level assertion alone passes even on a migration that omitted the constraint. |
| `test_53_e2e_harness_binary_staleness.py` | The harness itself (rmp task #271): `resolve_cli()` builds a binary when none exists, reuses one that is newer than every source file compiled into it, rebuilds one that is older than a touched `.go` file OR a touched `//go:embed`'d template/static asset (`internal/web/embed.go`) before handing it back — proved separately for each half, with the embedded-asset case leaving every `.go` file provably untouched — raises instead of returning a stale binary when the rebuild fails to compile, never selects a `bin/rmp`/`rmp` planted in an unrelated current working directory, and prints the resolved path and build identity exactly once per process even across several `GroadmapTestBase()` instances. |
| `test_54_audit_enrichment_e2e.py` | The enriched audit contract end to end (rmp task #268): each of the five `TASK_STATUS_*` transitions and the operation it writes, `commit_hash` landing on `TASK_STATUS_DOING`/`TASK_STATUS_COMPLETED` only and staying null everywhere else including `TASK_REOPEN`, the mirrored relational pairs (`SPRINT_ADD_TASK`/`TASK_STATUS_SPRINT`, `SPRINT_REMOVE_TASK`/`TASK_STATUS_BACKLOG`, the `SPRINT_MOVE_TASK_OUT`/`SPRINT_MOVE_TASK_IN` move pair, `TASK_ADD_DEP`/`TASK_REMOVE_DEP`) naming both entities with transposed ids and one shared `performed_at`, one audit row per changed field on `task edit`/`sprint update`, `audit list -o` accepting the whole catalogue fetched live from `--ai-help` (writable and the four LEGACY values) and rejecting anything outside it, `audit stats.by_operation` counting the enriched operations exactly, the full seven-key JSON shape, and the 1.11.0 to 1.12.0 migration against a fixture built directly in SQLite carrying all four row classes (determinable, matching no timestamp, belonging to a deleted task, and `TASK_UPDATE`) — asserted both directly against the migrated database and through the CLI's own JSON, proved idempotent across a second invocation, and closed with four non-vacuity proofs that inject each failure class directly into the fixture database and show the corresponding assertion go red before reverting it. |
| `test_55_error_string_parity.py` | SPEC/COMMANDS.md's published error strings driven against the compiled binary character for character (rmp task #277): a table/fenced-block extractor pulls every quoted `Error:` string the file publishes, substitutes the closed placeholder set (`X`/`N`/`M`/`Y` as whole words, `<field>`, `<flag>`, and the computable `<absolute path of ~/.roadmaps>`) with values this module chooses before each invocation, and asserts the captured stderr line equals the result exactly, across every command family (roadmap, task, sprint, comment, audit, backlog, stats, graph, web, dispatch failures, ai-help). Strings whose tail is a genuine external diagnostic are handled by one of two named, reasoned tables rather than silently skipped: `EXEMPT_KEYS` for a string with no deterministic hermetic trigger at all (an OS `net.OpError` bind failure, a Cypher-engine message, an unreachable internal store failure), and `TAIL_EXEMPT_KEYS` for a string that IS driven but compared only up to a named placeholder, so the `Error: ` prefix and the sentinel are still asserted character for character while the external tail is merely required to be non-empty and to carry the operation context. The six database-failure rows of the comment subcommands moved from the first table to the second in rmp task #319: they had been exempt outright, which is why the binary printing them with no sentinel at all went unnoticed. A final coverage report asserts every published string is either reached or exempted, that every tail narrowing was actually driven, and holds a floor on how many were driven against the binary.
| `test_56_graph_read_direction.py` | The relationship-READ direction contract (rmp task #288, SPEC/GRAPH.md § Relationship Read Direction): on a node pair carrying edges BOTH ways, GoGraph hydrates the reverse leg of an incoming or undirected traversal from the FORWARD pair, so the read reports another relationship's type and the reversed orientation, drops the row when a `WHERE type(e)` predicate reads it, and PERSISTS the wrong value when a node write derives from it. Every fixture is a two-way pair whose edges carry DIFFERENT types, because a one-way pair resolves correctly either way and a same-type pair hides the symptom. Refused and proven inert: the undirected and incoming reads of `type(e)`, the type-filtered incoming read, `startNode(e)`/`endNode(e)`, the `WHERE`-only use, `RETURN *`, the same shape under `graph search`, and the `SET <node>.p = type(e)` right-hand side, whose node property is read back absent rather than merely checked for exit 6. Admitted and proven correct by reading the result back: the outgoing read, the target-anchored outgoing rewrite that reaches the reverse edge, the `UNION ALL` of the two outgoing legs, an anonymous relationship, a relationship bound but never read, a named path, a variable-length hop, a node write, and `graph delete` removing the right edge and only that one — plus the `SET` target staying with the write-direction rule, so the two rules are shown to remain separate. |
| `test_57_positional_arity.py` | The positional-argument arity contract (rmp task #293, SPEC/COMMANDS.md § Positional Arguments): `FlagParser.Parse` collected unrecognised positional arguments into `ParseResult.Args` and no caller inspected the slice, so `rmp roadmap create alpha-service beta-service` exited 0, reported `alpha-service`, and silently discarded `beta-service` — eleven commands behaved that way. Every command now declares a maximum and one shared enforcement point refuses what exceeds it with exit 2 and `Error: invalid input: unexpected argument "X"`, naming the first offending token. The module drives all eleven recorded commands plus one command per declared arity (0, 1, 2, 3) one token over the maximum, and — the half that tells the rule apart from a blanket refusal of everything past the first argument — drives commands of arity 2 and 3 at their FULL arity and reads the result back to confirm the work was done. Outcomes are asserted rather than exit codes alone: neither roadmap exists after the named defect's invocation, no task row is deleted, the audit log does not grow, and a refusal against a roadmap that does not exist still exits 2 rather than 4, which is only possible if the refusal precedes opening the store. Also pinned: the first offending token is the one named, position on the command line is irrelevant, a comma-separated id list is one argument, a `-`-prefixed token stays an unknown flag, a dispatch failure stays 127, no help follows the refusal, the six global forms (`help`, `--help`, `-h`, `version`, `--version`, `-v`) refuse a stray token yet still work alone, and the four already-compliant families (`graph`, the eight comment subcommands, `web`, `ai-help`) keep the exact line each publishes. |
| `test_58_ai_contract_error_parity.py` | The `rmp --ai-help` contract's published example error strings driven against the compiled binary (rmp task #317). The registries' `Example.Stderr` fields reach AI agents verbatim through the contract, and eight of the sixty-eight named a line the binary does not print: five had dropped the sentinel between `Error: ` and the detail, two carried a duplicated `: invalid task status`/`: invalid sprint status` tail the enum-message deduplication had already removed, and one published an unquoted enum value the binary quotes. Because the message body is assembled by `fmt.Errorf` calls scattered across `internal/commands`, only the sentinel and the exit code are derivable statically, so this module reads the corpus from `rmp --ai-help` itself, derives each invocation from the `cmd` the contract publishes beside the string (hand-writing none), and replays all sixty-eight in one throwaway roadmap built to satisfy the preconditions the examples presuppose — tasks 1/3/7/42 in a sprint that is never started, task 7 in TESTING, twelve task comments and four sprint comments, a roadmap named `existing` present and one named `missing` absent. Each captured stderr line must equal the published string character for character, each exit code must equal the published one, and stdout must be empty. Separate from `test_55_error_string_parity.py`, which gates the OTHER surface (SPEC/COMMANDS.md), because that surface carries no invocation and must hand-write one per string; the two are still tied to one convention by a cross-surface check that reads the sentinel vocabulary out of SPEC/COMMANDS.md § Published Error Strings Are Exact and holds the registry surface to it, with the four genuinely sentinel-free strings named and reasoned. Strings whose tail is produced outside rmp (the Cypher engine diagnostic, the OS `net.OpError` after a failed bind) are held to their fixed prefix under a named, reasoned exemption; no published example takes that path today, so both loci are driven for real — a malformed Cypher query and a port this module occupies itself — through the same comparator, proving the machinery live. Non-vacuity is built in: the comparator must reject a published string degraded exactly as the eight defects were, a floor guards against an extraction that finds nothing, a coverage report requires every published example to have been driven, and a readback across the whole sweep proves the shared fixture is untouched. |
| `test_59_graph_property_value_content.py` | The knowledge-graph Cypher content rules (rmp task #298, SPEC/GRAPH.md): a Cypher property value written by `rmp graph create` / `graph update` was subject to neither of the two free-text content rules, so a control character was stored verbatim and invalid UTF-8 was SILENTLY REPLACED with U+FFFD while the command returned `{"ok": true}` and exit 0 — the store did not hold what was written and nothing reported the difference. The module pins the two rules and, crucially, their two different REACHES. **The encoding rule binds every subcommand that accepts a Cypher query**, because the engine's substitution happens before its grammar runs and so changes the statement rather than only a value it stores: `graph delete` gated by a malformed literal removed nothing and reported success, and that case is asserted BY READ-BACK — the target node must still be present afterwards — because the exit code alone cannot see it, the old behaviour having also exited 0; `graph query` and `graph search` are pinned the same way, and a partition test drives all five subcommands and asserts the set is complete. **The control-character rule binds only the two subcommands that write property values**, and that asymmetry is asserted in both directions: a value carrying a control character is planted through the one write path the rule cannot see (a computed right-hand side) and then reached by a read and removed by a delete, because the store legitimately holds such values and refusing those reads would leave the data unreadable rather than merely unwritable. The module also asserts the two facts that decide where each half can be enforced at all, rather than assuming them: a control character reaches the store through a query of PURE ASCII, since Cypher decodes its own `\uXXXX`, `\b` and `\f` escapes inside a string literal, so each such case checks the query text is clean before running it and a check on the query string would demonstrably pass it; and invalid UTF-8 never reaches the parsed value, since the engine's lexer replaces every byte that decodes to no character with U+FFFD before the grammar runs, so a check on the parsed value would demonstrably pass every shape. The malformed-UTF-8 corpus is not written here — it is extracted from `internal/testenv/malformedutf8.go`, the corpus rmp task 180 built from the shapes SPEC/MODELS.md enumerates, decoded to real bytes and carried to argv by surrogateescape, so a shape added there is exercised here with no edit. Refused and proven inert by reading the store back: every corpus shape under `graph create` and under `graph update`, a raw ESC, and the escape-encoded ESC / RIGHT-TO-LEFT OVERRIDE / BEL / BACKSPACE, each refusal naming the offending value by its property and the offending code point, with exit 6 and empty stdout. Admitted and proven correct by reading the value back byte for byte: accented Portuguese, CJK, emoji, and the permitted TAB/LF/CR, plus a value computed at execution time — the stated limit of the check, recorded as a test rather than only as a comment. Precedence is pinned three ways (the clause-class objection and the relationship-direction objection each outrank the content objection, and the encoding rule is applied before the control-character rule on a value that breaks both), the unattributed encoding refusal is required to explain why it names no property — in the terms true for its subcommand — the refusal message itself is scanned for the very characters it refuses, and non-vacuity is closed by round-tripping a GENUINE U+FFFD to prove the read-back comparator can tell a replaced byte apart from an unmodified one. |

`test_09_stress_load.py` is registered in `STRESS_TEST_MODULES`, so it runs
with `--stress` or `--all` and not in the default run. Every other module in
the table belongs to the standard run.

## Running the Suite

Build the binary first: the suite runs the compiled artefact, not the source.
`tests/base_test.py` also rebuilds automatically the first time a module in a
given run finds `bin/rmp` older than the newest source file compiled into it
— a `.go` file or a `//go:embed`'d template/static asset alike — so an
explicit build is a courtesy that saves the first module its own rebuild, not
a strict precondition.

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

Those same two modules discover their test classes by inspecting the module
rather than listing them, and print how many they found before running any of
them. A runner that names its classes one at a time silently ignores the next
class added to the file, which is the same class of defect as a dormant module:
the file grows coverage that never runs. Printing the count makes what ran
checkable against what the file holds.

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
- **No class shortfall** (`assert_no_class_shortfall`): every `Test*`/`*Tests`
  suite class a registered module defines must be referenced by that module's
  own runner, or the runner must discover its classes dynamically by
  introspecting the module's own namespace, as described above. A runner that
  names its classes one at a time and is not updated when a class is appended
  below it exits 0 while quietly never running that class (rmp task #303,
  measured on `test_48_graph_clause_surface.py`: 32 passed before the fix, 39
  after, with 7 tests that had never run); this gate reports the module and
  the unwired class name instead of letting the smaller count pass silently.

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
- `tests/base_test.py` ESTABLISHES the binary rather than merely discovering
  one: `resolve_cli()` looks only at `bin/rmp` and `rmp` under the repository
  root — never the current working directory, so a `bin/rmp` left over from
  an unrelated project can never be mistaken for the artefact under test —
  and compares whichever candidate it finds against the newest source file
  compiled into it. That comparison is not `.go`-only: `internal/web/embed.go`
  compiles `internal/web/templates/*.html` and `internal/web/static` straight
  into the binary via `//go:embed`, so `resolve_cli()` also walks every
  directory a `//go:embed` directive pulls in (derived from the directive
  itself, not hardcoded) and treats a touched template or static asset the
  same as a touched `.go` file. A missing or stale candidate is rebuilt with
  the same `go build -o ./bin/rmp ./cmd/rmp` the suite has always used as its
  last-resort build step; a rebuild that fails to compile raises instead of
  falling back to the stale binary, which fails the run loudly rather than
  certifying code that no longer matches the source. Resolution happens once
  per interpreter process and prints the resolved path and the binary's own
  `--version`/mtime identity exactly once, so a run's output says which
  artefact it exercised. `tests/test_53_e2e_harness_binary_staleness.py` is
  the regression guard for this contract, for both the `.go` and the
  embedded-asset half.
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
- `127` — dispatch failure: an unresolved command or subcommand name
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
