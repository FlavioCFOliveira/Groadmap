# Help Output Examples

This document defines the expected help output format for all commands and subcommands in Groadmap.
The style follows Unix CLI conventions (similar to `git` help output).

---

## Global Help (rmp --help)

Shown when running `rmp --help`, `rmp -h`, or `rmp` without arguments.

```
usage: rmp [-h | --help] [-v | --version] <command> [<args>]

Local Roadmap Manager - CLI for managing technical roadmaps, tasks, and sprints

These are common Groadmap commands used in various situations:

manage roadmaps
   roadmap    Create, list, and manage roadmaps
              (alias: road)

manage tasks
   task       Create, list, and manage tasks
              Includes status, priority, and severity management

manage sprints
   sprint     Create, manage, and track sprints
              Includes task assignment and sprint lifecycle

view audit trail
   audit      View audit log and entity history
              (alias: aud)

See 'rmp <command> --help' to read about a specific command.
See 'rmp <command> <subcommand> --help' for subcommand details.
```

---

## Roadmap Commands (rmp roadmap --help)

```
usage: rmp roadmap [-h | --help] <subcommand> [<args>]

Manage roadmaps - the top-level containers for tasks and sprints.
Each roadmap is stored as an independent SQLite database in ~/.roadmaps/

Subcommands:
   list       List all existing roadmaps
              (alias: ls)

   create     Create a new roadmap
              (alias: new)

   remove     Remove a roadmap permanently
              (alias: rm, delete)

See 'rmp roadmap <subcommand> --help' for more information.
```

### rmp roadmap list --help

```
usage: rmp roadmap list [-h | --help]

List all existing roadmaps in ~/.roadmaps/

Output: JSON array of roadmap objects

Example:
   rmp roadmap list
   rmp road ls
```

### rmp roadmap create --help

```
usage: rmp roadmap create [-h | --help] <name>

Create a new roadmap with the given name.
The roadmap will be stored as ~/.roadmaps/<name>.db

If a roadmap with the same name already exists, the command exits with code 5
and an error message. To replace an existing roadmap, first run
'rmp roadmap remove <name>' and then 'rmp roadmap create <name>'.

Arguments:
   <name>     Name for the new roadmap (lowercase letters, numbers, hyphens, underscores; max 50 chars)

Example:
   rmp roadmap create project1
   rmp road new myproject
```

### rmp roadmap remove --help

```
usage: rmp roadmap remove [-h | --help] <name>

Remove a roadmap permanently. This action cannot be undone.

Arguments:
   <name>     Name of the roadmap to remove

Example:
   rmp roadmap remove project1
   rmp road rm oldproject
```

---

## Task Commands (rmp task --help)

```
usage: rmp task [-h | --help] <subcommand> [<args>]

Manage tasks within a roadmap. Tasks track work with status,
priority, severity, and detailed descriptions.

Subcommands:
   list       List tasks in the selected roadmap
              (alias: ls)

   create     Create a new task
              (alias: new)

   get        Get detailed information about task(s)

   next       Get next N tasks from open sprint

   set-status Change task status
              (alias: stat)

   set-priority
              Change task priority (0-9)
              (alias: prio)

   set-severity
              Change task severity (0-9)
              (alias: sev)

   edit       Edit task properties

   remove     Remove task(s) permanently
              (alias: rm)

See 'rmp task <subcommand> --help' for more information.
```

### rmp task list --help

```
usage: rmp task list [-h | --help] [-r <name>] [-s <status>] [-p <n>] [--severity <n>] [-l <n>]

List tasks in the selected roadmap.

Options:
   -r, --roadmap <name>   Roadmap name (required)
   -s, --status <status>  Filter by status: BACKLOG, SPRINT, DOING, TESTING, COMPLETED
   -p, --priority <n>     Filter by minimum priority (0-9)
       --severity <n>     Filter by minimum severity (0-9)
   -l, --limit <n>        Limit number of results

Output: JSON array of task objects

Examples:
   rmp task list -r project1
   rmp task ls -r project1 -s DOING
   rmp task ls -r project1 -p 5 -l 20
```

