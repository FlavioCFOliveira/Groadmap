// Package commands — tests for the four comment subcommands of the `sprint`
// family.
//
// The suite mirrors the task-family suite, because the two families are served by
// one implementation and the contract they publish is the same one read against a
// sprint. What it asserts beyond that mirror is what the SPEC pins as DIFFERENT
// (SPEC/COMMANDS.md § Sprint Comments):
//
//   - the accepted type set is the FOUR sprint values, so HYPOTHESIS, TEST and
//     NOTE — all three valid on a task comment — are exit-6 refusals here, with a
//     message naming the sprint's own set;
//   - the audit entry is written against the parent SPRINT, with entity_type
//     SPRINT and the parent sprint's id, never the comment's own id and never a
//     new entity_type value;
//   - the comment id spaces are per table, so a `sprint comment-edit <id>` never
//     resolves against task_comments and a task comment is never reachable from
//     here — in either direction, including when both tables carry the same id;
//   - a comment is accepted in every sprint status, CLOSED included, and never
//     changes it.
//
// Every test asserts the outcome — the stored row, the printed JSON, the audit
// row — and not merely the error class. Forbidden control characters appear only
// as Go escape sequences: a literal one in the source would be the very hazard
// the rule exists to reject.
package commands

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// setupSprintCommentRoadmap creates a hermetic roadmap under a temporary HOME and
// seeds two PENDING sprints, so a test can comment on one and use the other to
// prove that a comment id and a sprint id are never confused. Both are left
// PENDING because at most one sprint may be OPEN at a time (idx_one_open_sprint);
// the status walk builds its own sprint.
func setupSprintCommentRoadmap(t *testing.T, name string) *db.DB {
	t.Helper()

	// Hermetic: ~/.roadmaps resolves under the test's own HOME, so a run never
	// touches (or depends on) the developer's real roadmaps.
	t.Setenv("HOME", t.TempDir())

	database, cleanup := setupTestTaskRoadmap(t, name)
	t.Cleanup(cleanup)

	seed := []struct{ title, description string }{
		{
			"Expiry hardening",
			"Close the JWT boundary-second defect and lock it behind a regression test.",
		},
		{
			"Session storage compliance",
			"Move session token storage onto the encrypted store legal requires.",
		},
	}
	for _, s := range seed {
		_ = captureStdout(t, func() {
			if err := sprintCreate([]string{"-r", name, "-t", s.title, "-d", s.description}); err != nil {
				t.Fatalf("seeding sprint %q: %v", s.title, err)
			}
		})
	}

	return database
}

// listSprintComments reads a sprint's comments straight from the database, so an
// assertion about what was stored never depends on the command that prints them.
func listSprintComments(t *testing.T, database *db.DB, sprintID int) []models.SprintComment {
	t.Helper()

	comments, err := database.ListSprintComments(context.Background(), sprintID, nil)
	if err != nil {
		t.Fatalf("listing comments of sprint %d: %v", sprintID, err)
	}
	return comments
}

// getSprintComment reads one sprint comment by its own id.
func getSprintComment(t *testing.T, database *db.DB, id int) *models.SprintComment {
	t.Helper()

	comment, err := database.GetSprintComment(context.Background(), id)
	if err != nil {
		t.Fatalf("reading sprint comment %d: %v", id, err)
	}
	return comment
}

// countSprintAudit counts the audit rows for one comment operation against one
// sprint. The entity type is fixed at SPRINT: that is the invariant this family
// asserts, so a mutation logged against a TASK would not be counted and the
// assertion would fail.
func countSprintAudit(t *testing.T, database *db.DB, op models.AuditOperation, sprintID int) int {
	t.Helper()
	return countCommentAudit(t, database, op, models.EntitySprint, sprintID)
}

// addSprintComment seeds a comment through the command under test. Tests that need
// the id read it back from the database, which also proves the row exists.
func addSprintComment(t *testing.T, roadmap string, sprintID int, commentType, body string) {
	t.Helper()

	out := captureStdout(t, func() {
		if err := sprintCommentAdd([]string{
			"-r", roadmap, itoa(sprintID), "--type", commentType, "--body", body,
		}); err != nil {
			t.Fatalf("comment-add(%s): %v", commentType, err)
		}
	})
	if !strings.Contains(out, `"id"`) {
		t.Fatalf("comment-add printed no id: %q", out)
	}
}

// seedTaskComment creates one task and one comment on it, returning both ids. It is
// the other family's row, used to prove the two comment id spaces never meet.
func seedTaskComment(t *testing.T, roadmap string) (taskID, commentID int) {
	t.Helper()

	_ = captureStdout(t, func() {
		if err := taskCreate([]string{
			"-r", roadmap,
			"-t", "Fix JWT boundary-second expiry",
			"-fr", "A token whose exp is the current second must be refused",
			"-tr", "Compare with !time.Now().Before(exp) instead of time.Now().After(exp)",
			"-ac", "A unit test covers the exact boundary second",
		}); err != nil {
			t.Fatalf("seeding the task: %v", err)
		}
	})

	database, err := db.OpenExisting(roadmap)
	if err != nil {
		t.Fatalf("opening the roadmap: %v", err)
	}
	defer database.Close()

	tasks, err := database.ListTasks(context.Background(), nil)
	if err != nil {
		t.Fatalf("reading the seeded task back: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("no task was seeded")
	}
	taskID = tasks[len(tasks)-1].ID

	_ = captureStdout(t, func() {
		if addErr := taskCommentAdd([]string{
			"-r", roadmap, itoa(taskID), "--type", "HYPOTHESIS",
			"--body", "The clock-skew allowance may be masking the boundary second.",
		}); addErr != nil {
			t.Fatalf("commenting on the task: %v", addErr)
		}
	})

	comments, err := database.ListTaskComments(context.Background(), taskID, nil)
	if err != nil || len(comments) == 0 {
		t.Fatalf("reading the task comment back: %v", err)
	}
	return taskID, comments[len(comments)-1].ID
}

// ---------------------------------------------------------------------------
// comment-add: body input sources and precedence
// ---------------------------------------------------------------------------

// TestSprintCommentAdd_BodyFromFlag verifies the flag form end to end: the row is
// stored trimmed, updated_at starts null, the audit entry is written against the
// parent sprint, and the printed payload carries the new comment's id.
func TestSprintCommentAdd_BodyFromFlag(t *testing.T) {
	const roadmap = "sprint-comment-add-body-flag"
	database := setupSprintCommentRoadmap(t, roadmap)

	const body = "Dropped the second migration from the sprint: its schema change is not settled yet."

	out := captureStdout(t, func() {
		if err := sprintCommentAdd([]string{
			"-r", roadmap, "1", "--type", "DECISION", "--body", "  " + body + "  \n",
		}); err != nil {
			t.Fatalf("comment-add: %v", err)
		}
	})

	comments := listSprintComments(t, database, 1)
	if len(comments) != 1 {
		t.Fatalf("stored comments = %d, want 1", len(comments))
	}
	got := comments[0]
	if got.Body != body {
		t.Errorf("stored body is not the trimmed form\n got: %q\nwant: %q", got.Body, body)
	}
	if got.Type != models.CommentDecision {
		t.Errorf("stored type = %q, want DECISION", got.Type)
	}
	if got.SprintID != 1 {
		t.Errorf("stored sprint_id = %d, want 1", got.SprintID)
	}
	if got.UpdatedAt != nil {
		t.Errorf("updated_at = %q, want null on a fresh comment", *got.UpdatedAt)
	}
	if got.CreatedAt == "" {
		t.Error("created_at is empty; the command must stamp it")
	}
	if want := `"id": ` + itoa(got.ID); !strings.Contains(out, want) {
		t.Errorf("stdout does not carry the new comment id\n got: %q\nwant substring: %q", out, want)
	}
	if n := countSprintAudit(t, database, models.OpSprintCommentCreate, 1); n != 1 {
		t.Errorf("SPRINT_COMMENT_CREATE audit entries against sprint 1 = %d, want 1", n)
	}
}

// TestSprintCommentAdd_BodyFromStdin verifies that an absent --body makes the whole
// of standard input the body, that interior line breaks survive, and that the
// leading/trailing whitespace is trimmed. This is the heredoc / pipe form.
func TestSprintCommentAdd_BodyFromStdin(t *testing.T) {
	const roadmap = "sprint-comment-add-body-stdin"
	database := setupSprintCommentRoadmap(t, roadmap)

	const piped = "\nThree of five tasks closed.\n\nThe migration task is blocked on the compliance review.\n\n"
	const want = "Three of five tasks closed.\n\nThe migration task is blocked on the compliance review."

	leftover := withStdin(t, piped, func() {
		_ = captureStdout(t, func() {
			if err := sprintCommentAdd([]string{"-r", roadmap, "1", "--type", "PROGRESS"}); err != nil {
				t.Fatalf("comment-add from stdin: %v", err)
			}
		})
	})
	if leftover != "" {
		t.Errorf("standard input was not read to EOF; %q left unread", leftover)
	}

	comments := listSprintComments(t, database, 1)
	if len(comments) != 1 {
		t.Fatalf("stored comments = %d, want 1", len(comments))
	}
	if comments[0].Body != want {
		t.Errorf("stored body\n got: %q\nwant: %q", comments[0].Body, want)
	}
	if comments[0].Type != models.CommentProgress {
		t.Errorf("stored type = %q, want PROGRESS", comments[0].Type)
	}
}

// TestSprintCommentAdd_FlagWinsOverStdin pins precedence rule 1: with both sources
// present the flag is the body and standard input is NOT read. The unread leftover
// is the proof — an implementation that read stdin first, or read it "just in
// case", would consume it.
func TestSprintCommentAdd_FlagWinsOverStdin(t *testing.T) {
	const roadmap = "sprint-comment-add-flag-beats-stdin"
	database := setupSprintCommentRoadmap(t, roadmap)

	const fromStdin = "THIS TEXT ARRIVES ON STANDARD INPUT AND MUST BE IGNORED"
	const fromFlag = "This text arrives on the flag and must win."

	leftover := withStdin(t, fromStdin, func() {
		_ = captureStdout(t, func() {
			if err := sprintCommentAdd([]string{"-r", roadmap, "1", "--type", "UPDATE", "--body", fromFlag}); err != nil {
				t.Fatalf("comment-add: %v", err)
			}
		})
	})

	if leftover != fromStdin {
		t.Errorf("standard input was consumed although --body was supplied; leftover = %q", leftover)
	}
	comments := listSprintComments(t, database, 1)
	if len(comments) != 1 || comments[0].Body != fromFlag {
		t.Fatalf("stored body = %q, want the flag value %q", comments[0].Body, fromFlag)
	}
}

// TestSprintCommentAdd_InlineBodyForms verifies the GNU-style inline spellings the
// shared flag parser accepts for every other flag: --body=<text> and -b=<text>.
func TestSprintCommentAdd_InlineBodyForms(t *testing.T) {
	const roadmap = "sprint-comment-add-inline-body"
	database := setupSprintCommentRoadmap(t, roadmap)

	cases := []struct{ name, arg, want string }{
		{"long form", "--body=Inline long form is accepted.", "Inline long form is accepted."},
		{"short form", "-b=Inline short form is accepted.", "Inline short form is accepted."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_ = captureStdout(t, func() {
				if err := sprintCommentAdd([]string{"-r", roadmap, "1", "--type=UPDATE", c.arg}); err != nil {
					t.Fatalf("comment-add %s: %v", c.arg, err)
				}
			})
		})
	}

	comments := listSprintComments(t, database, 1)
	if len(comments) != 2 {
		t.Fatalf("stored comments = %d, want 2", len(comments))
	}
	for i, want := range []string{cases[0].want, cases[1].want} {
		if comments[i].Body != want {
			t.Errorf("comment %d body = %q, want %q", i, comments[i].Body, want)
		}
	}
}

