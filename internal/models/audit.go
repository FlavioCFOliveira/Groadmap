package models

import (
	"errors"
	"fmt"
)

// Sentinel errors for audit validation.
var (
	ErrInvalidAuditOperation = errors.New("invalid audit operation")
	ErrInvalidEntityType     = errors.New("invalid entity type")
	ErrInvalidOperation      = errors.New("invalid operation")
	ErrEntityIDNotPositive   = errors.New("entity_id must be positive")
	ErrPerformedAtRequired   = errors.New("performed_at is required")
)

// AuditOperation represents the type of operation logged.
type AuditOperation string

// Audit operation constants as defined in SPEC/DATABASE.md.
const (
	// Task operations
	OpTaskCreate AuditOperation = "TASK_CREATE"
	OpTaskDelete AuditOperation = "TASK_DELETE"

	// Task status operations, in the order the canonical catalogue publishes
	// them. Each one names the state the task ENTERED, not the kind of change
	// that happened, so a reader learns the outcome from the operation value
	// alone and never has to correlate the row with the task's current status
	// (SPEC/DATABASE.md § One Row per Thing That Happened, rule 1).
	//
	// `task stat` writes four of them: it rejects the SPRINT target, so
	// TASK_STATUS_SPRINT has the single writer `sprint add-tasks`.
	// TASK_STATUS_BACKLOG has two, `task stat <ids> BACKLOG` and
	// `sprint remove-tasks`, and only the second names a counterpart sprint.
	//
	// TASK_STATUS_DOING and TASK_STATUS_COMPLETED are the only two operations
	// in the whole catalogue that carry a commit hash; see
	// OperationCarriesCommitHash below.
	OpTaskStatusBacklog   AuditOperation = "TASK_STATUS_BACKLOG"
	OpTaskStatusSprint    AuditOperation = "TASK_STATUS_SPRINT"
	OpTaskStatusDoing     AuditOperation = "TASK_STATUS_DOING"
	OpTaskStatusTesting   AuditOperation = "TASK_STATUS_TESTING"
	OpTaskStatusCompleted AuditOperation = "TASK_STATUS_COMPLETED"

	// The per-field operations of `task edit`, in the order the canonical
	// catalogue publishes them. Each one names the FIELD the invocation
	// supplied, not the bare fact that a field changed, so a reader learns
	// from the operation value alone what the edit was about — the same rule
	// that gives the status operations their destination
	// (SPEC/DATABASE.md § One Row per Thing That Happened, rule 1).
	//
	// The group stops at five because the last two fields `task edit` can set
	// already have an operation of their own. `task edit -p 5` and
	// `task prio <id> 5` perform the identical mutation, so they write the
	// identical operation, TASK_PRIORITY_CHANGE, and a filter on it finds
	// every priority change whichever command made it; --severity pairs with
	// `task sev` the same way (SPEC/COMMANDS.md § Edit Task).
	OpTaskTitleChange                  AuditOperation = "TASK_TITLE_CHANGE"
	OpTaskTypeChange                   AuditOperation = "TASK_TYPE_CHANGE"
	OpTaskFunctionalRequirementsChange AuditOperation = "TASK_FUNCTIONAL_REQUIREMENTS_CHANGE"
	OpTaskTechnicalRequirementsChange  AuditOperation = "TASK_TECHNICAL_REQUIREMENTS_CHANGE"
	OpTaskAcceptanceCriteriaChange     AuditOperation = "TASK_ACCEPTANCE_CRITERIA_CHANGE"

	OpTaskPriorityChange AuditOperation = "TASK_PRIORITY_CHANGE"
	OpTaskSeverityChange AuditOperation = "TASK_SEVERITY_CHANGE"
	OpTaskReopen         AuditOperation = "TASK_REOPEN"

	// Sprint operations
	OpSprintCreate AuditOperation = "SPRINT_CREATE"
	OpSprintDelete AuditOperation = "SPRINT_DELETE"
	OpSprintStart  AuditOperation = "SPRINT_START"
	OpSprintClose  AuditOperation = "SPRINT_CLOSE"
	OpSprintReopen AuditOperation = "SPRINT_REOPEN"

	// The per-field operations of `sprint update`, the four-member
	// counterpart of the five above and governed by the same rule. Each is
	// named for the column it records a change to rather than for the flag
	// that requests it, which is why --max-tasks writes
	// SPRINT_MAX_TASKS_CHANGE and --order writes SPRINT_ORDER_CHANGE against
	// the order_index column (SPEC/COMMANDS.md § Update Sprint).
	OpSprintTitleChange       AuditOperation = "SPRINT_TITLE_CHANGE"
	OpSprintDescriptionChange AuditOperation = "SPRINT_DESCRIPTION_CHANGE"
	OpSprintMaxTasksChange    AuditOperation = "SPRINT_MAX_TASKS_CHANGE"
	OpSprintOrderChange       AuditOperation = "SPRINT_ORDER_CHANGE"

	OpSprintAddTask    AuditOperation = "SPRINT_ADD_TASK"
	OpSprintRemoveTask AuditOperation = "SPRINT_REMOVE_TASK"

	// The two directions of a move. `sprint move-tasks` changes two sprints,
	// so it writes one row against each: OUT against the sprint the task left,
	// IN against the sprint it entered, both naming the task in
	// related_entity_id. The single SPRINT_MOVE_TASK operation they replace
	// wrote one row against the destination alone, so the source sprint's
	// history said nothing about losing the task (SPEC/DATABASE.md § One Row
	// per Thing That Happened, rule 2).
	OpSprintMoveTaskOut AuditOperation = "SPRINT_MOVE_TASK_OUT"
	OpSprintMoveTaskIn  AuditOperation = "SPRINT_MOVE_TASK_IN"

	// Sprint task ordering operations
	OpSprintReorderTasks     AuditOperation = "SPRINT_REORDER_TASKS"
	OpSprintTaskMovePosition AuditOperation = "SPRINT_TASK_MOVE_POSITION"
	OpSprintTaskSwap         AuditOperation = "SPRINT_TASK_SWAP"

	// Task dependency operations
	OpTaskAddDep    AuditOperation = "TASK_ADD_DEP"
	OpTaskRemoveDep AuditOperation = "TASK_REMOVE_DEP"

	// Comment operations. All six are recorded against the PARENT entity: a task
	// comment writes entity_type = TASK with the owning task's id, a sprint
	// comment writes entity_type = SPRINT with the owning sprint's id. The
	// comment's own id is never written and no COMMENT entity type exists, so the
	// entity_type value set stays exactly TASK and SPRINT (SPEC/DATABASE.md §
	// audit Table, "Comment operations are recorded against the parent entity").
	OpTaskCommentCreate   AuditOperation = "TASK_COMMENT_CREATE"
	OpTaskCommentUpdate   AuditOperation = "TASK_COMMENT_UPDATE"
	OpTaskCommentDelete   AuditOperation = "TASK_COMMENT_DELETE"
	OpSprintCommentCreate AuditOperation = "SPRINT_COMMENT_CREATE"
	OpSprintCommentUpdate AuditOperation = "SPRINT_COMMENT_UPDATE"
	OpSprintCommentDelete AuditOperation = "SPRINT_COMMENT_DELETE"

	// LEGACY operations: readable, never written (SPEC/DATABASE.md § audit
	// Table, "Legacy (readable, never written)"). A value the application stops
	// writing is not deleted. It stays declared and stays in
	// ValidAuditOperations, so the rows already carrying it remain reachable by
	// an `audit list --operation` filter and keep their own key in
	// `audit stats`; removing the constant would strand those rows behind a
	// filter value the CLI rejects, which is the defect this group exists to
	// prevent (SPEC/MODELS.md § Audit Operation, rule 3).
	//
	// All four members of the catalogue's group are here, in the order it
	// publishes them. TASK_UPDATE and SPRINT_UPDATE joined the group when the
	// per-field operations above took over from them, and the two say between
	// them why the group is retained rather than deleted: a stored TASK_UPDATE
	// row records that a task was edited without recording which field the
	// edit touched, so no migration can reclassify it into one of the five
	// operations that replaced it. The row keeps the only name it has.
	OpTaskStatusChange AuditOperation = "TASK_STATUS_CHANGE"
	OpTaskUpdate       AuditOperation = "TASK_UPDATE"
	OpSprintUpdate     AuditOperation = "SPRINT_UPDATE"
	OpSprintMoveTask   AuditOperation = "SPRINT_MOVE_TASK"
)

