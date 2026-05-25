// Package aihelp — static contract data.
//
// This file holds the values that are constant for any build of the
// rmp binary: the conventions block, the exit-code catalogue, and the
// enum descriptions that mirror SPEC/MODELS.md § Enums. They are
// declared as package-level functions (rather than vars) so they
// cannot be mutated by accident from the test suite or downstream
// callers; each call returns a fresh slice/map.
//
// Sources of truth:
//
//   - Conventions: SPEC/DATA_FORMATS.md § AI Agent Contract § conventions object
//   - Exit codes:  SPEC/ARCHITECTURE.md § Exit Codes
//   - Enums:       SPEC/MODELS.md § Enums (values + descriptions),
//     internal/models package (canonical value lists)
package aihelp

import (
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// staticConventions returns the conventions block declared in
// SPEC/DATA_FORMATS.md. The values are intentionally hard-coded here
// (rather than read from runtime state) so the contract remains
// deterministic and self-describing.
func staticConventions() Conventions {
	return Conventions{
		StdoutOnSuccess: "json",
		StderrOnError:   "plain_text",
		JSONIndent:      2,
		Charset:         "utf-8",
		Locale:          "C",
		DatetimeFormat:  "ISO 8601 UTC with milliseconds, suffix Z",
		DatetimeExample: "2026-05-24T14:30:00.000Z",
		RoadmapFlag: RoadmapFlag{
			Short:       "-r",
			Long:        "--roadmap",
			RequiredFor: "every command except roadmap list/create/remove and the help/version/ai-help commands",
		},
		ListSeparator: ",",
		AIAgentEnvVar: AIAgentEnvVar{
			Name:        "AI_AGENT",
			EnableValue: "1",
			Effect:      "Emits a one-line hint to stderr on every invocation pointing to --ai-help.",
		},
	}
}

// staticExitCodes returns the catalogue from SPEC/ARCHITECTURE.md §
// Exit Codes, in ascending order. The Sentinel field is populated only
// for codes produced by wrapping a sentinel error from internal/utils;
// 0 and 130 carry no sentinel.
//
// Whenever the table in SPEC/ARCHITECTURE.md changes (new exit code,
// renamed sentinel), this function is the one place to update.
func staticExitCodes() []ExitCode {
	return []ExitCode{
		{Code: 0, Name: "EXIT_SUCCESS", Meaning: "Command completed successfully."},
		{Code: 1, Name: "EXIT_FAILURE", Meaning: "General error (unexpected error, database failure).", Sentinel: "utils.ErrDatabase"},
		{Code: 2, Name: "EXIT_MISUSE", Meaning: "Misuse of command (invalid argument, syntax error, missing required flag).", Sentinel: "utils.ErrInvalidInput"},
		{Code: 3, Name: "EXIT_NO_ROADMAP", Meaning: "No roadmap selected for a command that requires one.", Sentinel: "utils.ErrNoRoadmap"},
		{Code: 4, Name: "EXIT_NOT_FOUND", Meaning: "Resource not found (roadmap, task, sprint).", Sentinel: "utils.ErrNotFound"},
		{Code: 5, Name: "EXIT_EXISTS", Meaning: "Resource already exists (duplicate name).", Sentinel: "utils.ErrAlreadyExists"},
		{Code: 6, Name: "EXIT_INVALID_DATA", Meaning: "Invalid input data (validation failure: dates, ranges, enums).", Sentinel: "utils.ErrValidation"},
		{Code: 126, Name: "EXIT_NOT_EXECUTABLE", Meaning: "Command not executable (filesystem permission issue)."},
		{Code: 127, Name: "EXIT_CMD_NOT_FOUND", Meaning: "Unknown command or subcommand."},
		{Code: 130, Name: "EXIT_SIGINT", Meaning: "Interrupted by SIGINT (Ctrl+C)."},
	}
}

// enumDescriptions maps an enum name + value to the short human-
// readable description from SPEC/MODELS.md § Enums. Values absent
// from this map serialise with an empty description string (the JSON
// key is still emitted, per the schema's `description: string`
// requirement). Centralising the map here means the canonical value
// list still lives in internal/models, while the AI-only descriptive
// text lives in this package alongside the rest of the contract data.
var enumDescriptions = map[string]map[string]string{
	"TaskStatus": {
		"BACKLOG":   "Task is in backlog, not assigned to a sprint.",
		"SPRINT":    "Task is assigned to a sprint. Set automatically by `sprint add-tasks`; cannot be set manually via `task stat`.",
		"DOING":     "Task is being worked on.",
		"TESTING":   "Task is in testing phase.",
		"COMPLETED": "Task is complete.",
	},
	"TaskType": {
		"USER_STORY":  "New feature from the end user's perspective. Focuses on who/what/why.",
		"TASK":        "Internal work unit that does not deliver direct user value but is necessary (e.g. configure database).",
		"BUG":         "Report of something not working as expected in existing code.",
		"SUB_TASK":    "Decomposition of a Story or Task into smaller steps for easier tracking.",
		"EPIC":        "Large body of work grouping multiple related Stories and Tasks. Spans multiple sprints.",
		"REFACTOR":    "Improvement of internal code structure without changing external behaviour. Reduces technical debt.",
		"CHORE":       "Necessary maintenance that does not add features or fix bugs (e.g. update dependencies).",
		"SPIKE":       "Research or prototyping task to reduce technical uncertainties before development.",
		"DESIGN_UX":   "Tasks focused on creating prototypes, wireframes, or interface flows.",
		"IMPROVEMENT": "Refinement of an existing working feature that can be optimised.",
	},
	"SprintStatus": {
		"PENDING": "Sprint is created but not yet started; tasks can be added freely.",
		"OPEN":    "Sprint is in progress; `task next` returns tasks from this sprint.",
		"CLOSED":  "Sprint is finished and immutable except for reopen.",
	},
	"AuditEntityType": {
		"TASK":   "Audit entry concerns a task.",
		"SPRINT": "Audit entry concerns a sprint.",
	},
	"TaskSort": {
		"priority": "Sort by priority descending (default).",
		"created":  "Sort by created_at ascending.",
		"status":   "Sort by status (state-machine order).",
		"severity": "Sort by severity descending.",
	},
	// AuditOperation values: descriptions are derived directly from
	// the operation name (e.g. TASK_CREATE → "task creation"), so
	// rather than duplicate every operation we leave the descriptions
	// empty here and let the generator emit empty-string descriptions
	// for them. The operation names themselves are self-explanatory.
}

// stateMachineRefs maps an enum name to the SPEC reference for its
// state machine, when one exists. Absent entries serialise as no
// state_machine_reference field on the enum (omitempty).
var stateMachineRefs = map[string]string{
	"TaskStatus":   "STATE_MACHINE.md § Task State Machine",
	"SprintStatus": "STATE_MACHINE.md § Sprint State Machine",
}

// enumValues returns the canonical ordered list of values for the
// named enum, sourced from internal/models. Unknown enum names cause
// the generator to skip the entry entirely (a contract referencing an
// undeclared enum is a registry bug, not a runtime failure mode).
func enumValues(name string) []string {
	switch name {
	case "TaskStatus":
		out := make([]string, 0, len(models.ValidTaskStatuses))
		for _, v := range models.ValidTaskStatuses {
			out = append(out, string(v))
		}
		return out
	case "TaskType":
		out := make([]string, 0, len(models.ValidTaskTypes))
		for _, v := range models.ValidTaskTypes {
			out = append(out, string(v))
		}
		return out
	case "SprintStatus":
		out := make([]string, 0, len(models.ValidSprintStatuses))
		for _, v := range models.ValidSprintStatuses {
			out = append(out, string(v))
		}
		return out
	case "AuditOperation":
		out := make([]string, 0, len(models.ValidAuditOperations))
		for _, v := range models.ValidAuditOperations {
			out = append(out, string(v))
		}
		return out
	case "AuditEntityType":
		// EntityType has no Valid* slice in internal/models. The
		// authoritative pair is declared as constants; mirror them
		// here in the order they appear in models/audit.go.
		return []string{string(models.EntityTask), string(models.EntitySprint)}
	case "TaskSort":
		out := make([]string, 0, len(models.ValidTaskSorts))
		for _, v := range models.ValidTaskSorts {
			out = append(out, string(v))
		}
		return out
	default:
		return nil
	}
}