### rmp task create --help

```
usage: rmp task create [-h | --help] -r <name> -t <title> -fr <fr> -tr <tr> -ac <ac> [-p <n>] [--severity <n>] [-sp <list>] [--parent <id>]

Create a new task in the specified roadmap.

Required Options:
   -r, --roadmap <name>                  Roadmap name
   -t, --title <title>                   Task title/summary
   -fr, --functional-requirements <fr>   Functional requirements (Why?)
   -tr, --technical-requirements <tr>    Technical requirements (How?)
   -ac, --acceptance-criteria <ac>       Acceptance criteria (How to verify?)

Optional Options:
   -y, --type <type>                     Task type: USER_STORY, TASK, BUG, SUB_TASK, EPIC,
                                         REFACTOR, CHORE, SPIKE, DESIGN_UX, IMPROVEMENT
                                         (default: TASK)
   -p, --priority <n>                    Priority 0-9 (default: 0)
       --severity <n>                    Severity 0-9 (default: 0)
   -sp, --specialists <list>             Comma-separated specialist tags
       --parent <id>                     Create as a sub-task of the given parent task

Output: JSON object with task ID

Examples:
   rmp task create -r project1 -t "Fix login bug" -fr "User can login" -tr "Update auth" -ac "Login works"
   rmp task new -r project1 -t "Update docs" -fr "Docs needed" -tr "Write README" -ac "Docs complete" -p 5
```

### rmp task next --help

```
usage: rmp task next [-h | --help] -r <name> [num]

Get the next N open tasks from the currently open sprint.
Tasks are returned in the sprint's defined task order (set via sprint reorder).

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   [num]                  Number of tasks to return (default: 1, max: 100)

Output: JSON array of task objects

Examples:
   rmp task next -r project1        # Returns 1 task
   rmp task next -r project1 5      # Returns up to 5 tasks
```

### rmp task get --help

```
usage: rmp task get [-h | --help] -r <name> <id>[,<id>,...]

Get detailed information about one or more tasks.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>[,<id>,...]        Comma-separated task IDs (no spaces)

Output: JSON array of task objects

Examples:
   rmp task get -r project1 42
   rmp task get -r project1 1,2,3,10
```

### rmp task set-status --help

```
usage: rmp task set-status [-h | --help] -r <name> <id>[,<id>,...] <state>

Change the status of one or more tasks.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>[,<id>,...]        Comma-separated task IDs (no spaces)
   <state>                New status (manual): BACKLOG, DOING, TESTING, COMPLETED
                          (SPRINT is set automatically by 'sprint add-tasks' and is rejected here)

Status Flow:
   BACKLOG ↔ SPRINT ↔ DOING ↔ TESTING → COMPLETED
   (Use 'COMPLETED → BACKLOG' to reopen a task)

Examples:
   rmp task set-status -r project1 42 DOING
   rmp task stat -r project1 1,2,3 COMPLETED
```

### rmp task set-priority --help

```
usage: rmp task set-priority [-h | --help] -r <name> <id>[,<id>,...] <priority>

Change the priority of one or more tasks.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>[,<id>,...]        Comma-separated task IDs (no spaces)
   <priority>             Priority value 0-9

Priority Scale:
   0 = low urgency, 9 = maximum urgency (Product Owner perspective)

Examples:
   rmp task set-priority -r project1 42 9
   rmp task prio -r project1 1,2,3 5
```

### rmp task set-severity --help

```
usage: rmp task set-severity [-h | --help] -r <name> <id>[,<id>,...] <severity>

Change the severity of one or more tasks.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>[,<id>,...]        Comma-separated task IDs (no spaces)
   <severity>             Severity value 0-9

Severity Scale:
   0 = minimal impact, 9 = critical impact (Dev Team perspective)

Examples:
   rmp task set-severity -r project1 42 5
   rmp task sev -r project1 1,2,3 9
```

