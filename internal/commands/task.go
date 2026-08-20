// Package commands implements CLI command handlers for Groadmap.
package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// HandleTask handles task commands by delegating to the central
// command registry. The dispatch (subcommand resolution, alias
// matching, --help routing) is implemented once in
// Command.DispatchFamily; this function exists only to expose the
// task-family entry point with its historic signature for callers and
// tests that resolve commands by name.
func HandleTask(args []string) error {
	return dispatchFamily("task", args)
}

// printTaskHelp prints task command help.
//
// Two distinct "Valid values" blocks cover -y, --type, and they are deliberately
// NOT merged: inside this one family the same flag spelling carries a TaskType on
// list / create / edit and a comment type on the four comment-* subcommands. The
// two enums are unrelated and never interchangeable, so a single list of
// seventeen values would be wrong (SPEC/HELP.md § Comment subcommand help
// specifics item 1). The comment-type list is rendered from
// models.ValidTaskCommentTypes rather than typed out, so it cannot go stale.
func printTaskHelp() {
	fmt.Printf(`Usage: rmp task [command] [arguments] [options]

Valid status values (for --status filter and 'stat' setter):
  BACKLOG, SPRINT, DOING, TESTING, COMPLETED

Valid task types (for -y, --type on 'list', 'create' and 'edit'):
  USER_STORY, TASK, BUG, SUB_TASK, EPIC, REFACTOR, CHORE, SPIKE, DESIGN_UX, IMPROVEMENT

Valid comment types (for -y, --type on 'comment-add', 'comment-list' and
'comment-edit'):
  %s
  The same -y, --type spelling carries two unrelated enums in this family: a
  task type on 'list'/'create'/'edit', a comment type on 'comment-add',
  'comment-list' and 'comment-edit'. A value from one set is rejected (exit 6)
  by the other. 'comment-remove' takes no -y, --type at all and rejects it
  (exit 2).

Numeric ranges:
  --priority, --severity      0-9 (0 = lowest, 9 = highest)

Status workflow (per SPEC/STATE_MACHINE.md):
  BACKLOG --[sprint add-tasks]--> SPRINT --[task stat DOING]--> DOING
        DOING --[task stat TESTING]--> TESTING
        TESTING --[task stat COMPLETED]--> COMPLETED --[task reopen / stat BACKLOG]--> BACKLOG
  Rules enforced:
    - 'task stat <id> SPRINT' is rejected (exit 6). Use 'sprint add-tasks' instead.
    - 'task remove' is only allowed while a task is in BACKLOG.
    - Marking COMPLETED is rejected (exit 6) if any subtask or dependency is not yet COMPLETED.
    - On COMPLETED transition you may attach a free-form summary with --summary / -s (max 4096 chars).
    - 'task reopen' (or 'stat BACKLOG' from COMPLETED) clears started_at, tested_at, closed_at, completion_summary.

Commands:
  list, ls [OPTIONS]                          List tasks (any status; filter with --status)
  create, new [OPTIONS]                       Create a new task (lands in BACKLOG)
  get <task-ids>                              Get tasks by id (CSV, no spaces)
  next [num]                                  Get next [num] incomplete tasks from the OPEN sprint
  edit <task-id> [OPTIONS]                    Edit fields of a task (status NOT editable here)
  remove, rm <task-ids>                       Remove task(s) — BACKLOG only, no active subtasks
  stat, set-status <task-ids> <new-status>    Set task status (manual transitions; SPRINT is rejected)
  reopen <task-ids>                           Reopen task(s) to BACKLOG, clearing lifecycle timestamps
  prio, set-priority <task-ids> <priority>    Set task priority (0-9) for one or many tasks
  sev, set-severity <task-ids> <severity>     Set task severity (0-9) for one or many tasks
  subtasks <task-id>                          List direct subtasks (one level; no grand-children)
  add-dep <task-id> <blocker-id>              Declare <task-id> depends on <blocker-id> (cycles rejected)
  remove-dep <task-id> <blocker-id>           Remove the dependency edge created by add-dep
  blockers <task-id>                          List tasks blocking <task-id> (incomplete dependencies)
  blocking <task-id>                          List tasks that depend on <task-id> (reverse of blockers)
  comment-add, c-add <task-id>                Add one typed comment to a task's work log
  comment-list, c-ls <task-id>                List a task's comments, oldest first
  comment-edit, c-edit <comment-id>           Change the type and/or body of one comment
  comment-remove, c-rm <comment-id>           Delete one comment (irreversible)

Options (shared by most subcommands):
  -r, --roadmap <name>              REQUIRED. Target roadmap.
  -h, --help                        Show this help message

Options (list — all filters compose with AND):
  -s, --status <state>              Filter by exact status
  -p, --priority <min>              Filter: priority >= min (0-9)
  --severity <min>                  Filter: severity >= min (0-9)
  -y, --type <type>                 Filter by task type
  --created-since <date>            Include tasks created on/after this date (RFC3339 or YYYY-MM-DD)
  --created-until <date>            Include tasks created on/before this date (RFC3339 or YYYY-MM-DD)
  --sort <field>                    Sort: priority (default), created, status, severity
  -l, --limit <n>                   Maximum results (1-100, default 100)

Options (create / edit):
  -t,  --title <text>               Task title (max 255 chars)
  -fr, --functional-requirements <text>
                                    Functional requirements — Why? (max 4096 chars)
  -tr, --technical-requirements <text>
                                    Technical requirements — How? (max 4096 chars)
  -ac, --acceptance-criteria <text>
                                    Acceptance criteria — How to verify? (max 4096 chars)
  -y,  --type <type>                Task type (default: TASK)
  -p,  --priority <n>               Initial/new priority (0-9, default 0)
       --severity <n>               Initial/new severity (0-9, default 0)
       --parent <id>                Parent task ID (on create only — makes a sub-task)

Options (stat to COMPLETED):
  -s, --summary <text>              Completion summary (max 4096 chars; only valid when
                                    target status is COMPLETED)

Options (comment-add / comment-list / comment-edit):
  -y, --type <TYPE>                 Comment type (see the comment-type list above).
                                    REQUIRED on comment-add, optional filter on
                                    comment-list, optional new value on comment-edit.
  -b, --body <text>                 Comment text (max 4096 chars). On comment-add and
                                    comment-edit the body may instead arrive on standard
                                    input, under a bounded read, when absent —
                                    on comment-edit only if --type is absent too, so a
                                    type-only edit never waits for input. Supplying
                                    neither source is an error (exit 2).

Comment rules (per SPEC/COMMANDS.md § Task Comments):
  - Comments are accepted in every status, including COMPLETED, and no comment
    changes or gates a task's status.
  - comment-add and comment-list take the TASK's id; comment-edit and
    comment-remove take the COMMENT's own id. Task and sprint comment ids are
    separate sequences.
  - comment-edit requires at least one change (--type, --body, or a body on
    standard input) and is rejected with exit 2 when none is requested.

Output (stdout JSON):
  list, get, next, subtasks, blockers, blocking   Array of task objects.
  create                                          {"id": <int>}
  edit, stat, prio, sev, reopen, remove           Empty (exit 0 on success).
  add-dep, remove-dep                             Empty (exit 0 on success).
  comment-add                                     {"id": <int>}
  comment-list                                    Array of comment objects.
  comment-edit, comment-remove                    Empty (exit 0 on success).
  Task object keys: id, title, status, type, functional_requirements,
  technical_requirements, acceptance_criteria, created_at,
  started_at, tested_at, closed_at, completion_summary, parent_task_id,
  priority, severity, subtask_count, depends_on, blocks.
  Comment object keys: id, task_id, type, body, created_at, updated_at
  (updated_at is null until the comment is first edited).

Exit codes:
  0   Success
  1   Database failure
  2   Misuse (missing required flag, bad syntax, no comment body supplied)
  3   No roadmap specified (-r missing)
  4   Task or comment not found
  6   Validation error (bad enum, out-of-range number, oversized field,
       invalid state transition, subtask/dependency guard, etc.)

Examples:
  rmp task list -r myproject
  rmp task list -r myproject --status BACKLOG --priority 5 --sort priority
  rmp task list -r myproject --created-since 2026-01-01 --type BUG
  rmp task create -r myproject -t "Fix bug" -fr "User can login" -tr "Update auth" -ac "Login works"
  rmp task create -r myproject -t "Add metrics" --type CHORE -p 3
  rmp task edit -r myproject 42 -t "Updated title" -p 8
  rmp task stat -r myproject 1,2,3 DOING
  rmp task stat -r myproject 7 COMPLETED --summary "Shipped behind feature flag"
  rmp task prio -r myproject 1,2,3 8
  rmp task sev -r myproject 5 9
  rmp task add-dep -r myproject 10 7
  rmp task blockers -r myproject 10
  rmp task next -r myproject 5
  rmp task comment-add -r myproject 42 --type FINDING --body "Boundary second is accepted by the parser."
  rmp task comment-add -r myproject 42 --type DECISION < decision.txt
  rmp task comment-list -r myproject 42 --type DECISION
  rmp task comment-edit -r myproject 12 --type NOTE
  rmp task comment-remove -r myproject 12
`, models.FormatCommentTypes(models.ValidTaskCommentTypes))
}
