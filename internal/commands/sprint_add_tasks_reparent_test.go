// Package commands — the gate for what `sprint add-tasks` does with a task that
// already belongs to another sprint (rmp task #327).
//
// The defect this closes was a published contradiction, not a wrong behaviour.
// SPEC/COMMANDS.md § Task Assignment carried a validation step ordering the
// command to "verify tasks are not already in another sprint", while
// SPEC/DATABASE.md § Add Task to Sprint with Position specified the opposite as
// the intended behaviour: the insert carries an `ON CONFLICT(task_id)` branch
// that re-parents the task's single membership row, which is what keeps a task
// in exactly one sprint without the caller having to remove it from the previous
// one first. The code follows DATABASE.md and holds no membership check at all.
// Two specifications, opposite rules, one of them implemented — and nothing in
// the tree exercised the case, so neither rule was held to anything.
//
// Re-parenting was ratified as the intended behaviour, so COMMANDS.md was
// corrected and no behaviour changed. What was missing, and is supplied here, is
// the test that fails if the surviving rule is ever quietly swapped for the
// other one.
//
// Why this gate is in Go rather than in tests/ against the binary.
//
// The rule has two halves, and only one of them is visible from outside the
// process. The half the invocation names — the destination sprint gains the task
// — any caller can read back with `sprint tasks`. The other half lands in the
// sprint the command line never mentions: the row leaves that sprint's position
// run, and the run must be compacted before the transaction commits. `position`
// is never handed back to a caller, is not a field of the `Task` object, and no
// command prints it (SPEC/DATABASE.md § Position Density Within a Sprint), so a
// test driving the binary can see the membership move and cannot see the state
// it leaves behind. This file drives the same handler `rmp` dispatches to and
// reads `sprint_tasks` directly, so it asserts the whole contract rather than
// the visible half of it. The externally visible half is asserted too: the
// handler returns the error `main` maps to an exit code, so a nil error here is
// exit code 0 there.
//
// Why the expectation is read out of the specification instead of typed in.
//
// The defect was a divergence between two documents, so a test that hard-codes
// one of them can only ever catch the code drifting. Classifying the section
// first and then requiring the binary to agree with it converges the two: edit
// COMMANDS.md back to the refusal rule and this file starts demanding a refusal,
// which the binary's acceptance fails; change the binary to refuse while the
// specification still declares re-parenting and the same test fails from the
// other side. The classifier recognises the two rules by narrow markers and
// fails loudly on anything else, which is the safe direction — a phrasing it
// does not know is a failure naming the section, never a silent pass.
package commands