### rmp task edit --help

```
usage: rmp task edit [-h | --help] -r <name> <id> [OPTIONS]

Edit an existing task's properties. Only specified fields are updated.

Options:
   -r, --roadmap <name>                  Roadmap name (required)
   -t, --title <text>                    Update task title
   -fr, --functional-requirements <text> Update functional requirements
   -tr, --technical-requirements <text>  Update technical requirements
   -ac, --acceptance-criteria <text>     Update acceptance criteria
   -y, --type <type>                     Update task type (see 'task create --help' for valid values)
   -p, --priority <n>                    Update priority (0-9)
       --severity <n>                    Update severity (0-9)
   -sp, --specialists <list>             Update comma-separated specialists

Arguments:
   <id>                                  Task ID to edit

Examples:
   rmp task edit -r project1 42 -t "New title" -p 7
   rmp task edit -r project1 1 --specialists "go-developer"
```

### rmp task remove --help

```
usage: rmp task remove [-h | --help] -r <name> <id>[,<id>,...]

Remove one or more tasks permanently. This action cannot be undone.

Constraint:
   Tasks can only be removed while in BACKLOG status. Attempts to remove a
   task in SPRINT, DOING, TESTING, or COMPLETED status are rejected with
   exit code 6. Move the task back to BACKLOG first (via 'sprint remove-tasks'
   for SPRINT, or 'task stat <id> BACKLOG' for other states).
   Tasks with subtasks must have their subtasks removed first.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>[,<id>,...]        Comma-separated task IDs (no spaces)

Examples:
   rmp task remove -r project1 42
   rmp task rm -r project1 1,2,3
```

### rmp task reopen --help

```
usage: rmp task reopen [-h | --help] -r <name> <id>[,<id>,...]

Return one or more tasks to BACKLOG status. Clears all lifecycle timestamps
(started_at, tested_at, closed_at) and removes the task from its current sprint
association. Accepts comma-separated IDs for bulk reopening.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>[,<id>,...]        Comma-separated task IDs (no spaces)

Valid source states: SPRINT, DOING, TESTING, COMPLETED (any non-BACKLOG state).
Reopening a task already in BACKLOG is a no-op.

Examples:
   rmp task reopen -r project1 42
   rmp task reopen -r project1 1,2,3
```

### rmp task subtasks --help

```
usage: rmp task subtasks [-h | --help] -r <name> <id>

List all direct subtasks of the given parent task. Subtasks are ordered by
priority descending, then created_at ascending.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>                   Parent task ID

Output: JSON array of Task objects (empty array if the task has no subtasks).

Examples:
   rmp task subtasks -r project1 10
```

### rmp task add-dep --help

```
usage: rmp task add-dep [-h | --help] -r <name> <task-id> <dep-id>

Mark <task-id> as depending on <dep-id>. The task cannot be marked COMPLETED
until <dep-id> is COMPLETED. Circular dependencies and self-dependencies are
rejected.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <task-id>              ID of the dependent task
   <dep-id>               ID of the task it depends on

Examples:
   rmp task add-dep -r project1 42 17
```

### rmp task remove-dep --help

```
usage: rmp task remove-dep [-h | --help] -r <name> <task-id> <dep-id>

Remove the dependency of <task-id> on <dep-id>.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <task-id>              ID of the dependent task
   <dep-id>               ID of the task it depends on

Examples:
   rmp task remove-dep -r project1 42 17
```

### rmp task blockers --help

```
usage: rmp task blockers [-h | --help] -r <name> <id>

List tasks that are blocking <id> — tasks that <id> depends on and that are
NOT yet COMPLETED.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>                   Task ID

Output: JSON array of Task objects (empty array if there are no blockers).

Examples:
   rmp task blockers -r project1 42
```

### rmp task blocking --help

