// Package commands — per-subcommand help text for `rmp task`.
//
// Each printer is invoked by HandleTask when 'rmp task <sub> --help' is
// detected (see hasHelpFlag in flags.go). The texts are deliberately
// self-contained: an LLM agent that lands on one of these does not need
// to also have read the family help to know how to call the subcommand.
package commands

import "fmt"

// printTaskListHelp — `rmp task list`.
func printTaskListHelp() {
	fmt.Print(`Usage: rmp task list -r <roadmap> [filters]

Returns tasks in the given roadmap across every status. Use 'rmp backlog
list' for BACKLOG-only, 'rmp sprint tasks <id>' for one sprint, or
'rmp sprint open-tasks <id>' for active sprint tasks (SPRINT/DOING/TESTING).

Aliases: ls.

Filters (compose with AND):
  -s, --status <state>            Exact status: BACKLOG, SPRINT, DOING, TESTING, COMPLETED
  -p, --priority <min>            priority >= <min> (0-9)
  --severity <min>                severity >= <min> (0-9)
  -y, --type <type>               One of: USER_STORY, TASK, BUG, SUB_TASK,
                                  EPIC, REFACTOR, CHORE, SPIKE, DESIGN_UX, IMPROVEMENT
  -sp, --specialists <substring>  Case-insensitive substring match against specialists
  --created-since <date>          Inclusive lower bound (RFC3339 or YYYY-MM-DD)
  --created-until <date>          Inclusive upper bound

Sorting and paging:
  --sort <field>                  priority (default) | created | status | severity
  -l, --limit <n>                 Maximum tasks returned (1-100, default 100;
                                  out-of-range values fail with exit 6)

Required:
  -r, --roadmap <name>            Target roadmap

Output (stdout JSON):
  Array of task objects; see 'rmp task --help' for the full key list.

Exit codes:
  0  Success
  3  Missing -r
  6  Invalid filter value (bad enum or date format)

Examples:
  rmp task list -r myproject
  rmp task list -r myproject --status BACKLOG --priority 7
  rmp task list -r myproject --type BUG --sort severity --limit 20
  rmp task list -r myproject --created-since 2026-01-01
`)
}

// printTaskCreateHelp — `rmp task create`.
func printTaskCreateHelp() {
	fmt.Print(`Usage: rmp task create -r <roadmap> -t <title> -fr <FR> -tr <TR> -ac <AC> [options]

Creates a new task in BACKLOG status. Title and the three requirement
fields are mandatory; everything else takes a default.

Aliases: new.

Required:
  -r, --roadmap <name>            Target roadmap
  -t, --title <text>              Task title (max 255 chars; whitespace trimmed)
  -fr, --functional-requirements <text>  Why? (max 4096 chars)
  -tr, --technical-requirements <text>   How? (max 4096 chars)
  -ac, --acceptance-criteria <text>      How to verify? (max 4096 chars)

Optional:
  -y, --type <type>               Default: TASK. Valid: USER_STORY, TASK, BUG,
                                  SUB_TASK, EPIC, REFACTOR, CHORE, SPIKE,
                                  DESIGN_UX, IMPROVEMENT
  -p, --priority <n>              0-9, default 0
  --severity <n>                  0-9, default 0
  -sp, --specialists <list>       Comma-separated names (max 500 chars total)
  --parent <id>                   Make this task a subtask of <id> (parent must exist)

Output (stdout JSON):
  {"id": <new-task-id>}

Exit codes:
  0  Success
  2  Missing required flag
  3  Missing -r
  4  --parent points to a missing task
  6  Validation error (oversize field, bad enum/range, bad type)

Examples:
  rmp task create -r myproject -t "Fix JWT expiry bug" \
                  -fr "Tokens expire 1h early under DST"  \
                  -tr "Add timezone-aware expiry calc"     \
                  -ac "Unit tests pass; staging cycle is clean"
  rmp task create -r myproject -t "Add metrics" --type CHORE -p 3
  rmp task create -r myproject -t "Subtask of #5" --parent 5 \
                  -fr "..." -tr "..." -ac "..."
`)
}

