// Package commands — per-subcommand help text for `rmp task`.
//
// Each printer is invoked by HandleTask when 'rmp task <sub> --help' is
// detected (see hasHelpFlag in flags.go). The texts are deliberately
// self-contained: an LLM agent that lands on one of these does not need
// to also have read the family help to know how to call the subcommand.
package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// printTaskListHelp — `rmp task list`.
func printTaskListHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task list -r <roadmap> [filters]

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

`+exitCodesBlock("task", "list")+`Examples:
  rmp task list -r myproject
  rmp task list -r myproject --status BACKLOG --priority 7
  rmp task list -r myproject --type BUG --sort severity --limit 20
  rmp task list -r myproject --created-since 2026-01-01
`)
}

// printTaskCreateHelp — `rmp task create`.
func printTaskCreateHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task create -r <roadmap> -t <title> -fr <FR> -tr <TR> -ac <AC> [options]

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
  --parent <id>                   Make this task a subtask of <id> (parent must exist)

Output (stdout JSON):
  {"id": <new-task-id>}

`+exitCodesBlock("task", "create")+`Examples:
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
	fmt.Fprint(helpDst(), `Usage: rmp task get -r <roadmap> <task-ids>

Returns one or more tasks by id. Each id is checked: if any id does not
exist in the roadmap, the request fails fast with exit 4 and no rows are
returned.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-ids>                      Comma-separated integer ids (no spaces, e.g. "1,4,7")

Output (stdout JSON):
  Array of task objects. Empty array (and exit 0) only if no ids were given;
  any unknown id raises exit 4.

`+exitCodesBlock("task", "get")+`Examples:
  rmp task get -r myproject 1
  rmp task get -r myproject 1,3,5
`)
}

// printTaskNextHelp — `rmp task next`.
func printTaskNextHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task next -r <roadmap> [num]

Returns the next <num> incomplete tasks from the currently OPEN sprint
(statuses SPRINT, DOING, TESTING — i.e. not COMPLETED), in the order
the sprint dictates. Used as the "what should I pick up next?"
planning shortcut.

The listing is NOT filtered by dependencies, and a task it returns may be
blocked. Selection is on sprint membership and task status alone: a task
whose declared dependencies are not yet COMPLETED is returned like any
other, at its own planned rank, and nothing in the returned object marks it
as blocked. The refusal arrives later and from a different command -- the
transition to COMPLETED is rejected with exit code 6 while a declared
dependency is not itself COMPLETED. Ask 'task blockers <task-id>' before
starting work on a task this command returns; it is the command that answers
what this one does not. The guarantee here is the order, not the readiness.

Compared to:
  - 'task blockers <task-id>': the dependencies of one task that are not yet
    COMPLETED -- the readiness question this command does not answer.
  - 'sprint open-tasks <id>': scope is "this sprint", any priority.
  - 'backlog show-next [count]': BACKLOG status only; a BACKLOG task
    belongs to no sprint.
  - 'task list --status SPRINT': any sprint, no implicit priority order.

Required:
  -r, --roadmap <name>            Target roadmap

Optional:
  [num]                           Maximum tasks to return (default 1, max 100;
                                  values above 100 are silently clamped)

Output (stdout JSON):
  Array of task objects, ordered by sprint position ASC (i.e. the order set
  by 'sprint reorder' / 'move-to' / 'top' / 'bottom'). The order is total --
  no two tasks of one sprint share a position -- so priority does not order
  this listing and cannot promote a task above another.
  Empty array (exit 0) if the OPEN sprint has no SPRINT/DOING/TESTING tasks.

`+exitCodesBlock("task", "next")+`Examples:
  rmp task next -r myproject              # returns the first 1 task
  rmp task next -r myproject 10
`)
}

// printTaskEditHelp — `rmp task edit`.
func printTaskEditHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task edit -r <roadmap> <task-id> [options]

Edits the supplied fields of an existing task; every other field is left
unchanged. Supplying no field at all changes nothing, and is accepted only
when the named task exists. Setting a text field to "" is rejected (use task
remove instead of clearing required fields). Status is NOT editable here —
use 'task stat'.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer id of the task

Optional:
  -t, --title <text>              Max 255 chars (whitespace trimmed)
  -fr, --functional-requirements <text>   Max 4096 chars
  -tr, --technical-requirements <text>    Max 4096 chars
  -ac, --acceptance-criteria <text>       Max 4096 chars
  -y, --type <type>               See 'rmp task create --help' for valid values
  -p, --priority <n>              0-9
  --severity <n>                  0-9

