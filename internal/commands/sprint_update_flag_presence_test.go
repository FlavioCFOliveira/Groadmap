// Package commands — regression tests for the flag-presence contract of
// `sprint update` (rmp task #270).
//
// The command used to read the empty string as "flag absent". One sentinel, and
// every decision downstream of it was wrong for the same input:
//
//   - `sprint update <id> -t ""` reported `required parameter missing: at least
//     one of --title, --description, --max-tasks or --order is required` and
//     exited 2, for an invocation that had supplied one of them.
//   - `sprint update <id> -t "" -d "New description"` exited 0. The empty title
//     was silently dropped, the description was written, and the audit log
//     recorded one entry for an invocation that had supplied two flags.
//   - `task edit <id> -t ""` exited 6 all along, so the two commands answered the
//     identical input shape with two different verdicts.
//
// SPEC/COMMANDS.md § Update Sprint settles it: the at-least-one requirement
// "counts the flags the invocation supplies, not the values they carry", a
// supplied-but-empty title or description is a rejected VALUE (exit code 6), and
// the audit trigger is "the presence of the flag, not a difference in value".
//
// Four properties are pinned here, each separately because each can be
// implemented almost right:
//
//  1. **A supplied empty value is rejected, not ignored.** Both `-t ""` and
//     `-d ""` exit 6, and neither reports a missing parameter.
//  2. **Rejection mutates nothing.** `-t "" -d "text"` leaves both columns at
//     their stored values and writes no audit entry — and, because the check
//     runs before the database is even opened, it rejects an id that does not
//     exist too, which is what tells "validated before the write transaction"
//     apart from "rolled back after it".
//  3. **`sprint update` and `task edit` agree.** Same sentinel, same exit code,
//     same message, for the same input shape.
//  4. **Presence, and only presence, drives the whole update.** Over all
//     sixteen supplied/omitted combinations of the four flags: the flagless one
//     is the only exit 2, and each of the other fifteen writes exactly the
//     columns it supplied, exactly the matching audit entries, in statement
//     order, sharing one performed_at.
//
// The row-level assertions reuse the helpers of field_edit_audit_test.go and
// task_status_audit_test.go, which read the audit table straight out of SQLite:
// a read path that dropped a column would otherwise hide the defect from the
// very tests meant to catch it.
package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The values a matrix subtest supplies. Each differs from the seeded value in
// setupFieldEditRoadmap, so "the column changed" and "the column was left alone"
// are distinguishable observations.
const (
	updatedSprintTitle       = "Read-path performance and caching"
	updatedSprintDescription = "Cut the cost of every read command the CLI runs."
	updatedSprintMaxTasks    = 12
	updatedSprintOrder       = 9
)

// wantEmptyTitleMsg and wantEmptyDescriptionMsg are the runtime messages, with
// the utils.ErrValidation prefix the wrapper contributes. They are spelled out
// rather than composed, so a change to either half of the string is a failure
// here rather than a silent divergence from `task edit`.
const (
	wantEmptyTitleMsg       = "validation error: title cannot be empty"
	wantEmptyDescriptionMsg = "validation error: description cannot be empty"
)

// exitCodeFor mirrors the sentinel switch of cmd/rmp/main.go's handleError, so a
// change to the sentinels these errors chain shows up here as a failure instead
// of silently changing the exit code the user sees. The constants are repeated
// rather than imported because they live in package main; they must match the
// block in cmd/rmp/main.go, which SPEC/ARCHITECTURE.md fixes.
//
// The ErrUnknownCommand case is listed first because handleError lists it
// first: it must never be shadowed by a broader class.
func exitCodeFor(err error) int {
	const (
		exitSuccess     = 0
		exitFailure     = 1
		exitMisuse      = 2
		exitNoRoadmap   = 3
		exitNotFound    = 4
		exitExists      = 5
		exitInvalidData = 6
		exitCmdNotFound = 127
	)

	if err == nil {
		return exitSuccess
	}
	switch {
	case errors.Is(err, utils.ErrUnknownCommand):
		return exitCmdNotFound
	case errors.Is(err, utils.ErrNotFound):
		return exitNotFound
	case errors.Is(err, utils.ErrAlreadyExists):
		return exitExists
	case errors.Is(err, utils.ErrNoRoadmap):
		return exitNoRoadmap
	case errors.Is(err, utils.ErrValidation), errors.Is(err, utils.ErrFieldTooLarge):
		return exitInvalidData
	case errors.Is(err, utils.ErrInvalidInput), errors.Is(err, utils.ErrRequired):
		return exitMisuse
	}
	return exitFailure
}

