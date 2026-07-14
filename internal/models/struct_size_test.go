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
	{"Task", Task{}, 240, "SPEC/MODELS.md § Memory Layout Optimization, Task struct"},
	{"SprintStats", SprintStats{}, 112, "SPEC/MODELS.md § Memory Layout Optimization, SprintStats struct"},
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
