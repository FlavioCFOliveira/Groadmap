// Package commands — the gate that holds `sprint add-tasks`, `remove-tasks`
// and `move-tasks` to the validation order SPEC/COMMANDS.md § Task Assignment
// publishes (rmp task #337).
//
// What went wrong, and why a test rather than a fix.
//
// The section published an order the binary did not follow: it put "validate
// sprint ID exists" ahead of "parse all task IDs", while all three commands
// finish reading the argv before they open the database. Eighteen of eighteen
// sibling commands do the same, and six other sections of COMMANDS.md publish
// parse-before-existence, one of them normatively. The specification was the
// wrong side, so it was rewritten to the measured order and no behaviour
// changed. What was missing, and is supplied here, is the test that fails if
// either side is ever moved without the other.
//
// How the expectation is derived.
//
// This file asserts NOTHING about which order is correct. It reads the numbered
// list out of the section, classifies each item into a closed enum, and then
// requires the binary to show the order the document declares. Swap two items
// in COMMANDS.md and the expectation swaps with them; change the binary while
// the document stands and the same assertion fails from the other side. That is
// what converges the two artefacts, which a transcribed order cannot do: a
// transcription only ever catches the code drifting away from a copy of the
// document made at the time the test was written.
//
// Extraction is in three phases, each with a loud failure.
//
//  1. Locate `**Validation Order:**` inside the section. Absent is fatal.
//  2. Consume a CONTIGUOUS run of `N. text` lines whose numbers are exactly
//     1, 2, 3, …, stopping at the first line that breaks the run. This matters:
//     § Task Assignment carries two further numbered lists — the "Four rules"
//     governing the audit entries, and the acceptance criteria — and a scan
//     that merely collected every numbered line in the section would swallow
//     both and hand the classifier text it cannot read. A floor on the run
//     length turns a scan that stopped matching into a failure rather than into
//     a gate that verifies nothing.
//  3. Classify each item by a narrow marker. An item matching zero markers, or
//     two, is a failure naming the file and the item — never a silent pass.
//
// Which pairs are driven, and why the rest are not.
//
// An ordering between two steps is observable only when one command line can
// fail both of them, so that the verdict says which check ran first. Four pairs
// qualify, and all four are driven below:
//
//   - the two lexical steps, which fail with the same exit code and are told
//     apart by which field the refusal names;
//   - the task-id lexical step against sprint existence (exit 2 against 4);
//   - the task-id lexical step against task existence (exit 2 against 4);
//   - the membership step against the execution step, whose discriminator is
//     exit 6 TOGETHER WITH the absence of a write, since a command that
//     executed before validating would leave the audit log longer.
//
// The remaining pairs are not orderings that any invocation can expose, and
// this file deliberately holds no probe for them rather than a probe that
// cannot fail:
//
//   - The re-parenting item has no failure mode at all. It records a check
//     `add-tasks` does not perform, so it can never lose a race.
//   - Opening the roadmap against every step that reads the database is a data
//     dependency, not a choice: whether sprint 7 exists is not a question that
//     can be asked of a database that was never opened.
//   - Steps governing DISJOINT commands have no ordering relation to observe.
//     Nothing runs both the capacity step and the membership step, because the
//     first belongs to `add-tasks` alone and the second to the other two.
//   - The execution step against the fail-fast step are the two arms of one
//     branch. A command takes one or the other, never both in sequence, so this
//     is a property — a rejected command writes nothing — and the membership
//     probe below already asserts it. It is not an ordering.
//
// Every probe carries a CONTROL that drives the suppressed step on its own. A
// probe asserting "the refusal was the lexical one" proves nothing if the other
// check would not have refused this input either: the control is what shows
// there really were two failures on the table and that the published order is
// what chose between them.
//
// This gate reads exit codes through exitCodeFor, which is a copy of the
// sentinel switch in cmd/rmp/main.go and therefore proves the sentinel chain
// rather than the process status. The number a caller actually branches on is
// asserted against the built binary in tests/test_06_edge_cases_errors.py,
// which derives its expectation from this same published list.
package commands

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Phase 1 and 2: reading the run out of the specification
// ---------------------------------------------------------------------------

const (
	// validationOrderLabel introduces the list inside § Task Assignment.
	validationOrderLabel = "**Validation Order:**"

	// minValidationSteps is the floor under the extracted run. The published
	// list is longer than this; a shorter one means the scan stopped matching,
	// and every assertion below would then be made against a fragment.
	minValidationSteps = 8

	// maxLinesBeforeFirstStep bounds the search for the head of the list, so a
	// list that stopped being a list cannot be replaced by the next numbered
	// list further down the section.
	maxLinesBeforeFirstStep = 24
)

