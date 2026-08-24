package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file is the command-level gate for
// SPEC/COMMANDS.md § UTF-8 Encoding Constraint (All Free-Text Fields), and for
// the canonical rule it defers to, SPEC/MODELS.md § Free-Text UTF-8 Encoding
// Constraint.
//
// It sweeps the SAME table of eight fields and their writers that the published
// name gate uses, rather than a list of its own: a rule that governs the whole
// set is only proved on the whole set, and a field or a command added to the
// SPEC table has one place to be added here.
//
// What was wrong before rmp task 180, reproduced against the compiled binary:
//
//	printf 'a\x80b' | rmp task comment-add ... --type NOTE
//
// stored the three raw bytes verbatim in a TEXT column, because the
// control-character rule decodes rune by rune and an invalid byte decodes to
// U+FFFD, which is not a forbidden code point. Every reader of that row then
// reported something else — Go's JSON encoder substitutes U+FFFD for each
// invalid byte — so the documented output could not round-trip what was stored.

// utf8RefusalFor is the full message SPEC/COMMANDS.md pins for one field, minus
// the "Error: " prefix the CLI adds when it prints it. The field name is taken
// from the shared definition, never spelled here.
func utf8RefusalFor(field utils.Field) string {
	return "validation error: " + field.String() + ": the value is not valid UTF-8"
}

// TestEveryFreeTextFieldRefusesMalformedUTF8 is the acceptance criterion, over
// every field of the SPEC table, every command that writes it, and every
// malformed shape the SPEC enumerates.
//
// The refusal is checked three ways, because two of them alone would not be
// enough: the exit class (6, through utils.IsValidation, which is what
// SPEC/ARCHITECTURE.md maps to the exit code), the exact message (a
// control-character refusal carries the same class and the same exit code, so
// only the wording tells the two rules apart), and the field name inside it
// (which must be the published one, and the one for the field actually under
// test rather than for a sibling that happened to be validated first).
func TestEveryFreeTextFieldRefusesMalformedUTF8(t *testing.T) {
	const roadmap = "utf8-encoding-sweep"
	_, taskCommentID, sprintCommentID := setupPublishedNameRoadmap(t, roadmap)

	cases := fieldWriterCases(roadmap, taskCommentID, sprintCommentID)
	if len(cases) != 8 {
		t.Fatalf("the sweep covers %d fields, but SPEC/COMMANDS.md publishes 8; a field is missing from the table", len(cases))
	}

	corpus := testenv.MalformedUTF8Corpus()
	if len(corpus) != 5 {
		t.Fatalf("the corpus holds %d entries; SPEC/MODELS.md enumerates 5 malformed shapes", len(corpus))
	}

	for _, tc := range cases {
		for _, w := range tc.writers {
			for _, c := range corpus {
				t.Run(tc.field.String()+"/"+w.command+"/"+c.Name, func(t *testing.T) {
					err := w.invoke(c.Value)
					if err == nil {
						t.Fatalf("%s accepted %q and would store it.\n  %s", w.command, c.Value, c.Why)
					}
					if !utils.IsValidation(err) {
						t.Errorf("%s: refusal must carry utils.ErrValidation (exit 6); got %v", w.command, err)
					}
					if want := utf8RefusalFor(tc.field); err.Error() != want {
						t.Errorf("%s: message\n got: %q\nwant: %q", w.command, err.Error(), want)
					}
				})
			}
		}
	}
}

