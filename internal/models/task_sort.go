// Package models — task sort enum.
//
// TaskSort enumerates the field names accepted by the --sort flag of
// `rmp task list` and `rmp backlog list`. It is declared here, rather
// than inlined in the command-layer parsers, because the AI-help
// contract emitter (internal/aihelp) projects every enum referenced by
// the command registry into the contract's top-level `enums` map. A
// single source of truth keeps the parser, the help text, and the
// machine-readable contract in lock-step.
package models

// TaskSort is the field name a list command sorts by.
type TaskSort string

// Valid task sort fields. The wire form is the lowercase token the user
// types after `--sort`.
const (
	// TaskSortPriority sorts by priority DESC (default for list commands).
	TaskSortPriority TaskSort = "priority"
	// TaskSortCreated sorts by created_at ASC.
	TaskSortCreated TaskSort = "created"
	// TaskSortStatus sorts by status (state-machine order).
	TaskSortStatus TaskSort = "status"
	// TaskSortSeverity sorts by severity DESC.
	TaskSortSeverity TaskSort = "severity"
)

// ValidTaskSorts contains all valid task sort field names in the order
// surfaced by --help and the AI-help contract.
var ValidTaskSorts = []TaskSort{
	TaskSortPriority,
	TaskSortCreated,
	TaskSortStatus,
	TaskSortSeverity,
}