// numberedListItem matches one item of a markdown ordered list.
var numberedListItem = regexp.MustCompile(`^(\d+)\. (.+)$`)

// extractValidationOrder returns the text of each item of the ordered list that
// follows validationOrderLabel, in published order. It is a pure function so
// that its own failure modes can be proved against synthetic text.
func extractValidationOrder(section string) ([]string, error) {
	at := strings.Index(section, validationOrderLabel)
	if at < 0 {
		return nil, fmt.Errorf("the section carries no %q label", validationOrderLabel)
	}
	lines := strings.Split(section[at+len(validationOrderLabel):], "\n")

	head := -1
	for i, line := range lines {
		if i > maxLinesBeforeFirstStep {
			break
		}
		if m := numberedListItem.FindStringSubmatch(line); m != nil {
			if m[1] != "1" {
				return nil, fmt.Errorf("the first numbered line after %q is %q, so the list does "+
					"not start at 1 and is not the run this gate can read",
					validationOrderLabel, line)
			}
			head = i
			break
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "**") {
			return nil, fmt.Errorf("the next thing after %q is %q, not an ordered list",
				validationOrderLabel, line)
		}
	}
	if head < 0 {
		return nil, fmt.Errorf("no ordered list starts within %d lines of %q",
			maxLinesBeforeFirstStep, validationOrderLabel)
	}

	items := make([]string, 0, len(lines)-head)
	for _, line := range lines[head:] {
		m := numberedListItem.FindStringSubmatch(line)
		if m == nil {
			break
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n != len(items)+1 {
			break
		}
		items = append(items, m[2])
	}
	if len(items) < minValidationSteps {
		return nil, fmt.Errorf("the run after %q holds %d items, want at least %d: the scan has "+
			"stopped matching the published list",
			validationOrderLabel, len(items), minValidationSteps)
	}
	return items, nil
}

// ---------------------------------------------------------------------------
// Phase 3: classifying each item
// ---------------------------------------------------------------------------

// assignmentStep is the closed set of validation steps § Task Assignment can
// publish. An item that is none of them is a failure, not a new member.
type assignmentStep int

const (
	stepUnclassified assignmentStep = iota
	stepSprintIDFormat
	stepTaskIDFormat
	stepRoadmapOpen
	stepSprintExists
	stepSprintNotClosed
	stepTasksExist
	stepCapacity
	stepMembership
	stepNoPriorSprintCheck
	stepExecute
	stepFailFast
)

func (s assignmentStep) String() string {
	switch s {
	case stepSprintIDFormat:
		return "the sprint-id lexical step"
	case stepTaskIDFormat:
		return "the task-id lexical step"
	case stepRoadmapOpen:
		return "the roadmap-open step"
	case stepSprintExists:
		return "the sprint-existence step"
	case stepSprintNotClosed:
		return "the CLOSED-sprint step"
	case stepTasksExist:
		return "the task-existence step"
	case stepCapacity:
		return "the capacity step"
	case stepMembership:
		return "the membership step"
	case stepNoPriorSprintCheck:
		return "the re-parenting item"
	case stepExecute:
		return "the execution step"
	case stepFailFast:
		return "the fail-fast step"
	case stepUnclassified:
		return "an unclassified item"
	}
	return "an unclassified item"
}

// assignmentStepMarkers recognises each step by a narrow phrase rather than by
// its ordinal, so renumbering the list does not disturb the classifier and
// rewording a step is a loud failure instead of a silent reclassification.
var assignmentStepMarkers = []struct {
	marker *regexp.Regexp
	step   assignmentStep
}{
	{regexp.MustCompile(`(?i)format and range of every sprint id`), stepSprintIDFormat},
	{regexp.MustCompile(`(?i)parse all task IDs and validate their format`), stepTaskIDFormat},
	{regexp.MustCompile(`(?i)open the roadmap`), stepRoadmapOpen},
	{regexp.MustCompile(`(?i)verify the sprint exists`), stepSprintExists},
	{regexp.MustCompile(`(?i)reject a CLOSED sprint`), stepSprintNotClosed},
	{regexp.MustCompile(`(?i)verify all task IDs exist in the roadmap`), stepTasksExist},
	{regexp.MustCompile(`max_tasks`), stepCapacity},
	{regexp.MustCompile(`(?i)currently a member of the sprint`), stepMembership},
	{regexp.MustCompile(`(?i)nothing is verified about the sprint a task already belongs to`), stepNoPriorSprintCheck},
	{regexp.MustCompile(`(?i)execute the operation`), stepExecute},
	{regexp.MustCompile(`(?i)exit immediately without making changes`), stepFailFast},
}

// matchAssignmentStep returns every step whose marker the item carries. Pure,
// so the "zero markers" and "two markers" verdicts can be proved directly.
func matchAssignmentStep(item string) []assignmentStep {
	matched := make([]assignmentStep, 0, 1)
	for _, m := range assignmentStepMarkers {
		if m.marker.MatchString(item) {
			matched = append(matched, m.step)
		}
	}
	return matched
}

// publishedValidationOrder is the three phases run together against the live
// document, with every failure mode fatal.
func publishedValidationOrder(t *testing.T) []assignmentStep {
	t.Helper()

	items, err := extractValidationOrder(taskAssignmentSection(t))
	if err != nil {
		t.Fatalf("reading the validation order out of %s %s: %v",
			assignmentSpecRelPath, assignmentHeading, err)
	}

	order := make([]assignmentStep, 0, len(items))
	for i, item := range items {
		matched := matchAssignmentStep(item)
		if len(matched) != 1 {
			t.Fatalf("%s %s validation order item %d matches %d of this gate's step markers (%v), "+
				"and exactly one must match. The item is:\n  %q\nA step this gate does not "+
				"recognise cannot be ordered against the others, so an unreadable item is a "+
				"failure and never a pass",
				assignmentSpecRelPath, assignmentHeading, i+1, len(matched), matched, item)
		}
		order = append(order, matched[0])
	}
	return order
}

// positionOf returns where a step stands in the published order.
func positionOf(t *testing.T, order []assignmentStep, step assignmentStep) int {
	t.Helper()

	for i, s := range order {
		if s == step {
			return i
		}
	}
	t.Fatalf("%s %s publishes no step this gate reads as %s, so the pair it belongs to cannot be "+
		"driven and this probe would assert nothing",
		assignmentSpecRelPath, assignmentHeading, step)
	return -1
}

// firstOf returns whichever of the two steps the document puts first. The
// verdict of every probe below is chosen by this function and by nothing else.
func firstOf(t *testing.T, order []assignmentStep, a, b assignmentStep) assignmentStep {
	t.Helper()

	if positionOf(t, order, a) < positionOf(t, order, b) {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// The probes
// ---------------------------------------------------------------------------

// Exit codes SPEC/COMMANDS.md § Task Assignment publishes for the steps driven
// below, taken from that section's own error table.
const (
	exitMalformedID  = 2 // "A task ID is not a positive integer"
	exitAbsentEntity = 4 // "Sprint ID does not exist" / "task IDs do not exist"
	exitNotMember    = 6 // "task IDs are not members"
)

// TestSprintAssignment_LexicalStepsRunInThePublishedOrder drives the two steps
// that read the argument text alone.
//
// Both refuse with the same exit code, so the discriminator is which field the
// refusal names. `add-tasks abc xyz` offers a malformed sprint id and a
// malformed task-id list at once; only the check that runs first is reported.
func TestSprintAssignment_LexicalStepsRunInThePublishedOrder(t *testing.T) {
	order := publishedValidationOrder(t)
	first := firstOf(t, order, stepSprintIDFormat, stepTaskIDFormat)

	want := "invalid sprint ID"
	if first == stepTaskIDFormat {
		want = "invalid task ID"
	}

	f := setupDensityRoadmap(t, "acquirer-lexical-order")

	err := runAssignment(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, "abc", "xyz"})
	})
	assertRefusal(t, err, exitMalformedID, want,
		fmt.Sprintf("%s %s puts %s first, so a command line that is malformed in both places is "+
			"refused by that one", assignmentSpecRelPath, assignmentHeading, first))

	// Control: the step the probe says was suppressed really does refuse this
	// argument on its own. Without it the probe would also pass if `xyz` were
	// somehow acceptable as a task-id list.
	err = runAssignment(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.planned), "xyz"})
	})
	assertRefusal(t, err, exitMalformedID, "invalid task ID",
		"control: the task-id lexical step refuses `xyz` when nothing runs ahead of it, so the "+
			"probe above really did have two failures to choose between")
}