// runSprintUpdate runs `sprint update` against the fixture's first sprint and
// returns the error, whatever it is. It is the rejecting counterpart of
// fieldEditFixture.update, which fails the test on any error.
func runSprintUpdate(t *testing.T, f *fieldEditFixture, flags ...string) error {
	t.Helper()

	args := append([]string{"-r", f.roadmap, itoa(f.sprintID)}, flags...)
	var err error
	out := captureStdout(t, func() { err = sprintUpdate(args) })
	if err != nil && out != "" {
		t.Errorf("rejected `sprint update %v` printed %q to stdout; errors go to stderr", args, out)
	}
	return err
}

// storedSprint reads the fixture's first sprint back. The seeded values are
// taken from the row rather than from the constants of setupFieldEditRoadmap,
// because the fixture picks its sprint out of a list ordered by created_at and
// both of its sprints are created within the same second.
func storedSprint(t *testing.T, f *fieldEditFixture) *models.Sprint {
	t.Helper()

	sprint, err := f.database.GetSprint(context.Background(), f.sprintID)
	if err != nil {
		t.Fatalf("reading sprint %d back: %v", f.sprintID, err)
	}
	return sprint
}

// assertSprintMatches requires every field of the fixture's sprint to still
// equal the snapshot taken before the invocation.
func assertSprintMatches(t *testing.T, f *fieldEditFixture, want *models.Sprint) {
	t.Helper()

	got := storedSprint(t, f)
	if got.Title != want.Title {
		t.Errorf("title = %q, want the stored %q; a rejected update changes nothing",
			got.Title, want.Title)
	}
	if got.Description != want.Description {
		t.Errorf("description = %q, want the stored %q; a rejected update changes nothing",
			got.Description, want.Description)
	}
	if !equalIntPtr(got.MaxTasks, want.MaxTasks) {
		t.Errorf("max_tasks = %v, want the stored %v; a rejected update changes nothing",
			derefInt(got.MaxTasks), derefInt(want.MaxTasks))
	}
	if got.Order != want.Order {
		t.Errorf("order_index = %d, want the stored %d; a rejected update changes nothing",
			got.Order, want.Order)
	}
}

// equalIntPtr compares two optional ints, NULL included.
func equalIntPtr(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// derefInt renders an optional int for a failure message, NULL included.
func derefInt(p *int) any {
	if p == nil {
		return "NULL"
	}
	return *p
}

// ---------------------------------------------------------------------------
// 1. A supplied empty value is rejected, not ignored
// ---------------------------------------------------------------------------

// TestSprintUpdate_ASuppliedEmptyValueIsRejectedNotReportedMissing is the first
// reported case of task #270 and acceptance criterion 5 of SPEC/COMMANDS.md
// § Update Sprint: `-t ""` and `-d ""` each exit 6 naming the empty value, and
// neither claims a parameter is missing — the invocation supplied one.
func TestSprintUpdate_ASuppliedEmptyValueIsRejectedNotReportedMissing(t *testing.T) {
	cases := []struct {
		name    string
		flags   []string
		wantMsg string
	}{
		{"title", []string{"-t", ""}, wantEmptyTitleMsg},
		{"title long form", []string{"--title", ""}, wantEmptyTitleMsg},
		{"title inline form", []string{"--title="}, wantEmptyTitleMsg},
		{"description", []string{"-d", ""}, wantEmptyDescriptionMsg},
		{"description long form", []string{"--description", ""}, wantEmptyDescriptionMsg},
		{"description inline form", []string{"--description="}, wantEmptyDescriptionMsg},
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setupFieldEditRoadmap(t, "sprintupdempty"+itoa(i))
			before := readAuditTable(t, f.database)
			seeded := storedSprint(t, f)

			err := runSprintUpdate(t, f, c.flags...)
			if err == nil {
				t.Fatalf("sprint update %v was accepted; an empty value is rejected", c.flags)
			}
			if got := exitCodeFor(err); got != 6 {
				t.Errorf("sprint update %v exits %d, want 6: %v", c.flags, got, err)
			}
			if errors.Is(err, utils.ErrRequired) {
				t.Errorf("sprint update %v reports a missing parameter (%v); the flag WAS supplied, "+
					"so the at-least-one requirement is met and only the value is rejected", c.flags, err)
			}
			if err.Error() != c.wantMsg {
				t.Errorf("sprint update %v error = %q, want %q", c.flags, err.Error(), c.wantMsg)
			}

			assertSprintMatches(t, f, seeded)
			if after := readAuditTable(t, f.database); len(after) != len(before) {
				t.Errorf("the rejected update wrote %d audit entries, want 0: %v",
					len(after)-len(before), operationsOf(after[len(before):]))
			}
		})
	}
}

