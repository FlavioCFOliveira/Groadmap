// Package commands — task family registry entries.
package commands

// taskTextFlags returns the shared title / requirements flag set used
// by `task create` (all required) and `task edit` (all optional). The
// caller flips Required as appropriate.
func taskTextFlags(required bool) []Flag {
	return []Flag{
		{Long: "--title", Short: "-t", Type: "string", Required: required, MaxLength: 255, Description: "Task title."},
		{Long: "--functional-requirements", Short: "-fr", Type: "string", Required: required, MaxLength: 4096, Description: "Why? Functional requirements."},
		{Long: "--technical-requirements", Short: "-tr", Type: "string", Required: required, MaxLength: 4096, Description: "How? Technical description."},
		{Long: "--acceptance-criteria", Short: "-ac", Type: "string", Required: required, MaxLength: 4096, Description: "How to verify? Completion criteria."},
	}
}

// taskCommonOptionalFlags returns the optional task fields shared
// across create and edit.
func taskCommonOptionalFlags() []Flag {
	return []Flag{
		{Long: "--type", Short: "-y", Type: "enum", Enum: "TaskType", Default: "TASK", Description: "Task type."},
		{Long: "--priority", Short: "-p", Type: "integer", HasRange: true, RangeMin: 0, RangeMax: 9, Default: "0", Description: "Priority (0 lowest, 9 highest)."},
		{Long: "--severity", Type: "integer", HasRange: true, RangeMin: 0, RangeMax: 9, Default: "0", Description: "Severity (0 lowest, 9 highest)."},
	}
}

