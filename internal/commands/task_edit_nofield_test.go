// Package commands — regression tests for a `task edit` that supplies no field
// (rmp task 492).
//
// The no-field early return used to sit ahead of the roadmap open, so an edit
// with no field exited 0 for a task the roadmap does not hold, for a roadmap
// that does not exist, and for a roadmap name that breaks § Roadmap Name
// Validation: three invocations the same command refuses as soon as one field is
// supplied. SPEC/COMMANDS.md § Edit Task, "A no-field edit resolves the roadmap
// and the task before the no-op", and its acceptance criteria 2, 6 and 7 fix the
// order: the no-op is reached only once the roadmap name, the roadmap and the
// task have all been resolved.
package commands

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestTaskEdit_NoFieldResolvesTheRoadmapAndTheTaskBeforeTheNoOp drives every
// outcome of a flagless `task edit` against one seeded roadmap.
//
// Each refusal is also compared with the refusal of the same invocation carrying
// one field, because the SPEC's requirement is that the two paths agree: the
// no-field path faces the same checks, in the same order, with the same lines.
func TestTaskEdit_NoFieldResolvesTheRoadmapAndTheTaskBeforeTheNoOp(t *testing.T) {
	f := setupFieldEditRoadmap(t, "fieldeditnofield")

	// run invokes `task edit` and returns what it wrote to stdout and its error.
	run := func(t *testing.T, args ...string) (string, error) {
		t.Helper()
		var err error
		out := captureStdout(t, func() { err = taskEdit(args) })
		return out, err
	}

	// refusalLine renders err as the line the top-level failure report prints,
	// failing the test when the invocation was accepted.
	refusalLine := func(t *testing.T, err error, args []string) string {
		t.Helper()
		if err == nil {
			t.Fatalf("task edit %q was accepted (exit 0); SPEC/COMMANDS.md § Edit Task refuses it", args)
		}
		return "Error: " + err.Error()
	}

	t.Run("a task the roadmap does not hold is refused with exit 4", func(t *testing.T) {
		const missing = 99999
		args := []string{"-r", f.roadmap, itoa(missing)}
		before := len(readAuditTable(t, f.database))

		out, err := run(t, args...)

		want := fmt.Sprintf("Error: resource not found: task %d not found", missing)
		if got := refusalLine(t, err, args); got != want {
			t.Errorf("task edit %q printed %q, want %q", args, got, want)
		}
		if code := exitCodeFor(err); code != 4 {
			t.Errorf("task edit %q exited %d, want 4", args, code)
		}
		if out != "" {
			t.Errorf("task edit %q wrote %q to stdout; a refusal writes nothing there", args, out)
		}
		if after := len(readAuditTable(t, f.database)); after != before {
			t.Errorf("task edit %q wrote %d audit entries, want 0", args, after-before)
		}

		fieldArgs := append(append([]string{}, args...), "-p", "3")
		_, fieldErr := run(t, fieldArgs...)
		if got := refusalLine(t, fieldErr, fieldArgs); got != want {
			t.Errorf("the field path printed %q and the no-field path %q; the two lines are one line", got, want)
		}
	})

	t.Run("a roadmap that does not exist is refused with exit 4", func(t *testing.T) {
		const absent = "ledger-archive"
		args := []string{"-r", absent, itoa(f.taskID)}

		out, err := run(t, args...)

		want := `Error: resource not found: roadmap "ledger-archive"`
		if got := refusalLine(t, err, args); got != want {
			t.Errorf("task edit %q printed %q, want %q", args, got, want)
		}
		if code := exitCodeFor(err); code != 4 {
			t.Errorf("task edit %q exited %d, want 4", args, code)
		}
		if out != "" {
			t.Errorf("task edit %q wrote %q to stdout; a refusal writes nothing there", args, out)
		}
		exists, existsErr := utils.RoadmapExists(absent)
		if existsErr != nil {
			t.Fatalf("checking whether roadmap %q exists: %v", absent, existsErr)
		}
		if exists {
			t.Errorf("task edit %q created roadmap %q; a refusal changes nothing", args, absent)
		}
	})

	// A reserved name and a malformed one reach § Roadmap Name Validation by
	// different rules, so each is driven on its own.
	for _, name := range []string{"con", "Bad Name!"} {
		t.Run(fmt.Sprintf("the invalid roadmap name %q is refused with exit 6", name), func(t *testing.T) {
			args := []string{"-r", name, itoa(f.taskID)}

			out, err := run(t, args...)

			got := refusalLine(t, err, args)
			if code := exitCodeFor(err); code != 6 {
				t.Errorf("task edit %q exited %d (%s), want 6", args, code, got)
			}
			if out != "" {
				t.Errorf("task edit %q wrote %q to stdout; a refusal writes nothing there", args, out)
			}

			fieldArgs := append(append([]string{}, args...), "-p", "3")
			_, fieldErr := run(t, fieldArgs...)
			if fieldGot := refusalLine(t, fieldErr, fieldArgs); fieldGot != got {
				t.Errorf("the field path printed %q and the no-field path %q; both publish the line of "+
					"§ Roadmap Name Validation", fieldGot, got)
			}
		})
	}

	t.Run("an existing task is the no-op", func(t *testing.T) {
		args := []string{"-r", f.roadmap, itoa(f.taskID)}
		auditBefore := len(readAuditTable(t, f.database))
		taskBefore, err := f.database.GetTask(context.Background(), f.taskID)
		if err != nil {
			t.Fatalf("reading the task before the edit: %v", err)
		}

		out, err := run(t, args...)

		if err != nil {
			t.Fatalf("task edit %q error = %v, want nil (exit 0)", args, err)
		}
		if out != "" {
			t.Errorf("task edit %q wrote %q to stdout, want nothing", args, out)
		}
		if after := len(readAuditTable(t, f.database)); after != auditBefore {
			t.Errorf("task edit %q wrote %d audit entries, want 0", args, after-auditBefore)
		}
		taskAfter, err := f.database.GetTask(context.Background(), f.taskID)
		if err != nil {
			t.Fatalf("reading the task after the edit: %v", err)
		}
		if !reflect.DeepEqual(taskBefore, taskAfter) {
			t.Errorf("task edit %q changed the task\nbefore: %+v\nafter:  %+v", args, taskBefore, taskAfter)
		}
	})
}