Output: empty (exit 0 on success).

`+exitCodesBlock("task", "edit")+`Examples:
  rmp task edit -r myproject 42 -t "Updated title"
  rmp task edit -r myproject 42 -p 8 --severity 3
  rmp task edit -r myproject 42 --type BUG -ac "Updated AC..."
`)
}

// printTaskRemoveHelp — `rmp task remove`.
func printTaskRemoveHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task remove -r <roadmap> <task-ids>

Deletes one or more tasks. ALL listed tasks must currently be in BACKLOG;
the batch fails-fast (exit 6) if any is in a later status. A BACKLOG task
belongs to no sprint, so a sprint member is first taken out with
'sprint remove-tasks' (a COMPLETED one first returned to SPRINT with
'task reopen'). Tasks with active subtasks cannot be deleted either —
remove the subtasks first.

Aliases: rm.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-ids>                      Comma-separated integer ids (no spaces, e.g. "1,3,5")

Output: empty (exit 0 on success).

`+exitCodesBlock("task", "remove")+`Examples:
  rmp task remove -r myproject 7
  rmp task rm -r myproject 1,3,5
`)
}

// printTaskStatHelp — `rmp task stat`.
func printTaskStatHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task stat -r <roadmap> <task-ids> <new-status> [-co <hash>] [-cc <hash>] [--summary <text>]

Changes the status of one or more tasks. The status machine is strict:

  Allowed manual transitions:
    SPRINT      -> DOING
    DOING       -> TESTING
    TESTING     -> DOING | COMPLETED

  Forbidden:
    'task stat <id> SPRINT'   (exit 6) — SPRINT is set by 'sprint add-tasks'
                              and by 'task reopen'.
    'task stat <id> BACKLOG'  (exit 6) — refused for every sprint member,
                              because a sprint member is never in BACKLOG
                              and this command never changes membership.
                              A SPRINT, DOING or TESTING task leaves its
                              sprint through 'sprint remove-tasks'; a
                              COMPLETED task stays in its sprint and is
                              returned to SPRINT with 'task reopen'.

  COMPLETED guards (both checked before mutation):
    - Every subtask must already be COMPLETED.
    - Every dependency (added via 'task add-dep') must already be COMPLETED.

  Commit tracking (see 'Optional' below for the flag spellings):
    - -co, --commit-open is accepted only when <new-status> is DOING, and is
      mandatory there: 'rmp task stat -r myproject 7 DOING' on its own is
      rejected (exit 6). On any other target status the flag is rejected
      (exit 6).
    - -cc, --commit-close is accepted only when <new-status> is COMPLETED,
      and is mandatory there: 'rmp task stat -r myproject 7 COMPLETED' on its
      own is rejected (exit 6). On any other target status the flag is
      rejected (exit 6).
    - -s, --summary is accepted only when <new-status> is COMPLETED, as
      above, but it is never mandatory.
    - Each value is a git commit hash of 7 to 64 hexadecimal characters,
      accepted in any letter case and stored lowercase.
    - You supply the hash. rmp runs no git command, inspects no working
      directory and reads no repository: it validates the format and never
      checks that the commit exists anywhere.
    - One hash applies to every id of a multi-id invocation.

  Side effects:
    DOING       sets started_at to now and commit_open to --commit-open
    TESTING     sets tested_at to now
    COMPLETED   sets closed_at to now and commit_close to --commit-close
                (and stores --summary if provided)

Aliases: set-status.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-ids>                      Comma-separated integer ids (no spaces, e.g. "1,3,5")
  <new-status>                    One of: DOING, TESTING, COMPLETED (BACKLOG
                                  and SPRINT are refused, as above)

Optional:
  -co, --commit-open <hash>       Git commit the work starts from, 7 to 64
                                  hexadecimal characters, stored lowercase.
                                  Required when <new-status> is DOING;
                                  rejected for every other target status.
  -cc, --commit-close <hash>      Git commit the work is concluded at, 7 to 64
                                  hexadecimal characters, stored lowercase.
                                  Required when <new-status> is COMPLETED;
                                  rejected for every other target status.
  -s,  --summary <text>           Completion summary (max 4096 chars). Accepted
                                  only when <new-status> is COMPLETED, and
                                  optional there.

Output: empty (exit 0 on success).

