package models

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file pins the rendered text of every enum and range rejection this
// package produces.
//
// The defect it guards against: a rejection built as
// fmt.Errorf("invalid task type: %q: %w", s, ErrInvalidTaskType) renders the
// sentinel's own text twice — once as the literal prefix and once again when
// %w expands the sentinel — so the user saw
//
//	Error: validation error: invalid task type: "BOGUS": invalid task type
//
// The convention that avoids it, already documented on ErrInvalidCommentType in
// comment.go, is that the SENTINEL supplies the message's opening clause through
// %w and the literal never restates it. Every rejection below is built that way.
//
// Two independent assertions run over each case, because either one alone is
// too weak. wantMsg pins the exact string, so a reappearing duplicate fails even
// if it is worded differently; wantSentinels pins the errors.Is chain, so a fix
// that tidies the text by dropping a %w — silently changing the command's exit
// code — fails too.

// dedupCase is one rejection under test.
type dedupCase struct {
	name string
	err  error
	// wantMsg is the complete rendered message, asserted with ==.
	wantMsg string
	// wantSentinels must all satisfy errors.Is, which is what maps the failure
	// to its exit code in cmd/rmp/main.go.
	wantSentinels []error
}

// allSentinels is every sentinel whose text could be duplicated by a rejection
// in this package. TestRejectionsNeverRepeatASentinel scans each message for
// all of them, so a duplicate introduced in a case nobody thought to enumerate
// is still caught.
var allSentinels = []error{
	ErrInvalidTaskType, ErrInvalidTaskStatus, ErrInvalidStatus, ErrInvalidType,
	ErrInvalidCurrentStatus, ErrInvalidTargetStatus, ErrCannotTransition,
	ErrPriorityOutOfRange, ErrSeverityOutOfRange, ErrInvalidCommitHash,
	ErrTaskLimitOutOfRange, ErrAuditLimitOutOfRange,
	ErrInvalidSprintStatus, ErrInvalidSprintOrder,
	ErrInvalidAuditOperation, ErrInvalidEntityType, ErrInvalidOperation,
	ErrEntityIDOutOfRange, ErrInvalidCommentType,
	utils.ErrValidation, utils.ErrFieldTooLarge,
}

// dedupValidCreatedAt is a timestamp validateDates accepts, so every task case below
// fails on the field it is actually testing rather than on a date.
const dedupValidCreatedAt = "2026-03-16T12:00:00.000Z"

