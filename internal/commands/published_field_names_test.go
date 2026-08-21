package commands

import (
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file is the behavioural gate for
// SPEC/COMMANDS.md § Published Field Names in Validation Messages.
//
// The defect it exists against was reproduced against the compiled binary: for
// one field and one rule, `task create` answered
//
//	Error: validation error: functional-requirements: control characters are not allowed
//
// while `task edit` answered
//
//	Error: validation error: functional_requirements: control characters are not allowed
//
// and `task edit` refused an empty value for the same field as
// `functional-requirements cannot be empty` while refusing an oversize one as
// `functional_requirements exceeds maximum length of 4096 characters`. A caller
// matching on the field name had to match two spellings for one field and could
// not tell from the message which it would get.
//
// The structural half of the fix — one shared definition, unreachable by a
// literal — is gated in internal/utils. These tests gate what a user sees.

// controlCharProbe is the value from the original report: a bare ESC between two
// letters. It is refused by the control-character rule on every free-text field,
// which makes it the one rule that can be triggered from every command that
// writes one, and therefore the rule the parity comparison is built on.
const controlCharProbe = "a\x1bb"

// fieldWriter is one command that writes one field, together with the
// invocation that makes it refuse a value.
type fieldWriter struct {
	invoke  func(value string) error
	command string
}

// fieldParityCase is one field of the SPEC table together with every command
// that writes it.
type fieldParityCase struct {
	writers []fieldWriter
	field   utils.Field
}

// setupPublishedNameRoadmap seeds a roadmap with everything the writers below
// need to reach their field validation: two tasks, one sprint, and one comment
// on each, since `comment-edit` needs a comment that already exists and both
// comment-add paths check the parent before they look at the body.
func setupPublishedNameRoadmap(t *testing.T, name string) (database *db.DB, taskCommentID, sprintCommentID int) {
	t.Helper()

	database = setupCommentRoadmap(t, name)

	_ = captureStdout(t, func() {
		if err := sprintCreate([]string{
			"-r", name,
			"-t", "Expiry hardening",
			"-d", "Close the JWT boundary-second defect and lock it behind a regression test.",
		}); err != nil {
			t.Fatalf("seeding the sprint: %v", err)
		}
		if err := taskCommentAdd([]string{
			"-r", name, "1", "--type", "DECISION", "--body", "Refuse the token whose exp is the current second.",
		}); err != nil {
			t.Fatalf("seeding the task comment: %v", err)
		}
		if err := sprintCommentAdd([]string{
			"-r", name, "1", "--type", "DECISION", "--body", "The sprint closes only once the regression test is green.",
		}); err != nil {
			t.Fatalf("seeding the sprint comment: %v", err)
		}
	})

	taskComments := listComments(t, database, 1)
	if len(taskComments) != 1 {
		t.Fatalf("seeded task comments = %d, want 1", len(taskComments))
	}
	sprintComments := listSprintComments(t, database, 1)
	if len(sprintComments) != 1 {
		t.Fatalf("seeded sprint comments = %d, want 1", len(sprintComments))
	}
	return database, taskComments[0].ID, sprintComments[0].ID
}

// taskCreateArgs builds a complete `task create` invocation in which exactly one
// of the four required free-text values is the probe and the other three are
// ordinary text. Without this the command would refuse the invocation for a
// missing parameter before it ever validated the field under test.
func taskCreateArgs(roadmap string, under utils.Field, value string) []string {
	values := map[utils.Field]string{
		utils.FieldTaskTitle:                  "Refuse a token that expired this second",
		utils.FieldTaskFunctionalRequirements: "A token whose exp is the current second must be refused",
		utils.FieldTaskTechnicalRequirements:  "Compare with !time.Now().Before(exp)",
		utils.FieldTaskAcceptanceCriteria:     "A unit test covers the exact boundary second",
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

// TestEveryCommandThatWritesAFieldNamesItTheSameWay is acceptance criteria 1 and
// 3: one rule is triggered on one field from every command that writes it, and
// every resulting message must carry the one published name.
//
// The name is EXTRACTED from each message rather than merely looked for in it,
// so a message that named the field wrongly fails with the two spellings side by
// side instead of failing a containment check that says nothing about what it
// did say.
func TestEveryCommandThatWritesAFieldNamesItTheSameWay(t *testing.T) {
	const roadmap = "published-field-name-parity"
	_, taskCommentID, sprintCommentID := setupPublishedNameRoadmap(t, roadmap)

	cases := []fieldParityCase{
		{field: utils.FieldTaskTitle, writers: []fieldWriter{
			{command: "task create -t", invoke: func(v string) error {
				return taskCreate(taskCreateArgs(roadmap, utils.FieldTaskTitle, v))
			}},
			{command: "task edit -t", invoke: func(v string) error {
				return taskEdit([]string{"-r", roadmap, "1", "-t", v})
			}},
		}},
		{field: utils.FieldTaskFunctionalRequirements, writers: []fieldWriter{
			{command: "task create -fr", invoke: func(v string) error {
				return taskCreate(taskCreateArgs(roadmap, utils.FieldTaskFunctionalRequirements, v))
			}},
			{command: "task edit -fr", invoke: func(v string) error {
				return taskEdit([]string{"-r", roadmap, "1", "-fr", v})
			}},
		}},
		{field: utils.FieldTaskTechnicalRequirements, writers: []fieldWriter{
			{command: "task create -tr", invoke: func(v string) error {
				return taskCreate(taskCreateArgs(roadmap, utils.FieldTaskTechnicalRequirements, v))
			}},
			{command: "task edit -tr", invoke: func(v string) error {
				return taskEdit([]string{"-r", roadmap, "1", "-tr", v})
			}},
		}},
		{field: utils.FieldTaskAcceptanceCriteria, writers: []fieldWriter{
			{command: "task create -ac", invoke: func(v string) error {
				return taskCreate(taskCreateArgs(roadmap, utils.FieldTaskAcceptanceCriteria, v))
			}},
			{command: "task edit -ac", invoke: func(v string) error {
				return taskEdit([]string{"-r", roadmap, "1", "-ac", v})
			}},
		}},
		{field: utils.FieldTaskCompletionSummary, writers: []fieldWriter{
			// The only command that writes it. It is here so the sweep covers
			// all eight fields of the SPEC table rather than only the ones with
			// two writers to compare.
			{command: "task stat COMPLETED --summary", invoke: func(v string) error {
				return taskSetStatus([]string{"-r", roadmap, "1", "COMPLETED", "--summary", v})
			}},
		}},
		{field: utils.FieldSprintTitle, writers: []fieldWriter{
			{command: "sprint create -t", invoke: func(v string) error {
				return sprintCreate([]string{"-r", roadmap, "-t", v, "-d", "Any description"})
			}},
			{command: "sprint update -t", invoke: func(v string) error {
				return sprintUpdate([]string{"-r", roadmap, "1", "-t", v})
			}},
		}},
		{field: utils.FieldSprintDescription, writers: []fieldWriter{
			{command: "sprint create -d", invoke: func(v string) error {
				return sprintCreate([]string{"-r", roadmap, "-t", "Any title", "-d", v})
			}},
			{command: "sprint update -d", invoke: func(v string) error {
				return sprintUpdate([]string{"-r", roadmap, "1", "-d", v})
			}},
		}},
		{field: utils.FieldCommentBody, writers: []fieldWriter{
			{command: "task comment-add --body", invoke: func(v string) error {
				return taskCommentAdd([]string{"-r", roadmap, "1", "--type", "DECISION", "--body", v})
			}},
			{command: "task comment-edit --body", invoke: func(v string) error {
				return taskCommentEdit([]string{"-r", roadmap, itoa(taskCommentID), "--body", v})
			}},
			{command: "sprint comment-add --body", invoke: func(v string) error {
				return sprintCommentAdd([]string{"-r", roadmap, "1", "--type", "DECISION", "--body", v})
			}},
			{command: "sprint comment-edit --body", invoke: func(v string) error {
				return sprintCommentEdit([]string{"-r", roadmap, itoa(sprintCommentID), "--body", v})
			}},
		}},
	}

	if len(cases) != 8 {
		t.Fatalf("the sweep covers %d fields, but SPEC/COMMANDS.md publishes 8; a field is missing from this table", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.field.String()+"/"+tc.writers[0].command, func(t *testing.T) {
			var first, firstCommand string
			for _, w := range tc.writers {
				err := w.invoke(controlCharProbe)
				if err == nil {
					t.Fatalf("%s accepted %q; every free-text field refuses a control character", w.command, controlCharProbe)
				}
				name := fieldNameFromControlCharRefusal(t, w.command, err.Error())
				if name != tc.field.String() {
					t.Errorf("%s names the field %q, but its published name is %q\n  message: %s",
						w.command, name, tc.field.String(), err.Error())
				}
				if first == "" {
					first, firstCommand = name, w.command
					continue
				}
				if name != first {
					t.Errorf("one field, two names: %s says %q and %s says %q",
						firstCommand, first, w.command, name)
				}
			}
		})
	}
}

// fieldNameFromControlCharRefusal pulls the field name out of a control-character
// refusal. The message is "validation error: <field>: control characters are not
// allowed", so the name is the last colon-separated part before the rule's own
// wording.
func fieldNameFromControlCharRefusal(t *testing.T, command, message string) string {
	t.Helper()

	const rule = ": control characters are not allowed"
	cut := strings.Index(message, rule)
	if cut < 0 {
		t.Fatalf("%s did not refuse with the control-character rule; message: %q", command, message)
	}
	parts := strings.Split(message[:cut], ": ")
	return parts[len(parts)-1]
}

// TestOneCommandNamesOneFieldOneWayWhateverTheRule is acceptance criterion 2.
//
// `task edit` publishes three refusals about one field, and until rmp task 297
// one of the three spelled it differently from the other two. Each rule is
// triggered here in turn on the same field of the same task, and all three
// messages must name it identically.
func TestOneCommandNamesOneFieldOneWayWhateverTheRule(t *testing.T) {
	const roadmap = "published-field-name-one-command"
	setupPublishedNameRoadmap(t, roadmap)

	const published = "functional_requirements"
	const kebab = "functional-requirements"

	rules := []struct {
		name  string
		value string
		want  string
	}{
		{"empty value", "", published + " cannot be empty"},
		{"oversize value", strings.Repeat("x", models.MaxTaskFunctionalRequirements+1),
			published + " exceeds maximum length of 4096 characters"},
		{"control character", controlCharProbe, published + ": control characters are not allowed"},
	}

	for _, rule := range rules {
		t.Run(rule.name, func(t *testing.T) {
			err := taskEdit([]string{"-r", roadmap, "1", "-fr", rule.value})
			if err == nil {
				t.Fatalf("task edit accepted the %s", rule.name)
			}
			if !strings.Contains(err.Error(), rule.want) {
				t.Errorf("message\n got: %q\nwant substring: %q", err.Error(), rule.want)
			}
			if strings.Contains(err.Error(), kebab) {
				t.Errorf("message names the field in kebab-case, which no validation message may do: %q", err.Error())
			}
		})
	}
}

// TestTaskCreateNamesTheFlagWhenNoValueReachedIt is acceptance criterion 5, and
// the boundary of this whole rule.
//
// A message whose subject is a FLAG keeps the flag's own spelling, hyphens and
// leading double dash included, because no value reached the application at all.
// Correcting the field messages must not drag this one along with them: after
// the change `task create` still reports the absent flag as
// `--functional-requirements` while reporting a supplied value that breaks a
// content rule as `functional_requirements`.
func TestTaskCreateNamesTheFlagWhenNoValueReachedIt(t *testing.T) {
	const roadmap = "published-field-name-missing-flag"
	setupPublishedNameRoadmap(t, roadmap)

	err := taskCreate([]string{
		"-r", roadmap,
		"-t", "Refuse a token that expired this second",
		"-tr", "Compare with !time.Now().Before(exp)",
		"-ac", "A unit test covers the exact boundary second",
	})
	if err == nil {
		t.Fatal("task create without --functional-requirements must fail")
	}
	if !utils.IsRequired(err) {
		t.Errorf("error must wrap utils.ErrRequired (exit 2); got %v", err)
	}
	if want := "required parameter missing: --functional-requirements"; !strings.Contains(err.Error(), want) {
		t.Errorf("message\n got: %q\nwant substring: %q", err.Error(), want)
	}

	// The same command, the same field, a value that did arrive: now the field
	// is named, and by its published name.
	err = taskCreate(taskCreateArgs(roadmap, utils.FieldTaskFunctionalRequirements, controlCharProbe))
	if err == nil {
		t.Fatal("task create must refuse a control character in --functional-requirements")
	}
	if want := "functional_requirements: control characters are not allowed"; !strings.Contains(err.Error(), want) {
		t.Errorf("message\n got: %q\nwant substring: %q", err.Error(), want)
	}
}