// printTaskGetHelp — `rmp task get`.
func printTaskGetHelp() {
	fmt.Print(`Usage: rmp task get -r <roadmap> <task-ids>

Returns one or more tasks by id. Each id is checked: if any id is missing,
the request fails fast with exit 4 and no rows are returned.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-ids>                      Comma-separated integer ids (no spaces, e.g. "1,4,7")

Output (stdout JSON):
  Array of task objects. Empty array (and exit 0) only if no ids were given;
  any unknown id raises exit 4.

Exit codes:
  0  Success
  3  Missing -r
  4  At least one id does not exist
  6  Invalid id syntax

Examples:
  rmp task get -r myproject 1
  rmp task get -r myproject 1,3,5
`)
}

// printTaskNextHelp — `rmp task next`.
func printTaskNextHelp() {
	fmt.Print(`Usage: rmp task next -r <roadmap> [num]

Returns the next <num> incomplete tasks from the currently OPEN sprint
(statuses SPRINT, DOING, TESTING — i.e. not COMPLETED), in the order
the sprint dictates. Used as the "what should I pick up next?"
planning shortcut.

Compared to:
  - 'sprint open-tasks <id>': scope is "this sprint", any priority.
  - 'backlog show-next [count]': operates on BACKLOG only (not yet in a sprint).
  - 'task list --status SPRINT': any sprint, no implicit priority order.

Required:
  -r, --roadmap <name>            Target roadmap

Optional:
  [num]                           Maximum tasks to return (default 1, max 100;
                                  values above 100 are silently clamped)

Output (stdout JSON):
  Array of task objects, ordered by sprint position ASC (i.e. the order set
  by 'sprint reorder' / 'move-to' / 'top' / 'bottom'); priority DESC is used
  only as a tiebreaker for tasks sharing the same position.
  Empty array (exit 0) if the OPEN sprint has no SPRINT/DOING/TESTING tasks.

Exit codes:
  0  Success
  3  Missing -r
  4  No sprint is OPEN
  6  Invalid <num> (non-numeric or < 1)

Examples:
  rmp task next -r myproject              # returns the first 1 task
  rmp task next -r myproject 10
`)
}

// printTaskEditHelp — `rmp task edit`.
func printTaskEditHelp() {
	fmt.Print(`Usage: rmp task edit -r <roadmap> <task-id> [options]

Edits one or more fields on an existing task. At least one option must be
provided; setting a text field to "" is rejected (use task remove instead
of clearing required fields). Status is NOT editable here — use 'task stat'.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer id of the task

At least one of:
  -t, --title <text>              Max 255 chars (whitespace trimmed)
  -fr, --functional-requirements <text>   Max 4096 chars
  -tr, --technical-requirements <text>    Max 4096 chars
  -ac, --acceptance-criteria <text>       Max 4096 chars
  -y, --type <type>               See 'rmp task create --help' for valid values
  -p, --priority <n>              0-9
  --severity <n>                  0-9
  -sp, --specialists <list>       Comma-separated names (max 500 chars total)

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  Task not found
  6  No fields supplied, empty value for required text field, oversize,
     bad type/priority/severity

Examples:
  rmp task edit -r myproject 42 -t "Updated title"
  rmp task edit -r myproject 42 -p 8 --severity 3
  rmp task edit -r myproject 42 --type BUG -ac "Updated AC..."
`)
}

// printTaskRemoveHelp — `rmp task remove`.
func printTaskRemoveHelp() {
	fmt.Print(`Usage: rmp task remove -r <roadmap> <task-ids>

Deletes one or more tasks. ALL listed tasks must currently be in BACKLOG;
the batch fails-fast (exit 6) if any is in a later status. Tasks with
active subtasks cannot be deleted either — remove the subtasks first.

Aliases: rm.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-ids>                      Comma-separated integer ids (no spaces, e.g. "1,3,5")

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  At least one id does not exist
  6  At least one task is not in BACKLOG, or has active subtasks

Examples:
  rmp task remove -r myproject 7
  rmp task rm -r myproject 1,3,5
`)
}

