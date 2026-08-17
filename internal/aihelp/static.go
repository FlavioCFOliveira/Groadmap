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
			RequiredFor: "every command except roadmap list/create/remove, web, and the help/version/ai-help commands",
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
	// The comment types are published as TWO keys, not one: a
	// flags[].enum value is a single key into this map, so the two
	// per-entity subsets of the CommentType enum cannot share one entry
	// without offering a sprint the three types a sprint rejects
	// (SPEC/DATA_FORMATS.md § enums map entry). Both entries are built
	// from one description table so the shared values are described
	// once; see commentEnumDescriptions.
	"TaskCommentType":   commentEnumDescriptions(models.ValidTaskCommentTypes),
	"SprintCommentType": commentEnumDescriptions(models.ValidSprintCommentTypes),
	// AuditOperation values: descriptions are derived directly from
	// the operation name (e.g. TASK_CREATE → "task creation"), so
	// rather than duplicate every operation we leave the descriptions
	// empty here and let the generator emit empty-string descriptions
	// for them. The operation names themselves are self-explanatory.
}

// commentTypeDescriptions is the one description per CommentType value,
// transcribed from the table in SPEC/MODELS.md § Comment Type. It is keyed
// by the model constant rather than by a bare string so a renamed or
// removed constant fails to compile here instead of silently dropping a
// description at runtime.
//
// The per-entity subsets are NOT encoded here: they are derived from
// internal/models (the same slices the rejection messages and the family
// helps render) by commentEnumDescriptions.
var commentTypeDescriptions = map[models.CommentType]string{
	models.CommentFinding: "Something discovered during the work: an observed behaviour, a measurement, " +
		"a cause identified, a constraint that turned out to apply.",
	models.CommentHypothesis: "A proposition raised to explain a problem or to guide the next step, " +
		"stated before it is confirmed or refuted.",
	models.CommentTest: "A test that was run and what it showed. Covers both automated tests and " +
		"manual verification.",
	models.CommentDecision: "A decision taken during the work, and the reasoning behind it.",
	models.CommentProgress: "A statement of how the work advanced: what was done, what remains.",
	models.CommentUpdate: "The reason behind a modification to the definition of the task or the sprint: " +
		"something added, updated, removed, complemented, or clarified.",
	models.CommentNote: "A remark that belongs in the log but fits none of the categories above.",
}

// Suffixes stating the per-entity subset a comment type belongs to. The
// enums map is the only place in the contract that can carry this fact —
// EnumDefinition has no per-entity field — so it travels in the value
// description (SPEC/MODELS.md § Comment Type, Per-entity valid subsets).
const (
	commentTypeBothEntities = " Accepted on both a task comment and a sprint comment."
	commentTypeTaskOnly     = " Accepted on a task comment only; on a sprint comment it is rejected with exit code 6."
)

// commentEnumDescriptions renders the description map for one per-entity
// comment-type subset. Membership of the sprint subset is asked of
// internal/models rather than restated here, so the "task only" marker
// cannot drift from the validation that enforces it.
func commentEnumDescriptions(types []models.CommentType) map[string]string {
	out := make(map[string]string, len(types))
	for _, t := range types {
		suffix := commentTypeTaskOnly
		if models.IsValidSprintCommentType(string(t)) {
			suffix = commentTypeBothEntities
		}
		out[string(t)] = commentTypeDescriptions[t] + suffix
	}
	return out
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
		return enumStrings(models.ValidTaskStatuses)
	case "TaskType":
		return enumStrings(models.ValidTaskTypes)
	case "SprintStatus":
		return enumStrings(models.ValidSprintStatuses)
	case "AuditOperation":
		return enumStrings(models.ValidAuditOperations)
	case "AuditEntityType":
		// EntityType has no Valid* slice in internal/models. The
		// authoritative pair is declared as constants; mirror them
		// here in the order they appear in models/audit.go.
		return []string{string(models.EntityTask), string(models.EntitySprint)}
	case "TaskSort":
		return enumStrings(models.ValidTaskSorts)
	case "TaskCommentType":
		// The two comment-type keys are the same enum narrowed to the
		// subset its entity accepts. Each reads its own canonical slice
		// from internal/models, which is also what the rejection message
		// and the family help render, so the three surfaces cannot drift.
		return enumStrings(models.ValidTaskCommentTypes)
	case "SprintCommentType":
		return enumStrings(models.ValidSprintCommentTypes)
	default:
		return nil
	}
}

// enumStrings converts a canonical enum slice from internal/models into
// the []string the contract emits, preserving declaration order. The type
// parameter is constrained to string-kinded types, which is what every
// enum in internal/models is.
func enumStrings[T ~string](values []T) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, string(v))
	}
	return out
}
