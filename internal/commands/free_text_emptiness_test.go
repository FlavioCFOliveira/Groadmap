package commands

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file is the regression suite for SPEC/MODELS.md (Free-Text Emptiness and
// Trimming Constraint) and SPEC/COMMANDS.md (Emptiness Constraint (All Required
// Free-Text Fields)).
//
// The constraint has three steps and they must run in this order:
//
//  1. the UTF-8 encoding rule and the control-character rule, on the value AS
//     SUPPLIED;
//  2. the trim;
//  3. the emptiness judgement, on the TRIMMED value.
//
// Every test below exists to pin one half of that, and the discriminating input
// is always the same: VT and FF are forbidden control characters that
// strings.TrimSpace ALSO removes, so a value made only of VT is refused by the
// specified order as a CONTROL-CHARACTER violation and by the intuitive
// trim-first order as an EMPTY value. Nothing else separates the two orders, and
// the exit code does not: both are 6.

// The forbidden control characters the trim would also remove.
const (
	freeTextVT = "\v" // VT, 0x0B
	freeTextFF = "\f" // FF, 0x0C
)

// Whitespace outside ASCII. Neither is a forbidden control character, so both
// belong to the emptiness rule; the whitespace set is Go's unicode.IsSpace, and
// SPEC/COMMANDS.md acceptance criterion 7 names these two. Written as escapes
// because they are invisible in a source file.
const (
	freeTextNBSP = "\u00a0" // U+00A0 NO-BREAK SPACE
	freeTextNEL  = "\u0085" // U+0085 NEXT LINE
)

// emptinessProbes are the values Rule 1 must refuse: every one of them trims
// away to nothing, and no one of them carries a forbidden control character.
// They are the whole of SPEC/COMMANDS.md acceptance criterion 7.
var emptinessProbes = []struct{ name, value string }{
	{"three spaces", "   "},
	{"a TAB", "\t"},
	{"an LF", "\n"},
	{"a CR", "\r"},
	{"a mixture of space, TAB, CR and LF", " \t\r\n "},
	{"a no-break space (U+00A0)", freeTextNBSP},
	{"a NEL (U+0085)", freeTextNEL},
}

// freeTextWriter is one (command, field) pair that writes a free-text value.
type freeTextWriter struct {
	command string
	field   utils.Field
	invoke  func(value string) error
}

// setupEmptinessRoadmap seeds a hermetic roadmap holding exactly one task and
// one sprint, which are the entities the update commands below address as id 1.
func setupEmptinessRoadmap(t *testing.T, name string) *db.DB {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	database, cleanup := setupTestTaskRoadmap(t, name)
	t.Cleanup(cleanup)

	_ = captureStdout(t, func() {
		if err := taskCreate([]string{
			"-r", name,
			"-t", "Refuse a token that expired this second",
			"-fr", "A token whose exp is the current second must be refused",
			"-tr", "Compare with !time.Now().Before(exp)",
			"-ac", "A unit test covers the exact boundary second",
		}); err != nil {
			t.Fatalf("seeding the task: %v", err)
		}
		if err := sprintCreate([]string{
			"-r", name,
			"-t", "Expiry hardening",
			"-d", "Close the JWT boundary-second defect and lock it behind a regression test.",
		}); err != nil {
			t.Fatalf("seeding the sprint: %v", err)
		}
	})

	return database
}

// taskCreateWithField builds a complete `task create` invocation in which
// exactly one of the four required free-text flags carries value and the other
// three carry realistic, valid text. Without that, a probe aimed at one field
// would be answered by the refusal of another.
func taskCreateWithField(roadmap string, under utils.Field, value string) []string {
	values := map[utils.Field]string{
		utils.FieldTaskTitle:                  "Reject an expired refresh token",
		utils.FieldTaskFunctionalRequirements: "A refresh token past its exp must not mint an access token",
		utils.FieldTaskTechnicalRequirements:  "Check exp inside the refresh handler before the signature lookup",
		utils.FieldTaskAcceptanceCriteria:     "A table-driven test covers the second on either side of exp",
	}
	values[under] = value

	return []string{
		"-r", roadmap,
		"-t", values[utils.FieldTaskTitle],
		"-fr", values[utils.FieldTaskFunctionalRequirements],
		"-tr", values[utils.FieldTaskTechnicalRequirements],
		"-ac", values[utils.FieldTaskAcceptanceCriteria],
	}
}

// requiredFreeTextWriters enumerates every (command, field) pair that writes one
// of the six required free-text fields the emptiness rule refuses with exit code
// 6 and a message naming the field.
//
// The seventh required field, the comment `body`, is refused with exit code 2
// under a rule of its own that predates this constraint
// (SPEC/COMMANDS.md, Comment Body Input Source and Precedence), so it is swept
// separately, through commentBodyWriters below, which also reaches it by the
// standard-input origin the flag-shaped signature here cannot express. The
// eighth free-text field, `completion_summary`, is optional and Rule 1 does not
// govern it at all.
func requiredFreeTextWriters(roadmap string) []freeTextWriter {
	return []freeTextWriter{
		{"task create -t", utils.FieldTaskTitle, func(v string) error {
			return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskTitle, v))
		}},
		{"task create -fr", utils.FieldTaskFunctionalRequirements, func(v string) error {
			return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskFunctionalRequirements, v))
		}},
		{"task create -tr", utils.FieldTaskTechnicalRequirements, func(v string) error {
			return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskTechnicalRequirements, v))
		}},
		{"task create -ac", utils.FieldTaskAcceptanceCriteria, func(v string) error {
			return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskAcceptanceCriteria, v))
		}},
		{"task edit -t", utils.FieldTaskTitle, func(v string) error {
			return taskEdit([]string{"-r", roadmap, "1", "-t", v})
		}},
		{"task edit -fr", utils.FieldTaskFunctionalRequirements, func(v string) error {
			return taskEdit([]string{"-r", roadmap, "1", "-fr", v})
		}},
		{"task edit -tr", utils.FieldTaskTechnicalRequirements, func(v string) error {
			return taskEdit([]string{"-r", roadmap, "1", "-tr", v})
		}},
		{"task edit -ac", utils.FieldTaskAcceptanceCriteria, func(v string) error {
			return taskEdit([]string{"-r", roadmap, "1", "-ac", v})
		}},
		{"sprint create -t", utils.FieldSprintTitle, func(v string) error {
			return sprintCreate([]string{"-r", roadmap, "-t", v, "-d", "Deliver the refresh-token guard."})
		}},
		{"sprint create -d", utils.FieldSprintDescription, func(v string) error {
			return sprintCreate([]string{"-r", roadmap, "-t", "Refresh-token guard", "-d", v})
		}},
		{"sprint update -t", utils.FieldSprintTitle, func(v string) error {
			return sprintUpdate([]string{"-r", roadmap, "1", "-t", v})
		}},
		{"sprint update -d", utils.FieldSprintDescription, func(v string) error {
			return sprintUpdate([]string{"-r", roadmap, "1", "-d", v})
		}},
	}
}

