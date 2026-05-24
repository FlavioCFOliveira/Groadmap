// Package commands — registry data declarations.
//
// This file builds the singleton Registry instance returned by
// AppRegistry(). Adding a new flag, alias, exit code, or example to
// any command means editing exactly one entry in this file; both the
// runtime dispatch (via registry.go) and the future AI-contract
// emitter pick the change up automatically.
package commands

import "sync"

var (
	registryOnce sync.Once
	registry     *Registry
)

// AppRegistry returns the singleton CLI registry, built on first call.
// The registry is process-wide and immutable after construction.
func AppRegistry() *Registry {
	registryOnce.Do(func() {
		registry = buildRegistry()
	})
	return registry
}

// buildRegistry assembles every Command in declaration order. Adding a
// new command family is one extra entry here plus the corresponding
// build* function.
func buildRegistry() *Registry {
	return &Registry{
		Globals: buildGlobalFlags(),
		Commands: []Command{
			buildRoadmapCommand(),
			buildTaskCommand(),
			buildSprintCommand(),
			buildBacklogCommand(),
			buildAuditCommand(),
			buildStatsCommand(),
		},
	}
}

// buildGlobalFlags lists every flag the binary recognises at the top
// level. --ai-help is intentionally absent here: it is introduced by
// task 2 of the AI-help sprint sequence; until then the registry
// reflects only the existing surface.
func buildGlobalFlags() []Flag {
	return []Flag{
		{
			Long:        "--help",
			Short:       "-h",
			Type:        "boolean",
			Description: "Show the global help message and exit.",
		},
		{
			Long:        "--version",
			Short:       "-v",
			Type:        "boolean",
			Description: "Print the application version and exit.",
		},
	}
}

// sharedRoadmapFlag is the -r / --roadmap flag attached to every
// subcommand except those under the `roadmap` family. It is duplicated
// into each subcommand's Flags slice (rather than living once on the
// Command level) so the AI contract emitter can render a complete
// per-subcommand flag list in a single pass.
func sharedRoadmapFlag() Flag {
	return Flag{
		Long:        "--roadmap",
		Short:       "-r",
		Type:        "string",
		Required:    true,
		MinLength:   1,
		MaxLength:   50,
		Description: "Target roadmap name (regex ^[a-z0-9_-]+$, max 50 chars).",
	}
}

// helpFlag is the -h / --help flag attached to every subcommand. It is
// handled by the dispatcher (Command.DispatchFamily) before any
// validation runs.
func helpFlag() Flag {
	return Flag{
		Long:        "--help",
		Short:       "-h",
		Type:        "boolean",
		Description: "Show help for this subcommand and exit.",
	}
}

// =====================================================================
// roadmap
// =====================================================================