import (
	"regexp"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Reading the rule out of the specification
// ---------------------------------------------------------------------------

const (
	// assignmentSpecRelPath is the document that publishes the command's
	// contract, and assignmentHeading is the section inside it.
	assignmentSpecRelPath = "SPEC/COMMANDS.md"
	assignmentHeading     = "### Task Assignment"

	// minAssignmentSectionBytes is the floor under the extraction. The section
	// runs to some nine thousand bytes; a scan that cut it short would hand the
	// classifier a fragment holding neither marker, and "unrecognised" would
	// then be reported as a specification problem rather than as the scanning
	// problem it is.
	minAssignmentSectionBytes = 3000
)

// assignmentSectionEnd matches the first heading at or above the level of
// § Task Assignment, which is where that section stops.
var assignmentSectionEnd = regexp.MustCompile(`(?m)^#{1,3} \S`)

// taskAssignmentSection returns the body of SPEC/COMMANDS.md § Task Assignment.
func taskAssignmentSection(t *testing.T) string {
	t.Helper()

	content := readSpecFile(t, assignmentSpecRelPath)
	start := strings.Index(content, assignmentHeading+"\n")
	if start < 0 {
		t.Fatalf("%s declares no %q heading, so this gate is not reading the section it names",
			assignmentSpecRelPath, assignmentHeading)
	}

	body := content[start+len(assignmentHeading):]
	if end := assignmentSectionEnd.FindStringIndex(body); end != nil {
		body = body[:end[0]]
	}
	if len(body) < minAssignmentSectionBytes {
		t.Fatalf("extracted %d bytes for %s %s, want at least %d: the section scan has stopped "+
			"matching, so anything read out of it is unsound",
			len(body), assignmentSpecRelPath, assignmentHeading, minAssignmentSectionBytes)
	}
	return body
}

// assignmentRule is what the specification says `sprint add-tasks` does with a
// task that already belongs to another sprint. The two named values are the only
// two rules the command can have, and they are mutually exclusive.
type assignmentRule int

const (
	// ruleUnrecognised means the section declares both rules or neither. Both
	// are failures: the first is the contradiction task #327 removed, the second
	// leaves the command's behaviour unpublished.
	ruleUnrecognised assignmentRule = iota
	// ruleReParent: the task is accepted and its membership row is moved.
	ruleReParent
	// ruleRefuse: the command rejects the batch and changes nothing.
	ruleRefuse
)

func (r assignmentRule) String() string {
	switch r {
	case ruleReParent:
		return "re-parent the task onto the named sprint"
	case ruleRefuse:
		return "refuse the batch"
	default:
		return "neither rule, or both at once"
	}
}

var (
	// refuseMarker is the refusal rule in the words the specification used for
	// it before task #327 removed the step.
	refuseMarker = regexp.MustCompile(`(?i)not already in (?:another|a different) sprint`)
	// reParentMarker is the rule that replaced it.
	reParentMarker = regexp.MustCompile(`(?i)re-parent`)
)

// classifyAssignmentRule reads one of the two rules out of the section, or
// reports that it can read neither.
func classifyAssignmentRule(section string) assignmentRule {
	refuses := refuseMarker.MatchString(section)
	reParents := reParentMarker.MatchString(section)
	switch {
	case reParents && !refuses:
		return ruleReParent
	case refuses && !reParents:
		return ruleRefuse
	default:
		return ruleUnrecognised
	}
}

// ---------------------------------------------------------------------------
// The behaviour
// ---------------------------------------------------------------------------

// reParentCase is one way of naming a task that already belongs to another
// sprint on an `add-tasks` command line.
type reParentCase struct {
	name string
	// roadmap is this case's own roadmap, because each case builds a fixture.
	roadmap string
	// pick returns the ids to hand to `add-tasks`, in the order they are given,
	// which is the order the destination sprint must append them in.
	pick func(t *testing.T, f *densityFixture) []int
}

// reParentCases takes its members from the MIDDLE of the sprint they belong to.
// Taking the last member would prove nothing about the run left behind: a dense
// run minus its last element is dense whether or not anything compacted it.
func reParentCases() []reParentCase {
	return []reParentCase{
		{
			name:    "one task, taken from the middle of the sprint it belongs to",
			roadmap: "acquirer-settlement",
			pick: func(t *testing.T, f *densityFixture) []int {
				return []int{f.order(t, f.planned)[2]}
			},
		},
		{
			// The fail-fast rule validates the whole batch before it changes
			// anything, so a membership check would reject this invocation
			// entirely — including the task that belongs to no sprint and that
			// no rule has ever objected to.
			name:    "a batch mixing a member of another sprint with a task that belongs to none",
			roadmap: "ledger-close-hardening",
			pick: func(t *testing.T, f *densityFixture) []int {
				return []int{f.order(t, f.planned)[1], f.newTask(t)}
			},
		},
	}
}

// TestSprintAddTasks_TaskAlreadyInAnotherSprint requires the binary to do what
// SPEC/COMMANDS.md § Task Assignment says it does, whichever of the two rules
// that section declares.
//
// Under the rule the section declares today the assertions are: the command
// succeeds, so `rmp` exits 0; the destination sprint gains the tasks appended
// after its last member, in the order the command line named them; the sprint
// the task came from no longer holds it, so the row moved rather than being
// copied; and every sprint in the roadmap still holds a dense 0..N-1 run, which
// is what proves the source sprint was compacted in the same transaction.
func TestSprintAddTasks_TaskAlreadyInAnotherSprint(t *testing.T) {
	section := taskAssignmentSection(t)
	rule := classifyAssignmentRule(section)
	if rule == ruleUnrecognised {
		t.Fatalf("%s %s declares %s for a task that already belongs to another sprint; exactly one "+
			"of the two rules must be readable there, and this gate cannot test a command whose "+
			"contract is contradictory or absent",
			assignmentSpecRelPath, assignmentHeading, rule)
	}

	for _, tc := range reParentCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := setupDensityRoadmap(t, tc.roadmap)

			moved := tc.pick(t, f)
			before := f.snapshot(t)
			source := f.order(t, f.planned)
			destination := f.order(t, f.running)

			var err error
			_ = captureStdout(t, func() {
				err = sprintAddTasks([]string{"-r", f.roadmap, itoa(f.running), joinIDs(moved)})
			})

			if rule == ruleRefuse {
				assertAddTasksRefused(t, f, moved, before, err)
				return
			}
			assertAddTasksReParented(t, f, moved, source, destination, err)
		})
	}
}