```
usage: rmp task blocking [-h | --help] -r <name> <id>

List tasks that <id> is blocking — tasks that depend on <id>.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>                   Task ID

Output: JSON array of Task objects (empty array if this task is not blocking
anything).

Examples:
   rmp task blocking -r project1 17
```

---

## Sprint Commands (rmp sprint --help)

```
usage: rmp sprint [-h | --help] <subcommand> [<args>]

Manage sprints within a roadmap. Sprints group tasks into time-boxed
iterations with lifecycle management (PENDING → OPEN → CLOSED).

Subcommands:
   list       List sprints in the selected roadmap
              (alias: ls)

   get        Get detailed information about a sprint

   show       Show comprehensive sprint report

   tasks      List tasks assigned to a sprint

   create     Create a new sprint
              (alias: new)

   add-tasks  Add tasks to a sprint
              (alias: add)

   remove-tasks
              Remove tasks from a sprint
              (alias: rm-tasks)

   move-tasks Move tasks between sprints
              (alias: mv-tasks)

   start      Start a sprint (PENDING → OPEN)

   close      Close a sprint (OPEN → CLOSED)

   reopen     Reopen a closed sprint (CLOSED → OPEN)

   update     Update sprint description
              (alias: upd)

   stats      Show sprint statistics

   remove     Remove a sprint
              (alias: rm)

   reorder    Reorder tasks in sprint
              (alias: order)

   move-to    Move task to specific position
              (alias: mvto)

   swap       Swap positions of two tasks

   top        Move task to top of sprint

   bottom     Move task to bottom of sprint
              (alias: btm)

See 'rmp sprint <subcommand> --help' for more information.
```

### rmp sprint list --help

```
usage: rmp sprint list [-h | --help] [-r <name>] [-s <status>]

List sprints in the selected roadmap.

Options:
   -r, --roadmap <name>   Roadmap name (required)
   -s, --status <status>  Filter by status: PENDING, OPEN, CLOSED

Output: JSON array of sprint objects

Examples:
   rmp sprint list -r project1
   rmp sprint ls -r project1 -s OPEN
```

### rmp sprint get --help

```
usage: rmp sprint get [-h | --help] -r <name> <id>

Get detailed information about a specific sprint.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>                   Sprint ID

Output: JSON sprint object

Example:
   rmp sprint get -r project1 1
```

### rmp sprint show --help

```
usage: rmp sprint show [-h | --help] -r <name> <id>

Show comprehensive sprint status report including task statistics,
progress percentages, severity distribution, and criticality distribution.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>                   Sprint ID

Output: JSON report object

Example:
   rmp sprint show -r project1 1
```

### rmp sprint tasks --help

```
usage: rmp sprint tasks [-h | --help] -r <name> <sprint-id> [-s <status>]

List tasks assigned to a specific sprint.

Options:
   -r, --roadmap <name>   Roadmap name (required)
   -s, --status <status>  Filter by task status

Arguments:
   <sprint-id>            Sprint ID

Output: JSON array of task objects

Examples:
   rmp sprint tasks -r project1 1
   rmp sprint tasks -r project1 1 -s DOING
```

### rmp sprint open-tasks --help

```
usage: rmp sprint open-tasks [-h | --help] -r <name> <id> [--order-by-priority]

List the incomplete tasks of a sprint (status SPRINT, DOING, or TESTING).
Useful for stand-ups and sprint reviews when only remaining work matters.

Options:
   -r, --roadmap <name>     Roadmap name (required)
       --order-by-priority  Order by priority DESC instead of sprint position

Arguments:
   <id>                     Sprint identifier

Default ordering: sprint position ASC.

Output: JSON array of Task objects with status SPRINT, DOING, or TESTING.
Empty array if no open tasks remain.

Examples:
   rmp sprint open-tasks -r project1 1
   rmp sprint open-tasks -r project1 1 --order-by-priority
```

### rmp sprint create --help

