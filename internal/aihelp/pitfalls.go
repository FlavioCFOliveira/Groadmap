// Package aihelp — pitfalls catalogue.
//
// This file holds the curated list of mistakes that AI agents driving
// the rmp CLI are known (or expected) to make. The list is mandated
// by SPEC/DATA_FORMATS.md § AI Agent Contract (mandatory `pitfalls`
// entries) and is hand-written, not derived from the registry: only a
// human reviewer can decide which failure modes are worth surfacing
// out of the much larger space of possible errors.
//
// Each entry exposes both a `wrong_example` and a `correct_example`.
// The wrong example is intentionally incorrect — it is the
// counter-example the agent should NOT execute. The correct example
// is a real, runnable rmp invocation (with placeholder values) whose
// first token resolves to a real registered command; this invariant
// is enforced by unit test in generator_test.go.
//
// The `reference` field points back to the artefact in the same
// contract (or in SPEC/COMMANDS.md / SPEC/STATE_MACHINE.md) that
// governs the rule, so an agent that hits a pitfall has a single
// place to look for the authoritative answer.
package aihelp

// staticPitfalls returns the canonical pitfalls: the twelve mandated by
// SPEC/DATA_FORMATS.md § AI Agent Contract plus the curated additions for
// the surfaces the mandatory table predates (the graph statement surface, the
// comment subcommands). Fresh slice on every call, matching the
// defensive-copy semantics of the other static helpers.
func staticPitfalls() []Pitfall {
	return []Pitfall{
		{
			ID: "roadmap_identified_by_name",
			Description: "Treating the roadmap as having a numeric ID. Roadmaps are identified by name only, " +
				"and every non-`roadmap` command needs `-r <name>` (or `--roadmap <name>`) to select one.",
			WrongExample:   "rmp task list -r 42",
			CorrectExample: "rmp task list -r myproject",
			Reference:      "conventions.roadmap_flag; SPEC/COMMANDS.md § Roadmap Selection.",
		},
		{
			ID: "manual_sprint_status",
			Description: "Attempting to set a task's status to SPRINT manually via `task stat`. The SPRINT " +
				"status is owned by sprint operations and is set atomically when a task is added to a sprint.",
			WrongExample:   "rmp task stat -r myproject 42 SPRINT",
			CorrectExample: "rmp sprint add-tasks -r myproject 7 42",
			Reference:      "sprint add-tasks; enums.TaskStatus SPRINT value; SPEC/STATE_MACHINE.md rejection rule.",
		},
		{
			ID: "delete_non_backlog_task",
			Description: "Calling `task remove` on a task that is not in BACKLOG. Removal tests the status " +
				"alone, so a non-BACKLOG task must first be returned to BACKLOG by one of three routes: " +
				"`task stat <ids> BACKLOG` from SPRINT or COMPLETED; `task reopen` from SPRINT, DOING, " +
				"TESTING or COMPLETED; `sprint remove-tasks` from SPRINT, DOING, TESTING or COMPLETED. " +
				"A task returned by `task stat <ids> BACKLOG` stays a member of its sprint, and is " +
				"removable all the same.",
			WrongExample:   "rmp task remove -r myproject 42",
			CorrectExample: "rmp sprint remove-tasks -r myproject 7 42 && rmp task remove -r myproject 42",
			Reference:      "task remove; SPEC/STATE_MACHINE.md § Task Deletion Precondition; SPEC/STATE_MACHINE.md § Sprint Membership and the BACKLOG Status.",
		},
		{
			ID: "add_tasks_to_closed_sprint",
			Description: "Calling `sprint add-tasks` against a sprint in CLOSED state. Closed sprints are " +
				"immutable; use a PENDING or OPEN sprint, or create a new one.",
			WrongExample:   "rmp sprint add-tasks -r myproject 3 42,43",
			CorrectExample: "rmp sprint create -r myproject -t \"Auth hardening\" -d \"Deliver session-based authentication for every write command.\" && rmp sprint add-tasks -r myproject 8 42,43",
			Reference:      "sprint add-tasks; enums.SprintStatus CLOSED value.",
		},
		{
			ID: "next_without_open_sprint",
			Description: "Calling `rmp task next` while no sprint is in OPEN state. `task next` only returns " +
				"tasks attached to the currently OPEN sprint; without one it has nothing to return.",
			WrongExample:   "rmp task next -r myproject",
			CorrectExample: "rmp sprint start -r myproject 7 && rmp task next -r myproject",
			Reference:      "task next; sprint start; enums.SprintStatus OPEN value.",
		},
		{
			ID: "complete_with_open_dependencies",
			Description: "Transitioning a task to COMPLETED while it has incomplete declared dependencies " +
				"(blockers). The transition is rejected; complete the blockers first or remove the dependency.",
			WrongExample:   "rmp task stat -r myproject 42 COMPLETED",
			CorrectExample: "rmp task blockers -r myproject 42 && rmp task stat -r myproject <blocker-id> COMPLETED --commit-close <hash> && rmp task stat -r myproject 42 COMPLETED --commit-close <hash>",
			Reference:      "task stat; task blockers; task remove-dep; SPEC/STATE_MACHINE.md § Dependency rules.",
		},
		{
			ID: "summary_on_non_completed_transition",
			Description: "Passing `--summary` on any transition other than `→ COMPLETED`. The flag is " +
				"accepted only when the target status is COMPLETED; using it on any other transition is rejected.",
			WrongExample:   "rmp task stat -r myproject 42 DOING --commit-open 5f93b51 --summary \"started work\"",
			CorrectExample: "rmp task stat -r myproject 42 COMPLETED --commit-close 2578d18 --summary \"work done and verified\"",
			Reference:      "task stat --summary flag.",
		},
		{
			ID: "missing_commit_hash_on_transition",
			Description: "Running `task stat <ids> DOING` without `--commit-open`, or " +
				"`task stat <ids> COMPLETED` without `--commit-close`. Each flag is mandatory on its " +
				"own transition and rejected on every other one. The agent must supply the hash " +
				"itself; rmp never reads a git repository to obtain it.",
			WrongExample:   "rmp task stat -r myproject 42 DOING",
			CorrectExample: "rmp task stat -r myproject 42 DOING --commit-open $(git rev-parse HEAD)",
			Reference:      "task stat --commit-open / --commit-close flags; SPEC/STATE_MACHINE.md § Commit Tracking Fields.",
		},
		{
			ID: "partial_reorder",
			Description: "Passing only a subset of a sprint's task IDs to `sprint reorder`. The command " +
				"requires the complete ordered set of the sprint's tasks; partial reorders are rejected.",
			WrongExample:   "rmp sprint reorder -r myproject 7 42,43",
			CorrectExample: "rmp sprint tasks -r myproject 7 && rmp sprint reorder -r myproject 7 42,43,44,45,46",
			Reference:      "sprint reorder; sprint tasks.",
		},
		{
			ID: "non_iso_date_input",
			Description: "Supplying dates in a non-ISO 8601 format to filter flags such as `--since`, " +
				"`--until`, `--created-since`, or `--created-until`. The contract's " +
				"`conventions.datetime_format` is the authoritative input format; date-range filters also " +
				"accept the shorter `YYYY-MM-DD` form.",
			WrongExample:   "rmp audit list -r myproject --since 24/05/2026",
			CorrectExample: "rmp audit list -r myproject --since 2026-05-24",
			Reference:      "conventions.datetime_format; audit list --since/--until flags.",
		},
		{
			ID: "assume_partial_batch_success",
			Description: "Assuming a batch operation may partially succeed. All batch operations in rmp are " +
				"fail-fast: either every ID is valid and the operation runs end-to-end, or no change is made.",
			WrongExample:   "rmp task stat -r myproject 42,99999,43 COMPLETED --commit-close 2578d18",
			CorrectExample: "rmp task get -r myproject 42,43 && rmp task stat -r myproject 42,43 COMPLETED --commit-close 2578d18",
			Reference:      "task stat; task get; SPEC/COMMANDS.md § Batch Operation Behavior (Fail-Fast).",
		},
		{
			ID: "invalid_roadmap_name",
			Description: "Creating a roadmap with characters outside `^[a-z0-9_-]+$` or longer than 50 " +
				"characters. The CLI rejects the name; validate it client-side before issuing `roadmap create`.",
			WrongExample:   "rmp roadmap create My Project!",
			CorrectExample: "rmp roadmap create my-project",
			Reference:      "roadmap create; SPEC/COMMANDS.md § Roadmap Name Validation.",
		},
		{
			ID: "parse_modification_stdout",
			Description: "Parsing stdout after a modification command (status change, priority change, " +
				"reorder, delete, etc.). Such commands deliberately return empty stdout on success; rely " +
				"on the exit code instead.",
			WrongExample:   "result=$(rmp task stat -r myproject 42 DOING --commit-open 5f93b51) && echo \"$result\"",
			CorrectExample: "rmp task stat -r myproject 42 DOING --commit-open 5f93b51 && echo \"transition succeeded\"",
			Reference:      "task stat; task prio; sprint reorder; conventions.stdout_on_success.",
		},
		{
			ID: "graph_statement_is_not_checked",
			Description: "Expecting `rmp graph` to refuse a statement for what it does. It has ONE subcommand, " +
				"execute, and no operation-class check: the same invocation runs a MATCH, a CREATE, a SET, a " +
				"DETACH DELETE, index and constraint DDL, and the SHOW INDEXES / SHOW CONSTRAINTS listings. " +
				"There is no subcommand whose contract is \"this cannot delete\", so the protection against " +
				"deleting through a command believed to be read-only is care with the text supplied, not the " +
				"subcommand chosen. The names create, query, update, delete and search were subcommands of " +
				"rmp graph and are not any more: each is now an unresolved subcommand and exits 127. Exit code " +
				"6 no longer means a class mismatch; on graph execute its only cause is a statement longer than " +
				"1048576 bytes.",
			WrongExample:   "rmp graph execute -r myproject --query \"MATCH (n:Spec) DETACH DELETE n\"  # nothing refuses this; it deletes",
			CorrectExample: "rmp graph execute -r myproject --query \"MATCH (n:Spec) RETURN n.key\"",
			Reference:      "graph execute; SPEC/COMMANDS.md § Graph Management; SPEC/GRAPH.md § What Groadmap Does Not Check.",
		},
		{
			ID: "graph_schema_two_statements_in_one_query",
			Description: "Putting a second clause after a schema statement in one `rmp graph execute` " +
				"invocation, as in \"CREATE INDEX ix FOR (n:Spec) ON (n.key) MATCH (m:Spec) SET m.reviewed = true\". " +
				"The engine's schema parser stops when its grammar is satisfied and discards whatever follows " +
				"without an error and without a notification, so the trailing clause NEVER RUNS while the " +
				"command prints {\"ok\": true} and exits 0. Groadmap does not inspect the statement and does " +
				"not refuse it, so nothing warns you: the index exists, the SET did not happen, and the exit " +
				"code says success. Issue the two statements as two invocations. The same applies to altering " +
				"an index: there is no ALTER INDEX, so a change of kind or definition is a DROP INDEX and then " +
				"a CREATE INDEX, two invocations that are not atomic — the index is absent between them.",
			WrongExample:   "rmp graph execute -r myproject --query \"CREATE INDEX spec_key FOR (n:Spec) ON (n.key) MATCH (m:Spec) SET m.reviewed = true\"",
			CorrectExample: "rmp graph execute -r myproject --query \"CREATE INDEX spec_key FOR (n:Spec) ON (n.key)\" && rmp graph execute -r myproject --query \"MATCH (m:Spec) SET m.reviewed = true\"",
			Reference:      "graph execute; SPEC/GRAPH.md § What Groadmap Does Not Check, item 6; § Altering and Recreating an Index.",
		},
		{
			ID: "graph_schema_failure_exit_code",
			Description: "Reading a failed schema statement as a validation error. A duplicate CREATE INDEX " +
				"or CREATE CONSTRAINT, a DROP INDEX or DROP CONSTRAINT naming an object that does not exist, a " +
				"definition the engine does not support, and a CREATE CONSTRAINT the data already in the graph " +
				"does not satisfy all exit 1, not 6: Groadmap cannot know whether an object exists without " +
				"opening the store, so the check belongs to the engine. A SHOW whose two keywords are not " +
				"separated by exactly one space also exits 1, as a syntax error from the general Cypher " +
				"grammar, and its message names SHOW rather than the spacing. The only cause of exit code 6 on " +
				"graph execute is a statement longer than 1048576 bytes. Write IF NOT EXISTS or IF EXISTS to " +
				"make a create or a drop a silent no-op instead of a failure.",
			WrongExample:   "rmp graph execute -r myproject --query \"DROP INDEX spec_key\"  # exits 1 when spec_key is not registered",
			CorrectExample: "rmp graph execute -r myproject --query \"DROP INDEX spec_key IF EXISTS\"",
			Reference:      "graph execute; SPEC/GRAPH.md § Schema Failure Classes.",
		},
		{
			ID: "task_only_comment_type_on_sprint",
			Description: "Using a task-only comment type on a sprint comment. HYPOTHESIS, TEST and NOTE are " +
				"accepted on a task comment and rejected on a sprint comment with exit code 6, because a sprint " +
				"comment records the progression of the work and not the execution diary of an individual task. " +
				"The sprint set is FINDING, DECISION, PROGRESS, UPDATE; the two sets are published as two enums, " +
				"SprintCommentType and TaskCommentType, so the valid values can be read off the contract before " +
				"the call is made.",
			WrongExample:   "rmp sprint comment-add -r myproject 7 --type HYPOTHESIS --body \"the velocity drop is caused by the migration work\"",
			CorrectExample: "rmp sprint comment-add -r myproject 7 --type FINDING --body \"the velocity drop is caused by the migration work, measured over sprints 5 to 7\"",
			Reference:      "sprint comment-add; enums.SprintCommentType vs enums.TaskCommentType; SPEC/MODELS.md § Comment Type.",
		},
		{
			ID: "parse_comment_mutation_stdout",
			Description: "Assuming every comment subcommand prints JSON. Only two do: `comment-add` returns " +
				"{\"id\": <int>} with the new comment's own id, and `comment-list` returns an array of comment " +
				"objects. `comment-edit` and `comment-remove` are mutations and deliberately print nothing on " +
				"success — rely on the exit code, and read the result back with `comment-list` when the new state " +
				"is needed. The id `comment-edit` and `comment-remove` take is the comment's own id returned by " +
				"`comment-add`, not the id of the task or sprint it belongs to.",
			WrongExample:   "edited=$(rmp task comment-edit -r myproject 12 --type DECISION) && echo \"$edited\"",
			CorrectExample: "rmp task comment-edit -r myproject 12 --type DECISION && rmp task comment-list -r myproject 42",
			Reference:      "task comment-edit; task comment-remove; task comment-add; conventions.stdout_on_success; see also pitfall parse_modification_stdout.",
		},
		{
			ID: "graph_missing_query",
			Description: "Invoking `rmp graph execute` without --query and without piping a statement on " +
				"stdin. When --query is absent the subcommand reads stdin; if stdin is also empty (terminal, " +
				"no pipe) the command fails with exit code 2. Either pass --query or pipe the Cypher: " +
				"`echo '<cypher>' | rmp graph execute -r <name>`.",
			WrongExample:   "rmp graph execute -r myproject",
			CorrectExample: "rmp graph execute -r myproject --query \"MATCH (n) RETURN count(n)\"",
			Reference:      "graph execute; SPEC/GRAPH.md § Cypher Input Source and Precedence.",
		},
	}
}