// TestSprintAssignment_TaskIDParseRunsAgainstSprintExistence drives the pair the
// rewritten section turns on: whether the argv is finished before the database
// is consulted.
//
// `add-tasks <absent sprint> abc` names a sprint that does not exist and a task
// id that is not a number. The lexical step refuses with exit 2, the sprint
// existence step with exit 4, and the exit code says which one ran.
func TestSprintAssignment_TaskIDParseRunsAgainstSprintExistence(t *testing.T) {
	order := publishedValidationOrder(t)
	first := firstOf(t, order, stepTaskIDFormat, stepSprintExists)

	wantCode, wantText := exitMalformedID, "invalid task ID"
	if first == stepSprintExists {
		wantCode, wantText = exitAbsentEntity, "sprint"
	}

	f := setupDensityRoadmap(t, "acquirer-parse-before-lookup")
	absentSprint := absentSprintID(t, f)

	err := runAssignment(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(absentSprint), "abc"})
	})
	assertRefusal(t, err, wantCode, wantText,
		fmt.Sprintf("%s %s puts %s ahead of the other, so that is the refusal a caller sees when "+
			"both would fail", assignmentSpecRelPath, assignmentHeading, first))

	// Control: sprint existence really does refuse this sprint id.
	err = runAssignment(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(absentSprint), itoa(f.newTask(t))})
	})
	assertRefusal(t, err, exitAbsentEntity, fmt.Sprintf("sprint %d", absentSprint),
		"control: the sprint-existence step refuses this sprint id when the task ids parse, so the "+
			"probe above really did suppress a second failure")
}