// TestEncodingIsReportedBeforeControlCharacters makes the ORDER observable at
// the command surface, for every field and every command that writes one.
//
// It is the decision the user recorded on rmp task 180 and SPEC/MODELS.md states
// under "Order": the encoding check runs immediately before the
// control-character check, everywhere. Both refusals carry utils.ErrValidation
// and exit code 6, so nothing but the WORDING can tell which rule answered —
// which is why the value fed in breaks both rules at once and the assertion is
// on the message.
//
// Sweeping the whole table matters here even though one shared helper applies
// both rules: what this pins is that every writer reaches the rules through that
// helper, and a command that went back to calling the control-character check on
// its own would fail here rather than pass unnoticed.
func TestEncodingIsReportedBeforeControlCharacters(t *testing.T) {
	const roadmap = "utf8-encoding-order"
	_, taskCommentID, sprintCommentID := setupPublishedNameRoadmap(t, roadmap)

	for _, tc := range fieldWriterCases(roadmap, taskCommentID, sprintCommentID) {
		for _, w := range tc.writers {
			for _, c := range testenv.MalformedUTF8Corpus() {
				t.Run(tc.field.String()+"/"+w.command+"/"+c.Name, func(t *testing.T) {
					// Malformed AND carrying an ESC, the control-character probe
					// the published-name gate uses. Short enough for the 255-byte
					// title cap, so the length rule cannot answer instead.
					err := w.invoke(c.Value + " " + controlCharProbe)
					if err == nil {
						t.Fatalf("%s accepted a value that breaks both content rules", w.command)
					}
					if want := utf8RefusalFor(tc.field); err.Error() != want {
						t.Errorf("%s: the encoding check must run first.\n got: %q\nwant: %q",
							w.command, err.Error(), want)
					}
				})
			}
		}
	}
}

// TestMalformedValueIsRefusedBeforeAnythingIsWritten pins the other half of the
// refusal: SPEC/MODELS.md says the application "stores nothing and changes no
// entity when it refuses". A message alone would not show that.
//
// Each writer is driven with a malformed value, and the database is read
// directly afterwards — never through the command that prints it — to confirm
// no task, sprint or comment appeared and none was modified.
func TestMalformedValueIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	const roadmap = "utf8-encoding-no-write"
	database, taskCommentID, sprintCommentID := setupPublishedNameRoadmap(t, roadmap)

	// The state the refusals must leave untouched.
	tasksBefore := listTasksForUTF8(t, database)
	sprintsBefore := listSprintsForUTF8(t, database)
	taskCommentsBefore := listComments(t, database, 1)
	sprintCommentsBefore := listSprintComments(t, database, 1)
	if len(tasksBefore) == 0 || len(sprintsBefore) == 0 ||
		len(taskCommentsBefore) != 1 || len(sprintCommentsBefore) != 1 {
		t.Fatalf("the fixture is not what the assertions below assume: %d tasks, %d sprints, %d/%d comments",
			len(tasksBefore), len(sprintsBefore), len(taskCommentsBefore), len(sprintCommentsBefore))
	}

	malformed := testenv.MalformedUTF8Corpus()[0].Value
	for _, tc := range fieldWriterCases(roadmap, taskCommentID, sprintCommentID) {
		for _, w := range tc.writers {
			if err := w.invoke(malformed); err == nil {
				t.Fatalf("%s accepted a malformed value", w.command)
			}
		}
	}

	if got := listTasksForUTF8(t, database); len(got) != len(tasksBefore) {
		t.Errorf("tasks: %d before the refusals, %d after; a refusal created one", len(tasksBefore), len(got))
	}
	if got := listSprintsForUTF8(t, database); len(got) != len(sprintsBefore) {
		t.Errorf("sprints: %d before the refusals, %d after; a refusal created one", len(sprintsBefore), len(got))
	}
	assertCommentsUnchanged(t, taskCommentsBefore, listComments(t, database, 1))
	assertSprintCommentsUnchanged(t, sprintCommentsBefore, listSprintComments(t, database, 1))

	// And the fields a refused EDIT would have overwritten are still what they
	// were: `task edit` and `sprint update` refuse in the argument-validation
	// phase, before the database is opened at all.
	task := tasksBefore[0]
	after := listTasksForUTF8(t, database)[0]
	if after.Title != task.Title || after.FunctionalRequirements != task.FunctionalRequirements {
		t.Errorf("a refused `task edit` modified the task:\n before: %q / %q\n after:  %q / %q",
			task.Title, task.FunctionalRequirements, after.Title, after.FunctionalRequirements)
	}
}