// countEntities returns how many tasks and sprints the roadmap holds, so a
// refusal can be shown to have created nothing.
func countEntities(t *testing.T, database *db.DB) (tasks, sprints int) {
	t.Helper()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	taskRows, err := database.ListTasks(ctx, nil)
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	sprintRows, err := database.ListSprints(ctx, nil)
	if err != nil {
		t.Fatalf("listing sprints: %v", err)
	}
	return len(taskRows), len(sprintRows)
}

// firstTaskAndSprint returns the seeded task and sprint, so an update refused by
// a rule can be shown to have changed nothing.
func firstTaskAndSprint(t *testing.T, database *db.DB) (*models.Task, *models.Sprint) {
	t.Helper()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	task, err := database.GetTask(ctx, 1)
	if err != nil {
		t.Fatalf("reading task 1: %v", err)
	}
	sprint, err := database.GetSprint(ctx, 1)
	if err != nil {
		t.Fatalf("reading sprint 1: %v", err)
	}
	return task, sprint
}

// TestWhitespaceOnlyRequiredValueIsRefusedNamingTheField is Rule 1 and
// SPEC/COMMANDS.md acceptance criteria 1 to 5 and 7, proved field by field and
// command by command rather than on the title alone.
//
// It asserts the exact message, not merely that something failed: the whole
// point of the rule is that a value that names nothing is refused as a FIELD
// (exit 6) and not as a missing FLAG (exit 2), and both are failures.
func TestWhitespaceOnlyRequiredValueIsRefusedNamingTheField(t *testing.T) {
	const roadmap = "free-text-emptiness-sweep"
	database := setupEmptinessRoadmap(t, roadmap)

	writers := requiredFreeTextWriters(roadmap)
	if len(writers) != 12 {
		t.Fatalf("the sweep covers %d (command, field) pairs; six required fields across four commands make 12", len(writers))
	}

	tasksBefore, sprintsBefore := countEntities(t, database)
	taskBefore, sprintBefore := firstTaskAndSprint(t, database)

	for _, w := range writers {
		for _, probe := range emptinessProbes {
			t.Run(w.command+"/"+probe.name, func(t *testing.T) {
				err := w.invoke(probe.value)
				if err == nil {
					t.Fatalf("%s accepted a value made only of %s", w.command, probe.name)
				}

				want := utils.FieldEmptyError(w.field).Error()
				if err.Error() != want {
					t.Errorf("%s\n got: %q\nwant: %q", w.command, err.Error(), want)
				}
				if !utils.IsValidation(err) {
					t.Errorf("%s: refusal must wrap ErrValidation (exit 6); got %v", w.command, err)
				}
				if utils.IsRequired(err) {
					t.Errorf("%s: a value DID reach the application, so the refusal must not be the missing-parameter class (exit 2); got %v", w.command, err)
				}
			})
		}
	}

	// Nothing was created and nothing was changed by any of the refusals above.
	tasksAfter, sprintsAfter := countEntities(t, database)
	if tasksAfter != tasksBefore {
		t.Errorf("task count moved from %d to %d: a refused `task create` created a task", tasksBefore, tasksAfter)
	}
	if sprintsAfter != sprintsBefore {
		t.Errorf("sprint count moved from %d to %d: a refused `sprint create` created a sprint", sprintsBefore, sprintsAfter)
	}

	taskAfter, sprintAfter := firstTaskAndSprint(t, database)
	if taskAfter.Title != taskBefore.Title || taskAfter.FunctionalRequirements != taskBefore.FunctionalRequirements ||
		taskAfter.TechnicalRequirements != taskBefore.TechnicalRequirements || taskAfter.AcceptanceCriteria != taskBefore.AcceptanceCriteria {
		t.Error("a refused `task edit` changed the stored task")
	}
	if sprintAfter.Title != sprintBefore.Title || sprintAfter.Description != sprintBefore.Description {
		t.Error("a refused `sprint update` changed the stored sprint")
	}
}

