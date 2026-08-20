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
	{"Task", Task{}, 232, "SPEC/MODELS.md § Memory Layout Optimization, Task struct"},
	{"SprintStats", SprintStats{}, 112, "SPEC/MODELS.md § Memory Layout Optimization, SprintStats struct"},
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
	{"TaskUpdate", TaskUpdate{}},
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