```
usage: rmp sprint create [-h | --help] -r <name> -d <desc>

Create a new sprint in the specified roadmap.

Options:
   -r, --roadmap <name>        Roadmap name (required)
   -d, --description <desc>     Sprint description

Output: JSON object with sprint ID

Example:
   rmp sprint create -r project1 -d "Sprint 1 - Initial Setup"
   rmp sprint new -r project1 -d "Sprint 2 - Features"
```

### rmp sprint add-tasks --help

```
usage: rmp sprint add-tasks [-h | --help] -r <name> <sprint-id> <task-ids>

Add tasks to a sprint. Tasks must be in BACKLOG status.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <sprint-id>            Sprint ID to add tasks to
   <task-ids>             Comma-separated task IDs (no spaces)

Examples:
   rmp sprint add-tasks -r project1 1 10,11,12
   rmp sprint add -r project1 2 5,6,7,8
```

### rmp sprint remove-tasks --help

```
usage: rmp sprint remove-tasks [-h | --help] -r <name> <sprint-id> <task-ids>

Remove tasks from a sprint. Tasks return to BACKLOG status.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <sprint-id>            Sprint ID to remove tasks from
   <task-ids>             Comma-separated task IDs (no spaces)

Examples:
   rmp sprint remove-tasks -r project1 1 10,11,12
   rmp sprint rm-tasks -r project1 1 5,6
```

### rmp sprint move-tasks --help

```
usage: rmp sprint move-tasks [-h | --help] -r <name> <from-sprint> <to-sprint> <task-ids>

Move tasks from one sprint to another.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <from-sprint>          Source sprint ID
   <to-sprint>            Destination sprint ID
   <task-ids>             Comma-separated task IDs (no spaces)

Examples:
   rmp sprint move-tasks -r project1 1 2 10,11,12
   rmp sprint mv-tasks -r project1 2 3 5,6,7
```

### rmp sprint start --help

```
usage: rmp sprint start [-h | --help] -r <name> <id>

Start a sprint, changing its status from PENDING to OPEN.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>                   Sprint ID to start

Example:
   rmp sprint start -r project1 1
```

### rmp sprint close --help

```
usage: rmp sprint close [-h | --help] -r <name> <id>

Close a sprint, changing its status from OPEN to CLOSED.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>                   Sprint ID to close

Example:
   rmp sprint close -r project1 1
```

### rmp sprint reopen --help

```
usage: rmp sprint reopen [-h | --help] -r <name> <id>

Reopen a closed sprint, changing its status from CLOSED to OPEN.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>                   Sprint ID to reopen

Example:
   rmp sprint reopen -r project1 1
```

### rmp sprint update --help

```
usage: rmp sprint update [-h | --help] -r <name> <id> -d <desc>

Update a sprint's description.

Options:
   -r, --roadmap <name>        Roadmap name (required)
   -d, --description <desc>     New description

Arguments:
   <id>                        Sprint ID

Example:
   rmp sprint update -r project1 1 -d "Sprint 1 - Setup and Config"
   rmp sprint upd -r project1 1 -d "Updated description"
```

### rmp sprint stats --help

```
usage: rmp sprint stats [-h | --help] -r <name> <id>

Show statistics for a sprint including task counts by status.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>                   Sprint ID

Output: JSON statistics object

Example:
   rmp sprint stats -r project1 1
```

### rmp sprint reorder --help

```
usage: rmp sprint reorder [-h | --help] -r <name> <sprint-id> <task-ids>

Set the exact order of tasks in a sprint. Tasks are ordered by position (0-based),
where position 0 is the first task in the sprint.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <sprint-id>            Sprint ID
   <task-ids>             Comma-separated task IDs in desired order (e.g., 5,3,1,4,2)

Validation:
   - All task IDs must belong to the specified sprint
   - Duplicate task IDs are not allowed
   - All sprint tasks must be included

Example:
   rmp sprint reorder -r project1 1 5,3,1,4,2
   rmp sprint order -r project1 1 10,20,30
```