// TestLiteralEmptyStringKeepsItsOwnRefusal is the third decision on rmp task 278:
// `-t ""` and `-t "   "` stay DIFFERENT refusals, and they are not unified.
//
// On the two create commands the flags are required parameters, so the literal
// empty string means the parameter never arrived: exit code 2, naming the FLAG.
// On the two update commands the same flags are optional, so the literal empty
// string is a value that was supplied and rejected: exit code 6, naming the
// FIELD. Neither is the whitespace-only case, which is always exit 6.
func TestLiteralEmptyStringKeepsItsOwnRefusal(t *testing.T) {
	const roadmap = "free-text-emptiness-empty-flag"
	setupEmptinessRoadmap(t, roadmap)

	t.Run("create commands name the flag with exit 2", func(t *testing.T) {
		cases := []struct {
			command string
			flag    string
			invoke  func() error
		}{
			{"task create -t", "--title", func() error {
				return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskTitle, ""))
			}},
			{"task create -fr", "--functional-requirements", func() error {
				return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskFunctionalRequirements, ""))
			}},
			{"task create -tr", "--technical-requirements", func() error {
				return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskTechnicalRequirements, ""))
			}},
			{"task create -ac", "--acceptance-criteria", func() error {
				return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskAcceptanceCriteria, ""))
			}},
			{"sprint create -t", "--title", func() error {
				return sprintCreate([]string{"-r", roadmap, "-t", "", "-d", "Deliver the refresh-token guard."})
			}},
			{"sprint create -d", "--description", func() error {
				return sprintCreate([]string{"-r", roadmap, "-t", "Refresh-token guard", "-d", ""})
			}},
		}

		for _, c := range cases {
			t.Run(c.command, func(t *testing.T) {
				err := c.invoke()
				if err == nil {
					t.Fatalf("%s accepted the literal empty string", c.command)
				}
				if !utils.IsRequired(err) {
					t.Errorf("%s: the literal empty string must stay the missing-parameter class (exit 2); got %v", c.command, err)
				}
				want := "required parameter missing: " + c.flag
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s\n got: %q\nwant substring: %q", c.command, err.Error(), want)
				}
				if strings.Contains(err.Error(), "cannot be empty") {
					t.Errorf("%s reported the field rule for a flag that carried no value: %q", c.command, err.Error())
				}
			})
		}
	})

	t.Run("update commands name the field with exit 6", func(t *testing.T) {
		cases := []struct {
			command string
			field   utils.Field
			invoke  func() error
		}{
			{"task edit -t", utils.FieldTaskTitle, func() error {
				return taskEdit([]string{"-r", roadmap, "1", "-t", ""})
			}},
			{"sprint update -t", utils.FieldSprintTitle, func() error {
				return sprintUpdate([]string{"-r", roadmap, "1", "-t", ""})
			}},
			{"sprint update -d", utils.FieldSprintDescription, func() error {
				return sprintUpdate([]string{"-r", roadmap, "1", "-d", ""})
			}},
		}

		for _, c := range cases {
			t.Run(c.command, func(t *testing.T) {
				err := c.invoke()
				if err == nil {
					t.Fatalf("%s accepted the literal empty string", c.command)
				}
				want := utils.FieldEmptyError(c.field).Error()
				if err.Error() != want {
					t.Errorf("%s\n got: %q\nwant: %q", c.command, err.Error(), want)
				}
				if utils.IsRequired(err) {
					t.Errorf("%s: a supplied flag is never a missing parameter; got %v", c.command, err)
				}
			})
		}
	})
}