// dedupCases builds every rejection under test. It is a function, not a var, so
// the helper values it needs are built in one obvious place.
func dedupCases(t *testing.T) []dedupCase {
	t.Helper()

	mustErr := func(label string, err error) error {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: want a rejection, got nil", label)
		}
		return err
	}

	_, taskTypeErr := ParseTaskType("BOGUS")
	_, taskStatusErr := ParseTaskStatus("BOGUS")
	_, sprintStatusErr := ParseSprintStatus("BOGUS")
	_, auditOpErr := ParseAuditOperation("BOGUS")
	_, entityTypeErr := ParseEntityType("BOGUS")

	// A task that is valid in every respect; each case below breaks exactly one
	// field, so the rejection under test is the one Validate reaches.
	validTask := Task{
		Title:                  "Harden the audit writer",
		FunctionalRequirements: "Reject an entry whose entity id is not positive",
		TechnicalRequirements:  "Enforce the rule in AuditEntry.Validate",
		AcceptanceCriteria:     "A non-positive entity id is refused with exit 6",
		Status:                 StatusBacklog,
		Type:                   TypeTask,
		CreatedAt:              dedupValidCreatedAt,
	}

	badPriority := validTask
	badPriority.Priority = 42

	badSeverity := validTask
	badSeverity.Severity = 42

	badStatus := validTask
	badStatus.Status = "NOPE"

	badType := validTask
	badType.Type = "NOPE"

	outOfRange := 42

	validAuditOp := string(ValidAuditOperations[0])

	return []dedupCase{
		{
			name:          "ParseTaskType",
			err:           mustErr("ParseTaskType", taskTypeErr),
			wantMsg:       `invalid task type: "BOGUS"`,
			wantSentinels: []error{ErrInvalidTaskType},
		},
		{
			name:          "ParseTaskStatus",
			err:           mustErr("ParseTaskStatus", taskStatusErr),
			wantMsg:       `invalid task status: "BOGUS"`,
			wantSentinels: []error{ErrInvalidTaskStatus},
		},
		{
			name:          "ParseSprintStatus",
			err:           mustErr("ParseSprintStatus", sprintStatusErr),
			wantMsg:       `invalid sprint status: "BOGUS"`,
			wantSentinels: []error{ErrInvalidSprintStatus},
		},
		{
			name:          "ParseAuditOperation",
			err:           mustErr("ParseAuditOperation", auditOpErr),
			wantMsg:       `invalid audit operation: "BOGUS"`,
			wantSentinels: []error{ErrInvalidAuditOperation},
		},
		{
			name:          "ParseEntityType",
			err:           mustErr("ParseEntityType", entityTypeErr),
			wantMsg:       `invalid entity type: "BOGUS"`,
			wantSentinels: []error{ErrInvalidEntityType},
		},
		{
			name:          "ValidateStatusTransition/current",
			err:           mustErr("current", ValidateStatusTransition("NOPE", "DOING")),
			wantMsg:       `invalid current status: "NOPE"`,
			wantSentinels: []error{ErrInvalidCurrentStatus},
		},
		{
			name:          "ValidateStatusTransition/target",
			err:           mustErr("target", ValidateStatusTransition("DOING", "NOPE")),
			wantMsg:       `invalid target status: "NOPE"`,
			wantSentinels: []error{ErrInvalidTargetStatus},
		},
		{
			// SPEC/STATE_MACHINE.md spells this rejection as
			// fmt.Errorf("cannot transition from %q to %q", ...) — with no
			// trailing sentinel — which is exactly what this asserts.
			name:          "ValidateStatusTransition/forbidden",
			err:           mustErr("forbidden", ValidateStatusTransition("BACKLOG", "COMPLETED")),
			wantMsg:       `cannot transition from "BACKLOG" to "COMPLETED"`,
			wantSentinels: []error{ErrCannotTransition},
		},
		{
			name:          "Task.Validate/priority",
			err:           mustErr("priority", badPriority.Validate()),
			wantMsg:       "validation error: priority must be between 0 and 9, got 42",
			wantSentinels: []error{utils.ErrValidation, ErrPriorityOutOfRange},
		},
		{
			name:          "Task.Validate/severity",
			err:           mustErr("severity", badSeverity.Validate()),
			wantMsg:       "validation error: severity must be between 0 and 9, got 42",
			wantSentinels: []error{utils.ErrValidation, ErrSeverityOutOfRange},
		},
		{
			name:          "Task.Validate/status",
			err:           mustErr("status", badStatus.Validate()),
			wantMsg:       `invalid status: "NOPE"`,
			wantSentinels: []error{ErrInvalidStatus},
		},
		{
			name:          "Task.Validate/type",
			err:           mustErr("type", badType.Validate()),
			wantMsg:       `invalid type: "NOPE"`,
			wantSentinels: []error{ErrInvalidType},
		},
		{
			name:          "TaskUpdate.Validate/priority",
			err:           mustErr("update priority", (&TaskUpdate{Priority: &outOfRange}).Validate()),
			wantMsg:       "validation error: priority must be between 0 and 9, got 42",
			wantSentinels: []error{utils.ErrValidation, ErrPriorityOutOfRange},
		},
		{
			name:          "TaskUpdate.Validate/severity",
			err:           mustErr("update severity", (&TaskUpdate{Severity: &outOfRange}).Validate()),
			wantMsg:       "validation error: severity must be between 0 and 9, got 42",
			wantSentinels: []error{utils.ErrValidation, ErrSeverityOutOfRange},
		},
		{
			// The `--limit` range rule, converged by rmp task 329. The two
			// bounds differ and the sentence does not: that is the whole of
			// what the task fixed, and asserting both messages side by side is
			// what keeps the difference confined to the number.
			name:          "ValidateTaskLimit",
			err:           mustErr("task limit", ValidateTaskLimit(0)),
			wantMsg:       "validation error: limit must be between 1 and 100, got 0",
			wantSentinels: []error{utils.ErrValidation, ErrTaskLimitOutOfRange},
		},
		{
			name:          "ValidateAuditLimit",
			err:           mustErr("audit limit", ValidateAuditLimit(501)),
			wantMsg:       "validation error: limit must be between 1 and 500, got 501",
			wantSentinels: []error{utils.ErrValidation, ErrAuditLimitOutOfRange},
		},
		{
			name: "AuditEntry.Validate/operation",
			err: mustErr("audit operation", (&AuditEntry{
				Operation: "NOPE", EntityType: string(EntityTask), EntityID: 1, PerformedAt: dedupValidCreatedAt,
			}).Validate()),
			wantMsg:       `invalid operation: "NOPE"`,
			wantSentinels: []error{ErrInvalidOperation},
		},
		{
			name: "AuditEntry.Validate/entity type",
			err: mustErr("audit entity type", (&AuditEntry{
				Operation: validAuditOp, EntityType: "NOPE", EntityID: 1, PerformedAt: dedupValidCreatedAt,
			}).Validate()),
			wantMsg:       `invalid entity type: "NOPE"`,
			wantSentinels: []error{ErrInvalidEntityType},
		},
		{
			name: "AuditEntry.Validate/entity id",
			err: mustErr("audit entity id", (&AuditEntry{
				Operation: validAuditOp, EntityType: string(EntityTask), EntityID: 0, PerformedAt: dedupValidCreatedAt,
			}).Validate()),
			// The id range rule, converged by rmp task 330. This sentinel
			// used to word one of the rule's two bounds and name neither
			// ("entity_id must be positive"), which made it a fourth wording
			// of the very refusal `audit list --entity-id` and `audit history`
			// print — over the identical field and under the identical name.
			// It now takes the whole sentence from the shared definition, and
			// chains utils.ErrValidation as every other range refusal does.
			wantMsg:       "validation error: entity_id must be between 1 and 2147483647, got 0",
			wantSentinels: []error{utils.ErrValidation, ErrEntityIDOutOfRange},
		},
	}
}