func buildTaskCommand() Command {
	return Command{
		Name:    "task",
		Aliases: []string{"t"},
		Summary: "Manage tasks across statuses BACKLOG/SPRINT/DOING/TESTING/COMPLETED.",
		Description: "Create, list, query, edit, transition, and dependency-manage tasks within a roadmap, " +
			"and keep each task's append-oriented comment log (the comment-* subcommands).",
		HelpPrinter:   printTaskHelp,
		HasSubcommand: true,
		Prerequisites: []string{"An existing roadmap selected via -r/--roadmap."},
		Subcommands: []Subcommand{
			{
				Name: "list", Aliases: []string{"ls"},
				Summary:     "List tasks (any status; filter with --status).",
				Description: "Returns tasks in the given roadmap across every status.",
				Usage:       "rmp task list -r <roadmap> [filters]",
				HelpPrinter: printTaskListHelp,
				Handler:     taskList,
				Flags: []Flag{
					sharedRoadmapFlag(),
					{Long: "--status", Short: "-s", Type: "enum", Enum: "TaskStatus", Description: "Exact status."},
					{Long: "--priority", Short: "-p", Type: "integer", HasRange: true, RangeMin: 0, RangeMax: 9, Description: "Filter: priority >= <min>."},
					{Long: "--severity", Type: "integer", HasRange: true, RangeMin: 0, RangeMax: 9, Description: "Filter: severity >= <min>."},
					{Long: "--type", Short: "-y", Type: "enum", Enum: "TaskType", Description: "Filter by task type."},
					{Long: "--created-since", Type: "date", Description: "Include tasks created on/after this date (RFC3339 or YYYY-MM-DD)."},
					{Long: "--created-until", Type: "date", Description: "Include tasks created on/before this date."},
					{Long: "--sort", Type: "enum", Enum: "TaskSort", Default: "priority", Description: "Sort field."},
					{Long: "--limit", Short: "-l", Type: "integer", HasRange: true, RangeMin: 1, RangeMax: 100, Default: "100", Description: "Maximum results."},
					helpFlag(),
				},
				Output:      SuccessOutput{Kind: "array", Schema: "Array of task objects."},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 6},
				Examples: []Example{
					{Title: "All tasks", Cmd: "rmp task list -r myproject", Exit: 0},
					{Title: "Filter BACKLOG p>=7", Cmd: "rmp task list -r myproject --status BACKLOG --priority 7", Exit: 0},
					{Title: "Bad sort", Cmd: "rmp task list -r myproject --sort foo", Stderr: "Error: --sort must be one of: priority, created, status, severity", Exit: 6},
				},
			},
			{
				Name: "create", Aliases: []string{"new"},
				Summary:     "Create a new task (lands in BACKLOG).",
				Description: "Creates a new task in BACKLOG status.",
				Usage:       "rmp task create -r <roadmap> -t <title> -fr <FR> -tr <TR> -ac <AC> [options]",
				HelpPrinter: printTaskCreateHelp,
				Handler:     taskCreate,
				Flags: append(append(append([]Flag{sharedRoadmapFlag()}, taskTextFlags(true)...), taskCommonOptionalFlags()...),
					Flag{Long: "--parent", Type: "integer", Description: "Parent task ID; creates this task as a sub-task of the given parent."},
					helpFlag()),
				Output:      SuccessOutput{Kind: "object", Schema: `{"id": <int>}`, Example: `{"id":42}`},
				SideEffects: SideEffects{Database: "INSERT into tasks and audit log; one transaction.", Filesystem: "None.", Network: "None."},
				Idempotent:  false,
				ExitCodes:   []int{0, 2, 3, 4, 6},
				Examples: []Example{
					{Title: "Create a task", Cmd: `rmp task create -r myproject -t "Fix bug" -fr "User can login" -tr "Update auth" -ac "Login works"`, Stdout: `{"id":42}`, Exit: 0},
					{Title: "Missing required", Cmd: "rmp task create -r myproject", Stderr: "Error: required parameter missing: --title", Exit: 2},
				},
			},
			{
				Name:        "get",
				Summary:     "Get tasks by id (CSV, no spaces).",
				Description: "Returns one or more tasks by id; fail-fast on any unknown id.",
				Usage:       "rmp task get -r <roadmap> <task-ids>",
				HelpPrinter: printTaskGetHelp,
				Handler:     taskGet,
				Positional: []Argument{
					{Name: "task-ids", Type: "csv:integer", Required: true, Description: "Comma-separated integer ids."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "array", Schema: "Array of task objects."},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Single task", Cmd: "rmp task get -r myproject 1", Exit: 0},
					{Title: "Bulk", Cmd: "rmp task get -r myproject 1,3,5", Exit: 0},
					{Title: "Unknown id", Cmd: "rmp task get -r myproject 99999", Stderr: "Error: resource not found: some tasks not found", Exit: 4},
				},
			},
			{
				Name:        "next",
				Summary:     "Get next [num] incomplete tasks from the OPEN sprint.",
				Description: "Returns the next N incomplete tasks from the OPEN sprint, in sprint position order.",
				Usage:       "rmp task next -r <roadmap> [num]",
				HelpPrinter: printTaskNextHelp,
				Handler:     taskNext,
				Positional: []Argument{
					{Name: "num", Type: "integer", Required: false, Description: "Maximum tasks to return (default 1, clamped to 100)."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "array", Schema: "Array of task objects (SPRINT/DOING/TESTING)."},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Next task", Cmd: "rmp task next -r myproject", Exit: 0},
					{Title: "Next 5", Cmd: "rmp task next -r myproject 5", Exit: 0},
					{Title: "No open sprint", Cmd: "rmp task next -r myproject", Stderr: "Error: resource not found: no sprint is currently open. Use 'rmp sprint start <id>' to open a sprint first", Exit: 4},
				},
			},
			{
				Name:        "edit",
				Summary:     "Edit fields of a task (status NOT editable here).",
				Description: "Edits one or more fields on an existing task. At least one option must be provided.",
				Usage:       "rmp task edit -r <roadmap> <task-id> [options]",
				HelpPrinter: printTaskEditHelp,
				Handler:     taskEdit,
				Positional: []Argument{
					{Name: "task-id", Type: "integer", Required: true, Description: "Integer id of the task."},
				},
				Flags:       append(append(append([]Flag{sharedRoadmapFlag()}, taskTextFlags(false)...), taskCommonOptionalFlags()...), helpFlag()),
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE tasks and audit log; one transaction.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Change title", Cmd: `rmp task edit -r myproject 42 -t "Updated title"`, Exit: 0},
					{Title: "Change priority", Cmd: "rmp task edit -r myproject 42 -p 8", Exit: 0},
					{Title: "Unknown task", Cmd: `rmp task edit -r myproject 99999 -t "x"`, Stderr: "Error: resource not found: task 99999 not found", Exit: 4},
				},
			},
			{
				Name: "remove", Aliases: []string{"rm"},
				Summary:     "Remove task(s) — BACKLOG only, no active subtasks.",
				Description: "Removes one or more tasks. All must be in BACKLOG and free of active subtasks.",
				Usage:       "rmp task remove -r <roadmap> <task-ids>",
				HelpPrinter: printTaskRemoveHelp,
				Handler:     taskRemove,
				Positional: []Argument{
					{Name: "task-ids", Type: "csv:integer", Required: true, Description: "Comma-separated integer ids."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "DELETE from tasks and audit log; one transaction.", Filesystem: "None.", Network: "None."},
				Idempotent:  false,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Remove one task", Cmd: "rmp task remove -r myproject 7", Exit: 0},
					{Title: "Remove non-BACKLOG", Cmd: "rmp task remove -r myproject 3", Stderr: "Error: task #3 cannot be deleted — status is SPRINT, must be BACKLOG", Exit: 6},
				},
			},
			{
				Name: "stat", Aliases: []string{"set-status"},
				Summary:     "Set task status (manual transitions; SPRINT is rejected).",
				Description: "Changes the status of one or more tasks; rejected transitions return exit 6. Entering DOING requires --commit-open and entering COMPLETED requires --commit-close: the caller supplies the git commit hash, and rmp runs no git command and reads no repository. A single hash applies to every id of a multi-id invocation.",
				Usage:       "rmp task stat -r <roadmap> <task-ids> <new-status> [--commit-open <hash>] [--commit-close <hash>] [--summary <text>]",
				HelpPrinter: printTaskStatHelp,
				Handler:     taskSetStatus,
				Positional: []Argument{
					{Name: "task-ids", Type: "csv:integer", Required: true, Description: "Comma-separated integer ids."},
					{Name: "new-status", Type: "enum", Enum: "TaskStatus", Required: true, Description: "Target status (BACKLOG, DOING, TESTING, COMPLETED)."},
				},
				Flags: []Flag{
					sharedRoadmapFlag(),
					{Long: "--commit-open", Short: "-co", Type: "string", MinLength: 7, MaxLength: 64, Description: "Git commit hash the work starts from, 7 to 64 hexadecimal characters, accepted in any letter case and stored lowercase. Mandatory when new-status is DOING and rejected for every other target status, so it is not required for the subcommand as a whole."},
					{Long: "--commit-close", Short: "-cc", Type: "string", MinLength: 7, MaxLength: 64, Description: "Git commit hash the work is concluded at, 7 to 64 hexadecimal characters, accepted in any letter case and stored lowercase. Mandatory when new-status is COMPLETED and rejected for every other target status, so it is not required for the subcommand as a whole."},
					{Long: "--summary", Short: "-s", Type: "string", MaxLength: 4096, Description: "Completion summary (only when new-status is COMPLETED; optional there)."},
					helpFlag(),
				},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE tasks + audit log; one transaction. Entering DOING writes commit_open, entering COMPLETED writes commit_close, and returning to BACKLOG clears commit_close while preserving commit_open.", Filesystem: "None.", Network: "None."},
				Idempotent:  false,
				ExitCodes:   []int{0, 2, 3, 4, 6},
				Examples: []Example{
					{Title: "Move to DOING", Cmd: "rmp task stat -r myproject 1 DOING --commit-open 5f93b51", Exit: 0},
					{Title: "Complete with summary", Cmd: `rmp task stat -r myproject 7 COMPLETED --commit-close 2578d18 --summary "Shipped"`, Exit: 0},
					{Title: "Reject manual SPRINT", Cmd: "rmp task stat -r myproject 1 SPRINT", Stderr: "Error: status SPRINT can only be set automatically via 'sprint add-tasks'", Exit: 6},
					{Title: "Reject DOING without a commit hash", Cmd: "rmp task stat -r myproject 1 DOING", Stderr: "Error: --commit-open is required when transitioning to DOING", Exit: 6},
					{Title: "Reject COMPLETED without a commit hash", Cmd: "rmp task stat -r myproject 7 COMPLETED", Stderr: "Error: --commit-close is required when transitioning to COMPLETED", Exit: 6},
					{Title: "Reject a malformed commit hash", Cmd: "rmp task stat -r myproject 1 DOING --commit-open zzzzzzz", Stderr: `Error: invalid commit hash for --commit-open: "zzzzzzz" (expected 7 to 64 hexadecimal characters)`, Exit: 6},
				},
			},
			{
				Name:        "reopen",
				Summary:     "Reopen task(s) to BACKLOG, clearing lifecycle timestamps.",
				Description: "Returns one or more tasks to BACKLOG and clears started_at/tested_at/closed_at/completion_summary/commit_close. commit_open is preserved: the commit the work started from stays a true historical fact after a return to the backlog, and no command ever clears it. A later 'task stat <ids> DOING --commit-open <hash>' replaces it.",
				Usage:       "rmp task reopen -r <roadmap> <task-ids>",
				HelpPrinter: printTaskReopenHelp,
				Handler:     taskReopen,
				Positional: []Argument{
					{Name: "task-ids", Type: "csv:integer", Required: true, Description: "Comma-separated integer ids."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE tasks + audit log per task; one transaction.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Reopen one", Cmd: "rmp task reopen -r myproject 7", Exit: 0},
					{Title: "Reopen bulk", Cmd: "rmp task reopen -r myproject 1,3,5", Exit: 0},
					{Title: "Unknown id", Cmd: "rmp task reopen -r myproject 99999", Stderr: "Error: resource not found: some tasks not found", Exit: 4},
				},
			},
			{
				Name: "prio", Aliases: []string{"set-priority"},
				Summary:     "Set task priority (0-9) for one or many tasks.",
				Description: "Sets the priority of one or more tasks to the same value.",
				Usage:       "rmp task prio -r <roadmap> <task-ids> <priority>",
				HelpPrinter: printTaskPrioHelp,
				Handler:     taskSetPriority,
				Positional: []Argument{
					{Name: "task-ids", Type: "csv:integer", Required: true, Description: "Comma-separated integer ids."},
					{Name: "priority", Type: "integer", Required: true, Description: "Integer 0-9 (0 lowest, 9 highest)."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE tasks + audit log; one transaction.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Bulk reprioritise", Cmd: "rmp task prio -r myproject 1,2,3 8", Exit: 0},
					{Title: "Unknown id", Cmd: "rmp task prio -r myproject 99999 5", Stderr: "Error: resource not found: some tasks not found", Exit: 4},
				},
			},
			{
				Name: "sev", Aliases: []string{"set-severity"},
				Summary:     "Set task severity (0-9) for one or many tasks.",
				Description: "Sets the severity of one or more tasks to the same value.",
				Usage:       "rmp task sev -r <roadmap> <task-ids> <severity>",
				HelpPrinter: printTaskSevHelp,
				Handler:     taskSetSeverity,
				Positional: []Argument{
					{Name: "task-ids", Type: "csv:integer", Required: true, Description: "Comma-separated integer ids."},
					{Name: "severity", Type: "integer", Required: true, Description: "Integer 0-9."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE tasks + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Single", Cmd: "rmp task sev -r myproject 5 9", Exit: 0},
					{Title: "Unknown id", Cmd: "rmp task sev -r myproject 99999 5", Stderr: "Error: resource not found: some tasks not found", Exit: 4},
				},
			},
			{
				Name:        "subtasks",
				Summary:     "List direct subtasks (one level; no grand-children).",
				Description: "Lists the direct subtasks of <task-id>.",
				Usage:       "rmp task subtasks -r <roadmap> <task-id>",
				HelpPrinter: printTaskSubtasksHelp,
				Handler:     taskSubtasks,
				Positional: []Argument{
					{Name: "task-id", Type: "integer", Required: true, Description: "Integer task id (parent)."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "array", Schema: "Array of task objects."},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4},
				Examples: []Example{
					{Title: "List subtasks", Cmd: "rmp task subtasks -r myproject 5", Exit: 0},
					{Title: "Unknown task", Cmd: "rmp task subtasks -r myproject 99999", Stderr: "Error: resource not found: task 99999", Exit: 4},
				},
			},
			{
				Name:        "add-dep",
				Summary:     "Declare task <id> depends on task <dep-id> (cycles rejected).",
				Description: "Records that <task-id> depends on <blocker-id>. Self-edges and cycles are rejected.",
				Usage:       "rmp task add-dep -r <roadmap> <task-id> <blocker-id>",
				HelpPrinter: printTaskAddDepHelp,
				Handler:     taskAddDep,
				Positional: []Argument{
					{Name: "task-id", Type: "integer", Required: true, Description: "Integer id of the dependent task."},
					{Name: "blocker-id", Type: "integer", Required: true, Description: "Integer id of the task that must complete first."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "INSERT into task_dependencies + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Add dep", Cmd: "rmp task add-dep -r myproject 10 7", Exit: 0},
					{Title: "Unknown dependent task", Cmd: "rmp task add-dep -r myproject 99999 7", Stderr: "Error: task #99999 not found: resource not found: task 99999", Exit: 4},
				},
			},
			{
				Name:        "remove-dep",
				Summary:     "Remove the dependency edge created by add-dep.",
				Description: "Removes the dependency of <task-id> on <blocker-id>.",
				Usage:       "rmp task remove-dep -r <roadmap> <task-id> <blocker-id>",
				HelpPrinter: printTaskRemoveDepHelp,
				Handler:     taskRemoveDep,
				Positional: []Argument{
					{Name: "task-id", Type: "integer", Required: true, Description: "Integer id of the dependent task."},
					{Name: "blocker-id", Type: "integer", Required: true, Description: "Integer id of the task that was a blocker."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "DELETE from task_dependencies + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  false,
				ExitCodes:   []int{0, 3, 4},
				Examples: []Example{
					{Title: "Remove dep", Cmd: "rmp task remove-dep -r myproject 10 7", Exit: 0},
					{Title: "No such dependency", Cmd: "rmp task remove-dep -r myproject 99999 88888", Stderr: "Error: resource not found: dependency from task #99999 to task #88888 not found", Exit: 4},
				},
			},
			{
				Name:        "blockers",
				Summary:     "List tasks blocking <id> (incomplete dependencies).",
				Description: "Returns tasks that <task-id> depends on and that are not yet COMPLETED.",
				Usage:       "rmp task blockers -r <roadmap> <task-id>",
				HelpPrinter: printTaskBlockersHelp,
				Handler:     taskBlockers,
				Positional: []Argument{
					{Name: "task-id", Type: "integer", Required: true, Description: "Integer task id."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "array", Schema: "Array of task objects (incomplete dependencies)."},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4},
				Examples: []Example{
					{Title: "Blockers", Cmd: "rmp task blockers -r myproject 10", Exit: 0},
					{Title: "Unknown task", Cmd: "rmp task blockers -r myproject 99999", Stderr: "Error: resource not found: task 99999", Exit: 4},
				},
			},
			{
				Name:        "blocking",
				Summary:     "List tasks that depend on <id> (reverse of blockers).",
				Description: "Returns tasks that depend on <task-id>.",
				Usage:       "rmp task blocking -r <roadmap> <task-id>",
				HelpPrinter: printTaskBlockingHelp,
				Handler:     taskBlocking,
				Positional: []Argument{
					{Name: "task-id", Type: "integer", Required: true, Description: "Integer task id."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "array", Schema: "Array of task objects (downstream dependents)."},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4},
				Examples: []Example{
					{Title: "Blocking", Cmd: "rmp task blocking -r myproject 7", Exit: 0},
					{Title: "Unknown task", Cmd: "rmp task blocking -r myproject 99999", Stderr: "Error: resource not found: task 99999", Exit: 4},
				},
			},
			taskCommentAddSubcommand(),
			taskCommentListSubcommand(),
			taskCommentEditSubcommand(),
			taskCommentRemoveSubcommand(),
		},
	}
}

// The four comment subcommands of the task family (SPEC/COMMANDS.md § Task
// Comments). They are declared in their own constructors rather than inline
// because each carries a long, self-contained contract, and because `-y, --type`
// needs one paragraph per subcommand to keep it apart from the TaskType the same
// flag spelling carries on list / create / edit.
//
// The type descriptions name TaskCommentType, the enum the sibling sprint
// subcommands must NOT reach: the seven task values and the four sprint values are
// two sets, never one (SPEC/HELP.md § Comment subcommand help specifics item 1).

// taskCommentTypeDescription is the `--type` contract text shared by the four
// subcommands, with the per-subcommand role prefixed by the caller.
const taskCommentTypeDescription = "It carries a comment type here, NOT the TaskType that the same -y, --type " +
	"spelling carries on task list / create / edit; a TaskType value such as BUG is rejected with exit code 6."

func taskCommentAddSubcommand() Subcommand {
	return Subcommand{
		Name: "comment-add", Aliases: []string{"c-add"},
		Summary:     "Add one typed comment to a task's work log.",
		Description: "Adds one comment to the given task, stored with its type, its body and a creation timestamp; updated_at starts null. Comments are accepted in every task status, including COMPLETED, and no comment changes or gates a task's status.",
		Usage:       "rmp task comment-add -r <roadmap> <task-id> --type <TYPE> [--body <text>]",
		HelpPrinter: printTaskCommentAddHelp,
		Handler:     taskCommentAdd,
		ReadsStdin:  true,
		Positional: []Argument{
			{Name: "task-id", Type: "integer", Required: true, Description: "Integer id of the task the comment is attached to."},
		},
		Flags: []Flag{
			sharedRoadmapFlag(),
			commentTypeFlag("TaskCommentType", "REQUIRED. Comment type. "+taskCommentTypeDescription, true),
			commentBodyFlag("Comment text, max 4096 characters. When the flag is absent the body is read from standard input under a bounded read; supplying neither is an error (exit code 2)."),
			helpFlag(),
		},
		Output:      SuccessOutput{Kind: "object", Schema: `{"id": <int>} — the id of the created comment.`, Example: `{"id":12}`},
		SideEffects: SideEffects{Database: "INSERT into task_comments plus a TASK_COMMENT_CREATE audit entry against the parent task; one transaction.", Filesystem: "None.", Network: "None."},
		Idempotent:  false,
		ExitCodes:   []int{0, 1, 2, 3, 4, 6},
		Examples: []Example{
			{
				Title:  "Record a finding",
				Cmd:    `rmp task comment-add -r myproject 42 --type FINDING --body "time.Now().After(exp) is false at equality, so the boundary second is accepted by the parser and refused by the handler."`,
				Stdout: `{"id":12}`,
				Exit:   0,
			},
			{
				Title:  "Body from a pipe (no --body)",
				Cmd:    `cat decision.txt | rmp task comment-add -r myproject 42 --type DECISION`,
				Stdout: `{"id":13}`,
				Exit:   0,
			},
			{
				Title:  "Type outside the task comment set",
				Cmd:    `rmp task comment-add -r myproject 42 --type BUG --body "..."`,
				Stderr: `Error: validation error: invalid comment type "BUG" for a task comment; valid types: FINDING, HYPOTHESIS, TEST, DECISION, PROGRESS, UPDATE, NOTE`,
				Exit:   6,
			},
			{
				Title:  "No body from either source",
				Cmd:    `rmp task comment-add -r myproject 42 --type NOTE < /dev/null`,
				Stderr: "Error: required parameter missing: no comment body supplied",
				Exit:   2,
			},
		},
	}
}

func taskCommentListSubcommand() Subcommand {
	return Subcommand{
		Name: "comment-list", Aliases: []string{"c-ls"},
		Summary:     "List a task's comments, oldest first.",
		Description: "Returns every comment of the given task in the order the work happened: created_at ascending, with the comment id as the tie-breaker. The result set is unbounded — there is no --limit, no --desc and no pagination.",
		Usage:       "rmp task comment-list -r <roadmap> <task-id> [--type <TYPE>]",
		HelpPrinter: printTaskCommentListHelp,
		Handler:     taskCommentList,
		Positional: []Argument{
			{Name: "task-id", Type: "integer", Required: true, Description: "Integer id of the task whose comments are listed."},
		},
		Flags: []Flag{
			sharedRoadmapFlag(),
			commentTypeFlag("TaskCommentType", "Optional filter: return only the comments of this type. "+taskCommentTypeDescription, false),
			helpFlag(),
		},
		Output:      SuccessOutput{Kind: "array", Schema: "Array of task-comment objects (id, task_id, type, body, created_at, updated_at); [] when the task has no comments or none of the requested type.", Example: `[{"id":12,"task_id":42,"type":"FINDING","body":"...","created_at":"2026-03-12T11:15:00.000Z","updated_at":null}]`},
		SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
		Idempotent:  true,
		ExitCodes:   []int{0, 2, 3, 4, 6},
		Examples: []Example{
			{Title: "Whole work log", Cmd: "rmp task comment-list -r myproject 42", Exit: 0},
			{Title: "Decisions only", Cmd: "rmp task comment-list -r myproject 42 --type DECISION", Exit: 0},
			{Title: "Unknown task", Cmd: "rmp task comment-list -r myproject 99999", Stderr: "Error: resource not found: task 99999 not found", Exit: 4},
		},
	}
}

func taskCommentEditSubcommand() Subcommand {
	return Subcommand{
		Name: "comment-edit", Aliases: []string{"c-edit"},
		Summary:     "Change the type and/or body of one task comment.",
		Description: "Edits one existing task comment, identified by the comment's own id, and stamps updated_at. At least one of --type and --body is required: unlike task edit, this command does not succeed as a no-op. The previous body is not retained anywhere — the audit log records that an edit happened, not what it replaced.",
		Usage:       "rmp task comment-edit -r <roadmap> <comment-id> [--type <TYPE>] [--body <text>]",
		HelpPrinter: printTaskCommentEditHelp,
		Handler:     taskCommentEdit,
		ReadsStdin:  true,
		Positional: []Argument{
			{Name: "comment-id", Type: "integer", Required: true, Description: "Integer id of the COMMENT itself, not of the task it belongs to; task and sprint comment ids are separate sequences."},
		},
		Flags: []Flag{
			sharedRoadmapFlag(),
			commentTypeFlag("TaskCommentType", "New comment type. "+taskCommentTypeDescription, false),
			commentBodyFlag("New comment text, max 4096 characters. When --body is absent AND --type is absent, the new body is read from standard input under a bounded read; when --type is present and --body is absent, the body is left unchanged and standard input is not read, so a type-only edit never waits for input."),
			helpFlag(),
		},
		Output:      SuccessOutput{Kind: "empty"},
		SideEffects: SideEffects{Database: "UPDATE task_comments plus a TASK_COMMENT_UPDATE audit entry against the parent task; one transaction.", Filesystem: "None.", Network: "None."},
		Idempotent:  true,
		ExitCodes:   []int{0, 1, 2, 3, 4, 6},
		Examples: []Example{
			{Title: "Reclassify", Cmd: "rmp task comment-edit -r myproject 12 --type DECISION", Exit: 0},
			{Title: "Replace the body from a file", Cmd: "rmp task comment-edit -r myproject 12 < revised.txt", Exit: 0},
			{Title: "Nothing requested", Cmd: "rmp task comment-edit -r myproject 12 < /dev/null", Stderr: "Error: required parameter missing: at least one of --type or --body is required", Exit: 2},
			{Title: "Unknown comment", Cmd: `rmp task comment-edit -r myproject 99999 --type NOTE`, Stderr: "Error: resource not found: task comment 99999 not found", Exit: 4},
		},
	}
}

func taskCommentRemoveSubcommand() Subcommand {
	return Subcommand{
		Name: "comment-remove", Aliases: []string{"c-rm"},
		Summary:     "Delete one task comment (irreversible).",
		Description: "Deletes one task comment, identified by the comment's own id. The row is removed outright: there is no soft delete and no recovery. The audit entry outlives the row, so the task's history still records that a comment existed and was removed. Exactly one id is accepted — no comma-separated list.",
		Usage:       "rmp task comment-remove -r <roadmap> <comment-id>",
		HelpPrinter: printTaskCommentRemoveHelp,
		Handler:     taskCommentRemove,
		Positional: []Argument{
			{Name: "comment-id", Type: "integer", Required: true, Description: "Integer id of the COMMENT itself, not of the task it belongs to; task and sprint comment ids are separate sequences."},
		},
		Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
		Output:      SuccessOutput{Kind: "empty"},
		SideEffects: SideEffects{Database: "DELETE from task_comments plus a TASK_COMMENT_DELETE audit entry against the parent task; one transaction.", Filesystem: "None.", Network: "None."},
		Idempotent:  false,
		ExitCodes:   []int{0, 1, 2, 3, 4},
		Examples: []Example{
			{Title: "Remove a comment", Cmd: "rmp task comment-remove -r myproject 12", Exit: 0},
			{Title: "Unknown comment", Cmd: "rmp task comment-remove -r myproject 99999", Stderr: "Error: resource not found: task comment 99999 not found", Exit: 4},
		},
	}
}