// alignedFreeTextWriters lists every (command, field) pair whose refusal is exit
// code 6 and names the field. Every one of them refuses a value made only of VT
// as a CONTROL-CHARACTER violation, which is the observable signature of step 1
// running on the value as supplied.
//
// It is the whole of that population: `task edit` was the one residual and rmp
// task 301 closed it, so there is no longer an exemption to record. The comment
// `body` is the one required free-text field this list does not carry, because
// its refusals belong to a different class entirely (exit code 2, the
// missing-parameter wording); commentBodyWriters below sweeps it under the same
// two probes.
func alignedFreeTextWriters(roadmap string) []freeTextWriter {
	return []freeTextWriter{
		{"task create -t", utils.FieldTaskTitle, func(v string) error {
			return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskTitle, v))
		}},
		{"task create -fr", utils.FieldTaskFunctionalRequirements, func(v string) error {
			return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskFunctionalRequirements, v))
		}},
		{"task create -tr", utils.FieldTaskTechnicalRequirements, func(v string) error {
			return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskTechnicalRequirements, v))
		}},
		{"task create -ac", utils.FieldTaskAcceptanceCriteria, func(v string) error {
			return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskAcceptanceCriteria, v))
		}},
		{"task edit -t", utils.FieldTaskTitle, func(v string) error {
			return taskEdit([]string{"-r", roadmap, "1", "-t", v})
		}},
		{"task edit -fr", utils.FieldTaskFunctionalRequirements, func(v string) error {
			return taskEdit([]string{"-r", roadmap, "1", "-fr", v})
		}},
		{"task edit -tr", utils.FieldTaskTechnicalRequirements, func(v string) error {
			return taskEdit([]string{"-r", roadmap, "1", "-tr", v})
		}},
		{"task edit -ac", utils.FieldTaskAcceptanceCriteria, func(v string) error {
			return taskEdit([]string{"-r", roadmap, "1", "-ac", v})
		}},
		{"sprint create -t", utils.FieldSprintTitle, func(v string) error {
			return sprintCreate([]string{"-r", roadmap, "-t", v, "-d", "Deliver the refresh-token guard."})
		}},
		{"sprint create -d", utils.FieldSprintDescription, func(v string) error {
			return sprintCreate([]string{"-r", roadmap, "-t", "Refresh-token guard", "-d", v})
		}},
		{"sprint update -t", utils.FieldSprintTitle, func(v string) error {
			return sprintUpdate([]string{"-r", roadmap, "1", "-t", v})
		}},
		{"sprint update -d", utils.FieldSprintDescription, func(v string) error {
			return sprintUpdate([]string{"-r", roadmap, "1", "-d", v})
		}},
		{"task stat COMPLETED --summary", utils.FieldTaskCompletionSummary, func(v string) error {
			return taskSetStatus([]string{"-r", roadmap, "1", "COMPLETED", "--summary", v})
		}},
	}
}

// TestAValueOfOnlyVTIsRefusedAsAControlCharacterAndNotAsEmpty is THE test of
// this task, and the one the reversal must break.
//
// VT is a forbidden control character AND whitespace to strings.TrimSpace, so a
// value made only of VT is refused either way and the exit code is 6 either way.
// What separates the two possible orders is WHICH rule answers:
//
//   - checks on the value as supplied, then the trim: "control characters are
//     not allowed". This is the specified order.
//   - the trim first: the VT is gone, nothing is left, and the answer becomes
//     "cannot be empty" -- the same reordering that silently accepts a leading
//     VT in front of real text and writes it to the database (CWE-150).
//
// Asserting a non-zero exit here would prove nothing at all.
func TestAValueOfOnlyVTIsRefusedAsAControlCharacterAndNotAsEmpty(t *testing.T) {
	const roadmap = "free-text-order-vt-only"
	setupEmptinessRoadmap(t, roadmap)

	for _, w := range alignedFreeTextWriters(roadmap) {
		for _, probe := range []struct{ name, value string }{
			{"only VT", freeTextVT},
			{"only FF", freeTextFF},
			{"VT surrounded by spaces", "  " + freeTextVT + "  "},
		} {
			t.Run(w.command+"/"+probe.name, func(t *testing.T) {
				err := w.invoke(probe.value)
				if err == nil {
					t.Fatalf("%s accepted a value made only of a forbidden control character", w.command)
				}

				wantControl := utils.ControlCharError(w.field).Error()
				if err.Error() == wantControl {
					return
				}
				if err.Error() == utils.FieldEmptyError(w.field).Error() {
					t.Fatalf("%s refused %s as an EMPTY value: the trim ran ahead of the control-character check, "+
						"which is the CWE-150 reordering SPEC/MODELS.md forbids\n got: %q\nwant: %q",
						w.command, probe.name, err.Error(), wantControl)
				}
				t.Fatalf("%s\n got: %q\nwant: %q", w.command, err.Error(), wantControl)
			})
		}
	}
}

// TestALeadingOrTrailingVTOrFFIsRefusedNotDiscarded is the other half of the
// same order, and the half that actually carries the security consequence: the
// value has real content, so the trim-first order does not refuse it at all --
// it strips the forbidden character and stores the rest with exit code 0.
func TestALeadingOrTrailingVTOrFFIsRefusedNotDiscarded(t *testing.T) {
	const roadmap = "free-text-order-vt-edges"
	setupEmptinessRoadmap(t, roadmap)

	const content = "Deliver the refresh-token guard"
	edges := []struct{ name, value string }{
		{"leading VT", freeTextVT + content},
		{"trailing VT", content + freeTextVT},
		{"leading FF", freeTextFF + content},
		{"trailing FF", content + freeTextFF},
	}

	for _, w := range alignedFreeTextWriters(roadmap) {
		for _, e := range edges {
			t.Run(w.command+"/"+e.name, func(t *testing.T) {
				err := w.invoke(e.value)
				if err == nil {
					t.Fatalf("%s ACCEPTED a value carrying a %s: the character was discarded in silence (CWE-150)", w.command, e.name)
				}
				want := utils.ControlCharError(w.field).Error()
				if err.Error() != want {
					t.Errorf("%s\n got: %q\nwant: %q", w.command, err.Error(), want)
				}
			})
		}
	}
}