func buildRoadmapCommand() Command {
	return Command{
		Name:          "roadmap",
		Aliases:       []string{"road"},
		Summary:       "Create, list, and remove roadmap database files (~/.roadmaps/<name>.db).",
		Description:   "Manages roadmap databases stored as individual SQLite files under ~/.roadmaps/.",
		HelpPrinter:   printRoadmapHelp,
		HasSubcommand: true,
		Subcommands: []Subcommand{
			{
				Name:        "list",
				Aliases:     []string{"ls"},
				Summary:     "List all roadmaps.",
				Description: "Lists every roadmap database file found under ~/.roadmaps/.",
				Usage:       "rmp roadmap list",
				HelpPrinter: printRoadmapListHelp,
				Handler:     func(args []string) error { return roadmapList() },
				Flags:       []Flag{helpFlag()},
				Output: SuccessOutput{
					Kind:    "array",
					Schema:  "Array of {name, path, size} objects.",
					Example: `[{"name":"project1","path":"~/.roadmaps/project1.db","size":24576}]`,
				},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "Read-only.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0},
				Examples: []Example{
					{Title: "List all roadmaps", Cmd: "rmp roadmap list", Exit: 0},
					{Title: "Same, via alias", Cmd: "rmp roadmap ls", Exit: 0},
				},
			},
			{
				Name:        "create",
				Aliases:     []string{"new"},
				Summary:     "Create a new roadmap.",
				Description: "Creates a new roadmap database at ~/.roadmaps/<name>.db (mode 0600).",
				Usage:       "rmp roadmap create <name>",
				HelpPrinter: printRoadmapCreateHelp,
				Handler:     roadmapCreate,
				Positional: []Argument{
					{Name: "name", Type: "string", Required: true, Description: "Roadmap name (regex ^[a-z0-9_-]+$, max 50 chars)."},
				},
				Flags: []Flag{helpFlag()},
				Output: SuccessOutput{
					Kind:    "object",
					Schema:  `{"name": "<name>"}`,
					Example: `{"name":"mobile-app"}`,
				},
				SideEffects: SideEffects{
					Database:   "Creates a new SQLite database and initialises its schema.",
					Filesystem: "Creates ~/.roadmaps/<name>.db (mode 0600); the directory is created with mode 0700 if absent.",
					Network:    "None.",
				},
				Idempotent: false,
				ExitCodes:  []int{0, 5, 6},
				Examples: []Example{
					{Title: "Create a roadmap", Cmd: "rmp roadmap create mobile-app", Stdout: `{"name":"mobile-app"}`, Exit: 0},
					{Title: "Roadmap already exists", Cmd: "rmp roadmap create existing", Stderr: "Error: roadmap \"existing\" already exists", Exit: 5},
				},
			},
			{
				Name:        "remove",
				Aliases:     []string{"rm", "delete"},
				Summary:     "Remove a roadmap (irreversible).",
				Description: "Deletes the roadmap database file and its SQLite sidecar files (-wal, -shm).",
				Usage:       "rmp roadmap remove <name>",
				HelpPrinter: printRoadmapRemoveHelp,
				Handler:     roadmapRemove,
				Positional: []Argument{
					{Name: "name", Type: "string", Required: true, Description: "Roadmap to remove. Must exist."},
				},
				Flags: []Flag{helpFlag()},
				Output: SuccessOutput{
					Kind:   "empty",
					Schema: "Empty stdout on success.",
				},
				SideEffects: SideEffects{
					Database:   "Deletes the SQLite database file.",
					Filesystem: "Removes ~/.roadmaps/<name>.db and any -wal/-shm sidecar files.",
					Network:    "None.",
				},
				Idempotent: false,
				ExitCodes:  []int{0, 4, 6},
				Examples: []Example{
					{Title: "Remove a roadmap", Cmd: "rmp roadmap remove mobile-app", Exit: 0},
					{Title: "Roadmap not found", Cmd: "rmp roadmap remove missing", Stderr: "Error: roadmap \"missing\" not found", Exit: 4},
				},
			},
		},
	}
}

// =====================================================================
// stats — leaf command, no subcommand
// =====================================================================

func buildStatsCommand() Command {
	return Command{
		Name:          "stats",
		Summary:       "Roadmap-wide statistics (sprint counts, task distribution, velocity).",
		Description:   "Provides comprehensive statistics about a roadmap.",
		HasSubcommand: false,
		HelpPrinter:   printStatsHelp,
		Prerequisites: []string{"An existing roadmap selected via -r/--roadmap."},
		Subcommands: []Subcommand{
			{
				Name:        "",
				Summary:     "Show roadmap statistics.",
				Description: "Returns sprint counts, per-status task counts, and average velocity across the last 5 closed sprints.",
				Usage:       "rmp stats -r <roadmap>",
				HelpPrinter: printStatsHelp,
				Handler:     HandleStats,
				Flags:       []Flag{sharedRoadmapFlag(), helpFlag()},
				Output: SuccessOutput{
					Kind:    "object",
					Schema:  "{roadmap, sprints:{current,total,completed,pending}, tasks:{backlog,sprint,doing,testing,completed}, average_velocity}",
					Example: `{"roadmap":"myproject","sprints":{"current":5,"total":12,"completed":10,"pending":1},"tasks":{"backlog":15,"sprint":8,"doing":5,"testing":3,"completed":42},"average_velocity":2.5}`,
				},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "Read-only.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 4},
				Examples: []Example{
					{Title: "Show stats", Cmd: "rmp stats -r myproject", Exit: 0},
				},
			},
		},
	}
}