`+exitCodesBlock("task", "stat")+`Examples:
  rmp task stat -r myproject 1 DOING --commit-open 5f93b51
  rmp task stat -r myproject 3,7 TESTING
  rmp task stat -r myproject 7 COMPLETED -cc 2578d18 --summary "Shipped behind feature flag"
`)
}

// printTaskReopenHelp — `rmp task reopen`.
func printTaskReopenHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task reopen -r <roadmap> <task-ids>

Returns a DOING, TESTING or COMPLETED task to SPRINT inside the sprint it
belongs to, clearing started_at/tested_at/closed_at/completion_summary. The
task keeps its sprint and its position: reopen never changes sprint
membership. Ids already in SPRINT or in BACKLOG are skipped with a stderr
note rather than rejected. A task whose sprint is CLOSED is refused (exit 6)
and nothing is changed: reopen the sprint first with 'sprint reopen'. A
COMPLETED task that belongs to no sprint, which only data written before the
sprint membership rules can hold, returns to BACKLOG instead.

Commit tracking:
  commit_close is cleared with the fields above — reopening withdraws the
  claim that the task was concluded at that commit.
  commit_open is PRESERVED. The commit the work was started from remains a
  true historical fact after the task is reopened, so reopen is
  deliberately asymmetric here and no command ever clears commit_open. A
  later 'task stat <ids> DOING --commit-open <hash>' replaces it.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-ids>                      Comma-separated integer ids (no spaces, e.g. "1,3,5")

Output: empty (exit 0 on success).

`+exitCodesBlock("task", "reopen")+`Examples:
  rmp task reopen -r myproject 7
  rmp task reopen -r myproject 1,3,5
`)
}

// printTaskPrioHelp — `rmp task prio`.
func printTaskPrioHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task prio -r <roadmap> <task-ids> <priority>

Sets the priority of one or more tasks to the same value. Use 'task edit'
for fine-grained per-task updates.

Aliases: set-priority.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-ids>                      Comma-separated integer ids (no spaces, e.g. "1,3,5")
  <priority>                      Integer 0-9 (0 = lowest, 9 = highest)

Output: empty (exit 0 on success).

`+exitCodesBlock("task", "prio")+`Examples:
  rmp task prio -r myproject 1,2,3 8
  rmp task set-priority -r myproject 7 9
`)
}

// printTaskSevHelp — `rmp task sev`.
func printTaskSevHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task sev -r <roadmap> <task-ids> <severity>

Sets the severity of one or more tasks to the same value. Severity is
typically used to rank bugs; for feature work, priority is preferred.

Aliases: set-severity.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-ids>                      Comma-separated integer ids (no spaces, e.g. "1,3,5")
  <severity>                      Integer 0-9 (0 = lowest, 9 = highest)

Output: empty (exit 0 on success).

`+exitCodesBlock("task", "sev")+`Examples:
  rmp task sev -r myproject 5 9
  rmp task set-severity -r myproject 1,2 6
`)
}

// printTaskSubtasksHelp — `rmp task subtasks`.
func printTaskSubtasksHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task subtasks -r <roadmap> <task-id>

Lists the direct subtasks of <task-id> — tasks whose parent_task_id
matches. Does not include grand-children; recurse from the result if
you need a deeper tree.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer task id (parent)

Output (stdout JSON):
  Array of task objects. Empty array (exit 0) if the parent has no subtasks.

`+exitCodesBlock("task", "subtasks")+`Examples:
  rmp task subtasks -r myproject 5
`)
}

// printTaskAddDepHelp — `rmp task add-dep`.
func printTaskAddDepHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task add-dep -r <roadmap> <task-id> <blocker-id>

Records that <task-id> depends on <blocker-id>: <blocker-id> must reach
COMPLETED before <task-id> can be marked COMPLETED. Self-edges and cycles
are rejected. Idempotent — adding the same edge twice is a no-op.

Audit log entries: TASK_ADD_DEP for both <task-id> and <blocker-id>.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer id of the dependent task
  <blocker-id>                    Integer id of the task that must complete first

Output: empty (exit 0 on success).

`+exitCodesBlock("task", "add-dep")+`Examples:
  rmp task add-dep -r myproject 10 7          # task 10 depends on task 7
  rmp task add-dep -r myproject 25 12
`)
}

// printTaskRemoveDepHelp — `rmp task remove-dep`.
func printTaskRemoveDepHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task remove-dep -r <roadmap> <task-id> <blocker-id>