// assertAddTasksReParented is the rule the specification declares today.
func assertAddTasksReParented(t *testing.T, f *densityFixture, moved, source, destination []int, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("`sprint add-tasks` refused %v: %v\n%s %s declares that a task already belonging "+
			"to another sprint is re-parented onto the named sprint and the command exits 0",
			moved, err, assignmentSpecRelPath, assignmentHeading)
	}

	wantDestination := make([]int, 0, len(destination)+len(moved))
	wantDestination = append(wantDestination, destination...)
	wantDestination = append(wantDestination, moved...)
	if got := f.order(t, f.running); !sameIDs(got, wantDestination) {
		t.Errorf("the named sprint holds %v, want %v: the re-parented row is appended after the "+
			"sprint's last member, in the order the command line named the tasks",
			got, wantDestination)
	}

	wantSource := withoutIDs(source, moved)
	if got := f.order(t, f.planned); !sameIDs(got, wantSource) {
		t.Errorf("the sprint the task came from holds %v, want %v: a task belongs to at most one "+
			"sprint, so the membership row moved rather than being copied",
			got, wantSource)
	}
	if len(wantSource) == len(source) {
		t.Fatalf("this case took nothing out of the source sprint, so it does not exercise the rule "+
			"at all: it named %v and the sprint held %v", moved, source)
	}

	// The source sprint is the one the command line never names, and the row
	// left a hole in the middle of its run. Checked over every sprint in the
	// roadmap, because a check scoped to the named sprint would look right and
	// see nothing (SPEC/DATABASE.md § Position Density Within a Sprint).
	f.assertDense(t, "after re-parenting "+joinIDs(moved))
}

// assertAddTasksRefused is the other rule, and the arm that runs if the
// specification is ever edited back to it. Refusing is fail-fast: the batch
// changes nothing at all, not even the part of it nothing objects to.
func assertAddTasksRefused(t *testing.T, f *densityFixture, moved []int, before []membership, err error) {
	t.Helper()

	if err == nil {
		t.Fatalf("`sprint add-tasks` accepted %v and re-parented it, which %s %s no longer allows: "+
			"the section declares that the command verifies tasks are not already in another sprint",
			moved, assignmentSpecRelPath, assignmentHeading)
	}
	if after := f.snapshot(t); !sameMemberships(after, before) {
		t.Errorf("a refused batch changed sprint membership: %v became %v, and a command rejected "+
			"during validation must make no changes at all", before, after)
	}
}

// withoutIDs returns ids with every member of remove taken out, order preserved.
func withoutIDs(ids, remove []int) []int {
	dropped := make(map[int]bool, len(remove))
	for _, id := range remove {
		dropped[id] = true
	}
	kept := make([]int, 0, len(ids))
	for _, id := range ids {
		if !dropped[id] {
			kept = append(kept, id)
		}
	}
	return kept
}

// ---------------------------------------------------------------------------
// The classifier's own gate
// ---------------------------------------------------------------------------

// TestSprintAddTasks_TheRuleClassifierReadsBothRules keeps the convergence
// honest. The gate above reads the live section, which declares one rule, so
// three of the classifier's four outcomes never occur there and would go on
// reporting success long after the classifier stopped working. They are proved
// against synthetic text instead, so demonstrating that the mechanism works
// never becomes a reason to keep a contradiction in SPEC/.
func TestSprintAddTasks_TheRuleClassifierReadsBothRules(t *testing.T) {
	const refusalStep = "4. For `add-tasks`: verify tasks are not already in another sprint"
	const reParentStep = "The task's single membership row is re-parented onto the sprint named " +
		"on the command line and appended after that sprint's last member."

	cases := []struct {
		name string
		text string
		want assignmentRule
	}{
		{
			name: "the refusal rule, in the words the specification carried before task #327",
			text: refusalStep,
			want: ruleRefuse,
		},
		{
			name: "the re-parenting rule the specification carries now",
			text: reParentStep,
			want: ruleReParent,
		},
		{
			name: "both rules at once, which is the contradiction task #327 removed",
			text: refusalStep + "\n\n" + reParentStep,
			want: ruleUnrecognised,
		},
		{
			name: "a section that declares neither rule",
			text: "**Validation Order:**\n1. Validate sprint ID exists\n2. Parse all task IDs",
			want: ruleUnrecognised,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyAssignmentRule(tc.text); got != tc.want {
				t.Errorf("classified %q as %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

// TestSprintAddTasks_TheLiveSectionDeclaresExactlyOneRule is the acceptance
// criterion of task #327 stated as a test: one rule about a task that already
// belongs to another sprint, and only one.
func TestSprintAddTasks_TheLiveSectionDeclaresExactlyOneRule(t *testing.T) {
	section := taskAssignmentSection(t)

	rule := classifyAssignmentRule(section)
	if rule == ruleUnrecognised {
		t.Fatalf("%s %s declares %s; it must declare exactly one, and the command's behaviour is "+
			"published nowhere else",
			assignmentSpecRelPath, assignmentHeading, rule)
	}
	if rule != ruleReParent {
		return
	}

	// The re-parenting rule is DATABASE.md's, and that document is canonical for
	// the statement that performs it. The citation is what keeps this section
	// from restating the mechanism and drifting from it a second time.
	if !strings.Contains(section, "`DATABASE.md § Add Task to Sprint with Position`") {
		t.Errorf("%s %s no longer cites DATABASE.md § Add Task to Sprint with Position, which is "+
			"canonical for the statement that performs the re-parenting; without the citation the "+
			"section either duplicates that document's reasoning or leaves the mechanism unstated",
			assignmentSpecRelPath, assignmentHeading)
	}
}