### rmp sprint move-to --help

```
usage: rmp sprint move-to [-h | --help] -r <name> <sprint-id> <task-id> <position>

Move a task to a specific position in the sprint, shifting other tasks accordingly.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <sprint-id>            Sprint ID
   <task-id>              Task to move
   <position>             Target position (0-based). If >= task count, moves to end.

Behavior:
   - Moving UP: Tasks between new position and current position-1 shift down by 1
   - Moving DOWN: Tasks between current position+1 and new position shift up by 1
   - Moving to same position: No-op
   - Moving to position >= task count: Task placed at the end

Example:
   rmp sprint move-to -r project1 1 5 0     # Move task 5 to top
   rmp sprint mvto -r project1 1 10 3       # Move task 10 to position 3
```

### rmp sprint swap --help

```
usage: rmp sprint swap [-h | --help] -r <name> <sprint-id> <task-id-1> <task-id-2>

Swap the positions of two tasks within a sprint.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <sprint-id>            Sprint ID
   <task-id-1>            First task to swap
   <task-id-2>            Second task to swap

Behavior:
   - Both tasks must belong to the same sprint
   - Positions are exchanged between the two tasks
   - No changes to other tasks

Example:
   rmp sprint swap -r project1 1 5 10       # Swap positions of tasks 5 and 10
```

### rmp sprint top --help

```
usage: rmp sprint top [-h | --help] -r <name> <sprint-id> <task-id>

Move a task to the beginning (top) of the sprint task list.
Equivalent to 'move-to <task-id> 0'.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <sprint-id>            Sprint ID
   <task-id>              Task to move to top

Example:
   rmp sprint top -r project1 1 5           # Move task 5 to top
```

### rmp sprint bottom --help

```
usage: rmp sprint bottom [-h | --help] -r <name> <sprint-id> <task-id>

Move a task to the end (bottom) of the sprint task list.
Equivalent to 'move-to <task-id> <task_count>'.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <sprint-id>            Sprint ID
   <task-id>              Task to move to bottom

Aliases:
   btm

Example:
   rmp sprint bottom -r project1 1 5        # Move task 5 to bottom
   rmp sprint btm -r project1 1 10          # Move task 10 to bottom
```

### rmp sprint remove --help

```
usage: rmp sprint remove [-h | --help] -r <name> <id>

Remove a sprint permanently. Tasks in the sprint are not deleted.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>                   Sprint ID to remove

Example:
   rmp sprint remove -r project1 1
   rmp sprint rm -r project1 2
```

---

## Audit Commands (rmp audit --help)

```
usage: rmp audit [-h | --help] <subcommand> [<args>]

View audit log and entity history. All changes to tasks and sprints
are automatically logged for traceability.

Subcommands:
   list       List audit log entries
              (alias: ls)

   history    View history for a specific entity
              (alias: hist)

   stats      Show audit statistics

See 'rmp audit <subcommand> --help' for more information.
```

### rmp audit list --help

```
usage: rmp audit list [-h | --help] -r <name> [-o <operation>] [-e <type>] [--entity-id <id>] [--since <date>] [--until <date>] [-l <n>]

List audit log entries with optional filtering.

Options:
   -r, --roadmap <name>        Roadmap name (required)
   -o, --operation <type>      Filter by operation type. See SPEC/DATABASE.md
                               for the canonical list of audit operations
                               (TASK_*, SPRINT_*). Examples: TASK_CREATE,
                               TASK_STATUS_CHANGE, TASK_REOPEN, TASK_ADD_DEP,
                               SPRINT_START, SPRINT_CLOSE, SPRINT_REORDER_TASKS.
   -e, --entity-type <type>    Filter by entity type: TASK, SPRINT
       --entity-id <id>        Filter by specific entity ID
       --since <date>          Include entries from this date (ISO 8601)
       --until <date>          Include entries until this date (ISO 8601)
   -l, --limit <n>             Limit number of results

Output: JSON array of audit entries

Examples:
   rmp audit list -r project1
   rmp audit ls -r project1 -o TASK_STATUS_CHANGE
   rmp audit ls -r project1 -e TASK --since 2026-03-01T00:00:00.000Z
```