// TestSprintUpdate_NoFlagAtAllStillReportsTheMissingParameter is acceptance
// criterion 7: keying on presence must not lose the requirement itself. The
// flagless invocation is the only one that reports a missing parameter, and it
// still exits 2 with the documented message.
func TestSprintUpdate_NoFlagAtAllStillReportsTheMissingParameter(t *testing.T) {
	f := setupFieldEditRoadmap(t, "sprintupdnoflag")
	before := readAuditTable(t, f.database)
	seeded := storedSprint(t, f)

	err := runSprintUpdate(t, f)
	if err == nil {
		t.Fatal("a flagless `sprint update` was accepted; at least one flag is required")
	}
	if !errors.Is(err, utils.ErrRequired) {
		t.Errorf("error = %v, want it to chain utils.ErrRequired", err)
	}
	if got := exitCodeFor(err); got != 2 {
		t.Errorf("exit code = %d, want 2", got)
	}
	const want = "required parameter missing: at least one of --title, --description, --max-tasks or --order is required"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}

	assertSprintMatches(t, f, seeded)
	if after := readAuditTable(t, f.database); len(after) != len(before) {
		t.Errorf("the rejected update wrote %d audit entries, want 0", len(after)-len(before))
	}
}

// ---------------------------------------------------------------------------
// 2. Rejection mutates nothing
// ---------------------------------------------------------------------------

// TestSprintUpdate_AnEmptyValueBesideAValidOneMutatesNothing is the second
// reported case of task #270 and acceptance criterion 6. It is the case the old
// sentinel got worst: the empty title looked absent, the valid description kept
// the update alive, so the command exited 0 having written one field and one
// audit entry for an invocation that supplied two flags.
//
// Both orderings are exercised, because an implementation that validates as it
// walks the flags can be right for one and wrong for the other.
func TestSprintUpdate_AnEmptyValueBesideAValidOneMutatesNothing(t *testing.T) {
	cases := []struct {
		name    string
		flags   []string
		wantMsg string
	}{
		{"empty title, valid description", []string{"-t", "", "-d", updatedSprintDescription}, wantEmptyTitleMsg},
		{"valid title, empty description", []string{"-t", updatedSprintTitle, "-d", ""}, wantEmptyDescriptionMsg},
		{"empty title, valid max-tasks", []string{"-t", "", "--max-tasks", itoa(updatedSprintMaxTasks)}, wantEmptyTitleMsg},
		{"empty description, valid order", []string{"-d", "", "--order", itoa(updatedSprintOrder)}, wantEmptyDescriptionMsg},
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setupFieldEditRoadmap(t, "sprintupdpair"+itoa(i))
			before := readAuditTable(t, f.database)
			seeded := storedSprint(t, f)

			err := runSprintUpdate(t, f, c.flags...)
			if err == nil {
				t.Fatalf("sprint update %v was accepted; the empty value is rejected whatever "+
					"else the invocation supplies", c.flags)
			}
			if got := exitCodeFor(err); got != 6 {
				t.Errorf("sprint update %v exits %d, want 6: %v", c.flags, got, err)
			}
			if err.Error() != c.wantMsg {
				t.Errorf("sprint update %v error = %q, want %q", c.flags, err.Error(), c.wantMsg)
			}

			// Neither field moved: not the empty one, and not the valid one
			// that travelled with it.
			assertSprintMatches(t, f, seeded)

			after := readAuditTable(t, f.database)
			if len(after) != len(before) {
				t.Errorf("the rejected update wrote %d audit entries, want 0: %v",
					len(after)-len(before), operationsOf(after[len(before):]))
			}
		})
	}
}