// OperationCarriesCommitHash reports whether op is one of the two operations
// that record the git commit bracketing a task's development work:
// TASK_STATUS_DOING carries the value supplied as --commit-open and
// TASK_STATUS_COMPLETED the one supplied as --commit-close.
//
// commit_hash is NULL on every other operation in the catalogue, and
// SPEC/DATABASE.md § The Commit Hash of an Audit Entry states that as a MUST
// NOT rather than as a convention — including for TASK_REOPEN, which clears
// tasks.commit_close without writing a hash anywhere. The rule is stated once
// here so the single audit writer can enforce it at the point of the INSERT
// instead of relying on every call site to observe it.
func OperationCarriesCommitHash(op AuditOperation) bool {
	return op == OpTaskStatusDoing || op == OpTaskStatusCompleted
}

// OperationCarriesRelatedEntity reports whether op is one of the eight
// operations of SPEC/DATABASE.md § The Two Entities of a Relational Operation,
// the only ones whose row may name a counterpart entity in related_entity_id.
//
// It answers MAY, not MUST, and the difference is the whole reason the question
// is asked per operation rather than per row. TASK_STATUS_BACKLOG has two
// producing commands: from `sprint remove-tasks` the row names the sprint the
// task left, and from `task stat <ids> BACKLOG` there is no second entity party
// to the operation, so the column is NULL. Only the call site knows which of the
// two wrote the row; what the operation alone decides is whether a counterpart
// is admissible at all.
//
// That is exactly the invariant the catalogue states over the stored table — no
// non-NULL related_entity_id outside these eight operations — so stating it once
// here lets the single audit writer enforce it at the point of the INSERT,
// rather than leaving it to the discipline of every call site.
func OperationCarriesRelatedEntity(op AuditOperation) bool {
	switch op {
	case OpSprintAddTask, OpTaskStatusSprint,
		OpSprintRemoveTask, OpTaskStatusBacklog,
		OpSprintMoveTaskOut, OpSprintMoveTaskIn,
		OpTaskAddDep, OpTaskRemoveDep:
		return true
	default:
		return false
	}
}

