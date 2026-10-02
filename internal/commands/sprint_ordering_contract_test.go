package commands

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The gates of rmp tasks 420, 565 and 467 over the five sprint ordering commands
// (`sprint reorder`, `move-to`, `swap`, `top`, `bottom`; SPEC/COMMANDS.md § Task
// Ordering):
//
//   - a task that is not a member of the sprint is refused with the membership
//     line of the batch assignment commands, `task N is not in sprint #M`, naming
//     the first such task in the order the command line supplied them;
//   - a missing selector exits 3 before any id is validated and before an
//     unrecognised flag is reported;
//   - each success object is written from a struct, with the published keys in
//     the published order, and its bytes are the bytes the map literal it
//     replaced produced.

// orderingFixture is a roadmap with one sprint of five members and one task
// outside the sprint, all created through the real commands.
type orderingFixture struct {
	roadmap  string
	sprintID int
	members  []int
	outsider int
}

func setupOrderingFixture(t *testing.T, roadmap string) *orderingFixture {
	t.Helper()

	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	t.Cleanup(cleanup)

	f := &orderingFixture{roadmap: roadmap}
	f.sprintID = createSprintViaCommand(t, roadmap, "Payout reconciliation", "Reconcile every payout against the bank file.")
	for _, title := range []string{
		"Parse the bank settlement file",
		"Match payouts to settlement lines",
		"Report unmatched payouts",
		"Alert on a missing settlement file",
		"Archive reconciled batches",
	} {
		f.members = append(f.members, mkSprintTask(t, roadmap, title))
	}
	run(t, func() error {
		return sprintAddTasks([]string{"-r", roadmap, itoa(f.sprintID), csv(f.members...)})
	})
	f.outsider = mkSprintTask(t, roadmap, "Rotate the bank SFTP credentials")
	return f
}

// order reads the sprint's members in position order through `sprint tasks`.
func (f *orderingFixture) order(t *testing.T) []int {
	t.Helper()
	out := captureStdout(t, func() {
		if err := sprintTasks([]string{"-r", f.roadmap, itoa(f.sprintID)}); err != nil {
			t.Fatalf("sprint tasks: %v", err)
		}
	})
	var rows []struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("decoding `sprint tasks`: %v", err)
	}
	ids := make([]int, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

// TestSprintOrdering_ANonMemberIsRefusedWithTheAssignmentLine drives all five
// commands with a task that is not a member, and requires the one line the batch
// assignment commands print for the same condition, exit code 6, and a sprint
// whose order did not move.
func TestSprintOrdering_ANonMemberIsRefusedWithTheAssignmentLine(t *testing.T) {
	f := setupOrderingFixture(t, "ordering-membership-line")
	before := f.order(t)
	m, x, s := f.members, itoa(f.outsider), itoa(f.sprintID)
	want := fmt.Sprintf("validation error: task %d is not in sprint #%d", f.outsider, f.sprintID)

	reordered := csv(m[1], m[0], m[2], m[3], f.outsider) // the right count, one outsider in place of a member
	cases := []struct {
		label string
		args  []string
	}{
		{"reorder", []string{"reorder", "-r", f.roadmap, s, reordered}},
		{"move-to", []string{"move-to", "-r", f.roadmap, s, x, "1"}},
		{"swap (first)", []string{"swap", "-r", f.roadmap, s, x, itoa(m[0])}},
		{"swap (second)", []string{"swap", "-r", f.roadmap, s, itoa(m[0]), x}},
		{"top", []string{"top", "-r", f.roadmap, s, x}},
		{"bottom", []string{"bottom", "-r", f.roadmap, s, x}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			out, err := dispatchInvocation(t, "sprint", tc.args...)
			assertPublishedRefusal(t, "sprint "+tc.label, err, utils.ErrValidation, 6, want)
			if out != "" {
				t.Errorf("a refused invocation wrote to stdout: %q", out)
			}
			if after := f.order(t); !slices.Equal(after, before) {
				t.Errorf("the refused invocation changed the order: %v -> %v", before, after)
			}
		})
	}
}

// TestSprintOrdering_TheFirstNonMemberInCommandLineOrderIsNamed pins the choice
// of N when several named tasks are not members: the first of them in the order
// the command line supplied them, on `reorder` and on `swap`, with the singular
// line and never the plural one of § Task Assignment. On `reorder` the count
// test comes first, so a list of the wrong length is refused with the count
// line even when it names a task from outside the sprint.
func TestSprintOrdering_TheFirstNonMemberInCommandLineOrderIsNamed(t *testing.T) {
	f := setupOrderingFixture(t, "ordering-first-non-member")
	second := mkSprintTask(t, f.roadmap, "Retire the legacy payout exporter")
	s, m := itoa(f.sprintID), f.members

	// Two outsiders, the higher id first: the refusal names it, not the lower.
	_, err := dispatchInvocation(t, "sprint", "reorder", "-r", f.roadmap, s,
		csv(m[0], second, m[2], f.outsider, m[4]))
	assertPublishedRefusal(t, "reorder with two outsiders", err, utils.ErrValidation, 6,
		fmt.Sprintf("validation error: task %d is not in sprint #%d", second, f.sprintID))

	_, err = dispatchInvocation(t, "sprint", "swap", "-r", f.roadmap, s, itoa(second), itoa(f.outsider))
	assertPublishedRefusal(t, "swap of two outsiders", err, utils.ErrValidation, 6,
		fmt.Sprintf("validation error: task %d is not in sprint #%d", second, f.sprintID))

	// The wrong count, carrying an outsider: the count test answers first.
	_, err = dispatchInvocation(t, "sprint", "reorder", "-r", f.roadmap, s, csv(m[0], f.outsider))
	assertPublishedRefusal(t, "reorder of the wrong length", err, utils.ErrValidation, 6,
		"validation error: expected 5 task IDs, got 2 (must include all sprint tasks)")
}