### rmp audit history --help

```
usage: rmp audit history [-h | --help] -r <name> -e <type> <id>

View complete history for a specific entity (task or sprint).

Options:
   -r, --roadmap <name>        Roadmap name (required)
   -e, --entity-type <type>    Entity type: TASK, SPRINT (required)

Arguments:
   <id>                        Entity ID

Output: JSON array of audit entries for the entity

Examples:
   rmp audit history -r project1 -e TASK 42
   rmp audit hist -r project1 -e SPRINT 1
```

### rmp audit stats --help

```
usage: rmp audit stats [-h | --help] -r <name> [--since <date>] [--until <date>]

Show audit statistics including operation counts and trends.

Options:
   -r, --roadmap <name>        Roadmap name (required)
       --since <date>          Include entries from this date (ISO 8601)
       --until <date>          Include entries until this date (ISO 8601)

Output: JSON statistics object

Examples:
   rmp audit stats -r project1
   rmp audit stats -r project1 --since 2026-03-01T00:00:00.000Z
```

---

## Backlog Commands (rmp backlog --help)

```
usage: rmp backlog [-h | --help] <subcommand> [<args>]

Manage and query tasks in the backlog. All subcommands operate exclusively
on tasks with status BACKLOG.

Subcommands:
   list         List backlog tasks with optional filters
   show-next    Show the top N highest-priority backlog tasks

Example:
   rmp backlog list -r project1
   rmp backlog show-next 10 -r project1
```

### rmp backlog list --help

```
usage: rmp backlog list [-h | --help] -r <name> [OPTIONS]
       rmp backlog ls   [-h | --help] -r <name> [OPTIONS]

List tasks with status BACKLOG, with optional filtering and sorting.

Options:
   -r, --roadmap <name>     Roadmap name (required)
   -p, --priority <min>     Minimum priority (inclusive)
   -y, --type <type>        Filter by task type (see 'task create --help'
                            for the 10 valid types)
       --sort <field>       Sort by: priority (default), created, status, severity
   -l, --limit <n>          Maximum number of tasks to return

Output: JSON array of Task objects.

Examples:
   rmp backlog list -r project1
   rmp backlog list -r project1 --priority 7
   rmp backlog list -r project1 --type BUG
   rmp backlog ls   -r project1 --limit 20
```

### rmp backlog show-next --help

```
usage: rmp backlog show-next [-h | --help] [count] -r <name>

Show the top N highest-priority backlog tasks for sprint-planning purposes.
Equivalent to 'backlog list --sort priority --limit <count>'.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   count                  Number of tasks to return (default: 5, max: 100)

Output: JSON array of Task objects ordered by priority descending.

Examples:
   rmp backlog show-next -r project1
   rmp backlog show-next 10 -r project1
```

---

## Statistics Command (rmp stats --help)

```
usage: rmp stats [-h | --help] -r <name>

Get comprehensive statistics for a roadmap, including totals, status
distribution, priority distribution, sprint summary, and average velocity.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Output: JSON object with the following top-level fields:
   total_tasks                Total task count
   status_distribution        Count of tasks per status
   priority_distribution      Count of tasks per priority band
   sprint_summary             Sprint totals by status (PENDING, OPEN, CLOSED)
   average_velocity           Average tasks completed per closed sprint

Examples:
   rmp stats -r project1
```

---

## Error Messages with Help

When a command is invoked incorrectly, an error message is shown followed by the specific help for that command.

### Example: Missing required arguments

