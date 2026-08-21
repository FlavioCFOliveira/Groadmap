package models

import (
	"errors"
	"testing"
)

// retiredAuditOperations are the two operation names the 1.9.0 → 1.10.0 change
// removed from the valid set together with the tasks.specialists column
// (rmp task #246). They were written by `task assign` and `task unassign`, which
// no longer exist.
var retiredAuditOperations = []string{"TASK_ASSIGN", "TASK_UNASSIGN"}

// TestRetiredAssignmentOperationsAreInvalid is the gate for acceptance criterion
// 4 of rmp task #246.
//
// The two names must be rejected by the same validation every other unknown name
// meets: they are not in ValidAuditOperations, IsValidAuditOperation says no, and
// ParseAuditOperation fails with ErrInvalidAuditOperation — which is what makes
// `audit list --operation TASK_ASSIGN` exit 6 rather than silently returning
// nothing (SPEC/DATABASE.md § audit Table, rule 1: "They are not in the valid
// set. […] Neither operation is reachable by name").
//
// This is deliberately NOT a statement that the rows are gone. Rule 2 of the same
// section requires the opposite — the stored rows are retained and keep being
// listed — and TestAssignmentAuditRowsSurviveTheMigration in internal/db pins
// that half. The two rules hold together: unwritable by name, undeleted on disk.
func TestRetiredAssignmentOperationsAreInvalid(t *testing.T) {
	for _, name := range retiredAuditOperations {
		t.Run(name, func(t *testing.T) {
			if IsValidAuditOperation(name) {
				t.Errorf("IsValidAuditOperation(%q) = true; the operation was retired with the "+
					"specialists field and no code path may write it again", name)
			}

			if _, err := ParseAuditOperation(name); !errors.Is(err, ErrInvalidAuditOperation) {
				t.Errorf("ParseAuditOperation(%q) error = %v, want ErrInvalidAuditOperation: a "+
					"filter naming a retired operation must be rejected as invalid input", name, err)
			}

			for _, op := range ValidAuditOperations {
				if string(op) == name {
					t.Errorf("ValidAuditOperations still contains %q", name)
				}
			}

			// An AuditEntry carrying the name must not validate, so the write
			// path cannot be talked into recording one.
			entry := AuditEntry{
				Operation:   name,
				EntityType:  string(EntityTask),
				EntityID:    42,
				PerformedAt: "2026-08-20T09:00:00.000Z",
			}
			if err := entry.Validate(); !errors.Is(err, ErrInvalidOperation) {
				t.Errorf("AuditEntry{Operation: %q}.Validate() error = %v, want ErrInvalidOperation",
					name, err)
			}
		})
	}
}

// TestValidAuditOperationsHasNoDuplicates guards the catalogue the coverage gate
// in spec_enum_coverage_test.go compares against: that test counts names, so a
// duplicate entry here would make its arithmetic report a phantom mismatch.
// Removing entries — as task #246 did — is exactly when a slice like this gets
// mis-edited.
func TestValidAuditOperationsHasNoDuplicates(t *testing.T) {
	seen := make(map[AuditOperation]struct{}, len(ValidAuditOperations))
	for _, op := range ValidAuditOperations {
		if _, dup := seen[op]; dup {
			t.Errorf("ValidAuditOperations lists %q more than once", op)
		}
		seen[op] = struct{}{}
		if op == "" {
			t.Error("ValidAuditOperations contains an empty operation name")
		}
	}
}