// TestSprintOrdering_AMissingSelectorExits3First pins the rows every table
// gained: with neither -r nor --roadmap, all five commands exit 3 before any id
// on the command line is validated and before an unrecognised flag is reported.
func TestSprintOrdering_AMissingSelectorExits3First(t *testing.T) {
	t.Setenv("HOME", shortHome(t))

	for _, args := range [][]string{
		{"reorder", "x", "1,2"},
		{"reorder", "1", "2,3", "--foo"},
		{"move-to", "1", "x", "0"},
		{"move-to", "1", "2", "0", "--foo"},
		{"swap", "1", "2", "x"},
		{"swap", "1", "2", "3", "--foo"},
		{"top", "1", "x"},
		{"top", "1", "2", "--foo"},
		{"bottom", "x", "2"},
		{"bottom", "1", "2", "--foo"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			_, err := dispatchInvocation(t, "sprint", args...)
			assertPublishedRefusal(t, "sprint "+strings.Join(args, " "), err, utils.ErrNoRoadmap, 3,
				"no roadmap selected: use -r <name> or --roadmap <name>")
		})
	}
}

// TestSprintOrdering_SuccessObjectsKeepTheirPublishedBytes pins rmp task 467 on
// each of the five success objects, three ways: the exact bytes, the key order
// the SPEC publishes, and byte identity with the map literal the struct
// replaced, rendered by the same encoder from the same values. The last is the
// proof that the change of representation changed no byte of the output.
func TestSprintOrdering_SuccessObjectsKeepTheirPublishedBytes(t *testing.T) {
	f := setupOrderingFixture(t, "ordering-success-bytes")
	s, m := f.sprintID, f.members

	reordered := []int{m[4], m[3], m[2], m[1], m[0]}
	cases := []struct {
		label  string
		args   []string
		keys   []string
		legacy map[string]any
	}{
		{
			label:  "reorder",
			args:   []string{"reorder", "-r", f.roadmap, itoa(s), csv(reordered...)},
			keys:   []string{"sprint_id", "success", "task_order"},
			legacy: map[string]any{"success": true, "sprint_id": s, "task_order": reordered},
		},
		{
			label:  "move-to",
			args:   []string{"move-to", "-r", f.roadmap, itoa(s), itoa(m[0]), "2"},
			keys:   []string{"position", "sprint_id", "success", "task_id"},
			legacy: map[string]any{"success": true, "sprint_id": s, "task_id": m[0], "position": 2},
		},
		{
			label:  "swap",
			args:   []string{"swap", "-r", f.roadmap, itoa(s), itoa(m[1]), itoa(m[3])},
			keys:   []string{"sprint_id", "success", "task_id_1", "task_id_2"},
			legacy: map[string]any{"success": true, "sprint_id": s, "task_id_1": m[1], "task_id_2": m[3]},
		},
		{
			label:  "top",
			args:   []string{"top", "-r", f.roadmap, itoa(s), itoa(m[2])},
			keys:   []string{"position", "sprint_id", "success", "task_id"},
			legacy: map[string]any{"success": true, "sprint_id": s, "task_id": m[2], "position": 0},
		},
		{
			label:  "bottom",
			args:   []string{"bottom", "-r", f.roadmap, itoa(s), itoa(m[2])},
			keys:   []string{"position", "sprint_id", "success", "task_id"},
			legacy: map[string]any{"success": true, "sprint_id": s, "task_id": m[2], "position": len(m) - 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			out, err := dispatchInvocation(t, "sprint", tc.args...)
			if err != nil {
				t.Fatalf("sprint %s: %v", tc.label, err)
			}

			legacy := captureStdout(t, func() {
				if err := utils.PrintJSON(tc.legacy); err != nil {
					t.Fatalf("rendering the map literal: %v", err)
				}
			})
			if out != legacy {
				t.Errorf("sprint %s wrote\n%s\nbut the map literal it replaced renders\n%s", tc.label, out, legacy)
			}

			if got := topLevelKeys(t, out); !slices.Equal(got, tc.keys) {
				t.Errorf("sprint %s writes the keys %v, want exactly %v in that order", tc.label, got, tc.keys)
			}
		})
	}
}

// topLevelKeys returns the keys of a JSON object in the order they are written.
func topLevelKeys(t *testing.T, doc string) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(doc))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("%q is not a JSON object: %v", doc, err)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("reading a key of %q: %v", doc, err)
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatalf("reading the value of %q: %v", tok, err)
		}
	}
	return keys
}