// =====================================================================
// backlog
// =====================================================================

func buildBacklogCommand() Command {
	return Command{
		Name:          "backlog",
		Aliases:       []string{"bl"},
		Summary:       "Query BACKLOG-status tasks (planning view for tasks not yet in a sprint).",
		Description:   "Dedicated commands for managing and querying tasks with status BACKLOG.",
		HelpPrinter:   printBacklogHelp,
		HasSubcommand: true,
		Prerequisites: []string{"An existing roadmap selected via -r/--roadmap."},
		Subcommands: []Subcommand{
			{
				Name:        "list",
				Aliases:     []string{"ls"},
				Summary:     "List all tasks in the backlog.",
				Description: "Returns every task with status BACKLOG, with optional filters and sorting.",
				Usage:       "rmp backlog list -r <roadmap> [filters]",
				HelpPrinter: printBacklogListHelp,
				Handler:     backlogList,
				Flags: []Flag{
					sharedRoadmapFlag(),
					{Long: "--priority", Short: "-p", Type: "integer", HasRange: true, RangeMin: 0, RangeMax: 9, Description: "Filter: priority >= <min> (0-9)."},
					{Long: "--type", Short: "-y", Type: "enum", Enum: "TaskType", Description: "Filter by task type."},
					{Long: "--sort", Type: "enum", Enum: "TaskSort", Default: "priority", Description: "Sort order."},
					{Long: "--limit", Short: "-l", Type: "integer", HasRange: true, RangeMin: 1, RangeMax: 100, Default: "100", Description: "Maximum tasks returned."},
					helpFlag(),
				},
				Output: SuccessOutput{
					Kind:   "array",
					Schema: "Array of task objects (status BACKLOG).",
				},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 6},
				Examples: []Example{
					{Title: "List backlog", Cmd: "rmp backlog list -r myproject", Exit: 0},
					{Title: "Filter by priority", Cmd: "rmp backlog list -r myproject --priority 7", Exit: 0},
					{Title: "Invalid type", Cmd: "rmp backlog list -r myproject --type FOO", Stderr: "Error: invalid task type: FOO", Exit: 6},
				},
			},
			{
				Name:        "show-next",
				Summary:     "Show top N backlog tasks by priority for sprint planning.",
				Description: "Returns the top-<count> BACKLOG tasks by priority DESC, then created_at ASC for ties.",
				Usage:       "rmp backlog show-next -r <roadmap> [count]",
				HelpPrinter: printBacklogShowNextHelp,
				Handler:     backlogShowNext,
				Positional: []Argument{
					{Name: "count", Type: "integer", Required: false, Description: "Maximum tasks to return (default 5, clamped to 100)."},
				},
				Flags: []Flag{sharedRoadmapFlag(), helpFlag()},
				Output: SuccessOutput{
					Kind:   "array",
					Schema: "Array of task objects (status BACKLOG, ordered priority DESC).",
				},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 6},
				Examples: []Example{
					{Title: "Top 5 backlog tasks", Cmd: "rmp backlog show-next -r myproject", Exit: 0},
					{Title: "Top 10", Cmd: "rmp backlog show-next -r myproject 10", Exit: 0},
				},
			},
		},
	}
}

// =====================================================================
// audit
// =====================================================================

