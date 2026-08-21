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
//     SPEC/DATABASE.md § `audit` Table (the canonical catalogue of
//     audit operations, and the source of the AuditOperation
//     descriptions), internal/models package (canonical value lists)
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
	// AuditOperation is transcribed from the canonical catalogue in
	// SPEC/DATABASE.md § `audit` Table rather than left to the reader to
	// infer from the operation name. The names carry less than they look
	// like they do: SPRINT_TASK_MOVE_POSITION and SPRINT_REORDER_TASKS
	// are indistinguishable by name alone, TASK_REOPEN does not say which
	// source states also drop the sprint_tasks row, and the six comment
	// operations do not say that they are recorded against the parent
	// entity. See auditOperationDescriptions.
	"AuditOperation": auditOperationEnumDescriptions(),
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

// auditOperationDescriptions is the one description per AuditOperation
// value, transcribed from the canonical catalogue in SPEC/DATABASE.md §
// `audit` Table — the section that declares itself canonical for the whole
// SPEC. Each entry is that catalogue's text verbatim, with a full stop added
// where the catalogue entry has none; the six comment operations additionally
// receive auditCommentParentSuffix (see auditOperationEnumDescriptions).
//
// Transcription is deliberate but not unguarded. Copying is unavoidable —
// the SPEC is markdown on disk and the contract must stay a self-contained
// binary that describes itself with no repository present — so the copy is
// pinned instead: TestGenerate_AuditOperationDescriptionsMatchSpecCatalogue
// re-derives every one of these strings from SPEC/DATABASE.md and fails on
// the first byte of drift, in either direction.
//
// Like commentTypeDescriptions above, it is keyed by the model constant
// rather than by a bare string, so a renamed or removed AuditOperation
// constant fails to compile here instead of silently dropping a description
// at runtime.
var auditOperationDescriptions = map[models.AuditOperation]string{
	// Task lifecycle.
	models.OpTaskCreate: "New task created via `task create`.",
	models.OpTaskUpdate: "LEGACY. The generic `task edit` operation the five per-field `TASK_*_CHANGE` " +
		"operations above replace. The migration reclassifies no row carrying it, because a field edit leaves no " +
		"trace of which field it touched.",
	models.OpTaskDelete: "Task deleted via `task remove` (only allowed while in BACKLOG; see Delete Task " +
		"precondition).",

	// Task status. Five operations, one per destination state, each
	// transcribed from the catalogue entry that describes it.
	models.OpTaskStatusBacklog: "Task entered `BACKLOG`. Written by `task stat <ids> BACKLOG` and by " +
		"`sprint remove-tasks`, one row per task in either case. From `sprint remove-tasks` the row names " +
		"the sprint the task left in `related_entity_id`; from `task stat` no sprint is party to the " +
		"operation and `related_entity_id` is NULL.",
	models.OpTaskStatusSprint: "Task entered `SPRINT`. Written by `sprint add-tasks` only, one row per " +
		"task, naming the sprint the task entered in `related_entity_id`; `task stat` cannot set `SPRINT`, " +
		"so no other command writes this operation and every row of it names a sprint.",
	models.OpTaskStatusDoing: "Task entered `DOING` via `task stat`, one row per task. The row carries " +
		"the `commit_hash` supplied as `--commit-open`.",
	models.OpTaskStatusTesting: "Task entered `TESTING` via `task stat`, one row per task.",
	models.OpTaskStatusCompleted: "Task entered `COMPLETED` via `task stat`, one row per task. The row " +
		"carries the `commit_hash` supplied as `--commit-close`.",
	models.OpTaskStatusChange: "LEGACY. The single status-change operation the five `TASK_STATUS_*` operations " +
		"above replace. It survives on rows the 1.11.0 to 1.12.0 migration could not reclassify (see `VERSION.md § " +
		"Migration 1.11.0 to 1.12.0`).",
	// Task field edits. One operation per field `task edit` can set, each
	// naming the field rather than the fact that an edit happened; the last
	// two of the seven fields are the priority and severity operations below,
	// which `task edit` shares with `task prio` and `task sev`.
	models.OpTaskTitleChange:                  "`title` supplied to `task edit`.",
	models.OpTaskTypeChange:                   "`type` supplied to `task edit`.",
	models.OpTaskFunctionalRequirementsChange: "`functional_requirements` supplied to `task edit`.",
	models.OpTaskTechnicalRequirementsChange:  "`technical_requirements` supplied to `task edit`.",
	models.OpTaskAcceptanceCriteriaChange:     "`acceptance_criteria` supplied to `task edit`.",

	models.OpTaskPriorityChange: "Priority change (0-9) via `task prio` or via `task edit`.",
	models.OpTaskSeverityChange: "Severity change (0-9) via `task sev` or via `task edit`.",
	models.OpTaskReopen: "Task returned to BACKLOG via `task reopen`; lifecycle timestamps, completion_summary, " +
		"and commit_close cleared, commit_open preserved. The sprint_tasks row is removed only when the source " +
		"state is SPRINT, DOING, or TESTING; from COMPLETED the row is kept. `task reopen` writes this operation " +
		"alone and writes no `TASK_STATUS_BACKLOG` row.",

	// Sprint lifecycle.
	models.OpSprintCreate: "New sprint created.",
	models.OpSprintUpdate: "LEGACY. The generic `sprint update` operation the four per-field `SPRINT_*_CHANGE` " +
		"operations above replace. The migration reclassifies no row carrying it, for the same reason.",
	models.OpSprintDelete: "Sprint deleted.",
	models.OpSprintStart:  "Sprint started (PENDING to OPEN).",
	models.OpSprintClose:  "Sprint closed (OPEN to CLOSED).",
	models.OpSprintReopen: "Sprint reopened (CLOSED to OPEN).",

	// Sprint field updates, the `sprint update` counterpart of the five task
	// field edits above. Each names the column it records, so --max-tasks and
	// --order appear here as their columns and not as their flags.
	models.OpSprintTitleChange:       "`title` supplied to `sprint update`.",
	models.OpSprintDescriptionChange: "`description` supplied to `sprint update`.",
	models.OpSprintMaxTasksChange:    "`max_tasks` supplied to `sprint update`.",
	models.OpSprintOrderChange:       "`order_index` supplied to `sprint update`.",

	models.OpSprintAddTask: "Task added to a sprint via `sprint add-tasks`; one row per task, against the " +
		"sprint, naming the task in `related_entity_id`.",
	models.OpSprintRemoveTask: "Task removed from a sprint via `sprint remove-tasks`; one row per task, against " +
		"the sprint, naming the task in `related_entity_id`.",
	models.OpSprintMoveTask: "LEGACY. The single move operation `SPRINT_MOVE_TASK_OUT` and `SPRINT_MOVE_TASK_IN` " +
		"replace. The migration reclassifies no row carrying it, because such a row names neither the task that " +
		"moved nor the sprint it came from.",
	models.OpSprintMoveTaskOut: "Task moved out of the source sprint via `sprint move-tasks`; one row per " +
		"task, against the source sprint, naming the task in `related_entity_id`.",
	models.OpSprintMoveTaskIn: "Task moved into the destination sprint via `sprint move-tasks`; one row " +
		"per task, against the destination sprint, naming the task in `related_entity_id`.",

	// Sprint task ordering. These three are the clearest case against
	// inferring a description from the name.
	models.OpSprintReorderTasks:     "Sprint tasks reordered (set exact order).",
	models.OpSprintTaskMovePosition: "Single task moved to specific position.",
	models.OpSprintTaskSwap:         "Two tasks swapped positions.",

	// Task dependencies.
	models.OpTaskAddDep: "Dependency added via `task add-dep`; two rows are written, one against each task of " +
		"the pair, and each row names the other task in `related_entity_id`.",
	models.OpTaskRemoveDep: "Dependency removed via `task remove-dep`; two rows are written, one against each " +
		"task of the pair, and each row names the other task in `related_entity_id`.",

	// Comments. The half of the rule the catalogue entries do not state —
	// that the comment's own id never appears — is appended once by
	// auditOperationEnumDescriptions rather than written out six times.
	models.OpTaskCommentCreate:   "Comment added to a task via `task comment-add` (logged against the parent task).",
	models.OpTaskCommentUpdate:   "Comment edited via `task comment-edit` (logged against the parent task).",
	models.OpTaskCommentDelete:   "Comment deleted via `task comment-remove` (logged against the parent task).",
	models.OpSprintCommentCreate: "Comment added to a sprint via `sprint comment-add` (logged against the parent sprint).",
	models.OpSprintCommentUpdate: "Comment edited via `sprint comment-edit` (logged against the parent sprint).",
	models.OpSprintCommentDelete: "Comment deleted via `sprint comment-remove` (logged against the parent sprint).",
}

