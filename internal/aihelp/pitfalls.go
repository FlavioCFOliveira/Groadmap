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

// staticPitfalls returns the twelve canonical pitfalls required by
// SPEC/DATA_FORMATS.md § AI Agent Contract. Fresh slice on every call,
// matching the defensive-copy semantics of the other static helpers.
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
			Description: "Calling `task remove` on a task that is not in BACKLOG. Removal is only allowed for " +
				"BACKLOG tasks; non-BACKLOG tasks must be moved back to BACKLOG first " +
				"(via `sprint remove-tasks` for SPRINT, or `task reopen` for COMPLETED).",
			WrongExample:   "rmp task remove -r myproject 42",
			CorrectExample: "rmp sprint remove-tasks -r myproject 7 42 && rmp task remove -r myproject 42",
			Reference:      "task remove; SPEC/STATE_MACHINE.md § Deletion rule.",
		},
		{
			ID: "add_tasks_to_closed_sprint",
			Description: "Calling `sprint add-tasks` against a sprint in CLOSED state. Closed sprints are " +
				"immutable; use a PENDING or OPEN sprint, or create a new one.",
			WrongExample:   "rmp sprint add-tasks -r myproject 3 42,43",
			CorrectExample: "rmp sprint create -r myproject -d \"Sprint 8\" && rmp sprint add-tasks -r myproject 8 42,43",
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
			CorrectExample: "rmp task blockers -r myproject 42 && rmp task stat -r myproject <blocker-id> COMPLETED && rmp task stat -r myproject 42 COMPLETED",
			Reference:      "task stat; task blockers; task remove-dep; SPEC/STATE_MACHINE.md § Dependency rules.",
		},
		{
			ID: "summary_on_non_completed_transition",
			Description: "Passing `--summary` on any transition other than `→ COMPLETED`. The flag is " +
				"accepted only when the target status is COMPLETED; using it on any other transition is rejected.",
			WrongExample:   "rmp task stat -r myproject 42 DOING --summary \"started work\"",
			CorrectExample: "rmp task stat -r myproject 42 COMPLETED --summary \"work done and verified\"",
			Reference:      "task stat --summary flag.",
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
			WrongExample:   "rmp task stat -r myproject 42,99999,43 COMPLETED",
			CorrectExample: "rmp task get -r myproject 42,43 && rmp task stat -r myproject 42,43 COMPLETED",
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
			WrongExample:   "result=$(rmp task stat -r myproject 42 DOING) && echo \"$result\"",
			CorrectExample: "rmp task stat -r myproject 42 DOING && echo \"transition succeeded\"",
			Reference:      "task stat; task prio; sprint reorder; conventions.stdout_on_success.",
		},
	}
}