// TestSprintAssignment_TaskIDParseRunsAgainstTaskExistence drives the lexical
// step against the existence lookup of the very ids it parses.
//
// The two are not the same check applied twice: an implementation that resolved
// each token as it read it would refuse the absent id before ever reaching the
// malformed one. `424242,abc` puts one of each on the command line.
func TestSprintAssignment_TaskIDParseRunsAgainstTaskExistence(t *testing.T) {
	order := publishedValidationOrder(t)
	first := firstOf(t, order, stepTaskIDFormat, stepTasksExist)

	wantCode, wantText := exitMalformedID, "invalid task ID"
	if first == stepTasksExist {
		wantCode, wantText = exitAbsentEntity, "task(s) not found"
	}

	f := setupDensityRoadmap(t, "acquirer-parse-before-task-lookup")
	absentTask := absentTaskID(t, f)

	err := runAssignment(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.planned), itoa(absentTask) + ",abc"})
	})
	assertRefusal(t, err, wantCode, wantText,
		fmt.Sprintf("%s %s puts %s ahead of the other, so that is the refusal a caller sees when "+
			"both would fail", assignmentSpecRelPath, assignmentHeading, first))

	// Control: the task-existence step really does refuse this id on its own.
	err = runAssignment(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.planned), itoa(absentTask)})
	})
	assertRefusal(t, err, exitAbsentEntity, fmt.Sprintf("[%d]", absentTask),
		"control: the task-existence step refuses this id when every token parses, so the probe "+
			"above really did suppress a second failure")
}