// commentBodyWriter is one of the eight ways a comment body reaches the
// application: each of the four subcommands, on the `--body` path and on the
// standard-input path. Both origins are swept because the two are handled by
// different code — the flag value arrives as a plain string, standard input goes
// through the bounded reader models.ReadCommentBody — and the SPEC gives them one
// verdict, so a test that exercised only the flag would leave the reader free to
// disagree with it.
//
// absent is the missing-parameter refusal the writer emits for a body that
// carries no forbidden character and trims away to nothing. It is "no comment
// body supplied" everywhere except `comment-edit` reading standard input: that
// invocation requested no other change either, so the pinned wording is the
// other one (SPEC/COMMANDS.md § Emptiness Constraint (All Required Free-Text
// Fields), the `body` row of the refusal table).
type commentBodyWriter struct {
	command string
	absent  string
	invoke  func(t *testing.T, value string) error
}

const (
	commentBodyAbsentMessage = "required parameter missing: no comment body supplied"
	commentBodyNoChange      = "required parameter missing: at least one of --type or --body is required"
)

// commentBodyWriters enumerates those eight. The two `comment-edit` standard-input
// entries deliberately supply no `--type`: standard input is read only when the
// type flag is absent, which is the rule that stops a type-only edit from
// blocking on a terminal (SPEC/COMMANDS.md § Comment Body Input Source and
// Precedence, rule 2).
func commentBodyWriters(roadmap string, taskCommentID, sprintCommentID int) []commentBodyWriter {
	return []commentBodyWriter{
		{"task comment-add --body", commentBodyAbsentMessage, func(_ *testing.T, v string) error {
			return taskCommentAdd([]string{"-r", roadmap, "1", "--type", "DECISION", "--body", v})
		}},
		{"task comment-add <stdin>", commentBodyAbsentMessage, func(t *testing.T, v string) error {
			var err error
			withStdin(t, v, func() {
				err = taskCommentAdd([]string{"-r", roadmap, "1", "--type", "DECISION"})
			})
			return err
		}},
		{"task comment-edit --body", commentBodyAbsentMessage, func(_ *testing.T, v string) error {
			return taskCommentEdit([]string{"-r", roadmap, itoa(taskCommentID), "--body", v})
		}},
		{"task comment-edit <stdin>", commentBodyNoChange, func(t *testing.T, v string) error {
			var err error
			withStdin(t, v, func() {
				err = taskCommentEdit([]string{"-r", roadmap, itoa(taskCommentID)})
			})
			return err
		}},
		{"sprint comment-add --body", commentBodyAbsentMessage, func(_ *testing.T, v string) error {
			return sprintCommentAdd([]string{"-r", roadmap, "1", "--type", "DECISION", "--body", v})
		}},
		{"sprint comment-add <stdin>", commentBodyAbsentMessage, func(t *testing.T, v string) error {
			var err error
			withStdin(t, v, func() {
				err = sprintCommentAdd([]string{"-r", roadmap, "1", "--type", "DECISION"})
			})
			return err
		}},
		{"sprint comment-edit --body", commentBodyAbsentMessage, func(_ *testing.T, v string) error {
			return sprintCommentEdit([]string{"-r", roadmap, itoa(sprintCommentID), "--body", v})
		}},
		{"sprint comment-edit <stdin>", commentBodyNoChange, func(t *testing.T, v string) error {
			var err error
			withStdin(t, v, func() {
				err = sprintCommentEdit([]string{"-r", roadmap, itoa(sprintCommentID)})
			})
			return err
		}},
	}
}

// seedCommentsForBodyProbes creates the one task comment and the one sprint
// comment the `comment-edit` writers address, and returns their ids.
func seedCommentsForBodyProbes(t *testing.T, roadmap string) (taskCommentID, sprintCommentID int) {
	t.Helper()

	_ = captureStdout(t, func() {
		if err := taskCommentAdd([]string{"-r", roadmap, "1", "--type", "DECISION", "--body", "Refuse the token whose exp is the current second."}); err != nil {
			t.Fatalf("seeding the task comment: %v", err)
		}
		if err := sprintCommentAdd([]string{"-r", roadmap, "1", "--type", "DECISION", "--body", "The sprint closes only once the regression test is green."}); err != nil {
			t.Fatalf("seeding the sprint comment: %v", err)
		}
	})
	return 1, 1
}