// TestSprintUpdate_AnEmptyValueIsRejectedBeforeTheDatabaseIsOpened separates the
// two ways acceptance criterion 6 could be satisfied. A rejection that rolled
// the transaction back would first have had to resolve the sprint, so it would
// report a missing sprint for an id that does not exist. Validating first
// reports the empty value instead — which is what this asserts, and what makes
// "nothing was mutated" a property of the control flow rather than of the
// rollback.
func TestSprintUpdate_AnEmptyValueIsRejectedBeforeTheDatabaseIsOpened(t *testing.T) {
	f := setupFieldEditRoadmap(t, "sprintupdprecheck")
	before := readAuditTable(t, f.database)

	args := []string{"-r", f.roadmap, "99999", "-t", ""}
	var err error
	_ = captureStdout(t, func() { err = sprintUpdate(args) })
	if err == nil {
		t.Fatal("sprint update on a missing sprint with an empty title was accepted")
	}
	if errors.Is(err, utils.ErrNotFound) {
		t.Errorf("error = %v; the sprint was resolved before the value was validated, so the "+
			"rejection happens inside the write path rather than ahead of it", err)
	}
	if err.Error() != wantEmptyTitleMsg {
		t.Errorf("error = %q, want %q", err.Error(), wantEmptyTitleMsg)
	}

	if after := readAuditTable(t, f.database); len(after) != len(before) {
		t.Errorf("the rejected update wrote %d audit entries, want 0", len(after)-len(before))
	}
}

// ---------------------------------------------------------------------------
// 3. `sprint update` and `task edit` agree
// ---------------------------------------------------------------------------

// TestSprintUpdate_AndTaskEditAnswerAnEmptyTitleIdentically is the third
// reported case of task #270: the two commands used to contradict each other for
// the identical input shape, `-t ""`. They must now agree on the sentinel, on
// the exit code, and on the wording.
func TestSprintUpdate_AndTaskEditAnswerAnEmptyTitleIdentically(t *testing.T) {
	f := setupFieldEditRoadmap(t, "sprintupdvstaskedit")

	sprintErr := runSprintUpdate(t, f, "-t", "")

	taskArgs := []string{"-r", f.roadmap, itoa(f.taskID), "-t", ""}
	var taskErr error
	_ = captureStdout(t, func() { taskErr = taskEdit(taskArgs) })

	if sprintErr == nil || taskErr == nil {
		t.Fatalf("both commands must reject an empty title; sprint update = %v, task edit = %v",
			sprintErr, taskErr)
	}
	if got, want := exitCodeFor(sprintErr), exitCodeFor(taskErr); got != want {
		t.Errorf("sprint update -t \"\" exits %d and task edit -t \"\" exits %d; the identical input "+
			"shape gets the identical verdict\nsprint: %v\ntask:   %v", got, want, sprintErr, taskErr)
	}
	if got := exitCodeFor(sprintErr); got != 6 {
		t.Errorf("both exit %d, want 6", got)
	}
	if sprintErr.Error() != taskErr.Error() {
		t.Errorf("sprint update says %q and task edit says %q; the wording is shared",
			sprintErr.Error(), taskErr.Error())
	}
	if sprintErr.Error() != wantEmptyTitleMsg {
		t.Errorf("error = %q, want %q", sprintErr.Error(), wantEmptyTitleMsg)
	}
}

