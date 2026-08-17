// Package commands — sprint family registry entries.
package commands

func buildSprintCommand() Command {
	return Command{
		Name:          "sprint",
		Aliases:       []string{"s"},
		Summary:       "Manage sprints and their task membership/ordering.",
		Description:   "Create, list, query, mutate, and order sprints and their task assignments.",
		HelpPrinter:   printSprintHelp,
		HasSubcommand: true,
		Prerequisites: []string{"An existing roadmap selected via -r/--roadmap."},
		Subcommands: []Subcommand{
			{
				Name: "list", Aliases: []string{"ls"},
				Summary:     "List sprints.",
				Description: "Lists every sprint in the roadmap, optionally filtered by status.",
				Usage:       "rmp sprint list -r <roadmap> [--status <state>]",
				HelpPrinter: printSprintListHelp,
				Handler:     sprintList,
				Flags: []Flag{
					sharedRoadmapFlag(),
					{Long: "--status", Type: "enum", Enum: "SprintStatus", Description: "Filter by sprint status."},
					helpFlag(),
				},
				Output:      SuccessOutput{Kind: "array", Schema: "Array of sprint objects."},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 6},
				Examples: []Example{
					{Title: "All sprints", Cmd: "rmp sprint list -r myproject", Exit: 0},
					{Title: "Invalid status filter", Cmd: "rmp sprint list -r myproject --status INVALID", Stderr: "Error: validation error: invalid sprint status: \"INVALID\": invalid sprint status", Exit: 6},
				},
			},
			{
				Name: "create", Aliases: []string{"new"},
				Summary:     "Create a new sprint (PENDING).",
				Description: "Creates a new sprint in PENDING status.",
				Usage:       "rmp sprint create -r <roadmap> -t <title> -d <description> [--max-tasks <n>] [--order <n>]",
				HelpPrinter: printSprintCreateHelp,
				Handler:     sprintCreate,
				Flags: []Flag{
					sharedRoadmapFlag(),
					{Long: "--title", Short: "-t", Type: "string", Required: true, MaxLength: 255, Description: "Sprint title."},
					{Long: "--description", Short: "-d", Type: "string", Required: true, MaxLength: 2048, Description: "Sprint description. Must state the high-level (macro) goal of the development effort the sprint delivers: a new development, a fix, a refactoring, or another kind of change. Together with the title, it must give a human or an AI agent a clear macro idea of what the sprint's tasks are specifically aimed at. Detailed scope, technical detail, and acceptance conditions belong in the sprint's tasks, not here."},
					{Long: "--max-tasks", Type: "integer", HasRange: true, RangeMin: 1, RangeMax: 10000, Description: "Capacity cap on active tasks; cannot be removed once set."},
					{Long: "--order", Type: "integer", HasRange: true, RangeMin: 1, Description: "Sprint execution order; positive integer (> 0), unique across the roadmap. Optional: auto-assigned to the highest existing order plus one when omitted. A value already used by another sprint is rejected with exit code 5."},
					helpFlag(),
				},
				Output:      SuccessOutput{Kind: "object", Schema: `{"id": <int>}`, Example: `{"id":1}`},
				SideEffects: SideEffects{Database: "INSERT into sprints + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  false,
				ExitCodes:   []int{0, 2, 3, 5, 6},
				Examples: []Example{
					{Title: "Create", Cmd: `rmp sprint create -r myproject -t "Authentication hardening" -d "Deliver session-based authentication for every write command."`, Stdout: `{"id":1}`, Exit: 0},
					{Title: "Missing required title", Cmd: `rmp sprint create -r myproject -d "Deliver session-based authentication for every write command."`, Stderr: "Error: required parameter missing: --title", Exit: 2},
				},
			},
			{
				Name:        "get",
				Summary:     "Get sprint details (sprint object only).",
				Description: "Returns the sprint object for <sprint-id>.",
				Usage:       "rmp sprint get -r <roadmap> <sprint-id>",
				HelpPrinter: printSprintGetHelp,
				Handler:     sprintGet,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "object", Schema: "Single sprint object."},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4},
				Examples: []Example{
					{Title: "Get sprint", Cmd: "rmp sprint get -r myproject 5", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint get -r myproject 99999", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name:        "show",
				Summary:     "Stand-up-style summary (counts, distributions).",
				Description: "Returns a stand-up summary of the sprint with task counts and per-severity distributions.",
				Usage:       "rmp sprint show -r <roadmap> <sprint-id>",
				HelpPrinter: printSprintShowHelp,
				Handler:     sprintShow,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "object", Schema: "{sprint_id, sprint_title, sprint_description, status, max_tasks, capacity_pct, current_load, task_order, summary, progress, severity_distribution, criticality_distribution}"},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4},
				Examples: []Example{
					{Title: "Show", Cmd: "rmp sprint show -r myproject 5", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint show -r myproject 99999", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name: "update", Aliases: []string{"upd"},
				Summary:     "Update sprint title, description, capacity cap or execution order.",
				Description: "Edits the title, description, capacity cap or execution order of an existing sprint. At least one option required.",
				Usage:       "rmp sprint update -r <roadmap> <sprint-id> [-t <title>] [-d <text>] [--max-tasks <n>] [--order <n>]",
				HelpPrinter: printSprintUpdateHelp,
				Handler:     sprintUpdate,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
				},
				Flags: []Flag{
					sharedRoadmapFlag(),
					{Long: "--title", Short: "-t", Type: "string", MaxLength: 255, Description: "New title."},
					{Long: "--description", Short: "-d", Type: "string", MaxLength: 2048, Description: "New sprint description. Carries the same semantics as on create: it must state the high-level (macro) goal of the development effort the sprint delivers: a new development, a fix, a refactoring, or another kind of change. Together with the title, it must give a human or an AI agent a clear macro idea of what the sprint's tasks are specifically aimed at. Detailed scope, technical detail, and acceptance conditions belong in the sprint's tasks, not here."},
					{Long: "--max-tasks", Type: "integer", HasRange: true, RangeMin: 1, RangeMax: 10000, Description: "New capacity cap."},
					{Long: "--order", Type: "integer", HasRange: true, RangeMin: 1, Description: "New sprint execution order; positive integer (> 0), unique across the roadmap. Allowed only while the sprint is PENDING or OPEN; once CLOSED the order is immutable and a change is rejected with exit code 6. A value already used by another sprint is rejected with exit code 5."},
					helpFlag(),
				},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE sprints + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 2, 3, 4, 5, 6},
				Examples: []Example{
					{Title: "Update description", Cmd: `rmp sprint update -r myproject 5 -d "Deliver authentication and request tracing for every write command."`, Exit: 0},
					{Title: "Sprint not found", Cmd: `rmp sprint update -r myproject 99999 -d "Refactor persistence onto a single write path."`, Stderr: "Error: resource not found: sprint 99999 not found", Exit: 4},
				},
			},
			{
				Name: "remove", Aliases: []string{"rm"},
				Summary:     "Delete sprint (member tasks revert to BACKLOG).",
				Description: "Deletes the sprint; member tasks revert to BACKLOG (not deleted).",
				Usage:       "rmp sprint remove -r <roadmap> <sprint-id>",
				HelpPrinter: printSprintRemoveHelp,
				Handler:     sprintRemove,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "DELETE sprint + UPDATE tasks + audit log; one transaction.", Filesystem: "None.", Network: "None."},
				Idempotent:  false,
				ExitCodes:   []int{0, 3, 4},
				Examples: []Example{
					{Title: "Remove sprint", Cmd: "rmp sprint remove -r myproject 5", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint remove -r myproject 99999", Stderr: "Error: resource not found: sprint 99999 not found", Exit: 4},
				},
			},
			{
				Name:        "start",
				Summary:     "PENDING -> OPEN (sets started_at).",
				Description: "Transitions a sprint from PENDING to OPEN. Only one sprint can be OPEN at a time.",
				Usage:       "rmp sprint start -r <roadmap> <sprint-id>",
				HelpPrinter: printSprintStartHelp,
				Handler:     sprintStart,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE sprints + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  false,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Start sprint", Cmd: "rmp sprint start -r myproject 5", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint start -r myproject 99999", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name:        "close",
				Summary:     "OPEN -> CLOSED (--force bypasses active-task check).",
				Description: "Transitions an OPEN sprint to CLOSED; rejects active tasks unless --force given.",
				Usage:       "rmp sprint close -r <roadmap> <sprint-id> [--force]",
				HelpPrinter: printSprintCloseHelp,
				Handler:     sprintClose,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
				},
				Flags: []Flag{
					sharedRoadmapFlag(),
					{Long: "--force", Type: "boolean", Description: "Close even when SPRINT/DOING/TESTING tasks remain (prints warning to stderr)."},
					helpFlag(),
				},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE sprints + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  false,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Close clean", Cmd: "rmp sprint close -r myproject 5", Exit: 0},
					{Title: "Force close", Cmd: "rmp sprint close -r myproject 5 --force", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint close -r myproject 99999", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name:        "reopen",
				Summary:     "CLOSED -> OPEN (clears closed_at; started_at preserved).",
				Description: "Transitions a CLOSED sprint back to OPEN. Rejected if another sprint is already OPEN.",
				Usage:       "rmp sprint reopen -r <roadmap> <sprint-id>",
				HelpPrinter: printSprintReopenHelp,
				Handler:     sprintReopen,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE sprints + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  false,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Reopen", Cmd: "rmp sprint reopen -r myproject 5", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint reopen -r myproject 99999", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name:        "tasks",
				Summary:     "List ALL tasks in sprint (incl. COMPLETED).",
				Description: "Lists every task assigned to <sprint-id>, regardless of status, optionally filtered by exact task status.",
				Usage:       "rmp sprint tasks -r <roadmap> <sprint-id> [-s <state>] [--order-by-priority]",
				HelpPrinter: printSprintTasksHelp,
				Handler:     sprintTasks,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
				},
				Flags: []Flag{
					sharedRoadmapFlag(),
					{Long: "--status", Short: "-s", Type: "enum", Enum: "TaskStatus", Description: "Filter by exact task status (BACKLOG, SPRINT, DOING, TESTING, COMPLETED)."},
					{Long: "--order-by-priority", Type: "boolean", Description: "Re-sort by priority DESC; default is sprint position ASC."},
					helpFlag(),
				},
				Output:      SuccessOutput{Kind: "array", Schema: "Array of task objects."},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "All sprint tasks", Cmd: "rmp sprint tasks -r myproject 5", Exit: 0},
					{Title: "Filter by status", Cmd: "rmp sprint tasks -r myproject 5 -s DOING", Exit: 0},
					{Title: "Invalid status", Cmd: "rmp sprint tasks -r myproject 5 -s INVALID", Stderr: "Error: validation error: invalid task status: \"INVALID\": invalid task status", Exit: 6},
				},
			},
			{
				Name:        "open-tasks",
				Summary:     "List incomplete tasks in sprint (SPRINT, DOING, TESTING).",
				Description: "Lists incomplete tasks in <sprint-id> (status SPRINT, DOING, or TESTING).",
				Usage:       "rmp sprint open-tasks -r <roadmap> <sprint-id> [--order-by-priority]",
				HelpPrinter: printSprintOpenTasksHelp,
				Handler:     sprintOpenTasks,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
				},
				Flags: []Flag{
					sharedRoadmapFlag(),
					{Long: "--order-by-priority", Type: "boolean", Description: "Sort by priority DESC; otherwise sprint position."},
					helpFlag(),
				},
				Output:      SuccessOutput{Kind: "array", Schema: "Array of task objects (excludes BACKLOG/COMPLETED)."},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4},
				Examples: []Example{
					{Title: "Open tasks", Cmd: "rmp sprint open-tasks -r myproject 5", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint open-tasks -r myproject 99999", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name:        "stats",
				Summary:     "Per-status counts, burndown, velocity, days_*.",
				Description: "Returns SprintStats: per-status counts, burndown, velocity, elapsed days.",
				Usage:       "rmp sprint stats -r <roadmap> <sprint-id>",
				HelpPrinter: printSprintStatsHelp,
				Handler:     sprintStats,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "object", Schema: "SprintStats object."},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4},
				Examples: []Example{
					{Title: "Stats", Cmd: "rmp sprint stats -r myproject 5", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint stats -r myproject 99999", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name: "add-tasks", Aliases: []string{"add"},
				Summary:     "Atomically add BACKLOG tasks -> SPRINT.",
				Description: "Atomically moves listed tasks into <sprint-id> and flips status BACKLOG -> SPRINT.",
				Usage:       "rmp sprint add-tasks -r <roadmap> <sprint-id> <task-ids>",
				HelpPrinter: printSprintAddTasksHelp,
				Handler:     sprintAddTasks,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id (must not be CLOSED)."},
					{Name: "task-ids", Type: "csv:integer", Required: true, Description: "Comma-separated integer task ids."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "INSERT sprint_tasks + UPDATE tasks + audit log; one transaction.", Filesystem: "None.", Network: "None."},
				Idempotent:  false,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Add tasks", Cmd: "rmp sprint add-tasks -r myproject 5 1,3,7", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint add-tasks -r myproject 99999 1", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name: "remove-tasks", Aliases: []string{"rm-tasks"},
				Summary:     "Remove tasks from sprint (revert to BACKLOG).",
				Description: "Removes listed tasks from the sprint and flips status back to BACKLOG.",
				Usage:       "rmp sprint remove-tasks -r <roadmap> <sprint-id> <task-ids>",
				HelpPrinter: printSprintRemoveTasksHelp,
				Handler:     sprintRemoveTasks,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
					{Name: "task-ids", Type: "csv:integer", Required: true, Description: "Comma-separated integer task ids."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "DELETE sprint_tasks + UPDATE tasks + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  false,
				ExitCodes:   []int{0, 3, 4},
				Examples: []Example{
					{Title: "Remove tasks", Cmd: "rmp sprint remove-tasks -r myproject 5 1,3,7", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint remove-tasks -r myproject 99999 1", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name: "move-tasks", Aliases: []string{"mv-tasks"},
				Summary:     "Move tasks between sprints (status preserved).",
				Description: "Moves tasks from one sprint to another in a single transaction.",
				Usage:       "rmp sprint move-tasks -r <roadmap> <from-id> <to-id> <task-ids>",
				HelpPrinter: printSprintMoveTasksHelp,
				Handler:     sprintMoveTasks,
				Positional: []Argument{
					{Name: "from-id", Type: "integer", Required: true, Description: "Source sprint id."},
					{Name: "to-id", Type: "integer", Required: true, Description: "Destination sprint id."},
					{Name: "task-ids", Type: "csv:integer", Required: true, Description: "Comma-separated integer task ids."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE sprint_tasks + audit log; one transaction.", Filesystem: "None.", Network: "None."},
				Idempotent:  false,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Move", Cmd: "rmp sprint move-tasks -r myproject 5 8 3,7", Exit: 0},
					{Title: "Source sprint not found", Cmd: "rmp sprint move-tasks -r myproject 99999 8 1", Stderr: "Error: resource not found: from sprint: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name: "reorder", Aliases: []string{"order"},
				Summary:     "Set exact full ordering (all members in CSV).",
				Description: "Sets the exact ordering of tasks within <sprint-id>. List must include every task.",
				Usage:       "rmp sprint reorder -r <roadmap> <sprint-id> <task-ids>",
				HelpPrinter: printSprintReorderHelp,
				Handler:     sprintReorder,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
					{Name: "task-ids", Type: "csv:integer", Required: true, Description: "Comma-separated task ids in the desired order."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE sprint_tasks positions + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Reorder", Cmd: "rmp sprint reorder -r myproject 5 3,1,7,2", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint reorder -r myproject 99999 1", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name: "move-to", Aliases: []string{"mvto"},
				Summary:     "Move one task to zero-based position.",
				Description: "Moves a single task to an exact position within the sprint (0-based).",
				Usage:       "rmp sprint move-to -r <roadmap> <sprint-id> <task-id> <position>",
				HelpPrinter: printSprintMoveToHelp,
				Handler:     sprintMoveTo,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
					{Name: "task-id", Type: "integer", Required: true, Description: "Integer id of the task to move."},
					{Name: "position", Type: "integer", Required: true, Description: "Zero-based target index."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE sprint_tasks positions + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Move to top", Cmd: "rmp sprint move-to -r myproject 5 7 0", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint move-to -r myproject 99999 1 0", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name:        "swap",
				Summary:     "Swap positions of two tasks.",
				Description: "Exchanges the positions of two tasks within the same sprint.",
				Usage:       "rmp sprint swap -r <roadmap> <sprint-id> <task-id-1> <task-id-2>",
				HelpPrinter: printSprintSwapHelp,
				Handler:     sprintSwap,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
					{Name: "task-id-1", Type: "integer", Required: true, Description: "Integer id of first task."},
					{Name: "task-id-2", Type: "integer", Required: true, Description: "Integer id of second task (must differ)."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE sprint_tasks positions + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4, 6},
				Examples: []Example{
					{Title: "Swap", Cmd: "rmp sprint swap -r myproject 5 3 7", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint swap -r myproject 99999 1 2", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name:        "top",
				Summary:     "Move task to position 0.",
				Description: "Moves a single task to the top of the sprint (position 0).",
				Usage:       "rmp sprint top -r <roadmap> <sprint-id> <task-id>",
				HelpPrinter: printSprintTopHelp,
				Handler:     sprintTop,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
					{Name: "task-id", Type: "integer", Required: true, Description: "Integer id of the task."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE sprint_tasks positions + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4},
				Examples: []Example{
					{Title: "Top", Cmd: "rmp sprint top -r myproject 5 7", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint top -r myproject 99999 7", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			{
				Name: "bottom", Aliases: []string{"btm"},
				Summary:     "Move task to last position.",
				Description: "Moves a single task to the last position of the sprint.",
				Usage:       "rmp sprint bottom -r <roadmap> <sprint-id> <task-id>",
				HelpPrinter: printSprintBottomHelp,
				Handler:     sprintBottom,
				Positional: []Argument{
					{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer sprint id."},
					{Name: "task-id", Type: "integer", Required: true, Description: "Integer id of the task."},
				},
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output:      SuccessOutput{Kind: "empty"},
				SideEffects: SideEffects{Database: "UPDATE sprint_tasks positions + audit log.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4},
				Examples: []Example{
					{Title: "Bottom", Cmd: "rmp sprint bottom -r myproject 5 7", Exit: 0},
					{Title: "Sprint not found", Cmd: "rmp sprint bottom -r myproject 99999 7", Stderr: "Error: resource not found: sprint 99999", Exit: 4},
				},
			},
			sprintCommentAddSubcommand(),
			sprintCommentListSubcommand(),
			sprintCommentEditSubcommand(),
			sprintCommentRemoveSubcommand(),
		},
	}
}

// The four comment subcommands of the sprint family (SPEC/COMMANDS.md § Sprint
// Comments). They are declared in their own constructors for the same reason the
// task ones are: each carries a long, self-contained contract that would bury the
// surrounding entries if inlined.
//
// The type descriptions name SprintCommentType, the enum that carries the FOUR
// sprint values. The sibling task subcommands name TaskCommentType and carry seven:
// the two sets are never conflated into one enum shared by both families
// (SPEC/HELP.md § Comment subcommand help specifics item 1).

// sprintCommentTypeDescription is the `--type` contract text shared by the four
// subcommands, with the per-subcommand role prefixed by the caller.
//
// Unlike the task family, `-y, --type` carries nothing else anywhere in the sprint
// family, so what has to be said is the opposite of the task family's warning: the
// flag has one meaning here, and the values it does NOT accept are the three
// task-only ones (SPEC/COMMANDS.md § Sprint Comments).
const sprintCommentTypeDescription = "It always means a comment type in the sprint family, which uses -y, --type for " +
	"nothing else; the task-only values HYPOTHESIS, TEST and NOTE are rejected with exit code 6."

func sprintCommentAddSubcommand() Subcommand {
	return Subcommand{
		Name: "comment-add", Aliases: []string{"c-add"},
		Summary:     "Add one typed comment to a sprint's log.",
		Description: "Adds one comment to the given sprint, stored with its type, its body and a creation timestamp; updated_at starts null. The log records how the sprint itself went — findings, decisions, progress and the reason behind a change to its definition — not the work done inside one task, which belongs in that task's comments. Comments are accepted in every sprint status, including CLOSED, and no comment changes or gates a sprint's status.",
		Usage:       "rmp sprint comment-add -r <roadmap> <sprint-id> --type <TYPE> [--body <text>]",
		HelpPrinter: printSprintCommentAddHelp,
		Handler:     sprintCommentAdd,
		ReadsStdin:  true,
		Positional: []Argument{
			{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer id of the sprint the comment is attached to."},
		},
		Flags: []Flag{
			sharedRoadmapFlag(),
			commentTypeFlag("SprintCommentType", "REQUIRED. Comment type. "+sprintCommentTypeDescription, true),
			commentBodyFlag("Comment text, max 4096 characters. When the flag is absent the body is read in full from standard input; supplying neither is an error (exit code 2)."),
			helpFlag(),
		},
		Output:      SuccessOutput{Kind: "object", Schema: `{"id": <int>} — the id of the created comment.`, Example: `{"id":4}`},
		SideEffects: SideEffects{Database: "INSERT into sprint_comments plus a SPRINT_COMMENT_CREATE audit entry against the parent sprint; one transaction.", Filesystem: "None.", Network: "None."},
		Idempotent:  false,
		ExitCodes:   []int{0, 1, 2, 3, 4, 6},
		Examples: []Example{
			{
				Title:  "Record a sprint-level decision",
				Cmd:    `rmp sprint comment-add -r myproject 3 --type DECISION --body "Dropped the second migration from the sprint: the schema change it needed is only settled once the expiry work lands."`,
				Stdout: `{"id":4}`,
				Exit:   0,
			},
			{
				Title:  "Body from a pipe (no --body)",
				Cmd:    `cat sprint-retro.txt | rmp sprint comment-add -r myproject 3 --type PROGRESS`,
				Stdout: `{"id":5}`,
				Exit:   0,
			},
			{
				Title:  "Task-only type refused on a sprint",
				Cmd:    `rmp sprint comment-add -r myproject 3 --type HYPOTHESIS --body "..."`,
				Stderr: `Error: validation error: invalid comment type "HYPOTHESIS" for a sprint comment; valid types: FINDING, DECISION, PROGRESS, UPDATE`,
				Exit:   6,
			},
			{
				Title:  "No body from either source",
				Cmd:    `rmp sprint comment-add -r myproject 3 --type UPDATE < /dev/null`,
				Stderr: "Error: required parameter missing: no comment body supplied",
				Exit:   2,
			},
		},
	}
}

func sprintCommentListSubcommand() Subcommand {
	return Subcommand{
		Name: "comment-list", Aliases: []string{"c-ls"},
		Summary:     "List a sprint's comments, oldest first.",
		Description: "Returns every comment of the given sprint in the order the work happened: created_at ascending, with the comment id as the tie-breaker. Read top to bottom, it is the account of how the sprint went. The result set is unbounded — there is no --limit, no --desc and no pagination.",
		Usage:       "rmp sprint comment-list -r <roadmap> <sprint-id> [--type <TYPE>]",
		HelpPrinter: printSprintCommentListHelp,
		Handler:     sprintCommentList,
		Positional: []Argument{
			{Name: "sprint-id", Type: "integer", Required: true, Description: "Integer id of the sprint whose comments are listed."},
		},
		Flags: []Flag{
			sharedRoadmapFlag(),
			commentTypeFlag("SprintCommentType", "Optional filter: return only the comments of this type. "+sprintCommentTypeDescription, false),
			helpFlag(),
		},
		Output:      SuccessOutput{Kind: "array", Schema: "Array of sprint-comment objects (id, sprint_id, type, body, created_at, updated_at); [] when the sprint has no comments or none of the requested type.", Example: `[{"id":4,"sprint_id":3,"type":"DECISION","body":"...","created_at":"2026-03-12T11:15:00.000Z","updated_at":null}]`},
		SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
		Idempotent:  true,
		ExitCodes:   []int{0, 2, 3, 4, 6},
		Examples: []Example{
			{Title: "Whole sprint log", Cmd: "rmp sprint comment-list -r myproject 3", Exit: 0},
			{Title: "Decisions only", Cmd: "rmp sprint comment-list -r myproject 3 --type DECISION", Exit: 0},
			{Title: "Unknown sprint", Cmd: "rmp sprint comment-list -r myproject 99999", Stderr: "Error: resource not found: sprint 99999 not found", Exit: 4},
		},
	}
}

func sprintCommentEditSubcommand() Subcommand {
	return Subcommand{
		Name: "comment-edit", Aliases: []string{"c-edit"},
		Summary:     "Change the type and/or body of one sprint comment.",
		Description: "Edits one existing sprint comment, identified by the comment's own id, and stamps updated_at. At least one of --type and --body is required: unlike sprint update, this command does not succeed as a no-op. The previous body is not retained anywhere — the audit log records that an edit happened, not what it replaced.",
		Usage:       "rmp sprint comment-edit -r <roadmap> <comment-id> [--type <TYPE>] [--body <text>]",
		HelpPrinter: printSprintCommentEditHelp,
		Handler:     sprintCommentEdit,
		ReadsStdin:  true,
		Positional: []Argument{
			{Name: "comment-id", Type: "integer", Required: true, Description: "Integer id of the COMMENT itself, not of the sprint it belongs to; sprint and task comment ids are separate sequences."},
		},
		Flags: []Flag{
			sharedRoadmapFlag(),
			commentTypeFlag("SprintCommentType", "New comment type. "+sprintCommentTypeDescription, false),
			commentBodyFlag("New comment text, max 4096 characters. When --body is absent AND --type is absent, the new body is read in full from standard input; when --type is present and --body is absent, the body is left unchanged and standard input is not read, so a type-only edit never waits for input."),
			helpFlag(),
		},
		Output:      SuccessOutput{Kind: "empty"},
		SideEffects: SideEffects{Database: "UPDATE sprint_comments plus a SPRINT_COMMENT_UPDATE audit entry against the parent sprint; one transaction.", Filesystem: "None.", Network: "None."},
		Idempotent:  true,
		ExitCodes:   []int{0, 1, 2, 3, 4, 6},
		Examples: []Example{
			{Title: "Reclassify", Cmd: "rmp sprint comment-edit -r myproject 4 --type UPDATE", Exit: 0},
			{Title: "Replace the body from a file", Cmd: "rmp sprint comment-edit -r myproject 4 < revised.txt", Exit: 0},
			{Title: "Nothing requested", Cmd: "rmp sprint comment-edit -r myproject 4 < /dev/null", Stderr: "Error: required parameter missing: at least one of --type or --body is required", Exit: 2},
			{Title: "Unknown comment", Cmd: `rmp sprint comment-edit -r myproject 99999 --type UPDATE`, Stderr: "Error: resource not found: sprint comment 99999 not found", Exit: 4},
		},
	}
}

func sprintCommentRemoveSubcommand() Subcommand {
	return Subcommand{
		Name: "comment-remove", Aliases: []string{"c-rm"},
		Summary:     "Delete one sprint comment (irreversible).",
		Description: "Deletes one sprint comment, identified by the comment's own id. The row is removed outright: there is no soft delete and no recovery. The audit entry outlives the row, so the sprint's history still records that a comment existed and was removed. Exactly one id is accepted — no comma-separated list.",
		Usage:       "rmp sprint comment-remove -r <roadmap> <comment-id>",
		HelpPrinter: printSprintCommentRemoveHelp,
		Handler:     sprintCommentRemove,
		Positional: []Argument{
			{Name: "comment-id", Type: "integer", Required: true, Description: "Integer id of the COMMENT itself, not of the sprint it belongs to; sprint and task comment ids are separate sequences."},
		},
		Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
		Output:      SuccessOutput{Kind: "empty"},
		SideEffects: SideEffects{Database: "DELETE from sprint_comments plus a SPRINT_COMMENT_DELETE audit entry against the parent sprint; one transaction.", Filesystem: "None.", Network: "None."},
		Idempotent:  false,
		ExitCodes:   []int{0, 1, 2, 3, 4},
		Examples: []Example{
			{Title: "Remove a comment", Cmd: "rmp sprint comment-remove -r myproject 4", Exit: 0},
			{Title: "Unknown comment", Cmd: "rmp sprint comment-remove -r myproject 99999", Stderr: "Error: resource not found: sprint comment 99999 not found", Exit: 4},
		},
	}
}