// auditCommentParentSuffix completes, for the six comment operations, the rule
// SPEC/DATABASE.md states in prose immediately below the catalogue: "They never
// write the comment's own id and never introduce a new entity_type value." The
// catalogue entries themselves say only that the operation is logged against the
// parent, so an agent reading the contract alone would not learn that
// `audit history TASK <task-id>` — never `audit history` on a comment id — is
// how a comment's trail is retrieved.
const auditCommentParentSuffix = " The audit entry names the parent entity; the comment's own id is never recorded."

// auditCommentOperations is the set the suffix above applies to. It is a set of
// constants rather than a name test (say, a check for "_COMMENT_" in the value)
// so that adding a seventh comment operation is a deliberate edit here, and so
// that renaming one of the six is a compile error rather than a silent loss of
// the sentence.
var auditCommentOperations = map[models.AuditOperation]bool{
	models.OpTaskCommentCreate:   true,
	models.OpTaskCommentUpdate:   true,
	models.OpTaskCommentDelete:   true,
	models.OpSprintCommentCreate: true,
	models.OpSprintCommentUpdate: true,
	models.OpSprintCommentDelete: true,
}

// auditOperationEnumDescriptions renders the description map for the
// AuditOperation enum, appending the parent-entity sentence to the six comment
// operations. It walks models.ValidAuditOperations rather than ranging over
// auditOperationDescriptions, so an operation the code can write but nobody
// described renders as the empty string the schema allows instead of being
// dropped — and the contract gate that requires every value to carry a
// description reports it.
func auditOperationEnumDescriptions() map[string]string {
	out := make(map[string]string, len(models.ValidAuditOperations))
	for _, op := range models.ValidAuditOperations {
		description := auditOperationDescriptions[op]
		if description != "" && auditCommentOperations[op] {
			description += auditCommentParentSuffix
		}
		out[string(op)] = description
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
