package models

import (
	"reflect"
	"strconv"
	"testing"
)

// The struct layouts SPEC/MODELS.md § Memory Layout Optimization pins by name.
// The byte counts are the ones the SPEC states for 64-bit targets, and they are
// asserted only there: on a 32-bit target the pointer, string and slice headers
// are half as wide, so the numbers do not apply. The zero-padding property
// asserted below holds on every target.
var specifiedLayouts = []struct {
	name     string
	value    any
	sizeOn64 uintptr
	specSays string
}{
	{"Task", Task{}, 248, "SPEC/MODELS.md § Memory Layout Optimization, Task struct"},
	{"SprintStats", SprintStats{}, 112, "SPEC/MODELS.md § Memory Layout Optimization, SprintStats struct"},
	{"AuditEntry", AuditEntry{}, 80, "SPEC/MODELS.md § Memory Layout Optimization, AuditEntry struct"},
	{"TaskComment", TaskComment{}, 72, "SPEC/MODELS.md § Memory Layout Optimization, TaskComment and SprintComment structs"},
	{"SprintComment", SprintComment{}, 72, "SPEC/MODELS.md § Memory Layout Optimization, TaskComment and SprintComment structs"},
}

// The domain structs whose field order the fieldalignment linter governs. None
// of them may carry padding: a struct that does has had a field moved out of
// the order the linter produced (SPEC/MODELS.md § Struct Field Ordering).
var paddingFreeStructs = []struct {
	name  string
	value any
}{
	{"Task", Task{}},
	{"Sprint", Sprint{}},
	{"SprintStats", SprintStats{}},
	{"AuditEntry", AuditEntry{}},
	{"TaskComment", TaskComment{}},
	{"SprintComment", SprintComment{}},
}

// TestSpecifiedStructSizes asserts the byte counts SPEC/MODELS.md pins.
//
// It replaces a benchmark that logged the sizes with b.Logf and asserted
// nothing at all, so it could never fail: the specified layout was enforced by
// nobody, and a field reordering that reintroduced padding would have shipped
// unnoticed.
func TestSpecifiedStructSizes(t *testing.T) {
	if strconv.IntSize != 64 {
		// Not a skipped test: these byte counts are simply not specified for
		// this target, and TestDomainStructsCarryNoPadding still holds the
		// layout to account here.
		t.Logf("target is %d-bit; SPEC/MODELS.md pins these sizes for 64-bit targets only", strconv.IntSize)
		return
	}

	for _, layout := range specifiedLayouts {
		t.Run(layout.name, func(t *testing.T) {
			got := reflect.TypeOf(layout.value).Size()
			if got != layout.sizeOn64 {
				t.Errorf("%s is %d bytes, but %s specifies %d bytes.\n"+
					"Either the field order drifted from the one the fieldalignment linter produces, or a "+
					"field was added or removed. Reconcile the code and the SPEC before changing this number.",
					layout.name, got, layout.specSays, layout.sizeOn64)
			}
		})
	}
}

// TestDomainStructsCarryNoPadding asserts the property the SPEC actually
// depends on: the compiler inserts no padding into the domain structs. It holds
// on any target, and it keeps holding when a field is added, because it weighs
// the struct against the sum of its own fields rather than against a constant.
func TestDomainStructsCarryNoPadding(t *testing.T) {
	for _, s := range paddingFreeStructs {
		t.Run(s.name, func(t *testing.T) {
			tp := reflect.TypeOf(s.value)

			var sumOfFields uintptr
			for i := range tp.NumField() {
				sumOfFields += tp.Field(i).Type.Size()
			}

			size := tp.Size()
			if size != sumOfFields {
				t.Errorf("%s occupies %d bytes but its fields account for only %d: the compiler inserted "+
					"%d bytes of padding.\nRun the fieldalignment linter and adopt the order it produces "+
					"(SPEC/MODELS.md § Struct Field Ordering).",
					s.name, size, sumOfFields, size-sumOfFields)
			}
		})
	}
}