// ---------------------------------------------------------------------------
// 4. Presence, and only presence, drives the whole update
// ---------------------------------------------------------------------------

// TestSprintUpdate_ThePresenceMatrixDrivesColumnsAndEntries walks all sixteen
// supplied/omitted combinations of the four flags. For each one it requires the
// columns the invocation supplied to carry the new values, the columns it
// omitted to carry the seeded ones, and the audit entries to be exactly the
// supplied fields in statement order, sharing one performed_at.
//
// The single omitted-everything combination is the only exit 2, which is the
// requirement stated as a partition of the whole input space rather than as one
// example of it.
func TestSprintUpdate_ThePresenceMatrixDrivesColumnsAndEntries(t *testing.T) {
	type field struct {
		name  string
		flags []string
		op    models.AuditOperation
	}
	// Declared in the order the UPDATE applies the columns (title,
	// description, max_tasks, order_index), so the expected operation slice a
	// combination builds is already in statement order.
	fields := []field{
		{"title", []string{"-t", updatedSprintTitle}, models.OpSprintTitleChange},
		{"description", []string{"-d", updatedSprintDescription}, models.OpSprintDescriptionChange},
		{"max-tasks", []string{"--max-tasks", itoa(updatedSprintMaxTasks)}, models.OpSprintMaxTasksChange},
		{"order", []string{"--order", itoa(updatedSprintOrder)}, models.OpSprintOrderChange},
	}

	for mask := range 1 << len(fields) {
		var (
			flags []string
			want  []models.AuditOperation
			names []string
		)
		for i, fl := range fields {
			if mask&(1<<i) == 0 {
				continue
			}
			flags = append(flags, fl.flags...)
			want = append(want, fl.op)
			names = append(names, fl.name)
		}

		label := "none"
		if len(names) > 0 {
			label = strings.Join(names, "+")
		}

		t.Run(label, func(t *testing.T) {
			f := setupFieldEditRoadmap(t, "sprintupdmatrix"+itoa(mask))
			before := len(auditRecordsForSprint(t, f.database, f.sprintID))
			seeded := storedSprint(t, f)

			err := runSprintUpdate(t, f, flags...)

			if mask == 0 {
				if err == nil || !errors.Is(err, utils.ErrRequired) {
					t.Fatalf("the flagless invocation returned %v; it is the only one that reports "+
						"a missing parameter", err)
				}
				if got := exitCodeFor(err); got != 2 {
					t.Errorf("exit code = %d, want 2", got)
				}
				assertSprintMatches(t, f, seeded)
				return
			}

			if err != nil {
				t.Fatalf("sprint update %v was rejected: %v", flags, err)
			}

			// Every column the invocation supplied carries the new value;
			// every column it omitted still carries the seeded one.
			expected := *seeded
			if mask&1 != 0 {
				expected.Title = updatedSprintTitle
			}
			if mask&2 != 0 {
				expected.Description = updatedSprintDescription
			}
			if mask&4 != 0 {
				mt := updatedSprintMaxTasks
				expected.MaxTasks = &mt
			}
			if mask&8 != 0 {
				expected.Order = updatedSprintOrder
			}

			got := storedSprint(t, f)
			if got.Title != expected.Title {
				t.Errorf("title = %q, want %q", got.Title, expected.Title)
			}
			if got.Description != expected.Description {
				t.Errorf("description = %q, want %q", got.Description, expected.Description)
			}
			if !equalIntPtr(got.MaxTasks, expected.MaxTasks) {
				t.Errorf("max_tasks = %v, want %v", derefInt(got.MaxTasks), derefInt(expected.MaxTasks))
			}
			if got.Order != expected.Order {
				t.Errorf("order_index = %d, want %d", got.Order, expected.Order)
			}

			records := auditRecordsForSprint(t, f.database, f.sprintID)
			assertFieldEntries(t, records, before, models.EntitySprint, f.sprintID, want)
		})
	}
}