// TestSprintAssignment_MembershipRunsBeforeExecution drives the only pair whose
// discriminator is not an exit code alone.
//
// A `remove-tasks` naming a task the sprint does not hold must be refused with
// exit 6 AND must leave the database untouched. The second half is what makes
// the ordering visible: a command that executed first would have deleted
// nothing (the DELETE is scoped to the sprint) but would still have written its
// audit entries and reset the task, so the audit log is the witness.
func TestSprintAssignment_MembershipRunsBeforeExecution(t *testing.T) {
	order := publishedValidationOrder(t)
	membershipFirst := firstOf(t, order, stepMembership, stepExecute) == stepMembership

	f := setupDensityRoadmap(t, "acquirer-membership-before-write")
	stranger := f.newTask(t) // BACKLOG, belongs to no sprint
	before := f.snapshot(t)
	auditBefore := auditRowCount(t, f)

	err := runAssignment(t, func() error {
		return sprintRemoveTasks([]string{"-r", f.roadmap, itoa(f.running), itoa(stranger)})
	})

	if !membershipFirst {
		// The other arm: the document would be declaring that the operation
		// runs before the check that could stop it, so the write must be there.
		if auditRowCount(t, f) == auditBefore {
			t.Fatalf("%s %s puts %s ahead of %s, so `remove-tasks` would have written before "+
				"discovering that task %d is not a member; the audit log did not grow",
				assignmentSpecRelPath, assignmentHeading, stepExecute, stepMembership, stranger)
		}
		return
	}

	assertRefusal(t, err, exitNotMember, fmt.Sprintf("task(s) not in sprint #%d", f.running),
		fmt.Sprintf("%s %s puts %s ahead of %s", assignmentSpecRelPath, assignmentHeading,
			stepMembership, stepExecute))

	if after := f.snapshot(t); !sameMemberships(after, before) {
		t.Errorf("a refused `remove-tasks` changed sprint membership: %v became %v", before, after)
	}
	if got := auditRowCount(t, f); got != auditBefore {
		t.Errorf("a refused `remove-tasks` wrote %d audit entries; a command rejected during "+
			"validation reaches no write at all", got-auditBefore)
	}

	// Control: the execution step really does write on this fixture, so
	// "nothing was written" above is a fact about the refusal and not about a
	// command that never writes anything.
	member := f.order(t, f.running)[0]
	if err := runAssignment(t, func() error {
		return sprintRemoveTasks([]string{"-r", f.roadmap, itoa(f.running), itoa(member)})
	}); err != nil {
		t.Fatalf("control: `remove-tasks` refused member %d of sprint %d: %v",
			member, f.running, err)
	}
	if got := auditRowCount(t, f); got <= auditBefore {
		t.Errorf("control: removing member %d left the audit log at %d entries, so the execution "+
			"step writes nothing and the probe above proved nothing", member, got)
	}
	if after := f.snapshot(t); sameMemberships(after, before) {
		t.Errorf("control: removing member %d left sprint membership unchanged, so the execution "+
			"step is a no-op and the probe above proved nothing", member)
	}
}

// ---------------------------------------------------------------------------
// Probe plumbing
// ---------------------------------------------------------------------------

// runAssignment runs one command handler and returns its error, keeping the
// handler's stdout out of the test log.
func runAssignment(t *testing.T, fn func() error) error {
	t.Helper()

	var err error
	_ = captureStdout(t, func() { err = fn() })
	return err
}

// assertRefusal requires one refusal to carry both the exit code a caller
// branches on and the text that says which check produced it.
func assertRefusal(t *testing.T, err error, wantCode int, wantText, why string) {
	t.Helper()

	if err == nil {
		t.Fatalf("the command succeeded; %s, so it must be refused with exit %d naming %q",
			why, wantCode, wantText)
	}
	if got := exitCodeFor(err); got != wantCode {
		t.Errorf("the command exits %d, want %d: %s\n  refusal: %v", got, wantCode, why, err)
	}
	if !strings.Contains(err.Error(), wantText) {
		t.Errorf("the refusal reads %q, want it to name %q: %s", err.Error(), wantText, why)
	}
}

// absentSprintID returns a sprint id the roadmap does not hold, and proves it
// rather than resting on the assumption that some number happens to be free.
func absentSprintID(t *testing.T, f *densityFixture) int {
	t.Helper()

	id := scanInt(t, f, "SELECT COALESCE(MAX(id), 0) + 1000 FROM sprints")
	if rows := scanInt(t, f, "SELECT COUNT(*) FROM sprints WHERE id = ?", id); rows != 0 {
		t.Fatalf("the roadmap holds sprint %d, which this gate needs to be absent", id)
	}
	return id
}

// absentTaskID returns a task id the roadmap does not hold, and proves it.
func absentTaskID(t *testing.T, f *densityFixture) int {
	t.Helper()

	id := scanInt(t, f, "SELECT COALESCE(MAX(id), 0) + 1000 FROM tasks")
	if rows := scanInt(t, f, "SELECT COUNT(*) FROM tasks WHERE id = ?", id); rows != 0 {
		t.Fatalf("the roadmap holds task %d, which this gate needs to be absent", id)
	}
	return id
}

// auditRowCount is the witness that the execution step wrote, or did not.
func auditRowCount(t *testing.T, f *densityFixture) int {
	t.Helper()
	return scanInt(t, f, "SELECT COUNT(*) FROM audit")
}

// scanInt runs one single-column query. Every query passed to it is a literal
// at the call site, so no statement is ever assembled from a value.
func scanInt(t *testing.T, f *densityFixture, query string, args ...any) int {
	t.Helper()

	var n int
	if err := f.database.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("running %q: %v", query, err)
	}
	return n
}

// ---------------------------------------------------------------------------
// The extractor's and the classifier's own gates
// ---------------------------------------------------------------------------