// TestRejectionMessagesAreExact pins the complete rendered text of every enum
// and range rejection, together with the sentinel chain that gives it its exit
// code. Restoring the duplicated form on any one of them fails this test.
func TestRejectionMessagesAreExact(t *testing.T) {
	for _, tc := range dedupCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.wantMsg {
				t.Errorf("rendered message differs\n got: %q\nwant: %q", got, tc.wantMsg)
			}
			for _, want := range tc.wantSentinels {
				if !errors.Is(tc.err, want) {
					t.Errorf("errors.Is must hold for %q, which is what maps this failure to its exit code; got %v",
						want, tc.err)
				}
			}
		})
	}
}

// TestRejectionsNeverRepeatASentinel is the general net behind the exact
// assertions: no rejection may render any sentinel's text more than once. It
// covers sentinels a case did not name — the doubling was originally spread
// across three files and seventeen call sites, and an exact-message list only
// protects the cases somebody remembered to add.
func TestRejectionsNeverRepeatASentinel(t *testing.T) {
	for _, tc := range dedupCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			for _, s := range allSentinels {
				if n := strings.Count(msg, s.Error()); n > 1 {
					t.Errorf("sentinel text %q appears %d times in one message; it must appear once\n message: %q",
						s.Error(), n, msg)
				}
			}
		})
	}
}