Removes the dependency edge created by 'task add-dep'. Fails if the edge
does not exist (so the user can tell apart "removed" from "was never
there").

Audit log entries: TASK_REMOVE_DEP for both <task-id> and <blocker-id>.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer id of the dependent task
  <blocker-id>                    Integer id of the task that was a blocker

Output: empty (exit 0 on success).

`+exitCodesBlock("task", "remove-dep")+`Examples:
  rmp task remove-dep -r myproject 10 7
`)
}

// printTaskBlockersHelp — `rmp task blockers`.
func printTaskBlockersHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task blockers -r <roadmap> <task-id>

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

`+exitCodesBlock("task", "blockers")+`Examples:
  rmp task blockers -r myproject 10
`)
}

// printTaskBlockingHelp — `rmp task blocking`.
func printTaskBlockingHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task blocking -r <roadmap> <task-id>

Lists the tasks that depend on <task-id>: the inverse of 'task blockers'.
Useful when completing <task-id> to know which downstream tasks become
candidates for work.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer task id

Output (stdout JSON):
  Array of task objects: all dependents regardless of status (contrast:
  'task blockers' returns only incomplete dependencies). Empty array
  (exit 0) if nothing depends on this task.

`+exitCodesBlock("task", "blocking")+`Examples:
  rmp task blocking -r myproject 7
`)
}

// taskCommentTypes renders the seven values a task comment accepts, exactly as
// the rejection message and the AI Agent Contract publish them. The list is never
// re-typed in a help body: models.FormatCommentTypes is the single source, so a
// change to the accepted set cannot leave a stale list behind in the help
// (SPEC/HELP.md § Comment subcommand help specifics item 1).
func taskCommentTypes() string {
	return models.FormatCommentTypes(models.ValidTaskCommentTypes)
}

// printTaskCommentAddHelp — `rmp task comment-add`.
func printTaskCommentAddHelp() {
	fmt.Fprintf(helpDst(), `Usage: rmp task comment-add -r <roadmap> <task-id> --type <TYPE> [--body <text>]

Adds one typed entry to a task's work log: what was found, what was tried,
what was decided and why. Comments are accepted in every task status,
including COMPLETED, and no comment ever changes or gates a task's status.

The positional argument is the TASK's id — the comment's own id is assigned
by this command and printed on success.

Aliases: c-add.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer id of the task being commented on
  -y, --type <TYPE>               Comment type; one of the values below

Valid comment types (for -y, --type on the comment-* subcommands):
  %s

  Note: -y, --type carries a COMMENT type here. The same spelling carries a
  TaskType on 'task list', 'task create' and 'task edit'; the two sets are
  unrelated, so a TaskType value such as BUG is rejected with exit code 6.

Optional:
  -b, --body <text>               Comment text, max 4096 characters. When this
                                  flag is absent the body is read from standard
                                  input under a bounded read (a heredoc, a pipe
                                  or a file redirect); supplying neither the flag
                                  nor a non-empty standard input fails with exit 2.
                                  Leading and trailing whitespace is trimmed;
                                  interior line breaks are preserved.

Validation order (a bad --type never leaves the command waiting on input):
  roadmap, then <task-id>, then --type presence, then the --type value, then
  the body, then the task's existence, then the body's length and control
  characters, then the insert and its audit entry in one transaction.

Output (stdout JSON):
  {"id": <new-comment-id>}

%sExamples:
  rmp task comment-add -r myproject 42 --type FINDING \
      --body "The expiry comparison is inclusive at the boundary second."
  rmp task comment-add -r myproject 42 --type DECISION < decision.txt
  cat finding.txt | rmp task comment-add -r myproject 42 -y FINDING
  rmp task comment-add -r myproject 42 --type DECISION <<'BODY'
Compare with !time.Now().Before(exp) so the boundary second expires.
Rejected widening the clock-skew allowance: it hides the boundary.
BODY
`, taskCommentTypes(), exitCodesBlock("task", "comment-add"))
}

// printTaskCommentListHelp — `rmp task comment-list`.
func printTaskCommentListHelp() {
	fmt.Fprintf(helpDst(), `Usage: rmp task comment-list -r <roadmap> <task-id> [--type <TYPE>]

Returns every comment of the given task, oldest first: created_at ascending
with the comment id as the tie-breaker. The order is the story the log
tells, so read it top to bottom to follow how the work progressed.

The positional argument is the TASK's id, not a comment id.

Aliases: c-ls.

