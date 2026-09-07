package utils

import (
	"errors"
	"testing"
)

// Regression tests for SPEC/COMMANDS.md § Task ID Lists (Batch Commands).
//
// The defect these guard was a count comparison standing in for a set test:
//
//	if len(tasks) != len(ids) { ...not found... }
//
// The database returns one row per DISTINCT id, so a repeated id made the counts
// differ even when every id existed. `rmp task get 7,7` was refused with
// `resource not found` on all six task batch commands and on the three sprint
// assignment ones -- a valid invocation rejected, over a cause that had not
// occurred.

func TestDistinctIDs_KeepsFirstOccurrenceOrder(t *testing.T) {
	got := DistinctIDs([]int{3, 1, 3, 2, 1})

	want := []int{3, 1, 2}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order must follow each id's FIRST occurrence, not sorted or "+
				"last-wins (§ Task ID Lists rule 2); want %v, got %v", want, got)
		}
	}
}

func TestMissingIDs_ARepeatedIDIsNotMissing(t *testing.T) {
	// The exact shape of the bug: every id exists, one is repeated.
	if got := MissingIDs([]int{7, 7}, []int{7}); len(got) != 0 {
		t.Errorf("a repeated id names the same task twice and is not a missing id; "+
			"reporting one is what refused `task get 7,7` over a task that exists "+
			"(§ Task ID Lists, \"The test is on the set, never on a count\").\ngot: %v", got)
	}
}

func TestMissingIDs_NamesOnlyTheAbsentOnes(t *testing.T) {
	got := MissingIDs([]int{1, 99999, 2}, []int{1, 2})

	if len(got) != 1 || got[0] != 99999 {
		t.Errorf("the list carries exactly the ids that resolved to nothing, and no "+
			"id that resolved (§ Task ID Lists rule 1).\ngot: %v", got)
	}
}

func TestMissingIDs_AnAbsentIDIsNamedOnce(t *testing.T) {
	got := MissingIDs([]int{1, 99999, 1, 99999}, []int{1})

	if len(got) != 1 || got[0] != 99999 {
		t.Errorf("an id the caller repeated is reported once (§ Task ID Lists rule 3).\ngot: %v", got)
	}
}

func TestMissingIDs_IgnoresExtraFoundIDs(t *testing.T) {
	// found may legitimately carry ids the caller did not ask about -- a sprint's
	// full membership, for instance -- and that must not change the answer.
	if got := MissingIDs([]int{1}, []int{1, 2, 3}); len(got) != 0 {
		t.Errorf("ids present but unrequested must not affect the result.\ngot: %v", got)
	}
}

func TestTasksNotFoundError_WordingFollowsTheMissingCount(t *testing.T) {
	for _, tc := range []struct {
		name    string
		missing []int
		want    string
	}{
		{"none", nil, ""},
		{"one", []int{99999}, "resource not found: task 99999 not found"},
		{"two", []int{88888, 99999}, "resource not found: tasks 88888, 99999 not found"},
	} {
		err := TasksNotFoundError(tc.missing)
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s: an empty list is not an error, so a call site may pass "+
					"MissingIDs unconditionally; got %v", tc.name, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%s: want an error", tc.name)
		}
		if err.Error() != tc.want {
			t.Errorf("%s: want %q, got %q", tc.name, tc.want, err.Error())
		}
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: must carry ErrNotFound so the CLI maps it to exit 4", tc.name)
		}
	}
}

// TestTasksNotFoundError_SingularForOneOfMany is rule 4 stated as a test: the
// wording follows how many ids are MISSING, never how many were supplied.
func TestTasksNotFoundError_SingularForOneOfMany(t *testing.T) {
	// Five ids supplied, one missing.
	err := TasksNotFoundError(MissingIDs([]int{1, 2, 3, 99999, 5}, []int{1, 2, 3, 5}))

	if err == nil || err.Error() != "resource not found: task 99999 not found" {
		t.Errorf("one missing id out of five supplied prints the SINGULAR line "+
			"(§ Task ID Lists rule 4).\ngot: %v", err)
	}
}

func TestTasksNotInSprintError_WordingAndSentinel(t *testing.T) {
	one := TasksNotInSprintError([]int{4}, 1)
	if one == nil || one.Error() != "validation error: task 4 is not in sprint #1" {
		t.Errorf("singular membership line; got %v", one)
	}
	if !errors.Is(one, ErrValidation) {
		t.Error("membership is a validation failure, not a lookup failure: the tasks " +
			"exist, so it carries ErrValidation and exit 6, never ErrNotFound")
	}

	many := TasksNotInSprintError([]int{4, 7}, 2)
	if many == nil || many.Error() != "validation error: tasks 4, 7 are not in sprint #2" {
		t.Errorf("plural membership line; got %v", many)
	}

	if TasksNotInSprintError(nil, 1) != nil {
		t.Error("an empty list is not an error")
	}
}

// TestJoinIDs_PublishedRendering pins the separator. Go's own slice rendering
// was used before and produced "[4 4]" -- bracketed, space separated, and
// undeduplicated -- which no error table ever published.
func TestJoinIDs_PublishedRendering(t *testing.T) {
	if got := JoinIDs([]int{4, 7, 11}); got != "4, 7, 11" {
		t.Errorf("ids are separated by a comma and a space, with no brackets "+
			"(SPEC/COMMANDS.md § Error Output, the <ids> placeholder); got %q", got)
	}
}
