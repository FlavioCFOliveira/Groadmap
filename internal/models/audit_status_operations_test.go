// Package models — tests for the five destination-named status operations and
// for the two nullable fields of an audit entry (rmp task #262).
//
// The catalogue used to hold one status operation, TASK_STATUS_CHANGE, which
// said that a task's status changed and nothing about where it went. The five
// operations tested here name the destination, so a reader learns the outcome
// from the operation value alone; the sixth stays declared and stays valid,
// because the rows already carrying it must remain reachable by an
// `--operation` filter (SPEC/MODELS.md § Audit Operation, rule 3).
package models

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"testing"
)

// TestTaskStatusOperationsAreDeclaredAndValid pins the five constants and their
// values. The string literals are spelled out rather than derived from the
// constants: a test that compared a constant with itself would pass through any
// rename, and the value is what a stored row carries and what an `--operation`
// filter has to match.
func TestTaskStatusOperationsAreDeclaredAndValid(t *testing.T) {
	// In the order the canonical catalogue publishes them.
	want := []struct {
		op    AuditOperation
		value string
	}{
		{OpTaskStatusBacklog, "TASK_STATUS_BACKLOG"},
		{OpTaskStatusSprint, "TASK_STATUS_SPRINT"},
		{OpTaskStatusDoing, "TASK_STATUS_DOING"},
		{OpTaskStatusTesting, "TASK_STATUS_TESTING"},
		{OpTaskStatusCompleted, "TASK_STATUS_COMPLETED"},
	}

	for _, w := range want {
		t.Run(w.value, func(t *testing.T) {
			if string(w.op) != w.value {
				t.Errorf("the constant carries %q, want %q; the value is what a stored row holds",
					string(w.op), w.value)
			}
			if !IsValidAuditOperation(w.value) {
				t.Errorf("IsValidAuditOperation(%q) = false, so `audit list --operation %q` is "+
					"rejected and no code path may write the operation", w.value, w.value)
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
				EntityType:  string(EntityTask),
				EntityID:    42,
				PerformedAt: "2026-08-20T09:00:00.000Z",
			}
			if err := entry.Validate(); err != nil {
				t.Errorf("AuditEntry{Operation: %q}.Validate() = %v, want nil", w.value, err)
			}
		})
	}

	// Declared exactly once each, in the catalogue's order, and contiguously:
	// they are one group in the catalogue and one group here.
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
	for i := 1; i < len(positions); i++ {
		if positions[i] != positions[i-1]+1 {
			t.Errorf("ValidAuditOperations places %s at %d and %s at %d; the five are one group and "+
				"appear in the order the catalogue publishes them (SPEC/MODELS.md § Audit Operation, "+
				"rule 1)", want[i-1].op, positions[i-1], want[i].op, positions[i])
		}
	}
}

// TestLegacyStatusChangeStaysValidAndUnwritten pins the LEGACY contract from the
// side this package owns. The value is never written by any code path — a
// property the command-level regression test in internal/commands asserts
// against a real database — but it stays in the valid set here, because dropping
// it would leave the rows a pre-1.12.0 binary wrote, and that the migration could
// not reclassify, with no filter value that reaches them.
func TestLegacyStatusChangeStaysValidAndUnwritten(t *testing.T) {
	const legacy = "TASK_STATUS_CHANGE"

	if string(OpTaskStatusChange) != legacy {
		t.Fatalf("the legacy constant carries %q, want %q", OpTaskStatusChange, legacy)
	}
	if !IsValidAuditOperation(legacy) {
		t.Errorf("IsValidAuditOperation(%q) = false; a LEGACY operation is readable, so the filter "+
			"must accept it (SPEC/DATABASE.md § audit Table, Legacy)", legacy)
	}
	if _, err := ParseAuditOperation(legacy); err != nil {
		t.Errorf("ParseAuditOperation(%q) error = %v, want the constant", legacy, err)
	}

	// LEGACY is not the same as uncatalogued: TASK_ASSIGN and TASK_UNASSIGN are
	// retained on disk but unreachable by name, and this value is the opposite.
	for _, retired := range retiredAuditOperations {
		if IsValidAuditOperation(retired) {
			t.Errorf("%q is valid; a retired operation is NOT in the valid set, unlike a legacy one",
				retired)
		}
	}

	// The LEGACY group is published last, as the catalogue publishes it, and in
	// the catalogue's order within the group. Asserting the whole tail rather
	// than only its first member is what keeps the property true as the group
	// grows: a legacy value appended anywhere else would still be readable, but
	// the enum would no longer read as the catalogue reads.
	tail := ValidAuditOperations[len(ValidAuditOperations)-len(legacyAuditOperations):]
	for i, want := range legacyAuditOperations {
		if tail[i] != want {
			t.Errorf("the LEGACY tail of ValidAuditOperations is %v, want %v; the group comes last, as "+
				"it does in the catalogue", tail, legacyAuditOperations)
			break
		}
	}
}