// TestWellFormedTextPassesTheContentRules is the non-vacuity guard for the sweep
// above. A validator that refused every value outright would satisfy both tests
// before it; this one fails unless the eight fields still let ordinary text
// through — accented Latin, CJK and emoji included.
//
// # Why it asserts what was NOT reported rather than plain success
//
// Not every writer in the table can succeed from one fixture state. `task stat
// COMPLETED --summary` validates the summary at step 3 and then, at step 4,
// requires --commit-close and a task already in TESTING (SPEC/COMMANDS.md §
// Change Status (stat)); driving that lifecycle here would mutate the fixture
// under the writers that run after it, and asserting success would then be
// asserting something about the state machine rather than about this rule.
//
// So the claim is made exactly as narrow as the rule it guards: the value
// reached, and passed, both content rules of the field. Neither refusal can have
// been produced — and the two are named through the shared definition, so a
// reworded message cannot make this pass by no longer matching. A command that
// then declines for an unrelated reason of its own is not this rule's business,
// and the E2E suite covers the storage round-trip that is
// (tests/test_16_boundary_unicode.py).
func TestWellFormedTextPassesTheContentRules(t *testing.T) {
	const roadmap = "utf8-encoding-accepts"
	_, taskCommentID, sprintCommentID := setupPublishedNameRoadmap(t, roadmap)

	// Short enough for the 255-byte task and sprint title cap, which is the
	// tightest of the eight, so one value can drive every writer.
	const value = "Reconciliação: medição 監査 \U0001F680"

	for _, tc := range fieldWriterCases(roadmap, taskCommentID, sprintCommentID) {
		for _, w := range tc.writers {
			t.Run(tc.field.String()+"/"+w.command, func(t *testing.T) {
				var err error
				_ = captureStdout(t, func() { err = w.invoke(value) })
				if err == nil {
					return // accepted outright: the strongest outcome
				}
				for _, forbidden := range []error{
					utils.InvalidUTF8Error(tc.field),
					utils.ControlCharError(tc.field),
				} {
					if strings.Contains(err.Error(), forbidden.Error()) {
						t.Errorf("%s refused well-formed text %q with a content rule: %v",
							w.command, value, err)
					}
				}
			})
		}
	}
}