func buildAuditCommand() Command {
	return Command{
		Name:          "audit",
		Aliases:       []string{"aud"},
		Summary:       "Query the per-roadmap audit log.",
		Description:   "Lists and aggregates audit-log entries recorded for tasks and sprints in a roadmap.",
		HelpPrinter:   printAuditHelp,
		HasSubcommand: true,
		Prerequisites: []string{"An existing roadmap selected via -r/--roadmap."},
		Subcommands: []Subcommand{
			{
				Name:        "list",
				Aliases:     []string{"ls"},
				Summary:     "List audit entries (newest first).",
				Description: "Returns audit-log entries for the roadmap, newest first; filters compose with AND.",
				Usage:       "rmp audit list -r <roadmap> [filters]",
				HelpPrinter: printAuditListHelp,
				Handler:     auditList,
				Flags: []Flag{
					sharedRoadmapFlag(),
					{Long: "--operation", Short: "-o", Type: "enum", Enum: "AuditOperation", Description: "Filter by operation."},
					{Long: "--entity-type", Short: "-e", Type: "enum", Enum: "AuditEntityType", Description: "Filter by entity type (TASK or SPRINT)."},
					{Long: "--entity-id", Type: "integer", Description: "Filter by specific entity numeric id."},
					{Long: "--since", Type: "date", Description: "Lower bound on performed_at (inclusive, ISO 8601)."},
					{Long: "--until", Type: "date", Description: "Upper bound on performed_at (inclusive, ISO 8601)."},
					{Long: "--limit", Short: "-l", Type: "integer", Default: "100", Description: "Maximum rows returned."},
					helpFlag(),
				},
				Output: SuccessOutput{
					Kind:   "array",
					Schema: "Array of audit entries: {id, operation, entity_type, entity_id, performed_at}.",
				},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 6},
				Examples: []Example{
					{Title: "All audit entries", Cmd: "rmp audit list -r myproject", Exit: 0},
					{Title: "Filter by operation", Cmd: "rmp audit list -r myproject -o TASK_STATUS_CHANGE -e TASK", Exit: 0},
				},
			},
			{
				Name:        "history",
				Aliases:     []string{"hist"},
				Summary:     "Show full history for one entity (TASK or SPRINT).",
				Description: "Returns every audit entry recorded for a single entity, newest first.",
				Usage:       "rmp audit history -r <roadmap> <entity-type> <entity-id>",
				HelpPrinter: printAuditHistoryHelp,
				Handler:     auditHistory,
				Positional: []Argument{
					{Name: "entity-type", Type: "enum", Enum: "AuditEntityType", Required: true, Description: "TASK or SPRINT."},
					{Name: "entity-id", Type: "integer", Required: true, Description: "Integer id within the entity type."},
				},
				Flags: []Flag{sharedRoadmapFlag(), helpFlag()},
				Output: SuccessOutput{
					Kind:   "array",
					Schema: "Array of audit entries (same shape as audit list).",
				},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 6},
				Examples: []Example{
					{Title: "Task history", Cmd: "rmp audit history -r myproject TASK 1", Exit: 0},
					{Title: "Sprint history", Cmd: "rmp audit history -r myproject SPRINT 3", Exit: 0},
				},
			},
			{
				Name:        "stats",
				Summary:     "Show aggregate audit counts.",
				Description: "Aggregates the audit log over an optional time window.",
				Usage:       "rmp audit stats -r <roadmap> [--since <date>] [--until <date>]",
				HelpPrinter: printAuditStatsHelp,
				Handler:     auditStats,
				Flags: []Flag{
					sharedRoadmapFlag(),
					{Long: "--since", Type: "date", Description: "Aggregation window start (inclusive)."},
					{Long: "--until", Type: "date", Description: "Aggregation window end (inclusive)."},
					helpFlag(),
				},
				Output: SuccessOutput{
					Kind:   "object",
					Schema: "{total_entries, first_entry_at, last_entry_at, by_operation, by_entity_type}",
				},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 3, 6},
				Examples: []Example{
					{Title: "Aggregate stats", Cmd: "rmp audit stats -r myproject", Exit: 0},
					{Title: "Bounded window", Cmd: "rmp audit stats -r myproject --since 2026-01-01 --until 2026-01-31", Exit: 0},
				},
			},
		},
	}
}