// ValidAuditOperations contains all valid audit operations, LEGACY ones
// included: the set is what `audit list --operation` accepts, and a legacy
// value is readable even though nothing writes it.
var ValidAuditOperations = []AuditOperation{
	OpTaskCreate,
	OpTaskDelete,
	OpTaskStatusBacklog,
	OpTaskStatusSprint,
	OpTaskStatusDoing,
	OpTaskStatusTesting,
	OpTaskStatusCompleted,
	OpTaskTitleChange,
	OpTaskTypeChange,
	OpTaskFunctionalRequirementsChange,
	OpTaskTechnicalRequirementsChange,
	OpTaskAcceptanceCriteriaChange,
	OpTaskPriorityChange,
	OpTaskSeverityChange,
	OpTaskReopen,
	OpSprintCreate,
	OpSprintDelete,
	OpSprintStart,
	OpSprintClose,
	OpSprintReopen,
	OpSprintTitleChange,
	OpSprintDescriptionChange,
	OpSprintMaxTasksChange,
	OpSprintOrderChange,
	OpSprintAddTask,
	OpSprintRemoveTask,
	OpSprintMoveTaskOut,
	OpSprintMoveTaskIn,
	OpSprintReorderTasks,
	OpSprintTaskMovePosition,
	OpSprintTaskSwap,
	OpTaskAddDep,
	OpTaskRemoveDep,
	OpTaskCommentCreate,
	OpTaskCommentUpdate,
	OpTaskCommentDelete,
	OpSprintCommentCreate,
	OpSprintCommentUpdate,
	OpSprintCommentDelete,

	// LEGACY, listed last exactly as the catalogue publishes its group, and in
	// its order within the group.
	OpTaskStatusChange,
	OpTaskUpdate,
	OpSprintUpdate,
	OpSprintMoveTask,
}