// TestCommentStructsKeepThePointerPrefixShort asserts the one property that
// decides the field order of the two comment structs. Both are 72 bytes whatever
// the order, because every field is 8-byte aligned, so the byte count cannot
// catch a reordering. What fieldalignment enforces here is the pointer-scan
// prefix: with the *string first and the three string headers after it, the last
// word that can hold a pointer ends at byte 48. Moving UpdatedAt after the
// strings pushes that boundary to 56 and the linter rejects the struct with
// "struct with 56 pointer bytes could be 48" — a validation-gate failure, not a
// style preference (SPEC/MODELS.md § Memory Layout Optimization).
func TestCommentStructsKeepThePointerPrefixShort(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Logf("target is %d-bit; the 48-byte prefix is stated for 64-bit targets only", strconv.IntSize)
		return
	}

	const wantPointerBytes = 48

	for _, s := range []struct {
		name  string
		value any
	}{
		{"TaskComment", TaskComment{}},
		{"SprintComment", SprintComment{}},
	} {
		t.Run(s.name, func(t *testing.T) {
			tp := reflect.TypeOf(s.value)

			if first := tp.Field(0); first.Name != "UpdatedAt" || first.Offset != 0 {
				t.Errorf("%s declares %q at offset %d first; UpdatedAt must come first",
					s.name, first.Name, first.Offset)
			}

			// The pointer-scan prefix ends after the last field that can hold a
			// pointer. For every kind present here (*T, string, int) the pointer
			// word, when there is one, is the field's first word.
			var pointerBytes uintptr
			for i := range tp.NumField() {
				f := tp.Field(i)
				switch f.Type.Kind() {
				case reflect.Pointer, reflect.String:
					pointerBytes = f.Offset + 8
				case reflect.Int:
					// Holds no pointer; must trail the pointer-bearing fields.
					if f.Offset < pointerBytes {
						t.Errorf("%s places the scalar %q at offset %d, inside the pointer prefix",
							s.name, f.Name, f.Offset)
					}
				default:
					t.Fatalf("%s field %q has kind %s, which this test does not model; extend it",
						s.name, f.Name, f.Type.Kind())
				}
			}

			if pointerBytes != wantPointerBytes {
				t.Errorf("%s has %d pointer bytes, want %d. The field order drifted from the one "+
					"fieldalignment produces; golangci-lint will reject it (SPEC/MODELS.md § Memory "+
					"Layout Optimization).", s.name, pointerBytes, wantPointerBytes)
			}
		})
	}
}

// TestTaskPointerGroupMatchesTheSpecifiedOrder pins the first of the four groups
// SPEC/MODELS.md § Memory Layout Optimization states for the Task struct: seven
// pointer fields, in the named order, occupying the leading 56 bytes.
//
// The byte count alone cannot catch every drift here. Every Task field is 8-byte
// aligned, so the struct is 248 bytes whatever the order and TestSpecifiedStructSizes
// stays green while the pointer group is permuted or a *string is swapped for a
// string of the same width. What fieldalignment actually decides is which fields
// lead, and the two commit hashes added at schema 1.11.0 belong in that group
// immediately after CompletionSummary — where the SPEC puts them, and where the
// linter keeps them.
func TestTaskPointerGroupMatchesTheSpecifiedOrder(t *testing.T) {
	// SPEC/MODELS.md § Memory Layout Optimization, Task struct, Group 1.
	want := []string{
		"ParentTaskID", "CompletionSummary", "CommitOpen", "CommitClose",
		"TestedAt", "ClosedAt", "StartedAt",
	}

	tp := reflect.TypeOf(Task{})

	var got []string
	for i := range tp.NumField() {
		f := tp.Field(i)
		if f.Type.Kind() != reflect.Pointer {
			break
		}
		got = append(got, f.Name)
	}

	if len(got) != len(want) {
		t.Fatalf("Task leads with %d pointer fields %v, but SPEC/MODELS.md specifies %d: %v",
			len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Task pointer field %d is %q, want %q (SPEC/MODELS.md § Memory Layout "+
				"Optimization, Task struct, Group 1: %v)", i, got[i], want[i], want)
		}
		if off := tp.Field(i).Offset; off != uintptr(i)*8 {
			t.Errorf("Task field %q sits at offset %d, want %d: the pointer group must be "+
				"contiguous from offset 0", got[i], off, i*8)
		}
	}

	// No pointer may hide behind the string, slice and int groups: the whole
	// point of the ordering is that the pointer-scan prefix ends at byte 56.
	for i := len(want); i < tp.NumField(); i++ {
		if f := tp.Field(i); f.Type.Kind() == reflect.Pointer {
			t.Errorf("Task declares the pointer field %q at offset %d, after the pointer group; "+
				"it belongs in Group 1", f.Name, f.Offset)
		}
	}
}

// TestTaskCommitFieldsAreNullableStrings pins the two commit-tracking fields to
// the Go type and the JSON tags SPEC/MODELS.md § Task gives them. They are
// *string, not string: SPEC/VERSION.md § Migration 1.10.0 → 1.11.0 requires the
// columns to be nullable because no truthful value exists for work already done,
// and a plain string could not carry that distinction — a task that never
// entered DOING would serialise as "" instead of null.
func TestTaskCommitFieldsAreNullableStrings(t *testing.T) {
	tp := reflect.TypeOf(Task{})

	for _, want := range []struct{ field, jsonTag string }{
		{"CommitOpen", "commit_open"},
		{"CommitClose", "commit_close"},
	} {
		f, ok := tp.FieldByName(want.field)
		if !ok {
			t.Errorf("Task has no field %s (SPEC/MODELS.md § Task)", want.field)
			continue
		}
		if f.Type.Kind() != reflect.Pointer || f.Type.Elem().Kind() != reflect.String {
			t.Errorf("Task.%s is %s, want *string (SPEC/MODELS.md § Task)", want.field, f.Type)
		}
		if tag := f.Tag.Get("json"); tag != want.jsonTag {
			t.Errorf("Task.%s carries the JSON tag %q, want %q (SPEC/DATA_FORMATS.md § Task)",
				want.field, tag, want.jsonTag)
		}
	}
}