// TestSprintCommentAdd_NoBodySupplied covers every way a body can fail to arrive.
// All of them are exit code 2 with ONE pinned message, and none of them may
// surface the domain's own "body is required" verdict, which is exit code 6:
// models.ValidateCommentBody deliberately does not chain utils.ErrRequired, so the
// shared command layer owns the emptiness decision (SPEC/COMMANDS.md rules 3-4).
func TestSprintCommentAdd_NoBodySupplied(t *testing.T) {
	const roadmap = "sprint-comment-add-no-body"
	setupSprintCommentRoadmap(t, roadmap)

	const wantMsg = "required parameter missing: no comment body supplied"

	cases := []struct {
		name  string
		args  []string
		stdin string
	}{
		{"flag absent, stdin empty", []string{"-r", roadmap, "1", "--type", "UPDATE"}, ""},
		{"flag absent, stdin whitespace only", []string{"-r", roadmap, "1", "--type", "UPDATE"}, " \n\t\n "},
		{"flag present, value empty", []string{"-r", roadmap, "1", "--type", "UPDATE", "--body", ""}, "ignored"},
		{"flag present, value whitespace only", []string{"-r", roadmap, "1", "--type", "UPDATE", "--body", "   "}, "ignored"},
		{"flag present, no value token", []string{"-r", roadmap, "1", "--type", "UPDATE", "--body"}, "ignored"},
		{"flag present, next token is a flag", []string{"-r", roadmap, "1", "--body", "--type", "UPDATE"}, "ignored"},
		{"inline form with an empty value", []string{"-r", roadmap, "1", "--type", "UPDATE", "--body="}, "ignored"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var err error
			leftover := withStdin(t, c.stdin, func() { err = sprintCommentAdd(c.args) })

			if err == nil {
				t.Fatal("want an error, got nil")
			}
			if !errors.Is(err, utils.ErrRequired) {
				t.Errorf("error must wrap utils.ErrRequired (exit 2); got %[1]T: %[1]v", err)
			}
			if errors.Is(err, utils.ErrValidation) {
				t.Errorf("an absent body must NOT be reported as a validation error (exit 6); got %v", err)
			}
			if err.Error() != wantMsg {
				t.Errorf("message\n got: %q\nwant: %q", err.Error(), wantMsg)
			}
			if errors.Is(err, models.ErrCommentBodyRequired) {
				t.Errorf("the domain's own body-required verdict must stay unreachable from the CLI; got %v", err)
			}
			// A flag that is present but unusable must never fall back to stdin.
			if c.stdin == "ignored" && leftover != "ignored" {
				t.Errorf("standard input was read although --body was present; leftover = %q", leftover)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// comment-add: --type presence, the SPRINT subset, and the ORDER of the checks
// ---------------------------------------------------------------------------

// TestSprintCommentAdd_TypeRequired verifies that an absent --type is exit code 2
// with the pinned message, and — the part that matters — that it is decided before
// standard input is consulted.
func TestSprintCommentAdd_TypeRequired(t *testing.T) {
	const roadmap = "sprint-comment-add-type-required"
	setupSprintCommentRoadmap(t, roadmap)

	var err error
	leftover := withStdin(t, "a body that must never be read", func() {
		err = sprintCommentAdd([]string{"-r", roadmap, "1"})
	})

	if err == nil {
		t.Fatal("want an error for a missing --type, got nil")
	}
	if !errors.Is(err, utils.ErrRequired) {
		t.Errorf("error must wrap utils.ErrRequired (exit 2); got %v", err)
	}
	if want := "required parameter missing: --type"; err.Error() != want {
		t.Errorf("message\n got: %q\nwant: %q", err.Error(), want)
	}
	if leftover != "a body that must never be read" {
		t.Errorf("standard input was read before --type was validated; leftover = %q", leftover)
	}
}

// TestSprintCommentAdd_TaskOnlyTypesRefused is the difference between the two
// families, asserted directly: HYPOTHESIS, TEST and NOTE are accepted on a task
// comment and MUST be refused on a sprint comment with exit code 6, with a message
// naming the sprint's own four values (SPEC/COMMANDS.md § Sprint Comments).
//
// The valid set is rendered from models.FormatCommentTypes, so the assertion cannot
// drift from the enum, and the message is checked to NOT name the task-only values:
// telling a caller that HYPOTHESIS is invalid while listing it as valid would be
// worse than no message at all.
func TestSprintCommentAdd_TaskOnlyTypesRefused(t *testing.T) {
	const roadmap = "sprint-comment-add-task-only-types"
	database := setupSprintCommentRoadmap(t, roadmap)

	validSet := models.FormatCommentTypes(models.ValidSprintCommentTypes)
	taskOnly := []string{"HYPOTHESIS", "TEST", "NOTE"}

	for _, bad := range taskOnly {
		t.Run("type="+bad, func(t *testing.T) {
			// The same value must be accepted by the task family, or this test is
			// asserting nothing about the SUBSET.
			if !models.IsValidTaskCommentType(bad) {
				t.Fatalf("test premise broken: %q is not a task comment type either", bad)
			}

			var err error
			leftover := withStdin(t, "unread body", func() {
				err = sprintCommentAdd([]string{"-r", roadmap, "1", "--type", bad})
			})

			if err == nil {
				t.Fatalf("want an error for --type %q, got nil", bad)
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("error must wrap utils.ErrValidation (exit 6); got %v", err)
			}
			want := `invalid comment type "` + bad + `" for a sprint comment; valid types: ` + validSet
			if got := err.Error(); !strings.HasSuffix(got, want) {
				t.Errorf("message must name the sprint's accepted set\n got: %q\nwant suffix: %q", got, want)
			}
			for _, forbidden := range taskOnly {
				if strings.Contains(err.Error(), "valid types: ") &&
					strings.Contains(strings.SplitN(err.Error(), "valid types: ", 2)[1], forbidden) {
					t.Errorf("the valid-type list names the task-only value %q: %q", forbidden, err.Error())
				}
			}
			// The whole point of validating the type first: no stdin read.
			if leftover != "unread body" {
				t.Errorf("standard input was read although --type was invalid; leftover = %q", leftover)
			}
		})
	}

	if n := len(listSprintComments(t, database, 1)); n != 0 {
		t.Errorf("a refused type stored %d comments, want 0", n)
	}
}

// TestSprintCommentAdd_TypeOutsideSprintSet covers the values that belong to no
// comment enum at all, plus a SprintStatus value: `-y, --type` has exactly one
// meaning in this family, so nothing else may sneak through it.
func TestSprintCommentAdd_TypeOutsideSprintSet(t *testing.T) {
	const roadmap = "sprint-comment-add-type-invalid"
	setupSprintCommentRoadmap(t, roadmap)

	validSet := models.FormatCommentTypes(models.ValidSprintCommentTypes)

	for _, bad := range []string{"BUG", "OPEN", "CLOSED", "decision", "", "WHATEVER"} {
		t.Run("type="+bad, func(t *testing.T) {
			var err error
			leftover := withStdin(t, "unread body", func() {
				err = sprintCommentAdd([]string{"-r", roadmap, "1", "--type", bad})
			})

			if err == nil {
				t.Fatalf("want an error for --type %q, got nil", bad)
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("error must wrap utils.ErrValidation (exit 6); got %v", err)
			}
			want := `invalid comment type "` + bad + `" for a sprint comment; valid types: ` + validSet
			if got := err.Error(); !strings.HasSuffix(got, want) {
				t.Errorf("message must name the accepted set\n got: %q\nwant suffix: %q", got, want)
			}
			if leftover != "unread body" {
				t.Errorf("standard input was read although --type was invalid; leftover = %q", leftover)
			}
		})
	}
}

// TestSprintCommentList_TaskOnlyTypeFilterRefused pins that the subset governs the
// FILTER too: a task-only value is exit code 6, not an empty array. A caller must
// be told the filter is meaningless here rather than concluding the sprint has no
// such comments.
func TestSprintCommentList_TaskOnlyTypeFilterRefused(t *testing.T) {
	const roadmap = "sprint-comment-list-task-only-filter"
	setupSprintCommentRoadmap(t, roadmap)

	addSprintComment(t, roadmap, 1, "FINDING", "The compliance review is the sprint's real critical path.")

	for _, bad := range []string{"HYPOTHESIS", "TEST", "NOTE"} {
		err := sprintCommentList([]string{"-r", roadmap, "1", "--type", bad})
		if err == nil {
			t.Fatalf("--type %q must be refused as a filter, got nil", bad)
		}
		if !errors.Is(err, utils.ErrValidation) {
			t.Errorf("--type %q: error must wrap utils.ErrValidation (exit 6); got %v", bad, err)
		}
		if want := "for a sprint comment"; !strings.Contains(err.Error(), want) {
			t.Errorf("--type %q: message must name the sprint entity; got %q", bad, err.Error())
		}
	}
}

// TestSprintCommentEdit_TaskOnlyTypeRefused pins the subset on comment-edit, and
// pins that the refusal happens before the row is touched.
func TestSprintCommentEdit_TaskOnlyTypeRefused(t *testing.T) {
	const roadmap = "sprint-comment-edit-task-only-type"
	database := setupSprintCommentRoadmap(t, roadmap)

	addSprintComment(t, roadmap, 1, "FINDING", "The compliance review gates two of the five tasks.")
	original := listSprintComments(t, database, 1)[0]

	for _, bad := range []string{"HYPOTHESIS", "TEST", "NOTE"} {
		err := sprintCommentEdit([]string{"-r", roadmap, itoa(original.ID), "--type", bad})
		if !errors.Is(err, utils.ErrValidation) {
			t.Errorf("--type %q: error must wrap utils.ErrValidation (exit 6); got %v", bad, err)
		}
	}

	after := getSprintComment(t, database, original.ID)
	if after.Type != original.Type {
		t.Errorf("a refused type changed the row: %q -> %q", original.Type, after.Type)
	}
	if after.UpdatedAt != nil {
		t.Error("a refused edit must not stamp updated_at")
	}
	if n := countSprintAudit(t, database, models.OpSprintCommentUpdate, 1); n != 0 {
		t.Errorf("a refused edit wrote %d audit entries, want 0", n)
	}
}

// TestSprintCommentAdd_InvalidTypeDoesNotBlockOnStandardInput is the regression
// guard for the validation ORDER as a runtime property rather than a message.
//
// Standard input is a pipe whose write end stays open and carries nothing, so any
// read of it blocks until this test closes that end. A handler that resolved the
// body before validating the type would therefore hang; the pinned order requires
// it to return the type verdict immediately. The type used is HYPOTHESIS, which is
// a perfectly good task comment type: on a sprint it must fail fast just the same.
func TestSprintCommentAdd_InvalidTypeDoesNotBlockOnStandardInput(t *testing.T) {
	const roadmap = "sprint-comment-add-no-block-on-stdin"
	setupSprintCommentRoadmap(t, roadmap)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	restore := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = restore
		_ = w.Close() // releases any read still blocked on the pipe
		_ = r.Close()
	})

	subcommands := map[string]func([]string) error{
		"comment-add":  sprintCommentAdd,
		"comment-edit": sprintCommentEdit,
	}
	for name, handler := range subcommands {
		t.Run(name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() { done <- handler([]string{"-r", roadmap, "1", "--type", "HYPOTHESIS"}) }()

			select {
			case err := <-done:
				if !errors.Is(err, utils.ErrValidation) {
					t.Errorf("want the exit-6 type verdict, got %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("%s blocked reading standard input instead of rejecting --type first", name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// comment-add: body validation (length, control characters) and the parent sprint
// ---------------------------------------------------------------------------

// TestSprintCommentAdd_BodyLengthBoundary pins the cap at exactly
// models.MaxCommentBody CHARACTERS: 4096 is accepted, 4097 is exit code 6, and a
// 4096-rune multi-byte body is accepted even though it is more bytes than that —
// the unit is characters, matching the schema's length() CHECK.
func TestSprintCommentAdd_BodyLengthBoundary(t *testing.T) {
	const roadmap = "sprint-comment-add-body-length"
	database := setupSprintCommentRoadmap(t, roadmap)

	atCap := strings.Repeat("a", models.MaxCommentBody)
	overCap := strings.Repeat("a", models.MaxCommentBody+1)
	multiByteAtCap := strings.Repeat("ç", models.MaxCommentBody)

	_ = captureStdout(t, func() {
		if err := sprintCommentAdd([]string{"-r", roadmap, "1", "--type", "UPDATE", "--body", atCap}); err != nil {
			t.Fatalf("a %d-character body must be accepted: %v", models.MaxCommentBody, err)
		}
	})
	_ = captureStdout(t, func() {
		if err := sprintCommentAdd([]string{"-r", roadmap, "1", "--type", "UPDATE", "--body", multiByteAtCap}); err != nil {
			t.Fatalf("a %d-rune multi-byte body must be accepted (characters, not bytes): %v", models.MaxCommentBody, err)
		}
	})

	err := sprintCommentAdd([]string{"-r", roadmap, "1", "--type", "UPDATE", "--body", overCap})
	if err == nil {
		t.Fatalf("a %d-character body must be rejected", models.MaxCommentBody+1)
	}
	if !errors.Is(err, utils.ErrFieldTooLarge) {
		t.Errorf("error must wrap utils.ErrFieldTooLarge (exit 6); got %v", err)
	}
	if want := "body exceeds maximum length of 4096 characters"; !strings.Contains(err.Error(), want) {
		t.Errorf("message\n got: %q\nwant substring: %q", err.Error(), want)
	}

	if n := len(listSprintComments(t, database, 1)); n != 2 {
		t.Errorf("stored comments = %d, want 2 (the oversize body must not be stored)", n)
	}
}

// TestSprintCommentAdd_ControlCharactersRejected pins the control-character rule
// against the body AS SUPPLIED.
//
// The leading VT and FF cases are the regression guard: strings.TrimSpace strips
// both, so a handler that trimmed before validating would silently drop a forbidden
// character instead of rejecting the input that carried it (CWE-150 / Trojan
// Source). TAB, LF and CR are permitted and must still pass. Every forbidden code
// point is written as a Go escape sequence, never as a literal character.
func TestSprintCommentAdd_ControlCharactersRejected(t *testing.T) {
	const roadmap = "sprint-comment-add-control-chars"
	database := setupSprintCommentRoadmap(t, roadmap)

	rejected := map[string]string{
		"leading vertical tab":  "\vsprint log entry",
		"trailing form feed":    "sprint log entry\f",
		"interior vertical tab": "sprint\vlog entry",
		"escape sequence":       "sprint \x1b[31mlog entry",
		"nul byte":              "sprint\x00log entry",
		"delete":                "sprint\x7flog entry",
		"bidi override":         "sprint \u202e log entry",
		"zero-width no-break":   "sprint \ufeff log entry",
	}
	for name, body := range rejected {
		t.Run("rejects "+name, func(t *testing.T) {
			err := sprintCommentAdd([]string{"-r", roadmap, "1", "--type", "UPDATE", "--body", body})
			if err == nil {
				t.Fatalf("body %q must be rejected", body)
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("error must wrap utils.ErrValidation (exit 6); got %v", err)
			}
			if want := "body: control characters are not allowed"; !strings.Contains(err.Error(), want) {
				t.Errorf("message\n got: %q\nwant substring: %q", err.Error(), want)
			}
		})
	}

	const allowed = "first line\n\tindented second line\r\nthird line"
	_ = captureStdout(t, func() {
		if err := sprintCommentAdd([]string{"-r", roadmap, "1", "--type", "UPDATE", "--body", allowed}); err != nil {
			t.Fatalf("TAB, LF and CR are permitted: %v", err)
		}
	})

	comments := listSprintComments(t, database, 1)
	if len(comments) != 1 {
		t.Fatalf("stored comments = %d, want 1 (only the permitted body)", len(comments))
	}
	if comments[0].Body != allowed {
		t.Errorf("stored body\n got: %q\nwant: %q", comments[0].Body, allowed)
	}
}

// TestSprintCommentAdd_UnknownSprint verifies the exit-4 condition and its message,
// and the step order around it: an unknown sprint with an oversize body reports the
// sprint, not the body.
func TestSprintCommentAdd_UnknownSprint(t *testing.T) {
	const roadmap = "sprint-comment-add-unknown-sprint"
	database := setupSprintCommentRoadmap(t, roadmap)

	err := sprintCommentAdd([]string{"-r", roadmap, "4242", "--type", "UPDATE", "--body", "text"})
	if err == nil {
		t.Fatal("want an error for an unknown sprint, got nil")
	}
	if !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("error must wrap utils.ErrNotFound (exit 4); got %v", err)
	}
	if want := "resource not found: sprint 4242 not found"; err.Error() != want {
		t.Errorf("message\n got: %q\nwant: %q", err.Error(), want)
	}

	// Step 6 precedes step 7: the sprint is reported, not the oversize body.
	err = sprintCommentAdd([]string{
		"-r", roadmap, "4242", "--type", "UPDATE",
		"--body", strings.Repeat("a", models.MaxCommentBody+1),
	})
	if !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("an unknown sprint must be reported before the body is validated; got %v", err)
	}

	if n := countSprintAudit(t, database, models.OpSprintCommentCreate, 4242); n != 0 {
		t.Errorf("a failed comment-add wrote %d audit entries, want 0", n)
	}
}

// TestSprintComment_PositionalIDClassification pins the exit-code class of every
// malformed positional id on all four subcommands, and the entity each names:
// "sprint" on comment-add / comment-list, "comment" on comment-edit /
// comment-remove.
//
// SPEC/COMMANDS.md classifies the whole "positive integer" constraint as exit code
// 2 (invalid input) for these subcommands, including a non-positive or oversized
// value that utils.ValidateIDString would otherwise report as a validation error
// (exit code 6). The assertion is deliberately two-sided: it requires
// ErrInvalidInput AND rejects ErrValidation.
func TestSprintComment_PositionalIDClassification(t *testing.T) {
	const roadmap = "sprint-comment-positional-ids"
	setupSprintCommentRoadmap(t, roadmap)

	handlers := map[string]struct {
		run    func([]string) error
		entity string
		extra  []string
	}{
		"comment-add":    {sprintCommentAdd, "sprint", []string{"--type", "UPDATE", "--body", "text"}},
		"comment-list":   {sprintCommentList, "sprint", nil},
		"comment-edit":   {sprintCommentEdit, "comment", []string{"--type", "UPDATE"}},
		"comment-remove": {sprintCommentRemove, "comment", nil},
	}

	for name, h := range handlers {
		t.Run(name, func(t *testing.T) {
			for _, bad := range []string{"abc", "1.5", "0", "-7", "2147483648", "99999999999999999999"} {
				args := append([]string{"-r", roadmap, bad}, h.extra...)
				err := h.run(args)
				if err == nil {
					t.Errorf("id %q: want an error, got nil", bad)
					continue
				}
				if !errors.Is(err, utils.ErrInvalidInput) {
					t.Errorf("id %q: error must wrap utils.ErrInvalidInput (exit 2); got %v", bad, err)
				}
				if errors.Is(err, utils.ErrValidation) {
					t.Errorf("id %q: a malformed id is exit 2 here, never exit 6; got %v", bad, err)
				}
				if want := "invalid " + h.entity + " ID"; !strings.Contains(err.Error(), want) {
					t.Errorf("id %q: message must name the %s id\n got: %q", bad, h.entity, err.Error())
				}
			}

			// A wholly absent id is a missing parameter, with its own message.
			err := h.run([]string{"-r", roadmap})
			if !errors.Is(err, utils.ErrRequired) {
				t.Errorf("missing id: error must wrap utils.ErrRequired (exit 2); got %v", err)
			}
			if want := h.entity + " ID required"; err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("missing id: message\n got: %v\nwant substring: %q", err, want)
			}
		})
	}
}

// TestSprintComment_MissingRoadmap verifies that all four subcommands report a
// missing -r as exit code 3, before anything else is parsed.
func TestSprintComment_MissingRoadmap(t *testing.T) {
	handlers := map[string]func([]string) error{
		"comment-add":    sprintCommentAdd,
		"comment-list":   sprintCommentList,
		"comment-edit":   sprintCommentEdit,
		"comment-remove": sprintCommentRemove,
	}
	for name, run := range handlers {
		t.Run(name, func(t *testing.T) {
			err := run([]string{"1", "--type", "UPDATE", "--body", "text"})
			if err == nil {
				t.Fatal("want an error for a missing -r, got nil")
			}
			if !errors.Is(err, utils.ErrNoRoadmap) {
				t.Errorf("error must wrap utils.ErrNoRoadmap (exit 3); got %v", err)
			}
		})
	}
}

// TestSprintComment_UnknownFlagRejected verifies that an unrecognised flag is exit
// code 2 on every subcommand, including comment-remove, which accepts no flag of
// its own — so `--type` is unknown there.
func TestSprintComment_UnknownFlagRejected(t *testing.T) {
	const roadmap = "sprint-comment-unknown-flag"
	setupSprintCommentRoadmap(t, roadmap)

	cases := map[string]struct {
		run  func([]string) error
		args []string
	}{
		"comment-add":                    {sprintCommentAdd, []string{"-r", roadmap, "1", "--type", "UPDATE", "--body", "text", "--foo"}},
		"comment-list":                   {sprintCommentList, []string{"-r", roadmap, "1", "--foo"}},
		"comment-edit":                   {sprintCommentEdit, []string{"-r", roadmap, "1", "--type", "UPDATE", "--foo"}},
		"comment-remove":                 {sprintCommentRemove, []string{"-r", roadmap, "1", "--foo"}},
		"comment-remove takes no --type": {sprintCommentRemove, []string{"-r", roadmap, "1", "--type", "UPDATE"}},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := c.run(c.args)
			if err == nil {
				t.Fatal("want an error for an unknown flag, got nil")
			}
			if !errors.Is(err, utils.ErrInvalidInput) {
				t.Errorf("error must wrap utils.ErrInvalidInput (exit 2); got %v", err)
			}
			if !strings.Contains(err.Error(), "unknown flag") {
				t.Errorf("message must name the unknown flag; got %q", err.Error())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// comment-list
// ---------------------------------------------------------------------------

// TestSprintCommentList_OldestFirstAndTypeFilter verifies the ordering contract and
// the filter, on the printed JSON rather than on the query, and verifies that a
// sprint with nothing recorded prints an empty array rather than null.
func TestSprintCommentList_OldestFirstAndTypeFilter(t *testing.T) {
	const roadmap = "sprint-comment-list-order"
	database := setupSprintCommentRoadmap(t, roadmap)

	addSprintComment(t, roadmap, 1, "FINDING", "First: the compliance review gates two of the five tasks.")
	addSprintComment(t, roadmap, 1, "DECISION", "Second: the migration moves to the next sprint.")
	addSprintComment(t, roadmap, 1, "FINDING", "Third: the review board meets fortnightly, not weekly.")
	addSprintComment(t, roadmap, 2, "PROGRESS", "A comment on the other sprint, which must never appear.")

	out := captureStdout(t, func() {
		if err := sprintCommentList([]string{"-r", roadmap, "1"}); err != nil {
			t.Fatalf("comment-list: %v", err)
		}
	})

	// Ordering is asserted on the printed document: the bodies must appear in the
	// order the work happened.
	first := strings.Index(out, "First:")
	second := strings.Index(out, "Second:")
	third := strings.Index(out, "Third:")
	if first < 0 || second < 0 || third < 0 {
		t.Fatalf("comment-list did not print all three comments:\n%s", out)
	}
	if first > second || second > third {
		t.Errorf("comments are not oldest first (offsets %d, %d, %d):\n%s", first, second, third, out)
	}
	if strings.Contains(out, "other sprint") {
		t.Errorf("comment-list leaked another sprint's comments:\n%s", out)
	}
	if !strings.Contains(out, `"sprint_id": 1`) {
		t.Errorf("the printed objects must carry sprint_id, not task_id:\n%s", out)
	}

	// The stored order must match, so the contract does not depend on the printer.
	stored := listSprintComments(t, database, 1)
	if len(stored) != 3 {
		t.Fatalf("stored comments = %d, want 3", len(stored))
	}
	for i := 1; i < len(stored); i++ {
		if stored[i-1].CreatedAt > stored[i].CreatedAt {
			t.Errorf("stored order is not created_at ascending: %q then %q",
				stored[i-1].CreatedAt, stored[i].CreatedAt)
		}
		if stored[i-1].ID > stored[i].ID {
			t.Errorf("ties must break on the id ascending: %d then %d", stored[i-1].ID, stored[i].ID)
		}
	}

	filtered := captureStdout(t, func() {
		if err := sprintCommentList([]string{"-r", roadmap, "1", "--type", "FINDING"}); err != nil {
			t.Fatalf("comment-list --type FINDING: %v", err)
		}
	})
	if strings.Contains(filtered, "Second:") {
		t.Errorf("--type FINDING returned a DECISION comment:\n%s", filtered)
	}
	if !strings.Contains(filtered, "First:") || !strings.Contains(filtered, "Third:") {
		t.Errorf("--type FINDING dropped a matching comment:\n%s", filtered)
	}

	empty := captureStdout(t, func() {
		if err := sprintCommentList([]string{"-r", roadmap, "1", "--type", "UPDATE"}); err != nil {
			t.Fatalf("comment-list --type UPDATE: %v", err)
		}
	})
	if strings.TrimSpace(empty) != "[]" {
		t.Errorf("a filter matching nothing must print an empty array, got %q", empty)
	}
}

// TestSprintCommentList_EmptyLogAndUnknownSprint verifies that a sprint with no
// comments prints [] with no error, while an unknown sprint is exit code 4.
func TestSprintCommentList_EmptyLogAndUnknownSprint(t *testing.T) {
	const roadmap = "sprint-comment-list-empty"
	setupSprintCommentRoadmap(t, roadmap)

	out := captureStdout(t, func() {
		if err := sprintCommentList([]string{"-r", roadmap, "2"}); err != nil {
			t.Fatalf("comment-list on a sprint with no comments: %v", err)
		}
	})
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("want an empty array for a sprint with no comments, got %q", out)
	}

	err := sprintCommentList([]string{"-r", roadmap, "4242"})
	if !errors.Is(err, utils.ErrNotFound) {
		t.Fatalf("unknown sprint: error must wrap utils.ErrNotFound (exit 4); got %v", err)
	}
	if want := "resource not found: sprint 4242 not found"; err.Error() != want {
		t.Errorf("message\n got: %q\nwant: %q", err.Error(), want)
	}
}

// TestSprintCommentList_WritesNoAuditEntry pins that listing is a read: no audit
// row of any comment operation appears after it.
func TestSprintCommentList_WritesNoAuditEntry(t *testing.T) {
	const roadmap = "sprint-comment-list-no-audit"
	database := setupSprintCommentRoadmap(t, roadmap)

	addSprintComment(t, roadmap, 1, "PROGRESS", "A comment to have something to list.")
	before := countSprintAudit(t, database, models.OpSprintCommentCreate, 1)

	_ = captureStdout(t, func() {
		if err := sprintCommentList([]string{"-r", roadmap, "1"}); err != nil {
			t.Fatalf("comment-list: %v", err)
		}
	})

	for _, op := range []models.AuditOperation{
		models.OpSprintCommentCreate, models.OpSprintCommentUpdate, models.OpSprintCommentDelete,
	} {
		want := 0
		if op == models.OpSprintCommentCreate {
			want = before
		}
		if got := countSprintAudit(t, database, op, 1); got != want {
			t.Errorf("comment-list changed the %s audit count: got %d, want %d", op, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// comment-edit
// ---------------------------------------------------------------------------

// TestSprintCommentEdit_TypeOnly verifies a type-only edit: the type changes, the
// body and created_at do not, updated_at is stamped, the audit entry is written
// against the parent sprint — and standard input is NOT read, so a type-only edit
// never waits for input (SPEC precedence rule 2).
func TestSprintCommentEdit_TypeOnly(t *testing.T) {
	const roadmap = "sprint-comment-edit-type-only"
	database := setupSprintCommentRoadmap(t, roadmap)

	addSprintComment(t, roadmap, 2, "PROGRESS", "Two of five tasks closed; the migration is still blocked.")
	before := listSprintComments(t, database, 2)[0]

	const untouched = "A BODY ON STANDARD INPUT THAT A TYPE-ONLY EDIT MUST NOT READ"
	var err error
	leftover := withStdin(t, untouched, func() {
		err = sprintCommentEdit([]string{"-r", roadmap, itoa(before.ID), "--type", "UPDATE"})
	})
	if err != nil {
		t.Fatalf("comment-edit --type: %v", err)
	}
	if leftover != untouched {
		t.Errorf("standard input was read although --type was present; leftover = %q", leftover)
	}

	after := getSprintComment(t, database, before.ID)
	if after.Type != models.CommentUpdate {
		t.Errorf("type = %q, want UPDATE", after.Type)
	}
	if after.Body != before.Body {
		t.Errorf("a type-only edit changed the body\n got: %q\nwant: %q", after.Body, before.Body)
	}
	if after.CreatedAt != before.CreatedAt {
		t.Errorf("created_at was rewritten: %q -> %q", before.CreatedAt, after.CreatedAt)
	}
	if after.UpdatedAt == nil {
		t.Error("updated_at must be stamped by an edit")
	}
	if n := countSprintAudit(t, database, models.OpSprintCommentUpdate, 2); n != 1 {
		t.Errorf("SPRINT_COMMENT_UPDATE audit entries against sprint 2 = %d, want 1", n)
	}
}

// TestSprintCommentEdit_BodyFromStdinIsAValidEdit verifies the flagless form: with
// neither --type nor --body, the body on standard input IS the change, so the
// command is an edit and not a no-op.
func TestSprintCommentEdit_BodyFromStdinIsAValidEdit(t *testing.T) {
	const roadmap = "sprint-comment-edit-stdin-body"
	database := setupSprintCommentRoadmap(t, roadmap)

	addSprintComment(t, roadmap, 1, "FINDING", "The original finding text.")
	original := listSprintComments(t, database, 1)[0]

	const revised = "The revised finding: the review board meets fortnightly, so the migration cannot land in this sprint."
	var err error
	leftover := withStdin(t, "\n"+revised+"\n", func() {
		err = sprintCommentEdit([]string{"-r", roadmap, itoa(original.ID)})
	})
	if err != nil {
		t.Fatalf("flagless comment-edit reading stdin: %v", err)
	}
	if leftover != "" {
		t.Errorf("standard input must be read to EOF; %q left unread", leftover)
	}

	after := getSprintComment(t, database, original.ID)
	if after.Body != revised {
		t.Errorf("body\n got: %q\nwant: %q", after.Body, revised)
	}
	if after.Type != original.Type {
		t.Errorf("a body-only edit changed the type: %q -> %q", original.Type, after.Type)
	}
	if after.UpdatedAt == nil {
		t.Error("updated_at must be stamped by an edit")
	}
}

// TestSprintCommentEdit_TypeAndBody verifies that both columns change in one call.
func TestSprintCommentEdit_TypeAndBody(t *testing.T) {
	const roadmap = "sprint-comment-edit-type-and-body"
	database := setupSprintCommentRoadmap(t, roadmap)

	addSprintComment(t, roadmap, 1, "PROGRESS", "Two of five tasks closed.")
	original := listSprintComments(t, database, 1)[0]

	const body = "Sprint goal widened to include the regression test the fix needs to stay fixed."
	if err := sprintCommentEdit([]string{
		"-r", roadmap, itoa(original.ID), "--type", "UPDATE", "--body", body,
	}); err != nil {
		t.Fatalf("comment-edit --type --body: %v", err)
	}

	after := getSprintComment(t, database, original.ID)
	if after.Type != models.CommentUpdate || after.Body != body {
		t.Errorf("after the edit: type = %q, body = %q; want UPDATE and %q", after.Type, after.Body, body)
	}
}

// TestSprintCommentEdit_NoChangeRequested verifies the one case that requests
// nothing at all — no --type, no --body, nothing usable on standard input — and its
// own pinned message, which differs from the comment-add wording because the two
// subcommands are missing different things.
func TestSprintCommentEdit_NoChangeRequested(t *testing.T) {
	const roadmap = "sprint-comment-edit-no-change"
	database := setupSprintCommentRoadmap(t, roadmap)

	addSprintComment(t, roadmap, 1, "UPDATE", "Unchanged by the calls below.")
	original := listSprintComments(t, database, 1)[0]

	const wantMsg = "required parameter missing: at least one of --type or --body is required"
	for _, stdin := range []string{"", "   \n\t"} {
		var err error
		withStdin(t, stdin, func() {
			err = sprintCommentEdit([]string{"-r", roadmap, itoa(original.ID)})
		})
		if err == nil {
			t.Fatalf("stdin %q: want an error, got nil", stdin)
		}
		if !errors.Is(err, utils.ErrRequired) {
			t.Errorf("stdin %q: error must wrap utils.ErrRequired (exit 2); got %v", stdin, err)
		}
		if err.Error() != wantMsg {
			t.Errorf("stdin %q: message\n got: %q\nwant: %q", stdin, err.Error(), wantMsg)
		}
	}

	after := getSprintComment(t, database, original.ID)
	if after.UpdatedAt != nil {
		t.Error("a rejected edit must not stamp updated_at")
	}
	if n := countSprintAudit(t, database, models.OpSprintCommentUpdate, 1); n != 0 {
		t.Errorf("a rejected edit wrote %d audit entries, want 0", n)
	}
}

// TestSprintCommentEdit_UnknownComment verifies the exit-4 condition and the
// message, which names the SPRINT comment: the two tables report their own rows.
func TestSprintCommentEdit_UnknownComment(t *testing.T) {
	const roadmap = "sprint-comment-edit-unknown-id"
	setupSprintCommentRoadmap(t, roadmap)

	err := sprintCommentEdit([]string{"-r", roadmap, "9999", "--type", "UPDATE"})
	if !errors.Is(err, utils.ErrNotFound) {
		t.Fatalf("error must wrap utils.ErrNotFound (exit 4); got %v", err)
	}
	if want := "resource not found: sprint comment 9999 not found"; err.Error() != want {
		t.Errorf("message\n got: %q\nwant: %q", err.Error(), want)
	}

	err = sprintCommentRemove([]string{"-r", roadmap, "9999"})
	if !errors.Is(err, utils.ErrNotFound) {
		t.Fatalf("comment-remove: error must wrap utils.ErrNotFound (exit 4); got %v", err)
	}
	if want := "resource not found: sprint comment 9999 not found"; err.Error() != want {
		t.Errorf("comment-remove message\n got: %q\nwant: %q", err.Error(), want)
	}
}

// ---------------------------------------------------------------------------
// The two comment id spaces are independent
// ---------------------------------------------------------------------------

// TestSprintComment_IDSpaceIsPerTable pins that a comment id is resolved against
// the family's OWN table and never the other one, in both directions.
//
// Two cases, and the second is the one that matters. First: an id that exists in
// task_comments and NOT in sprint_comments is a not-found condition here, and the
// task comment survives untouched. Second: the SAME id existing in both tables
// addresses two unrelated rows — editing it through the sprint family must change
// the sprint comment and leave the task comment exactly as it was.
func TestSprintComment_IDSpaceIsPerTable(t *testing.T) {
	const roadmap = "sprint-comment-id-space"
	database := setupSprintCommentRoadmap(t, roadmap)

	taskID, taskCommentID := seedTaskComment(t, roadmap)

	// Case 1: nothing has been written to sprint_comments yet, so the task
	// comment's id is unknown here.
	if _, err := database.GetSprintComment(context.Background(), taskCommentID); err == nil {
		t.Fatalf("test premise broken: sprint comment %d exists, so the id spaces cannot be told apart",
			taskCommentID)
	}
	if err := sprintCommentEdit([]string{
		"-r", roadmap, itoa(taskCommentID), "--type", "UPDATE",
	}); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("a task comment id must be not-found in the sprint family; got %v", err)
	}
	if err := sprintCommentRemove([]string{"-r", roadmap, itoa(taskCommentID)}); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("comment-remove must not reach task_comments; got %v", err)
	}

	surviving, err := database.GetTaskComment(context.Background(), taskCommentID)
	if err != nil {
		t.Fatalf("the task comment was affected by a sprint-family call: %v", err)
	}
	if surviving.TaskID != taskID || surviving.UpdatedAt != nil {
		t.Errorf("task comment altered: task_id = %d, updated_at = %v", surviving.TaskID, surviving.UpdatedAt)
	}

	// Case 2: the same id in both tables. Both sequences start at 1, so one comment
	// on each side is enough to collide.
	addSprintComment(t, roadmap, 1, "DECISION", "The sprint-side row that shares an id with a task comment.")
	sprintComments := listSprintComments(t, database, 1)
	if len(sprintComments) != 1 || sprintComments[0].ID != taskCommentID {
		t.Fatalf("fixture: want one sprint comment with id %d, got %+v", taskCommentID, sprintComments)
	}
	shared := sprintComments[0]

	const revised = "Reclassified through the sprint family only."
	if err := sprintCommentEdit([]string{
		"-r", roadmap, itoa(shared.ID), "--type", "UPDATE", "--body", revised,
	}); err != nil {
		t.Fatalf("comment-edit on the shared id: %v", err)
	}

	afterSprint := getSprintComment(t, database, shared.ID)
	if afterSprint.Type != models.CommentUpdate || afterSprint.Body != revised {
		t.Errorf("the sprint comment was not edited: type = %q, body = %q", afterSprint.Type, afterSprint.Body)
	}

	afterTask, err := database.GetTaskComment(context.Background(), taskCommentID)
	if err != nil {
		t.Fatalf("reading the task comment back: %v", err)
	}
	if afterTask.Type != models.CommentHypothesis {
		t.Errorf("the task comment's type changed to %q; the two ids are unrelated rows", afterTask.Type)
	}
	if afterTask.Body == revised {
		t.Error("the sprint edit overwrote the task comment's body")
	}
	if afterTask.UpdatedAt != nil {
		t.Error("the sprint edit stamped updated_at on the task comment")
	}

	// The audit trail followed the sprint, not the task.
	if n := countSprintAudit(t, database, models.OpSprintCommentUpdate, 1); n != 1 {
		t.Errorf("SPRINT_COMMENT_UPDATE against sprint 1 = %d, want 1", n)
	}
	if n := countCommentAudit(t, database, models.OpTaskCommentUpdate, models.EntityTask, taskID); n != 0 {
		t.Errorf("the sprint edit wrote %d TASK_COMMENT_UPDATE entries, want 0", n)
	}

	// Removing through the sprint family leaves the task comment in place.
	if err := sprintCommentRemove([]string{"-r", roadmap, itoa(shared.ID)}); err != nil {
		t.Fatalf("comment-remove on the shared id: %v", err)
	}
	if _, err := database.GetTaskComment(context.Background(), taskCommentID); err != nil {
		t.Errorf("the task comment was removed with the sprint comment: %v", err)
	}
}

// ---------------------------------------------------------------------------
// comment-remove
// ---------------------------------------------------------------------------

// TestSprintCommentRemove_DeletesRowAndKeepsAudit verifies that the row goes, the
// siblings stay, the delete is recorded against the parent sprint, and the audit
// entries of the removed comment outlive it.
func TestSprintCommentRemove_DeletesRowAndKeepsAudit(t *testing.T) {
	const roadmap = "sprint-comment-remove-row"
	database := setupSprintCommentRoadmap(t, roadmap)

	addSprintComment(t, roadmap, 2, "FINDING", "The comment that will be removed.")
	addSprintComment(t, roadmap, 2, "DECISION", "The comment that must survive.")
	comments := listSprintComments(t, database, 2)
	if len(comments) != 2 {
		t.Fatalf("fixture: stored comments = %d, want 2", len(comments))
	}
	doomed, surviving := comments[0], comments[1]

	if err := sprintCommentRemove([]string{"-r", roadmap, itoa(doomed.ID)}); err != nil {
		t.Fatalf("comment-remove: %v", err)
	}

	left := listSprintComments(t, database, 2)
	if len(left) != 1 || left[0].ID != surviving.ID {
		t.Fatalf("after the remove: %d comments left, want only comment %d", len(left), surviving.ID)
	}
	if _, err := database.GetSprintComment(context.Background(), doomed.ID); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("the removed comment is still readable: %v", err)
	}
	if n := countSprintAudit(t, database, models.OpSprintCommentDelete, 2); n != 1 {
		t.Errorf("SPRINT_COMMENT_DELETE audit entries against sprint 2 = %d, want 1", n)
	}
	// The audit trail of the removed comment outlives the row.
	if n := countSprintAudit(t, database, models.OpSprintCommentCreate, 2); n != 2 {
		t.Errorf("SPRINT_COMMENT_CREATE audit entries = %d, want 2 (the delete must not erase history)", n)
	}

	// Removing it a second time is a not-found condition, not a silent success.
	if err := sprintCommentRemove([]string{"-r", roadmap, itoa(doomed.ID)}); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("a second remove must be exit 4; got %v", err)
	}
}

// TestSprintCommentRemove_TakesExactlyOneID verifies that a comma-separated list is
// not a valid id here: comment-remove is a single-id command, so the batch form
// accepted by `task remove` is rejected as a malformed id.
func TestSprintCommentRemove_TakesExactlyOneID(t *testing.T) {
	const roadmap = "sprint-comment-remove-single-id"
	database := setupSprintCommentRoadmap(t, roadmap)

	addSprintComment(t, roadmap, 1, "UPDATE", "First comment, must survive.")
	addSprintComment(t, roadmap, 1, "UPDATE", "Second comment, must survive.")
	comments := listSprintComments(t, database, 1)

	err := sprintCommentRemove([]string{"-r", roadmap, itoa(comments[0].ID) + "," + itoa(comments[1].ID)})
	if err == nil {
		t.Fatal("a comma-separated id list must be rejected")
	}
	if !errors.Is(err, utils.ErrInvalidInput) {
		t.Errorf("error must wrap utils.ErrInvalidInput (exit 2); got %v", err)
	}
	if n := len(listSprintComments(t, database, 1)); n != 2 {
		t.Errorf("comments left = %d, want 2 (nothing may be deleted)", n)
	}
}

// ---------------------------------------------------------------------------
// Audit: always the parent sprint, never the comment
// ---------------------------------------------------------------------------

// TestSprintComment_AuditTargetsTheParentSprint pins the audit contract for all
// three mutations at once, using a fixture where the comment id and the sprint id
// differ, so an implementation that logged the comment's own id would fail.
func TestSprintComment_AuditTargetsTheParentSprint(t *testing.T) {
	const roadmap = "sprint-comment-audit-parent"
	database := setupSprintCommentRoadmap(t, roadmap)

	// Comment on sprint 2 first, so comment id 1 belongs to sprint 2: the two
	// numbers are different, which is what makes the assertion meaningful.
	addSprintComment(t, roadmap, 2, "FINDING", "A finding recorded against the second sprint.")
	comment := listSprintComments(t, database, 2)[0]
	if comment.ID == comment.SprintID {
		t.Fatalf("fixture is not discriminating: comment id and sprint id are both %d", comment.ID)
	}

	if err := sprintCommentEdit([]string{"-r", roadmap, itoa(comment.ID), "--type", "DECISION"}); err != nil {
		t.Fatalf("comment-edit: %v", err)
	}
	if err := sprintCommentRemove([]string{"-r", roadmap, itoa(comment.ID)}); err != nil {
		t.Fatalf("comment-remove: %v", err)
	}

	for _, op := range []models.AuditOperation{
		models.OpSprintCommentCreate, models.OpSprintCommentUpdate, models.OpSprintCommentDelete,
	} {
		if n := countSprintAudit(t, database, op, comment.SprintID); n != 1 {
			t.Errorf("%s against the parent sprint %d = %d entries, want 1", op, comment.SprintID, n)
		}
		if n := countSprintAudit(t, database, op, comment.ID); n != 0 {
			t.Errorf("%s was recorded against the comment id %d (%d entries); it must name the parent sprint",
				op, comment.ID, n)
		}
		// entity_type is SPRINT for every sprint-comment operation: the same
		// operation recorded against a TASK would be a different, wrong row.
		if n := countCommentAudit(t, database, op, models.EntityTask, comment.SprintID); n != 0 {
			t.Errorf("%s was recorded with entity_type TASK (%d entries), want SPRINT", op, n)
		}
	}

	// No new entity_type was invented for comments: every audit row still carries
	// one of the two documented values.
	entries, err := database.GetAuditEntries(context.Background(), &db.AuditFilter{})
	if err != nil {
		t.Fatalf("querying the audit log: %v", err)
	}
	for _, e := range entries {
		if !models.IsValidEntityType(string(e.EntityType)) {
			t.Errorf("audit row for %s carries an unknown entity_type %q", e.Operation, e.EntityType)
		}
	}
}

// TestSprintComment_AcceptedInEveryStatus pins that a comment is accepted whatever
// the sprint's status is — PENDING, OPEN and CLOSED — and that commenting never
// changes it. CLOSED is the case that matters: a sprint's log is written mostly
// while the sprint runs, but the account of how it went is often completed after it
// is closed, so the SPEC accepts comments there too and no comment gates a
// transition.
func TestSprintComment_AcceptedInEveryStatus(t *testing.T) {
	const roadmap = "sprint-comment-any-status"
	database := setupSprintCommentRoadmap(t, roadmap)

	// Sprint 1 walks PENDING -> OPEN -> CLOSED. Sprint 2 stays PENDING, so at most
	// one sprint is ever OPEN (idx_one_open_sprint).
	steps := []struct {
		status models.SprintStatus
		move   func() error
	}{
		{models.SprintPending, nil},
		{models.SprintOpen, func() error { return sprintStart([]string{"-r", roadmap, "1"}) }},
		{models.SprintClosed, func() error { return sprintClose([]string{"-r", roadmap, "1"}) }},
	}

	for _, s := range steps {
		if s.move != nil {
			if err := s.move(); err != nil {
				t.Fatalf("moving the sprint to %s: %v", s.status, err)
			}
		}

		_ = captureStdout(t, func() {
			if err := sprintCommentAdd([]string{
				"-r", roadmap, "1", "--type", "PROGRESS",
				"--body", "Recorded while the sprint was " + string(s.status) + ".",
			}); err != nil {
				t.Fatalf("comment-add while %s: %v", s.status, err)
			}
		})

		sprint, err := database.GetSprint(context.Background(), 1)
		if err != nil {
			t.Fatalf("reading the sprint back: %v", err)
		}
		if sprint.Status != s.status {
			t.Errorf("commenting changed the status: got %s, want %s", sprint.Status, s.status)
		}
	}

	stored := listSprintComments(t, database, 1)
	if len(stored) != len(steps) {
		t.Fatalf("stored comments = %d, want %d (one per status)", len(stored), len(steps))
	}

	// The CLOSED sprint accepted the last one, and every mutation is editable and
	// removable there too: nothing about a closed sprint freezes its log.
	closedComment := stored[len(stored)-1]
	if !strings.Contains(closedComment.Body, string(models.SprintClosed)) {
		t.Fatalf("the last comment is not the CLOSED one: %q", closedComment.Body)
	}
	if err := sprintCommentEdit([]string{
		"-r", roadmap, itoa(closedComment.ID), "--type", "UPDATE",
		"--body", "Revised after the sprint closed: the migration ships in the next one.",
	}); err != nil {
		t.Fatalf("comment-edit on a CLOSED sprint: %v", err)
	}
	if err := sprintCommentRemove([]string{"-r", roadmap, itoa(closedComment.ID)}); err != nil {
		t.Fatalf("comment-remove on a CLOSED sprint: %v", err)
	}

	sprint, err := database.GetSprint(context.Background(), 1)
	if err != nil {
		t.Fatalf("reading the sprint back: %v", err)
	}
	if sprint.Status != models.SprintClosed {
		t.Errorf("editing and removing comments changed the status to %s, want CLOSED", sprint.Status)
	}
	if sprint.ClosedAt == nil {
		t.Error("closed_at was cleared by a comment operation")
	}

	for _, op := range []models.AuditOperation{
		models.OpSprintCommentCreate, models.OpSprintCommentUpdate, models.OpSprintCommentDelete,
	} {
		if n := countSprintAudit(t, database, op, 1); n == 0 {
			t.Errorf("no %s audit entry was written against sprint 1", op)
		}
	}
}

// ---------------------------------------------------------------------------
// The shared implementation serves both families
// ---------------------------------------------------------------------------

// TestCommentFamilies_ShareOneImplementation is the executable form of the reuse
// requirement: the two families are the same four bodies bound to two
// commentFamily values, so the descriptors must differ in every field that the
// SPEC says differs, and the handlers must be the shared functions rather than
// per-family copies.
//
// It fails if a future change forks one family's handler: a fork would have to
// stop routing through commentAdd/commentList/commentEdit/commentRemove, and the
// only way to keep this test passing would be to keep the descriptor as the single
// point of difference.
func TestCommentFamilies_ShareOneImplementation(t *testing.T) {
	// The accepted sets are the SPEC's two subsets, and the sprint set is strictly
	// smaller.
	if len(models.ValidSprintCommentTypes) != 4 || len(models.ValidTaskCommentTypes) != 7 {
		t.Fatalf("accepted sets changed: sprint = %v, task = %v",
			models.ValidSprintCommentTypes, models.ValidTaskCommentTypes)
	}
	for _, taskOnly := range []models.CommentType{models.CommentHypothesis, models.CommentTest, models.CommentNote} {
		if models.IsValidSprintCommentType(string(taskOnly)) {
			t.Errorf("%s must not be a sprint comment type", taskOnly)
		}
	}

	// The per-family data is genuinely different.
	if sprintCommentFamily.entityType == taskCommentFamily.entityType {
		t.Errorf("both families audit against entity_type %q", sprintCommentFamily.entityType)
	}
	if sprintCommentFamily.parentLabel == taskCommentFamily.parentLabel {
		t.Errorf("both families name their parent %q", sprintCommentFamily.parentLabel)
	}
	ops := map[models.AuditOperation]string{
		taskCommentFamily.opCreate:   "task create",
		taskCommentFamily.opUpdate:   "task update",
		taskCommentFamily.opDelete:   "task delete",
		sprintCommentFamily.opCreate: "sprint create",
		sprintCommentFamily.opUpdate: "sprint update",
		sprintCommentFamily.opDelete: "sprint delete",
	}
	if len(ops) != 6 {
		t.Errorf("the six comment audit operations are not distinct: %v", ops)
	}

	// Every field of the shared contract is populated on both sides: an unset
	// closure would panic at run time rather than fail a build.
	families := map[string]*commentFamily{"task": &taskCommentFamily, "sprint": &sprintCommentFamily}
	for name, f := range families {
		if f.parseType == nil || f.parentExists == nil || f.parentOf == nil ||
			f.insert == nil || f.update == nil || f.remove == nil || f.list == nil {
			t.Errorf("%s family has an unset behaviour: %+v", name, f)
		}
		if f.entityType == "" || f.parentLabel == "" ||
			f.opCreate == "" || f.opUpdate == "" || f.opDelete == "" {
			t.Errorf("%s family has an unset field: %+v", name, f)
		}
	}
}

// TestSprintComment_TransactionAtomicity pins that the row and its audit entry
// commit together: a write that cannot log its audit entry must leave nothing
// behind. The failure is injected the only way it can be without touching
// production code — by removing the audit table inside the same connection the
// handler will use, so LogAuditTx fails after the insert has already succeeded.
func TestSprintComment_TransactionAtomicity(t *testing.T) {
	const roadmap = "sprint-comment-atomic"
	database := setupSprintCommentRoadmap(t, roadmap)

	// The handler reopens the roadmap, so the schema change has to be committed.
	if err := database.WithTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE audit RENAME TO audit_moved_aside`)
		return err
	}); err != nil {
		t.Fatalf("moving the audit table aside: %v", err)
	}
	t.Cleanup(func() {
		_ = database.WithTransaction(func(tx *sql.Tx) error {
			_, err := tx.Exec(`ALTER TABLE audit_moved_aside RENAME TO audit`)
			return err
		})
	})

	err := sprintCommentAdd([]string{
		"-r", roadmap, "1", "--type", "DECISION", "--body", "This must not survive its own audit failure.",
	})
	if err == nil {
		t.Fatal("comment-add must fail when its audit entry cannot be written")
	}
	// The failure must come from the audit write, not from opening the roadmap: the
	// insert has to have succeeded for the rollback to be worth asserting.
	if !strings.Contains(err.Error(), "audit") {
		t.Fatalf("the injected failure is not the audit write: %v", err)
	}

	if n := len(listSprintComments(t, database, 1)); n != 0 {
		t.Errorf("the comment was committed without its audit entry: %d rows stored", n)
	}
}