```
$ rmp task create -r project1
Error: Missing required options: --title, --functional-requirements, --technical-requirements, --acceptance-criteria

usage: rmp task create [-h | --help] -r <name> -t <title> -f <fr> -h <tr> -a <ac> [-p <n>] [--severity <n>] [--specialists <list>]

Create a new task in the specified roadmap.

Required Options:
   -r, --roadmap <name>                  Roadmap name
   -t, --title <title>                   Task title/summary
   -f, --functional-requirements <fr>    Functional requirements (Why?)
   -h, --technical-requirements <tr>   Technical requirements (How?)
   -a, --acceptance-criteria <ac>        Acceptance criteria (How to verify?)

Optional Options:
   -p, --priority <n>                    Priority 0-9 (default: 0)
       --severity <n>                    Severity 0-9 (default: 0)
       --specialists <list>              Comma-separated specialist tags

Output: JSON object with task ID

Examples:
   rmp task create -r project1 -t "Fix login bug" -fr "User can login" -tr "Update auth" -ac "Login works"
   rmp task new -r project1 -t "Update docs" -fr "Docs needed" -tr "Write README" -ac "Docs complete" -p 5
```

### Example: Unknown subcommand

```
$ rmp task unknown
Error: Unknown subcommand 'unknown' for command 'task'

usage: rmp task [-h | --help] <subcommand> [<args>]

Manage tasks within a roadmap. Tasks track work with status,
priority, severity, and detailed descriptions.

Subcommands:
   list       List tasks in the selected roadmap
              (alias: ls)

   create     Create a new task
              (alias: new)

   get        Get detailed information about task(s)

   next       Get next N tasks from open sprint

   set-status Change task status
              (alias: stat)

   set-priority
              Change task priority (0-9)
              (alias: prio)

   set-severity
              Change task severity (0-9)
              (alias: sev)

   edit       Edit task properties

   remove     Remove task(s) permanently
              (alias: rm)

See 'rmp task <subcommand> --help' for more information.
```

### Example: Invalid argument format

```
$ rmp task prio -r project1 abc 5
Error: Invalid argument 'abc': expected comma-separated integers

usage: rmp task set-priority [-h | --help] -r <name> <id>[,<id>,...] <priority>

Change the priority of one or more tasks.

Options:
   -r, --roadmap <name>   Roadmap name (required)

Arguments:
   <id>[,<id>,...]        Comma-separated task IDs (no spaces)
   <priority>             Priority value 0-9

Priority Scale:
   0 = low urgency, 9 = maximum urgency (Product Owner perspective)

Examples:
   rmp task set-priority -r project1 42 9
   rmp task prio -r project1 1,2,3 5
```

### Example: Missing required flag

```
$ rmp task list
Error: Roadmap not specified. Use -r <name> or --roadmap <name>

usage: rmp task list [-h | --help] [-r <name>] [-s <status>] [-p <n>] [--severity <n>] [-l <n>]

List tasks in the selected roadmap.

Options:
   -r, --roadmap <name>   Roadmap name (required)
   -s, --status <status>  Filter by status: BACKLOG, SPRINT, DOING, TESTING, COMPLETED
   -p, --priority <n>     Filter by minimum priority (0-9)
       --severity <n>     Filter by minimum severity (0-9)
   -l, --limit <n>        Limit number of results

Output: JSON array of task objects

Examples:
   rmp task list -r project1
   rmp task ls -r project1 -s DOING
   rmp task ls -r project1 -p 5 -l 20
```

---

## Exit Codes Reference

The canonical exit-code catalogue lives in `SPEC/ARCHITECTURE.md` — Exit Codes section. In summary:

- `0` success; `1` system/database failure; `2` malformed input or missing required argument
- `3` no roadmap specified; `4` resource not found; `5` resource already exists; `6` validation failed (range, enum, format, length, state-rule)
- `126` not executable; `127` unknown command; `130` interrupted (Ctrl+C)

For sentinel-to-exit-code mappings and the per-error-code table, refer to `ARCHITECTURE.md`.