Required:
  -r, --roadmap <name>            Target roadmap
  <task-id>                       Integer id of the task whose log is read

Optional:
  -y, --type <TYPE>               Return only the comments of this type. The
                                  value MUST be one of the values below; any
                                  other value fails with exit 6, including a
                                  value that is valid only on a sprint comment.

Valid comment types (for -y, --type on the comment-* subcommands):
  %s

  Note: -y, --type carries a COMMENT type here, not the TaskType carried by
  the same spelling on 'task list', 'task create' and 'task edit'.

Result-set size:
  Unbounded. Every matching comment is returned; there is no --limit, no
  --desc and no pagination.

Output (stdout JSON):
  Array of comment objects, oldest first. Keys: id, task_id, type, body,
  created_at, updated_at (null until the comment is first edited).
  Empty array (exit 0) when the task has no comments, or none of the
  requested type.

%sExamples:
  rmp task comment-list -r myproject 42
  rmp task comment-list -r myproject 42 --type DECISION
  rmp task c-ls -r myproject 42 -y FINDING
`, taskCommentTypes(), exitCodesBlock("task", "comment-list"))
}

// printTaskCommentEditHelp — `rmp task comment-edit`.
func printTaskCommentEditHelp() {
	fmt.Fprintf(helpDst(), `Usage: rmp task comment-edit -r <roadmap> <comment-id> [--type <TYPE>] [--body <text>]

Changes the type and/or the body of one existing task comment and stamps
updated_at, so a later listing shows that the comment was altered. The
previous text is not retained anywhere and cannot be recovered: the audit
log records that an edit happened, not what it replaced.

The positional argument is the COMMENT's own id, NOT the id of the task it
belongs to. Task comment ids and sprint comment ids are separate sequences,
so an id that exists under 'sprint comment-edit' is not found here.

Aliases: c-edit.

Required:
  -r, --roadmap <name>            Target roadmap
  <comment-id>                    Integer id of the comment itself
  At least one change: a --type value, a --body value, or a body on standard
  input. Unlike 'task edit', a request with no change is rejected (exit 2),
  not accepted as a no-op.

Valid comment types (for -y, --type on the comment-* subcommands):
  %s

  Note: -y, --type carries a COMMENT type here, not the TaskType carried by
  the same spelling on 'task list', 'task create' and 'task edit'.

Optional:
  -y, --type <TYPE>               New comment type; one of the values above
  -b, --body <text>               New comment text, max 4096 characters. When
                                  --body is absent AND --type is absent, the
                                  new body is read from standard input,
                                  so 'comment-edit <comment-id> < revised.txt'
                                  is a valid edit. Standard input is the new
                                  body only when --type is absent as well. When
                                  --type is present and --body is absent, the
                                  body is left unchanged: a terminal on standard
                                  input is not read, so a type-only edit typed
                                  at a terminal never waits for input, and a
                                  standard input that carries data is refused
                                  (exit 2) rather than ignored.

Output (stdout JSON):
  Empty (exit 0 on success), as for 'task edit' and 'sprint update'.

%sExamples:
  rmp task comment-edit -r myproject 12 --type DECISION
  rmp task comment-edit -r myproject 12 \
      --body "Superseded: the boundary second is now defined, not skewed."
  rmp task comment-edit -r myproject 12 < revised.txt
  rmp task c-edit -r myproject 12 -y NOTE -b "Kept for context only."
`, taskCommentTypes(), exitCodesBlock("task", "comment-edit"))
}

// printTaskCommentRemoveHelp — `rmp task comment-remove`.
func printTaskCommentRemoveHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp task comment-remove -r <roadmap> <comment-id>

Deletes one task comment. The row is removed outright: there is no soft
delete and no recovery. The audit entry outlives the row, so the task's
history still records that a comment existed and was removed.

The positional argument is the COMMENT's own id, NOT the id of the task it
belongs to. Task comment ids and sprint comment ids are separate sequences,
so an id that exists under 'sprint comment-remove' is not found here.

Exactly one id is accepted: this command takes no comma-separated list, so
the batch fail-fast rules of 'task remove' do not apply.

Aliases: c-rm.

Required:
  -r, --roadmap <name>            Target roadmap
  <comment-id>                    Integer id of the comment itself

Output (stdout JSON):
  Empty (exit 0 on success).

`+exitCodesBlock("task", "comment-remove")+`Examples:
  rmp task comment-remove -r myproject 12
  rmp task c-rm -r myproject 12
`)
}