// printTaskStatHelp — `rmp task stat`.
func printTaskStatHelp() {
	fmt.Print(`Usage: rmp task stat -r <roadmap> <task-ids> <new-status> [--summary <text>]

Changes the status of one or more tasks. The status machine is strict:

  Allowed manual transitions:
    SPRINT      -> DOING | BACKLOG
    DOING       -> TESTING
    TESTING     -> DOING | COMPLETED
    COMPLETED   -> BACKLOG  (alias of 'task reopen')

  Forbidden:
    'task stat <id> SPRINT'   (exit 6) — use 'sprint add-tasks' instead.

  COMPLETED guards (both checked before mutation):
    - Every subtask must already be COMPLETED.
    - Every dependency (added via 'task add-dep') must already be COMPLETED.

  Side effects:
    DOING       sets started_at to now
    TESTING     sets tested_at to now
    COMPLETED   sets closed_at to now (and stores --summary if provided)
    BACKLOG     clears started_at, tested_at, closed_at, completion_summary

Aliases: set-status.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-ids>                      Comma-separated integer ids (no spaces, e.g. "1,3,5")
  <new-status>                    One of: BACKLOG, DOING, TESTING, COMPLETED

Optional (only valid when <new-status> == COMPLETED):
  -s, --summary <text>            Completion summary (max 4096 chars)

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  At least one task id does not exist
  6  Invalid status, invalid transition, manual SPRINT attempt, --summary
     supplied for a non-COMPLETED target, summary too long, or subtask/
     dependency guard violation

Examples:
  rmp task stat -r myproject 1 DOING
  rmp task stat -r myproject 3,7 TESTING
  rmp task stat -r myproject 7 COMPLETED --summary "Shipped behind feature flag"
  rmp task stat -r myproject 9 BACKLOG    # reopen (equivalent to 'task reopen')
`)
}

// printTaskReopenHelp — `rmp task reopen`.
func printTaskReopenHelp() {
	fmt.Print(`Usage: rmp task reopen -r <roadmap> <task-ids>

Resets the listed tasks to BACKLOG status and clears their started_at,
tested_at, closed_at, and completion_summary fields. Equivalent to
'task stat <ids> BACKLOG' from a SPRINT/DOING/TESTING/COMPLETED source,
but more explicit and slightly more permissive (also tolerates ids that
are already in BACKLOG — they are skipped with a stderr note).

Required:
  -r, --roadmap <name>            Target roadmap
  <task-ids>                      Comma-separated integer ids (no spaces, e.g. "1,3,5")

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  At least one id does not exist
  6  Invalid id syntax

Examples:
  rmp task reopen -r myproject 7
  rmp task reopen -r myproject 1,3,5
`)
}

// printTaskPrioHelp — `rmp task prio`.
func printTaskPrioHelp() {
	fmt.Print(`Usage: rmp task prio -r <roadmap> <task-ids> <priority>

Sets the priority of one or more tasks to the same value. Use 'task edit'
for fine-grained per-task updates.

Aliases: set-priority.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-ids>                      Comma-separated integer ids (no spaces, e.g. "1,3,5")
  <priority>                      Integer 0-9 (0 = lowest, 9 = highest)

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  Task not found
  6  Priority out of range or non-numeric

Examples:
  rmp task prio -r myproject 1,2,3 8
  rmp task set-priority -r myproject 7 9
`)
}

// printTaskSevHelp — `rmp task sev`.
func printTaskSevHelp() {
	fmt.Print(`Usage: rmp task sev -r <roadmap> <task-ids> <severity>

Sets the severity of one or more tasks to the same value. Severity is
typically used to rank bugs; for feature work, priority is preferred.

Aliases: set-severity.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-ids>                      Comma-separated integer ids (no spaces, e.g. "1,3,5")
  <severity>                      Integer 0-9 (0 = lowest, 9 = highest)

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  Task not found
  6  Severity out of range or non-numeric

Examples:
  rmp task sev -r myproject 5 9
  rmp task set-severity -r myproject 1,2 6
`)
}

// printTaskAssignHelp — `rmp task assign`.
func printTaskAssignHelp() {
	fmt.Print(`Usage: rmp task assign -r <roadmap> <task-id> <specialist>

Adds <specialist> to the comma-separated specialists list on <task-id>.
Idempotent: assigning an already-present name is a no-op and exits 0.
A note is written to stderr in the no-op case for transparency — it is
informational, not an error, and callers parsing stderr as an error
signal should ignore it. Use 'task unassign' to remove a specialist.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer task id
  <specialist>                    Free-form specialist label (kept as one token)

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  Task not found
  6  Specialist value pushes specialists past 500 chars

Examples:
  rmp task assign -r myproject 7 alice
  rmp task assign -r myproject 12 backend-team
`)
}