// legacyAuditOperations are the members of the catalogue's LEGACY group —
// readable, never written — in the order the catalogue publishes them. All four
// are here: TASK_UPDATE and SPRINT_UPDATE joined the group when the per-field
// operations of `task edit` and `sprint update` took over from them.
var legacyAuditOperations = []AuditOperation{
	OpTaskStatusChange,
	OpTaskUpdate,
	OpSprintUpdate,
	OpSprintMoveTask,
}

// TestOperationCarriesCommitHash pins the two-operation answer that the single
// audit writer enforces at the point of the INSERT. The check is exhaustive over
// the whole valid set, so an operation added later is covered without anyone
// remembering to extend this test: only the two named below may answer true.
func TestOperationCarriesCommitHash(t *testing.T) {
	permitted := map[AuditOperation]bool{
		OpTaskStatusDoing:     true,
		OpTaskStatusCompleted: true,
	}

	for _, op := range ValidAuditOperations {
		if got, want := OperationCarriesCommitHash(op), permitted[op]; got != want {
			t.Errorf("OperationCarriesCommitHash(%s) = %v, want %v; commit_hash belongs to "+
				"TASK_STATUS_DOING and TASK_STATUS_COMPLETED alone (SPEC/DATABASE.md § The Commit "+
				"Hash of an Audit Entry)", op, got, want)
		}
	}

	// The two really are in the valid set, so the loop above compared something.
	for op := range permitted {
		if !IsValidAuditOperation(string(op)) {
			t.Errorf("%s is not in the valid set, so the assertion above is vacuous for it", op)
		}
	}
}

// TestAuditEntryNullableFieldsArePointers pins the Go types and the JSON tags
// SPEC/MODELS.md § Audit Entry gives the two nullable fields. They are *int and
// *string, not int and string: an entity id of 0 and an empty hash are both
// values the column CHECK constraints reject, so a non-pointer field could not
// tell absence from corruption.
func TestAuditEntryNullableFieldsArePointers(t *testing.T) {
	tp := reflect.TypeOf(AuditEntry{})

	for _, want := range []struct {
		field, jsonTag string
		elemKind       reflect.Kind
	}{
		{"RelatedEntityID", "related_entity_id", reflect.Int},
		{"CommitHash", "commit_hash", reflect.String},
	} {
		f, ok := tp.FieldByName(want.field)
		if !ok {
			t.Errorf("AuditEntry has no field %s (SPEC/MODELS.md § Audit Entry)", want.field)
			continue
		}
		if f.Type.Kind() != reflect.Pointer || f.Type.Elem().Kind() != want.elemKind {
			t.Errorf("AuditEntry.%s is %s, want *%s", want.field, f.Type, want.elemKind)
		}
		if tag := f.Tag.Get("json"); tag != want.jsonTag {
			t.Errorf("AuditEntry.%s carries the JSON tag %q, want %q (SPEC/DATA_FORMATS.md § Audit "+
				"Entry)", want.field, tag, want.jsonTag)
		}
	}

	// The two pointers lead the struct, which is what keeps the pointer-scan
	// prefix at 56 bytes and the fieldalignment linter quiet
	// (SPEC/MODELS.md § Memory Layout Optimization).
	if strconv.IntSize == 64 {
		for i, name := range []string{"RelatedEntityID", "CommitHash"} {
			f := tp.Field(i)
			if f.Name != name {
				t.Errorf("AuditEntry field %d is %q, want %q: the pointer group leads the struct",
					i, f.Name, name)
			}
			if f.Offset != uintptr(i)*8 {
				t.Errorf("AuditEntry field %q sits at offset %d, want %d", f.Name, f.Offset, i*8)
			}
		}
	}
}