// TestAVTOnlyCommentBodyIsRefusedAsAControlCharacterAndNotAsAnAbsentBody is the
// comment layer's half of the ORDER, and the counterpart of
// TestAValueOfOnlyVTIsRefusedAsAControlCharacterAndNotAsEmpty above.
//
// The two possible orders are separated here by the exit CLASS as well as by the
// message, because the comment `body` answers an absent value with exit code 2
// rather than 6:
//
//   - checks on the value as supplied, then the trim: exit 6, "body: control
//     characters are not allowed". This is the specified order.
//   - the trim first: the VT is gone, nothing is left, the body looks like one
//     that never arrived, and the answer becomes exit 2 "no comment body
//     supplied" -- with the forbidden character discarded in silence (CWE-150).
//
// This is what rmp task 301 closed on both origins.
func TestAVTOnlyCommentBodyIsRefusedAsAControlCharacterAndNotAsAnAbsentBody(t *testing.T) {
	const roadmap = "free-text-order-comment-body-vt-only"
	setupEmptinessRoadmap(t, roadmap)
	taskCommentID, sprintCommentID := seedCommentsForBodyProbes(t, roadmap)

	want := utils.ControlCharError(utils.FieldCommentBody).Error()

	for _, w := range commentBodyWriters(roadmap, taskCommentID, sprintCommentID) {
		for _, probe := range []struct{ name, value string }{
			{"only VT", freeTextVT},
			{"only FF", freeTextFF},
			{"VT surrounded by spaces", "  " + freeTextVT + "  "},
			{"FF surrounded by TAB and LF", "\t" + freeTextFF + "\n"},
		} {
			t.Run(w.command+"/"+probe.name, func(t *testing.T) {
				err := w.invoke(t, probe.value)
				if err == nil {
					t.Fatalf("%s accepted a body made only of a forbidden control character", w.command)
				}
				if err.Error() == want {
					return
				}
				if utils.IsRequired(err) {
					t.Fatalf("%s reported %s as a body that never ARRIVED: the trim ran ahead of the "+
						"control-character check, which is the CWE-150 reordering SPEC/MODELS.md forbids\n"+
						" got: %q\nwant: %q", w.command, probe.name, err.Error(), want)
				}
				t.Fatalf("%s\n got: %q\nwant: %q", w.command, err.Error(), want)
			})
		}
	}
}

// TestALeadingOrTrailingVTOrFFInACommentBodyIsRefusedNotDiscarded is the other
// half, and the half that carries the security consequence: the body has real
// content, so the trim-first order does not refuse it at all -- it strips the
// forbidden character and stores the rest.
//
// The `--body` origin already behaved this way before rmp task 301; the
// standard-input origin is swept beside it so the bounded reader is held to the
// same verdict rather than trusted to reach it.
func TestALeadingOrTrailingVTOrFFInACommentBodyIsRefusedNotDiscarded(t *testing.T) {
	const roadmap = "free-text-order-comment-body-vt-edges"
	database := setupEmptinessRoadmap(t, roadmap)
	taskCommentID, sprintCommentID := seedCommentsForBodyProbes(t, roadmap)

	const content = "The refresh path reuses the access-token clock."
	want := utils.ControlCharError(utils.FieldCommentBody).Error()

	for _, w := range commentBodyWriters(roadmap, taskCommentID, sprintCommentID) {
		for _, e := range []struct{ name, value string }{
			{"leading VT", freeTextVT + content},
			{"trailing VT", content + freeTextVT},
			{"leading FF", freeTextFF + content},
			{"trailing FF", content + freeTextFF},
		} {
			t.Run(w.command+"/"+e.name, func(t *testing.T) {
				err := w.invoke(t, e.value)
				if err == nil {
					t.Fatalf("%s ACCEPTED a body carrying a %s: the character was discarded in silence (CWE-150)",
						w.command, e.name)
				}
				if err.Error() != want {
					t.Errorf("%s\n got: %q\nwant: %q", w.command, err.Error(), want)
				}
			})
		}
	}

	assertSeededCommentsIntact(t, database, roadmap, taskCommentID)
}

// assertSeededCommentsIntact proves the refusals above wrote nothing: the two
// seeded comments are still the only two, and the task one still carries the text
// it was created with.
func assertSeededCommentsIntact(t *testing.T, database *db.DB, roadmap string, taskCommentID int) {
	t.Helper()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	comments, err := database.ListTaskComments(ctx, 1, nil)
	if err != nil {
		t.Fatalf("listing the task comments of %s: %v", roadmap, err)
	}
	if len(comments) != 1 {
		t.Errorf("a refused comment write changed the log: %d comments, want 1", len(comments))
	}

	comment, err := database.GetTaskComment(ctx, taskCommentID)
	if err != nil {
		t.Fatalf("reading the seeded task comment: %v", err)
	}
	if comment.Body != "Refuse the token whose exp is the current second." {
		t.Errorf("a refused comment write changed the stored body: %q", comment.Body)
	}
}

// TestWhitespaceOnlyCommentBodyIsAMissingParameter is SPEC/COMMANDS.md
// acceptance criterion 6: the comment `body` is the one required free-text field
// whose empty refusal is NOT exit code 6. A body that is empty once trimmed is
// the same condition as a body that never arrived, so all four subcommands
// report a missing parameter, and this constraint leaves that rule exactly as it
// stands.
//
// It is also the guard on the two tests above: they moved the emptiness
// judgement behind the content rules, and this proves that the move refused
// nothing new. Every probe here trims away to nothing WITHOUT carrying a
// forbidden character, so every one must still reach the missing-parameter
// verdict on both origins.
func TestWhitespaceOnlyCommentBodyIsAMissingParameter(t *testing.T) {
	const roadmap = "free-text-emptiness-comment-body"
	database := setupEmptinessRoadmap(t, roadmap)
	taskCommentID, sprintCommentID := seedCommentsForBodyProbes(t, roadmap)

	for _, w := range commentBodyWriters(roadmap, taskCommentID, sprintCommentID) {
		for _, probe := range emptinessProbes {
			t.Run(w.command+"/"+probe.name, func(t *testing.T) {
				err := w.invoke(t, probe.value)
				if err == nil {
					t.Fatalf("%s accepted a body made only of %s", w.command, probe.name)
				}
				if !utils.IsRequired(err) {
					t.Errorf("%s: a whitespace-only body is the missing-parameter class (exit 2); got %v", w.command, err)
				}
				if !strings.Contains(err.Error(), w.absent) {
					t.Errorf("%s\n got: %q\nwant substring: %q", w.command, err.Error(), w.absent)
				}
			})
		}
	}

	// None of the refusals wrote a comment.
	assertSeededCommentsIntact(t, database, roadmap, taskCommentID)
}