// IsValidAuditOperation checks if a string is a valid audit operation.
func IsValidAuditOperation(s string) bool {
	for _, op := range ValidAuditOperations {
		if string(op) == s {
			return true
		}
	}
	return false
}

// ParseAuditOperation parses a string into an AuditOperation.
func ParseAuditOperation(s string) (AuditOperation, error) {
	if !IsValidAuditOperation(s) {
		return "", fmt.Errorf("invalid audit operation: %q: %w", s, ErrInvalidAuditOperation)
	}
	return AuditOperation(s), nil
}

// EntityType represents the type of entity being audited.
type EntityType string

// Entity type constants.
const (
	EntityTask   EntityType = "TASK"
	EntitySprint EntityType = "SPRINT"
)

// IsValidEntityType checks if a string is a valid entity type.
func IsValidEntityType(s string) bool {
	return s == string(EntityTask) || s == string(EntitySprint)
}

// ParseEntityType parses a string into an EntityType.
func ParseEntityType(s string) (EntityType, error) {
	if !IsValidEntityType(s) {
		return "", fmt.Errorf("invalid entity type: %q: %w", s, ErrInvalidEntityType)
	}
	return EntityType(s), nil
}

// AuditEntry represents one entry in the roadmap's audit log. It is immutable:
// nothing updates or deletes an entry once written, which is what lets a
// TASK_STATUS_COMPLETED entry keep the hash of the commit that concluded a task
// after `task reopen` has cleared that hash from the task itself.
//
// The two nullable columns are pointers, not empty values, so that "no
// counterpart" and "no commit" serialise as JSON null rather than as 0 and "".
// An entity id of 0 and an empty hash are both rejected by the column CHECK
// constraints, so a non-pointer field could not tell absence from corruption
// (SPEC/MODELS.md § Audit Entry).
//
// Field order optimized for memory layout (80 bytes, zero padding on 64-bit
// systems): the two pointers lead and the two ints trail, so the pointer-scan
// prefix ends at byte 56 (SPEC/MODELS.md § Memory Layout Optimization).
type AuditEntry struct {
	// 8-byte pointer fields
	RelatedEntityID *int    `json:"related_entity_id"` // Counterpart entity of the producing operation; nil when it has none
	CommitHash      *string `json:"commit_hash"`       // Git commit bracketing the work; nil on every operation but two

	// 16-byte fields
	Operation   string `json:"operation"`    // One AuditOperation value, treated as opaque on read
	EntityType  string `json:"entity_type"`  // One EntityType value: TASK or SPRINT
	PerformedAt string `json:"performed_at"` // ISO 8601 UTC

	// 8-byte fields
	ID       int `json:"id"`
	EntityID int `json:"entity_id"` // The entity whose history this entry belongs to
}

// Validate checks if the audit entry data is valid.
func (a *AuditEntry) Validate() error {
	if !IsValidAuditOperation(a.Operation) {
		return fmt.Errorf("invalid operation: %q: %w", a.Operation, ErrInvalidOperation)
	}
	if !IsValidEntityType(a.EntityType) {
		return fmt.Errorf("invalid entity type: %q: %w", a.EntityType, ErrInvalidEntityType)
	}
	if a.EntityID <= 0 {
		return fmt.Errorf("entity_id must be positive, got %d: %w", a.EntityID, ErrEntityIDNotPositive)
	}
	if a.PerformedAt == "" {
		return ErrPerformedAtRequired
	}
	return nil
}

// Roadmap represents a roadmap file metadata.
type Roadmap struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// AuditStats represents statistics for audit entries.
type AuditStats struct {
	ByOperation  map[string]int `json:"by_operation"`
	ByEntityType map[string]int `json:"by_entity_type"`
	// FirstEntryAt and LastEntryAt are nil (serialized as JSON null) when no
	// audit entries match; otherwise they point to ISO 8601 UTC timestamps.
	// The keys are always present per SPEC/DATA_FORMATS.md, so no omitempty.
	FirstEntryAt *string `json:"first_entry_at"`
	LastEntryAt  *string `json:"last_entry_at"`
	TotalEntries int     `json:"total_entries"`
}
