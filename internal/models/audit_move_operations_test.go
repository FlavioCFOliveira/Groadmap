// Package models — tests for the two directions of a sprint move and for the
// operations that may name a counterpart entity (rmp task #263).
//
// SPRINT_MOVE_TASK said that a task had moved. It did not say which task, and it
// was written against the destination sprint alone, so the source sprint's
// history had no record of losing anything. SPRINT_MOVE_TASK_OUT and
// SPRINT_MOVE_TASK_IN replace it with one row per sprint, each naming the task,
// and the value they replace stays declared and stays valid, because the rows
// already carrying it must remain reachable by an `--operation` filter
// (SPEC/MODELS.md § Audit Operation, rule 3).
package models

import "testing"

// TestSprintMoveOperationsAreDeclaredAndValid pins the two constants and their
// values. The string literals are spelled out rather than derived from the
// constants: a test that compared a constant with itself would pass through any
// rename, and the value is what a stored row carries and what an `--operation`
// filter has to match.
func TestSprintMoveOperationsAreDeclaredAndValid(t *testing.T) {
	// In the order the canonical catalogue publishes them.
	want := []struct {
		op    AuditOperation
		value string
	}{
		{OpSprintMoveTaskOut, "SPRINT_MOVE_TASK_OUT"},
		{OpSprintMoveTaskIn, "SPRINT_MOVE_TASK_IN"},
	}

	for _, w := range want {
		t.Run(w.value, func(t *testing.T) {
			if string(w.op) != w.value {
				t.Errorf("the constant carries %q, want %q; the value is what a stored row holds",
					string(w.op), w.value)
			}
			if !IsValidAuditOperation(w.value) {
				t.Errorf("IsValidAuditOperation(%q) = false, so `audit list --operation %q` is rejected "+
					"and no code path may write the operation", w.value, w.value)
			}
			parsed, err := ParseAuditOperation(w.value)
			if err != nil {
				t.Errorf("ParseAuditOperation(%q) error = %v, want the constant", w.value, err)
			}
			if parsed != w.op {
				t.Errorf("ParseAuditOperation(%q) = %q, want %q", w.value, parsed, w.op)
			}

			entry := AuditEntry{
				Operation:   w.value,
				EntityType:  string(EntitySprint),
				EntityID:    3,
				PerformedAt: "2026-08-21T08:30:00.000Z",
			}
			if err := entry.Validate(); err != nil {
				t.Errorf("AuditEntry{Operation: %q}.Validate() = %v, want nil", w.value, err)
			}
		})
	}

	// Declared exactly once each, contiguously, in the catalogue's order: they
	// are one group in the catalogue and one group here.
	positions := make([]int, len(want))
	for i, w := range want {
		positions[i] = -1
		count := 0
		for j, op := range ValidAuditOperations {
			if op == w.op {
				count++
				if positions[i] < 0 {
					positions[i] = j
				}
			}
		}
		if count != 1 {
			t.Errorf("ValidAuditOperations lists %s %d times, want exactly 1", w.op, count)
		}
	}
	if positions[1] != positions[0]+1 {
		t.Errorf("ValidAuditOperations places %s at %d and %s at %d; the two are one group and appear in "+
			"the order the catalogue publishes them", want[0].op, positions[0], want[1].op, positions[1])
	}
}

// TestLegacySprintMoveTaskStaysValidAndUnwritten pins the LEGACY contract from
// the side this package owns. The command-level regression test in
// internal/commands asserts against a real database that nothing writes the
// value; what is asserted here is that it stays readable, because dropping it
// would strand every row a pre-1.12.0 binary wrote behind a filter value the CLI
// rejects.
//
// The migration cannot reclassify such a row, and the reason is in the row
// itself: it names one sprint and no task, so nothing in it says whether the
// sprint it names is the one the task left or the one it entered.
func TestLegacySprintMoveTaskStaysValidAndUnwritten(t *testing.T) {
	const legacy = "SPRINT_MOVE_TASK"

	if string(OpSprintMoveTask) != legacy {
		t.Fatalf("the legacy constant carries %q, want %q", OpSprintMoveTask, legacy)
	}
	if !IsValidAuditOperation(legacy) {
		t.Errorf("IsValidAuditOperation(%q) = false; a LEGACY operation is readable, so the filter must "+
			"accept it (SPEC/DATABASE.md § audit Table, Legacy)", legacy)
	}
	if _, err := ParseAuditOperation(legacy); err != nil {
		t.Errorf("ParseAuditOperation(%q) error = %v, want the constant", legacy, err)
	}

	// It is a member of the LEGACY group, whose position in the enum is pinned
	// by TestLegacyStatusChangeStaysValidAndUnwritten.
	found := false
	for _, op := range legacyAuditOperations {
		if op == OpSprintMoveTask {
			found = true
		}
	}
	if !found {
		t.Errorf("%s is not listed in legacyAuditOperations, so the tail assertion that keeps the LEGACY "+
			"group last does not cover it", legacy)
	}

	// A legacy operation may not name a counterpart: its rows predate the
	// column, and no code path writes the value again to fill one in.
	if OperationCarriesRelatedEntity(OpSprintMoveTask) {
		t.Errorf("OperationCarriesRelatedEntity(%s) = true; the operation is LEGACY and the two that "+
			"replace it are the ones that name the task", legacy)
	}
}

// TestOperationCarriesRelatedEntity pins the eight-operation answer that the
// single audit writer enforces at the point of the INSERT. The check is
// exhaustive over the whole valid set, so an operation added later is covered
// without anyone remembering to extend this test: only the eight named below may
// answer true.
//
// The predicate answers MAY, not MUST, and TASK_STATUS_BACKLOG is why: it is
// written by `sprint remove-tasks`, which names the sprint the task left, and by
// `task stat <ids> BACKLOG`, which has no second entity to name. Which of the
// two wrote a row is known only to the call site; what the operation alone
// decides is whether a counterpart is admissible at all.
func TestOperationCarriesRelatedEntity(t *testing.T) {
	permitted := map[AuditOperation]bool{
		OpSprintAddTask:     true,
		OpTaskStatusSprint:  true,
		OpSprintRemoveTask:  true,
		OpTaskStatusBacklog: true,
		OpSprintMoveTaskOut: true,
		OpSprintMoveTaskIn:  true,
		OpTaskAddDep:        true,
		OpTaskRemoveDep:     true,
	}

	for _, op := range ValidAuditOperations {
		if got, want := OperationCarriesRelatedEntity(op), permitted[op]; got != want {
			t.Errorf("OperationCarriesRelatedEntity(%s) = %v, want %v; related_entity_id belongs to the "+
				"eight operations of SPEC/DATABASE.md § The Two Entities of a Relational Operation and "+
				"is NULL on every other one", op, got, want)
		}
	}

	// The eight really are in the valid set, so the loop above compared
	// something for each of them.
	for op := range permitted {
		if !IsValidAuditOperation(string(op)) {
			t.Errorf("%s is not in the valid set, so the assertion above is vacuous for it", op)
		}
	}
	if len(permitted) != 8 {
		t.Errorf("the permitted set holds %d operations, want the 8 the catalogue's table lists",
			len(permitted))
	}

	// The two columns are independent: no operation carries both a commit hash
	// and a counterpart, which is why the writer checks them separately rather
	// than deciding once per row.
	for _, op := range ValidAuditOperations {
		if OperationCarriesCommitHash(op) && OperationCarriesRelatedEntity(op) {
			t.Errorf("%s is listed as carrying both a commit hash and a counterpart; the catalogue gives "+
				"the two columns disjoint operation sets", op)
		}
	}
}