// createdID runs a create command, captures the {"id": N} it prints, and returns
// N, so a test can read back exactly the row the command wrote.
func createdID(t *testing.T, create func() error) int {
	t.Helper()

	var createErr error
	out := captureStdout(t, func() { createErr = create() })
	if createErr != nil {
		t.Fatalf("create failed: %v", createErr)
	}

	match := createdIDPattern.FindStringSubmatch(out)
	if match == nil {
		t.Fatalf("no id in the create output: %q", out)
	}
	id, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("id %q is not an integer: %v", match[1], err)
	}
	return id
}

var createdIDPattern = regexp.MustCompile(`"id"\s*:\s*(\d+)`)

// TestTheStoredValueIsTheTrimmedValue is Rule 2 and SPEC/COMMANDS.md acceptance
// criterion 8: the application removes leading and trailing whitespace before
// the value reaches the database, on every command that writes a free-text
// field, and the value read back afterwards is the trimmed one.
//
// It is verified by reading the row back, not by inspecting the argument the
// command was handed: what this rule is about is what the column holds.
func TestTheStoredValueIsTheTrimmedValue(t *testing.T) {
	const roadmap = "free-text-trim-storage"
	database := setupEmptinessRoadmap(t, roadmap)

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// The padding deliberately mixes the whitespace kinds, and the core text
	// deliberately contains interior spaces and an interior line break, which
	// the rule must NOT touch.
	const (
		pad      = " \t\r\n "
		taskCore = "Reject an expired refresh token\nbefore the signature lookup"
		reqCore  = "A refresh token past its exp must not mint an access token"
	)

	t.Run("task create", func(t *testing.T) {
		id := createdID(t, func() error {
			return taskCreate([]string{
				"-r", roadmap,
				"-t", pad + taskCore + pad,
				"-fr", pad + reqCore + pad,
				"-tr", pad + "Check exp inside the refresh handler" + pad,
				"-ac", pad + "A table-driven test covers both sides of exp" + pad,
			})
		})

		task, err := database.GetTask(ctx, id)
		if err != nil {
			t.Fatalf("reading task %d: %v", id, err)
		}
		if task.Title != taskCore {
			t.Errorf("title stored as %q, want %q", task.Title, taskCore)
		}
		if task.FunctionalRequirements != reqCore {
			t.Errorf("functional_requirements stored as %q, want %q", task.FunctionalRequirements, reqCore)
		}
		if task.TechnicalRequirements != "Check exp inside the refresh handler" {
			t.Errorf("technical_requirements stored as %q", task.TechnicalRequirements)
		}
		if task.AcceptanceCriteria != "A table-driven test covers both sides of exp" {
			t.Errorf("acceptance_criteria stored as %q", task.AcceptanceCriteria)
		}
		if !strings.Contains(task.Title, "\n") {
			t.Error("the interior line break was removed; only leading and trailing whitespace may be")
		}
	})

	t.Run("task edit", func(t *testing.T) {
		const edited = "Reject a refresh token whose exp is the current second"
		if err := taskEdit([]string{"-r", roadmap, "1", "-t", pad + edited + pad}); err != nil {
			t.Fatalf("task edit: %v", err)
		}
		task, err := database.GetTask(ctx, 1)
		if err != nil {
			t.Fatalf("reading task 1: %v", err)
		}
		if task.Title != edited {
			t.Errorf("title stored as %q, want %q", task.Title, edited)
		}
	})

	t.Run("sprint create", func(t *testing.T) {
		const title = "Refresh-token guard"
		const description = "Close the refresh path and lock it behind a regression test."

		id := createdID(t, func() error {
			return sprintCreate([]string{"-r", roadmap, "-t", pad + title + pad, "-d", pad + description + pad})
		})

		sprint, err := database.GetSprint(ctx, id)
		if err != nil {
			t.Fatalf("reading sprint %d: %v", id, err)
		}
		if sprint.Title != title {
			t.Errorf("title stored as %q, want %q", sprint.Title, title)
		}
		if sprint.Description != description {
			t.Errorf("description stored as %q, want %q", sprint.Description, description)
		}
	})

	t.Run("sprint update", func(t *testing.T) {
		const title = "Expiry hardening, second pass"
		const description = "Extend the guard to the refresh path."

		if err := sprintUpdate([]string{"-r", roadmap, "1", "-t", pad + title + pad, "-d", pad + description + pad}); err != nil {
			t.Fatalf("sprint update: %v", err)
		}
		sprint, err := database.GetSprint(ctx, 1)
		if err != nil {
			t.Fatalf("reading sprint 1: %v", err)
		}
		if sprint.Title != title {
			t.Errorf("title stored as %q, want %q", sprint.Title, title)
		}
		if sprint.Description != description {
			t.Errorf("description stored as %q, want %q", sprint.Description, description)
		}
	})

	t.Run("task stat --summary", func(t *testing.T) {
		const summary = "Closed the boundary second and covered it with a table-driven test."

		// completion_summary is only writable on the transition into COMPLETED,
		// so the task has to be driven the whole way there first.
		if err := database.AddTasksToSprint(ctx, 1, []int{1}); err != nil {
			t.Fatalf("adding task 1 to sprint 1: %v", err)
		}
		for _, step := range []struct {
			status string
			flags  []string
		}{
			{"DOING", []string{"--commit-open", "5f93b51"}},
			{"TESTING", nil},
		} {
			args := append([]string{"-r", roadmap, "1", step.status}, step.flags...)
			if err := taskSetStatus(args); err != nil {
				t.Fatalf("transition to %s: %v", step.status, err)
			}
		}
		if err := taskSetStatus([]string{
			"-r", roadmap, "1", "COMPLETED", "--commit-close", "2578d18", "--summary", pad + summary + pad,
		}); err != nil {
			t.Fatalf("task stat COMPLETED: %v", err)
		}

		task, err := database.GetTask(ctx, 1)
		if err != nil {
			t.Fatalf("reading task 1: %v", err)
		}
		if task.CompletionSummary == nil {
			t.Fatal("completion_summary is NULL after a COMPLETED transition that supplied one")
		}
		if *task.CompletionSummary != summary {
			t.Errorf("completion_summary stored as %q, want %q", *task.CompletionSummary, summary)
		}
	})
}

