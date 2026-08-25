package models

import (
	"errors"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file owns the `--limit` range rule: a list command's `--limit` must fall
// between MinListLimit and that command's maximum, and a value outside it is
// refused with exit code 6 (SPEC/COMMANDS.md § List Tasks, § List Backlog
// Tasks, § List Audit Log).
//
// # Why the rule lives here and not at the three call sites
//
// It used to live at the call sites, written out three times, and the three
// copies did not agree. `task list` and `backlog list` refused an out-of-range
// value as
//
//	Error: validation error: limit must be between 1 and 100
//
// while `audit list` refused it as
//
//	Error: validation error: --limit must be between 1 and 500 (got 0)
//
// One rule, one failure class, one exit code, announced two ways: the flag was
// named with its `--` prefix on one command and without it on the other two, and
// the offending value was echoed on one and dropped on the other two. Only the
// maximum is a real difference — 100 and 500 are genuinely different caps — and
// a real difference is the only one a user should have to notice (rmp task 329).
//
// The wording is not spelled here either. utils.NumericRangeMessage is the only
// place the sentence exists and utils.NumericRangeError the only place the
// ", got N" suffix and the utils.ErrValidation chain are assembled, which is
// what makes `limit`, `priority` and `severity` one sentence with three
// subjects rather than three sentences (rmp task 318 established this; see
// ValidatePriority in task.go).

// The two bounds the rule is instantiated at, one sentinel each.
//
// Each carries the whole wording of its own refusal, taken from the shared
// definition, so errors.Is can tell which cap was exceeded while neither can
// word the rule differently from the other.
var (
	ErrTaskLimitOutOfRange = errors.New(utils.NumericRangeMessage(
		utils.FieldListLimit, MinListLimit, MaxTaskLimit))
	ErrAuditLimitOutOfRange = errors.New(utils.NumericRangeMessage(
		utils.FieldListLimit, MinListLimit, MaxAuditLimit))
)

// ValidateTaskLimit refuses a `--limit` outside MinListLimit..MaxTaskLimit. It
// is the only check of that bound in the application: `task list` and
// `backlog list` both call it, and neither compares the bound itself.
//
// `backlog list` is a task listing, which is why it shares this cap rather than
// carrying one of its own.
func ValidateTaskLimit(limit int) error {
	return checkListLimit(limit, MaxTaskLimit, ErrTaskLimitOutOfRange)
}

// ValidateAuditLimit refuses a `--limit` outside MinListLimit..MaxAuditLimit,
// the bound `audit list` publishes. It is to the audit listing exactly what
// ValidateTaskLimit is to a task listing, down to the sentence it produces.
func ValidateAuditLimit(limit int) error {
	return checkListLimit(limit, MaxAuditLimit, ErrAuditLimitOutOfRange)
}

// checkListLimit is the ONLY comparison of a `--limit` against its bounds in the
// application. The two exported validators differ in the ceiling and the
// sentinel and in nothing else, so they are parameters here rather than a second
// copy of the comparison: the defect this file exists against was one rule
// written out more than once, and two hand-written comparisons in this package
// would be a smaller instance of it.
//
// rule is the sentinel that owns the ceiling, chained by utils.NumericRangeError
// so errors.Is still identifies which cap was exceeded.
func checkListLimit(limit, ceiling int, rule error) error {
	if limit < MinListLimit || limit > ceiling {
		return utils.NumericRangeError(rule, limit)
	}
	return nil
}