// printTaskUnassignHelp — `rmp task unassign`.
func printTaskUnassignHelp() {
	fmt.Print(`Usage: rmp task unassign -r <roadmap> <task-id> <specialist>

Removes <specialist> from the task's specialists list. If the list
becomes empty, the specialists field is set to NULL. Idempotent: if the
specialist is not present on the task, the call is a no-op and exits 0.
A note is written to stderr in the no-op case for transparency — it is
informational, not an error.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer task id
  <specialist>                    Specialist label to remove

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  Task not found

Examples:
  rmp task unassign -r myproject 7 alice
`)
}

// printTaskSubtasksHelp — `rmp task subtasks`.
func printTaskSubtasksHelp() {
	fmt.Print(`Usage: rmp task subtasks -r <roadmap> <task-id>

Lists the direct subtasks of <task-id> — tasks whose parent_task_id
matches. Does not include grand-children; recurse from the result if
you need a deeper tree.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer task id (parent)

Output (stdout JSON):
  Array of task objects. Empty array (exit 0) if the parent has no subtasks.

Exit codes:
  0  Success
  3  Missing -r
  4  Parent task not found

Examples:
  rmp task subtasks -r myproject 5
`)
}

// printTaskAddDepHelp — `rmp task add-dep`.
func printTaskAddDepHelp() {
	fmt.Print(`Usage: rmp task add-dep -r <roadmap> <task-id> <blocker-id>

Records that <task-id> depends on <blocker-id>: <blocker-id> must reach
COMPLETED before <task-id> can be marked COMPLETED. Self-edges and cycles
are rejected. Idempotent — adding the same edge twice is a no-op.

Audit log entries: TASK_ADD_DEP for both <task-id> and <blocker-id>.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer id of the dependent task
  <blocker-id>                    Integer id of the task that must complete first

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  Either task does not exist
  6  Self-dependency, would create a cycle, or invalid id syntax

Examples:
  rmp task add-dep -r myproject 10 7          # task 10 depends on task 7
  rmp task add-dep -r myproject 25 12
`)
}

// printTaskRemoveDepHelp — `rmp task remove-dep`.
func printTaskRemoveDepHelp() {
	fmt.Print(`Usage: rmp task remove-dep -r <roadmap> <task-id> <blocker-id>

Removes the dependency edge created by 'task add-dep'. Fails if the edge
does not exist (so the user can tell apart "removed" from "was never
there").

Audit log entries: TASK_REMOVE_DEP for both <task-id> and <blocker-id>.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer id of the dependent task
  <blocker-id>                    Integer id of the task that was a blocker

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  No such edge

Examples:
  rmp task remove-dep -r myproject 10 7
`)
}

// printTaskBlockersHelp — `rmp task blockers`.
func printTaskBlockersHelp() {
	fmt.Print(`Usage: rmp task blockers -r <roadmap> <task-id>

Lists the tasks that <task-id> depends on AND are not yet COMPLETED.
Used to answer "what's blocking task X right now?". The returned list
shrinks as dependencies complete; it becomes empty when the task is
unblocked.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer task id

Output (stdout JSON):
  Array of task objects (incomplete dependencies). Empty array (exit 0)
  if all dependencies are COMPLETED — or if there are none.

Exit codes:
  0  Success
  3  Missing -r
  4  Task not found

Examples:
  rmp task blockers -r myproject 10
`)
}

// printTaskBlockingHelp — `rmp task blocking`.
func printTaskBlockingHelp() {
	fmt.Print(`Usage: rmp task blocking -r <roadmap> <task-id>

Lists the tasks that depend on <task-id>: the inverse of 'task blockers'.
Useful when completing <task-id> to know which downstream tasks become
candidates for work.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer task id

Output (stdout JSON):
  Array of task objects (downstream dependents). Empty array (exit 0)
  if nothing depends on this task.

Exit codes:
  0  Success
  3  Missing -r
  4  Task not found

Examples:
  rmp task blocking -r myproject 7
`)
}