// TestMaximumLengthIsMeasuredOnTheTrimmedValue is the consequence of Rule 2 that
// SPEC/MODELS.md marks as required: the cap measures the same value the database
// stores, so a value of exactly the maximum length carrying surrounding
// whitespace is accepted.
//
// This is also the side defect the trim closes. Before it, `sprint create` and
// `sprint update` measured the value AS SUPPLIED, so 255 real characters wrapped
// in spaces were refused there and accepted by `task create` -- one cap, two
// answers, for a value the column would have held either way.
//
// The paired negative case is what stops the test being vacuous: one character
// more than the maximum, with no padding at all, is still refused.
func TestMaximumLengthIsMeasuredOnTheTrimmedValue(t *testing.T) {
	const roadmap = "free-text-cap-on-trimmed"
	setupEmptinessRoadmap(t, roadmap)

	const pad = "   "
	exactly := func(n int) string { return strings.Repeat("A", n) }

	cases := []struct {
		command string
		field   utils.Field
		limit   int
		invoke  func(v string) error
	}{
		{"sprint create -t", utils.FieldSprintTitle, models.MaxSprintTitle, func(v string) error {
			return sprintCreate([]string{"-r", roadmap, "-t", v, "-d", "Deliver the refresh-token guard."})
		}},
		{"sprint create -d", utils.FieldSprintDescription, models.MaxSprintDescription, func(v string) error {
			return sprintCreate([]string{"-r", roadmap, "-t", "Refresh-token guard", "-d", v})
		}},
		{"sprint update -t", utils.FieldSprintTitle, models.MaxSprintTitle, func(v string) error {
			return sprintUpdate([]string{"-r", roadmap, "1", "-t", v})
		}},
		{"sprint update -d", utils.FieldSprintDescription, models.MaxSprintDescription, func(v string) error {
			return sprintUpdate([]string{"-r", roadmap, "1", "-d", v})
		}},
		{"task create -t", utils.FieldTaskTitle, models.MaxTaskTitle, func(v string) error {
			return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskTitle, v))
		}},
		{"task create -fr", utils.FieldTaskFunctionalRequirements, models.MaxTaskFunctionalRequirements, func(v string) error {
			return taskCreate(taskCreateWithField(roadmap, utils.FieldTaskFunctionalRequirements, v))
		}},
		{"task edit -t", utils.FieldTaskTitle, models.MaxTaskTitle, func(v string) error {
			return taskEdit([]string{"-r", roadmap, "1", "-t", v})
		}},
	}

	for _, c := range cases {
		t.Run(c.command+"/exactly the maximum, padded", func(t *testing.T) {
			// captureStdout keeps the {"id": N} a successful create prints out
			// of the test log; the verdict is the error, not the output.
			captureStdout(t, func() {
				if err := c.invoke(pad + exactly(c.limit) + pad); err != nil {
					t.Fatalf("%s refused %d real characters wrapped in whitespace, which the column holds: %v",
						c.command, c.limit, err)
				}
			})
		})

		t.Run(c.command+"/one over the maximum, unpadded", func(t *testing.T) {
			err := c.invoke(exactly(c.limit + 1))
			if err == nil {
				t.Fatalf("%s accepted %d characters, one over its maximum", c.command, c.limit+1)
			}
			want := utils.FieldTooLargeError(c.field, c.limit).Error()
			if err.Error() != want {
				t.Errorf("%s\n got: %q\nwant: %q", c.command, err.Error(), want)
			}
		})
	}
}