// TestOversizedMalformedValueIsRefusedForItsLength pins the ORDER consequence
// SPEC/MODELS.md marks as deliberate and to be preserved: the encoding check
// takes the control-character check's position and moves no other check, so
// wherever a command applies its length limit FIRST, a value that is at once
// oversized and malformed is still refused for its length.
//
// Only the commands that already cap the field before the control-character
// check are listed. The others cap it afterwards, or not at that point at all,
// and unifying them is deliberately out of scope here — rmp task 302 owns that
// question, and this rule was specified relationally so that it stays true under
// either answer.
func TestOversizedMalformedValueIsRefusedForItsLength(t *testing.T) {
	const roadmap = "utf8-encoding-length-wins"
	_, taskCommentID, sprintCommentID := setupPublishedNameRoadmap(t, roadmap)

	malformed := testenv.MalformedUTF8Corpus()[0].Value

	for _, tc := range []struct {
		name    string
		field   utils.Field
		limit   int
		invoke  func(value string) error
		command string
	}{
		{
			name: "task comment-add --body", field: utils.FieldCommentBody, limit: models.MaxCommentBody,
			command: "task comment-add",
			invoke: func(v string) error {
				return taskCommentAdd([]string{"-r", roadmap, "1", "--type", "DECISION", "--body", v})
			},
		},
		{
			name: "task comment-edit --body", field: utils.FieldCommentBody, limit: models.MaxCommentBody,
			command: "task comment-edit",
			invoke: func(v string) error {
				return taskCommentEdit([]string{"-r", roadmap, itoa(taskCommentID), "--body", v})
			},
		},
		{
			name: "sprint comment-add --body", field: utils.FieldCommentBody, limit: models.MaxCommentBody,
			command: "sprint comment-add",
			invoke: func(v string) error {
				return sprintCommentAdd([]string{"-r", roadmap, "1", "--type", "DECISION", "--body", v})
			},
		},
		{
			name: "sprint comment-edit --body", field: utils.FieldCommentBody, limit: models.MaxCommentBody,
			command: "sprint comment-edit",
			invoke: func(v string) error {
				return sprintCommentEdit([]string{"-r", roadmap, itoa(sprintCommentID), "--body", v})
			},
		},
		{
			name: "task stat COMPLETED --summary", field: utils.FieldTaskCompletionSummary,
			limit: models.MaxTaskCompletionSummary, command: "task stat",
			invoke: func(v string) error {
				return taskSetStatus([]string{"-r", roadmap, "1", "COMPLETED", "--summary", v})
			},
		},
		{
			name: "sprint update -t", field: utils.FieldSprintTitle, limit: models.MaxSprintTitle,
			command: "sprint update",
			invoke: func(v string) error {
				return sprintUpdate([]string{"-r", roadmap, "1", "-t", v})
			},
		},
		{
			name: "sprint update -d", field: utils.FieldSprintDescription, limit: models.MaxSprintDescription,
			command: "sprint update",
			invoke: func(v string) error {
				return sprintUpdate([]string{"-r", roadmap, "1", "-d", v})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Malformed early, and long enough to break the cap regardless of
			// whether the cap counts bytes or characters.
			value := malformed + strings.Repeat("x", tc.limit)

			err := tc.invoke(value)
			if err == nil {
				t.Fatalf("%s accepted a value that is both oversized and malformed", tc.command)
			}
			if !utils.IsFieldTooLarge(err) {
				t.Errorf("%s: the length rule must win here, and it must keep winning.\n got: %v\n"+
					"The encoding check takes the control-character check's position and moves no "+
					"other check, so the cap stays ahead of it on this command.", tc.command, err)
			}
			if want := utils.FieldTooLargeError(tc.field, tc.limit).Error(); err.Error() != want {
				t.Errorf("%s: message\n got: %q\nwant: %q", tc.command, err.Error(), want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Fixture readers.
//
// They go to the database rather than to the JSON a command prints, so an
// assertion about what was written never depends on the command under test.
// ---------------------------------------------------------------------------

func listTasksForUTF8(t *testing.T, database *db.DB) []models.Task {
	t.Helper()

	tasks, err := database.ListTasks(context.Background(), nil)
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	return tasks
}

func listSprintsForUTF8(t *testing.T, database *db.DB) []models.Sprint {
	t.Helper()

	sprints, err := database.ListSprints(context.Background(), nil)
	if err != nil {
		t.Fatalf("listing sprints: %v", err)
	}
	return sprints
}

func assertCommentsUnchanged(t *testing.T, before, after []models.TaskComment) {
	t.Helper()

	if len(before) != len(after) {
		t.Fatalf("task comments: %d before the refusals, %d after", len(before), len(after))
	}
	for i := range before {
		if before[i].Body != after[i].Body || before[i].Type != after[i].Type {
			t.Errorf("a refused task comment write modified comment %d:\n before: %q/%q\n after:  %q/%q",
				before[i].ID, before[i].Type, before[i].Body, after[i].Type, after[i].Body)
		}
		if after[i].UpdatedAt != nil {
			t.Errorf("a refused task comment write stamped updated_at on comment %d", after[i].ID)
		}
	}
}

func assertSprintCommentsUnchanged(t *testing.T, before, after []models.SprintComment) {
	t.Helper()

	if len(before) != len(after) {
		t.Fatalf("sprint comments: %d before the refusals, %d after", len(before), len(after))
	}
	for i := range before {
		if before[i].Body != after[i].Body || before[i].Type != after[i].Type {
			t.Errorf("a refused sprint comment write modified comment %d:\n before: %q/%q\n after:  %q/%q",
				before[i].ID, before[i].Type, before[i].Body, after[i].Type, after[i].Body)
		}
		if after[i].UpdatedAt != nil {
			t.Errorf("a refused sprint comment write stamped updated_at on comment %d", after[i].ID)
		}
	}
}