// TestAuditEntryMarshalsAbsenceAsNull is acceptance criterion 2 of
// SPEC/MODELS.md § Audit Entry, and the reason the two fields are pointers at
// all. An entry that carries neither value must render both keys with the value
// null — never 0 and never "" — because a consumer distinguishes "this operation
// had no counterpart" from "it had one and it went unrecorded" by exactly that.
func TestAuditEntryMarshalsAbsenceAsNull(t *testing.T) {
	empty := AuditEntry{
		ID:          1,
		Operation:   string(OpTaskStatusTesting),
		EntityType:  string(EntityTask),
		EntityID:    42,
		PerformedAt: "2026-08-20T09:00:00.000Z",
	}

	encoded, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshalling an entry that carries neither value: %v", err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding %s: %v", encoded, err)
	}

	for _, key := range []string{"related_entity_id", "commit_hash"} {
		raw, present := decoded[key]
		if !present {
			t.Errorf("the key %q is absent from %s; every entry carries all seven keys, the nullable "+
				"ones with the value null (SPEC/DATA_FORMATS.md § Audit Entry)", key, encoded)
			continue
		}
		if string(raw) != "null" {
			t.Errorf("the key %q renders as %s, want null", key, raw)
		}
	}

	// A present value round-trips as itself, so the null above is a statement
	// about absence rather than about the field never carrying anything.
	related := 7
	hash := "5f93b51"
	filled := AuditEntry{
		RelatedEntityID: &related,
		CommitHash:      &hash,
		ID:              2,
		Operation:       string(OpTaskStatusDoing),
		EntityType:      string(EntityTask),
		EntityID:        42,
		PerformedAt:     "2026-08-20T09:05:00.000Z",
	}
	encoded, err = json.Marshal(filled)
	if err != nil {
		t.Fatalf("marshalling a filled entry: %v", err)
	}

	var back AuditEntry
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("decoding %s: %v", encoded, err)
	}
	if back.RelatedEntityID == nil || *back.RelatedEntityID != related {
		t.Errorf("related_entity_id round-tripped as %v, want %d", back.RelatedEntityID, related)
	}
	if back.CommitHash == nil || *back.CommitHash != hash {
		t.Errorf("commit_hash round-tripped as %v, want %q", back.CommitHash, hash)
	}
}

// TestAuditEntryValidateStillRejectsAnUnknownOperation guards the one thing the
// widened valid set could have loosened: adding five names must not turn
// Validate into a formality. errors.Is is used rather than a string comparison
// so the assertion survives a reworded message.
func TestAuditEntryValidateStillRejectsAnUnknownOperation(t *testing.T) {
	for _, name := range []string{"TASK_STATUS_ARCHIVED", "TASK_STATUS", "TASK_STATUS_DOING "} {
		entry := AuditEntry{
			Operation:   name,
			EntityType:  string(EntityTask),
			EntityID:    42,
			PerformedAt: "2026-08-20T09:00:00.000Z",
		}
		if err := entry.Validate(); !errors.Is(err, ErrInvalidOperation) {
			t.Errorf("AuditEntry{Operation: %q}.Validate() = %v, want ErrInvalidOperation", name, err)
		}
	}
}