// TestSprintAssignment_TheOrderExtractorStopsAtTheEndOfTheRun proves the second
// phase against synthetic text. Driven against the live document alone, the
// extractor's failure modes never occur and would go on reporting success long
// after they stopped working.
func TestSprintAssignment_TheOrderExtractorStopsAtTheEndOfTheRun(t *testing.T) {
	// A run long enough to clear the floor, so these cases exercise the scan
	// rather than the floor.
	run := func(n int) string {
		var b strings.Builder
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&b, "%d. step number %d\n", i, i)
		}
		return b.String()
	}

	cases := []struct {
		name    string
		section string
		want    int // items expected, ignored when wantErr
		wantErr bool
	}{
		{
			name:    "the run stops before a second numbered list further down the section",
			section: validationOrderLabel + "\n\n" + run(9) + "\nFour rules govern these entries:\n\n1. The sprint entry names the task.\n2. The task entry exists.\n",
			want:    9,
		},
		{
			name:    "a preamble between the label and the list is skipped",
			section: validationOrderLabel + "\n\nThe order below is normative.\nIt is also the order applied.\n\n" + run(8),
			want:    8,
		},
		{
			name:    "the run stops where the numbering skips",
			section: validationOrderLabel + "\n\n1. one\n2. two\n4. four\n5. five\n6. six\n7. seven\n8. eight\n9. nine\n10. ten\n",
			wantErr: true, // only two items survive, which is under the floor
		},
		{
			name:    "a list that does not start at 1 is refused",
			section: validationOrderLabel + "\n\n2. two\n3. three\n",
			wantErr: true,
		},
		{
			name:    "a missing label is refused",
			section: "**Re-parenting on `add-tasks`:**\n\n1. one\n",
			wantErr: true,
		},
		{
			name:    "a label followed by prose and no list is refused",
			section: validationOrderLabel + "\n\nThere is no list here.\n\n**Audit:** one entry.\n",
			wantErr: true,
		},
		{
			name:    "a run shorter than the floor is refused",
			section: validationOrderLabel + "\n\n" + run(minValidationSteps-1),
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, err := extractValidationOrder(tc.section)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("extracted %d items, want a failure: %v", len(items), items)
				}
				return
			}
			if err != nil {
				t.Fatalf("extraction failed: %v", err)
			}
			if len(items) != tc.want {
				t.Errorf("extracted %d items, want %d: %v", len(items), tc.want, items)
			}
		})
	}
}

// TestSprintAssignment_TheStepClassifierIsExactlyOneMarkerPerItem proves the
// third phase's two failure verdicts, which the live document never produces.
func TestSprintAssignment_TheStepClassifierIsExactlyOneMarkerPerItem(t *testing.T) {
	cases := []struct {
		name string
		item string
		want int
	}{
		{
			name: "a step the classifier knows",
			item: "Verify that every task is currently a member of the sprint it is being taken out of",
			want: 1,
		},
		{
			name: "a step no marker recognises",
			item: "Verify that the moon is waxing before touching the ledger",
			want: 0,
		},
		{
			name: "an item conflating two steps, which is the shape task #337 removed",
			item: "Validate the format and range of every sprint id, then verify the sprint exists",
			want: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchAssignmentStep(tc.item); len(got) != tc.want {
				t.Errorf("matched %d markers (%v), want %d, for %q", len(got), got, tc.want, tc.item)
			}
		})
	}
}

// TestSprintAssignment_EveryKnownStepIsPublishedExactlyOnce is the inverse of
// the classifier's own check, and it is what keeps this gate from quietly
// shrinking. "Every published item is a step this gate knows" is satisfied by a
// document that publishes only two of them; requiring every step the gate knows
// to appear exactly once turns a step deleted from COMMANDS.md into a failure
// rather than into one fewer thing asserted.
func TestSprintAssignment_EveryKnownStepIsPublishedExactlyOnce(t *testing.T) {
	order := publishedValidationOrder(t)

	seen := make(map[assignmentStep]int, len(assignmentStepMarkers))
	for _, s := range order {
		seen[s]++
	}
	for _, m := range assignmentStepMarkers {
		switch seen[m.step] {
		case 1:
		case 0:
			t.Errorf("%s %s publishes no item this gate reads as %s; the step is either gone from "+
				"the document or reworded past its marker, and either way the pairs it belongs to "+
				"are no longer being driven",
				assignmentSpecRelPath, assignmentHeading, m.step)
		default:
			t.Errorf("%s %s publishes %s %d times, so its position in the order is not a single "+
				"fact and nothing can be derived from it",
				assignmentSpecRelPath, assignmentHeading, m.step, seen[m.step])
		}
	}
}
