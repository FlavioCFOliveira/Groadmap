// Package commands — tests for the four comment subcommands of the `task` family.
//
// The suite is organised around the behaviours SPEC/COMMANDS.md § Task Comments
// pins and that nothing below the command layer can enforce:
//
//   - the body's input sources and their precedence, including the two cases the
//     SPEC deliberately reports differently from the domain (an absent body is a
//     missing parameter, exit code 2, not a validation error, exit code 6);
//   - the validation ORDER, which is behaviour: an invalid --type must be
//     reported without ever reading standard input, so a bad type on a terminal
//     cannot leave the command blocked;
//   - the exit-code classification of every refusal;
//   - the audit entry, which is always written against the PARENT task, with the
//     parent's id, inside the same transaction as the mutation.
//
// Every test asserts the outcome — the stored row, the printed JSON, the audit
// row — and not merely the error class.
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

// setupCommentRoadmap creates a hermetic roadmap under a temporary HOME and
// seeds two tasks, so a test can comment on one and use the other to prove that
// a comment id and a task id are never confused. The returned handle is open on
// the same database the handlers reopen through db.OpenExisting.
func setupCommentRoadmap(t *testing.T, name string) *db.DB {
	t.Helper()

	// Hermetic: ~/.roadmaps resolves under the test's own HOME, so a run never
	// touches (or depends on) the developer's real roadmaps.
	t.Setenv("HOME", t.TempDir())

	database, cleanup := setupTestTaskRoadmap(t, name)
	t.Cleanup(cleanup)

	seed := []struct{ title, fr, tr, ac string }{
		{
			"Fix JWT boundary-second expiry",
			"A token whose exp is the current second must be refused",
			"Compare with !time.Now().Before(exp) instead of time.Now().After(exp)",
			"A unit test covers the exact boundary second",
		},
		{
			"Add expiry regression test",
			"The boundary second must stay covered by the suite",
			"Table-driven test around exp with a frozen clock",
			"The test fails against the old comparison",
		},
	}
	for _, s := range seed {
		if err := taskCreate([]string{
			"-r", name, "-t", s.title, "-fr", s.fr, "-tr", s.tr, "-ac", s.ac,
		}); err != nil {
			t.Fatalf("seeding task %q: %v", s.title, err)
		}
	}

	return database
}

// listComments reads a task's comments straight from the database, so an
// assertion about what was stored never depends on the command that prints them.
func listComments(t *testing.T, database *db.DB, taskID int) []models.TaskComment {
	t.Helper()

	comments, err := database.ListTaskComments(context.Background(), taskID, nil)
	if err != nil {
		t.Fatalf("listing comments of task %d: %v", taskID, err)
	}
	return comments
}

// getComment reads one comment by its own id.
func getComment(t *testing.T, database *db.DB, id int) *models.TaskComment {
	t.Helper()

	comment, err := database.GetTaskComment(context.Background(), id)
	if err != nil {
		t.Fatalf("reading comment %d: %v", id, err)
	}
	return comment
}

// countAudit counts the audit rows for one comment operation against one task. It
// fixes the entity type at TASK, which is the invariant this family asserts: every
// task-comment operation is recorded against the parent TASK and never against a
// sprint (SPEC/DATA_FORMATS.md § Audit Entry).
func countAudit(t *testing.T, database *db.DB, op models.AuditOperation, taskID int) int {
	t.Helper()
	return countCommentAudit(t, database, op, models.EntityTask, taskID)
}

// addComment is the shorthand for seeding a comment through the command under
// test, returning nothing: tests that need the id read it back from the database,
// which also proves the row exists.
func addComment(t *testing.T, roadmap string, taskID int, commentType, body string) {
	t.Helper()

	out := captureStdout(t, func() {
		if err := taskCommentAdd([]string{
			"-r", roadmap, itoa(taskID), "--type", commentType, "--body", body,
		}); err != nil {
			t.Fatalf("comment-add(%s): %v", commentType, err)
		}
	})
	if !strings.Contains(out, `"id"`) {
		t.Fatalf("comment-add printed no id: %q", out)
	}
}

// ---------------------------------------------------------------------------
// comment-add: body input sources and precedence
// ---------------------------------------------------------------------------

// TestTaskCommentAdd_BodyFromFlag verifies the flag form end to end: the row is
// stored trimmed, updated_at starts null, the audit entry is written against the
// parent task, and the printed payload carries the new comment's id.
func TestTaskCommentAdd_BodyFromFlag(t *testing.T) {
	const roadmap = "comment-add-body-flag"
	database := setupCommentRoadmap(t, roadmap)

	const body = "time.Now().After(exp) is false at equality, so the boundary second is accepted."

	out := captureStdout(t, func() {
		if err := taskCommentAdd([]string{"-r", roadmap, "1", "--type", "FINDING", "--body", "  " + body + "  \n"}); err != nil {
			t.Fatalf("comment-add: %v", err)
		}
	})

	comments := listComments(t, database, 1)
	if len(comments) != 1 {
		t.Fatalf("stored comments = %d, want 1", len(comments))
	}
	got := comments[0]
	if got.Body != body {
		t.Errorf("stored body is not the trimmed form\n got: %q\nwant: %q", got.Body, body)
	}
	if got.Type != models.CommentFinding {
		t.Errorf("stored type = %q, want FINDING", got.Type)
	}
	if got.TaskID != 1 {
		t.Errorf("stored task_id = %d, want 1", got.TaskID)
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
	if n := countAudit(t, database, models.OpTaskCommentCreate, 1); n != 1 {
		t.Errorf("TASK_COMMENT_CREATE audit entries against task 1 = %d, want 1", n)
	}
}

// TestTaskCommentAdd_BodyFromStdin verifies that an absent --body makes the whole
// of standard input the body, that interior line breaks survive, and that the
// leading/trailing whitespace is trimmed. This is the heredoc / pipe form.
func TestTaskCommentAdd_BodyFromStdin(t *testing.T) {
	const roadmap = "comment-add-body-stdin"
	database := setupCommentRoadmap(t, roadmap)

	const piped = "\nCompare with !time.Now().Before(exp).\n\nRejected widening the clock skew: it hides the boundary.\n\n"
	const want = "Compare with !time.Now().Before(exp).\n\nRejected widening the clock skew: it hides the boundary."

	leftover := withStdin(t, piped, func() {
		_ = captureStdout(t, func() {
			if err := taskCommentAdd([]string{"-r", roadmap, "1", "--type", "DECISION"}); err != nil {
				t.Fatalf("comment-add from stdin: %v", err)
			}
		})
	})
	if leftover != "" {
		t.Errorf("standard input was not read to EOF; %q left unread", leftover)
	}

	comments := listComments(t, database, 1)
	if len(comments) != 1 {
		t.Fatalf("stored comments = %d, want 1", len(comments))
	}
	if comments[0].Body != want {
		t.Errorf("stored body\n got: %q\nwant: %q", comments[0].Body, want)
	}
	if comments[0].Type != models.CommentDecision {
		t.Errorf("stored type = %q, want DECISION", comments[0].Type)
	}
}

// TestTaskCommentAdd_FlagWinsOverStdin pins precedence rule 1: with both sources
// present the flag is the body and standard input is NOT read. The unread
// leftover is the proof — an implementation that read stdin first, or read it
// "just in case", would consume it.
func TestTaskCommentAdd_FlagWinsOverStdin(t *testing.T) {
	const roadmap = "comment-add-flag-beats-stdin"
	database := setupCommentRoadmap(t, roadmap)

	const fromStdin = "THIS TEXT ARRIVES ON STANDARD INPUT AND MUST BE IGNORED"
	const fromFlag = "This text arrives on the flag and must win."

	leftover := withStdin(t, fromStdin, func() {
		_ = captureStdout(t, func() {
			if err := taskCommentAdd([]string{"-r", roadmap, "1", "--type", "NOTE", "--body", fromFlag}); err != nil {
				t.Fatalf("comment-add: %v", err)
			}
		})
	})

	if leftover != fromStdin {
		t.Errorf("standard input was consumed although --body was supplied; leftover = %q", leftover)
	}
	comments := listComments(t, database, 1)
	if len(comments) != 1 || comments[0].Body != fromFlag {
		t.Fatalf("stored body = %q, want the flag value %q", comments[0].Body, fromFlag)
	}
}

// TestTaskCommentAdd_InlineBodyForms verifies the GNU-style inline spellings the
// shared flag parser accepts for every other flag: --body=<text> and -b=<text>.
func TestTaskCommentAdd_InlineBodyForms(t *testing.T) {
	const roadmap = "comment-add-inline-body"
	database := setupCommentRoadmap(t, roadmap)

	cases := []struct{ name, arg, want string }{
		{"long form", "--body=Inline long form is accepted.", "Inline long form is accepted."},
		{"short form", "-b=Inline short form is accepted.", "Inline short form is accepted."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_ = captureStdout(t, func() {
				if err := taskCommentAdd([]string{"-r", roadmap, "1", "--type=NOTE", c.arg}); err != nil {
					t.Fatalf("comment-add %s: %v", c.arg, err)
				}
			})
		})
	}

	comments := listComments(t, database, 1)
	if len(comments) != 2 {
		t.Fatalf("stored comments = %d, want 2", len(comments))
	}
	for i, want := range []string{cases[0].want, cases[1].want} {
		if comments[i].Body != want {
			t.Errorf("comment %d body = %q, want %q", i, comments[i].Body, want)
		}
	}
}

// TestTaskCommentAdd_NoBodySupplied covers every way a body can fail to arrive.
// All of them are exit code 2 with ONE pinned message, and none of them may
// surface the domain's own "body is required" verdict, which is exit code 6:
// models.ValidateCommentBody deliberately does not chain utils.ErrRequired, so
// this layer owns the emptiness decision (SPEC/COMMANDS.md rules 3-4).
func TestTaskCommentAdd_NoBodySupplied(t *testing.T) {
	const roadmap = "comment-add-no-body"
	setupCommentRoadmap(t, roadmap)

	const wantMsg = "required parameter missing: no comment body supplied"

	cases := []struct {
		name  string
		args  []string
		stdin string
	}{
		{"flag absent, stdin empty", []string{"-r", roadmap, "1", "--type", "NOTE"}, ""},
		{"flag absent, stdin whitespace only", []string{"-r", roadmap, "1", "--type", "NOTE"}, " \n\t\n "},
		{"flag present, value empty", []string{"-r", roadmap, "1", "--type", "NOTE", "--body", ""}, "ignored"},
		{"flag present, value whitespace only", []string{"-r", roadmap, "1", "--type", "NOTE", "--body", "   "}, "ignored"},
		{"flag present, no value token", []string{"-r", roadmap, "1", "--type", "NOTE", "--body"}, "ignored"},
		{"flag present, next token is a flag", []string{"-r", roadmap, "1", "--body", "--type", "NOTE"}, "ignored"},
		{"inline form with an empty value", []string{"-r", roadmap, "1", "--type", "NOTE", "--body="}, "ignored"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var err error
			leftover := withStdin(t, c.stdin, func() { err = taskCommentAdd(c.args) })

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
// comment-add: --type presence, value, and the ORDER of the two checks
// ---------------------------------------------------------------------------

// TestTaskCommentAdd_TypeRequired verifies that an absent --type is exit code 2
// with the pinned message, and — the part that matters — that it is decided
// before standard input is consulted.
func TestTaskCommentAdd_TypeRequired(t *testing.T) {
	const roadmap = "comment-add-type-required"
	setupCommentRoadmap(t, roadmap)

	var err error
	leftover := withStdin(t, "a body that must never be read", func() {
		err = taskCommentAdd([]string{"-r", roadmap, "1"})
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

// TestTaskCommentAdd_TypeOutsideTaskSet verifies that a value outside the seven
// task values is exit code 6 and that the message names the valid set, rendered
// from models.FormatCommentTypes so the test cannot drift from the enum.
//
// A TaskType value is included on purpose: `-y, --type` carries a TaskType on
// task list/create/edit and a comment type here, and the two are never
// interchangeable (SPEC/COMMANDS.md § Task Comments, third shared property).
func TestTaskCommentAdd_TypeOutsideTaskSet(t *testing.T) {
	const roadmap = "comment-add-type-invalid"
	setupCommentRoadmap(t, roadmap)

	validSet := models.FormatCommentTypes(models.ValidTaskCommentTypes)

	for _, bad := range []string{"BUG", "SUB_TASK", "finding", "", "WHATEVER"} {
		t.Run("type="+bad, func(t *testing.T) {
			var err error
			leftover := withStdin(t, "unread body", func() {
				err = taskCommentAdd([]string{"-r", roadmap, "1", "--type", bad})
			})

			if err == nil {
				t.Fatalf("want an error for --type %q, got nil", bad)
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("error must wrap utils.ErrValidation (exit 6); got %v", err)
			}
			want := `invalid comment type "` + bad + `" for a task comment; valid types: ` + validSet
			if got := err.Error(); !strings.HasSuffix(got, want) {
				t.Errorf("message must name the accepted set\n got: %q\nwant suffix: %q", got, want)
			}
			// The whole point of validating the type first: no stdin read.
			if leftover != "unread body" {
				t.Errorf("standard input was read although --type was invalid; leftover = %q", leftover)
			}
		})
	}
}

// TestTaskCommentAdd_InvalidTypeDoesNotBlockOnStandardInput is the regression
// guard for the validation ORDER as a runtime property rather than a message.
//
// Standard input is a pipe whose write end stays open and carries nothing, so any
// read of it blocks until this test closes that end. A handler that resolved the
// body before validating the type would therefore hang; the pinned order requires
// it to return the type verdict immediately.
func TestTaskCommentAdd_InvalidTypeDoesNotBlockOnStandardInput(t *testing.T) {
	const roadmap = "comment-add-no-block-on-stdin"
	setupCommentRoadmap(t, roadmap)

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
		"comment-add":  taskCommentAdd,
		"comment-edit": taskCommentEdit,
	}
	for name, handler := range subcommands {
		t.Run(name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() { done <- handler([]string{"-r", roadmap, "1", "--type", "BOGUS"}) }()

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
// comment-add: body validation (length, control characters) and the parent task
// ---------------------------------------------------------------------------

// TestTaskCommentAdd_BodyLengthBoundary pins the cap at exactly
// models.MaxCommentBody CHARACTERS: 4096 is accepted, 4097 is exit code 6, and a
// 4096-rune multi-byte body is accepted even though it is three times that many
// bytes — the unit is characters, matching the schema's length() CHECK.
func TestTaskCommentAdd_BodyLengthBoundary(t *testing.T) {
	const roadmap = "comment-add-body-length"
	database := setupCommentRoadmap(t, roadmap)

	atCap := strings.Repeat("a", models.MaxCommentBody)
	overCap := strings.Repeat("a", models.MaxCommentBody+1)
	multiByteAtCap := strings.Repeat("ç", models.MaxCommentBody)

	_ = captureStdout(t, func() {
		if err := taskCommentAdd([]string{"-r", roadmap, "1", "--type", "NOTE", "--body", atCap}); err != nil {
			t.Fatalf("a %d-character body must be accepted: %v", models.MaxCommentBody, err)
		}
	})
	_ = captureStdout(t, func() {
		if err := taskCommentAdd([]string{"-r", roadmap, "1", "--type", "NOTE", "--body", multiByteAtCap}); err != nil {
			t.Fatalf("a %d-rune multi-byte body must be accepted (characters, not bytes): %v", models.MaxCommentBody, err)
		}
	})

	err := taskCommentAdd([]string{"-r", roadmap, "1", "--type", "NOTE", "--body", overCap})
	if err == nil {
		t.Fatalf("a %d-character body must be rejected", models.MaxCommentBody+1)
	}
	if !errors.Is(err, utils.ErrFieldTooLarge) {
		t.Errorf("error must wrap utils.ErrFieldTooLarge (exit 6); got %v", err)
	}
	if want := "body exceeds maximum length of 4096 characters"; !strings.Contains(err.Error(), want) {
		t.Errorf("message\n got: %q\nwant substring: %q", err.Error(), want)
	}

	if n := len(listComments(t, database, 1)); n != 2 {
		t.Errorf("stored comments = %d, want 2 (the oversize body must not be stored)", n)
	}
}

// TestTaskCommentAdd_ControlCharactersRejected pins the control-character rule
// against the body AS SUPPLIED.
//
// The leading VT and FF cases are the regression guard: strings.TrimSpace strips
// both, so a handler that trimmed before validating would silently drop a
// forbidden character instead of rejecting the input that carried it
// (CWE-150 / Trojan Source). TAB, LF and CR are permitted and must still pass.
func TestTaskCommentAdd_ControlCharactersRejected(t *testing.T) {
	const roadmap = "comment-add-control-chars"
	database := setupCommentRoadmap(t, roadmap)

	rejected := map[string]string{
		"leading vertical tab":  "\vbody text",
		"trailing form feed":    "body text\f",
		"interior vertical tab": "body\vtext",
		"escape sequence":       "body \x1b[31mtext",
		"nul byte":              "body\x00text",
		"delete":                "body\x7ftext",
		"bidi override":         "body \u202e text",
		"zero-width no-break":   "body \ufeff text",
	}
	for name, body := range rejected {
		t.Run("rejects "+name, func(t *testing.T) {
			err := taskCommentAdd([]string{"-r", roadmap, "1", "--type", "NOTE", "--body", body})
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
		if err := taskCommentAdd([]string{"-r", roadmap, "1", "--type", "NOTE", "--body", allowed}); err != nil {
			t.Fatalf("TAB, LF and CR are permitted: %v", err)
		}
	})

	comments := listComments(t, database, 1)
	if len(comments) != 1 {
		t.Fatalf("stored comments = %d, want 1 (only the permitted body)", len(comments))
	}
	if comments[0].Body != allowed {
		t.Errorf("stored body\n got: %q\nwant: %q", comments[0].Body, allowed)
	}
}

// TestTaskCommentAdd_UnknownTask verifies the exit-4 condition and its message,
// and that the task's existence is checked before the body is validated only in
// the order the SPEC pins: an unknown task with an oversize body reports the
// task, not the body.
func TestTaskCommentAdd_UnknownTask(t *testing.T) {
	const roadmap = "comment-add-unknown-task"
	database := setupCommentRoadmap(t, roadmap)

	err := taskCommentAdd([]string{"-r", roadmap, "4242", "--type", "NOTE", "--body", "text"})
	if err == nil {
		t.Fatal("want an error for an unknown task, got nil")
	}
	if !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("error must wrap utils.ErrNotFound (exit 4); got %v", err)
	}
	if want := "resource not found: task 4242 not found"; err.Error() != want {
		t.Errorf("message\n got: %q\nwant: %q", err.Error(), want)
	}

	// Step 6 precedes step 7: the task is reported, not the oversize body.
	err = taskCommentAdd([]string{
		"-r", roadmap, "4242", "--type", "NOTE",
		"--body", strings.Repeat("a", models.MaxCommentBody+1),
	})
	if !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("an unknown task must be reported before the body is validated; got %v", err)
	}

	if n := countAudit(t, database, models.OpTaskCommentCreate, 4242); n != 0 {
		t.Errorf("a failed comment-add wrote %d audit entries, want 0", n)
	}
}

// TestTaskComment_PositionalIDClassification pins the exit-code class of every
// malformed positional id on all four subcommands.
//
// SPEC/COMMANDS.md classifies the whole "positive integer" constraint as exit
// code 2 (invalid input) for these subcommands, including a non-positive or
// oversized value that utils.ValidateIDString would otherwise report as a
// validation error (exit code 6). The assertion is deliberately two-sided: it
// requires ErrInvalidInput AND rejects ErrValidation.
func TestTaskComment_PositionalIDClassification(t *testing.T) {
	const roadmap = "comment-positional-ids"
	setupCommentRoadmap(t, roadmap)

	handlers := map[string]struct {
		run    func([]string) error
		entity string
		extra  []string
	}{
		"comment-add":    {taskCommentAdd, "task", []string{"--type", "NOTE", "--body", "text"}},
		"comment-list":   {taskCommentList, "task", nil},
		"comment-edit":   {taskCommentEdit, "comment", []string{"--type", "NOTE"}},
		"comment-remove": {taskCommentRemove, "comment", nil},
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
			err := h.run(append([]string{"-r", roadmap}, nil...))
			if !errors.Is(err, utils.ErrRequired) {
				t.Errorf("missing id: error must wrap utils.ErrRequired (exit 2); got %v", err)
			}
			if want := h.entity + " ID required"; err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("missing id: message\n got: %v\nwant substring: %q", err, want)
			}
		})
	}
}

// TestTaskComment_MissingRoadmap verifies that all four subcommands report a
// missing -r as exit code 3, before anything else is parsed.
func TestTaskComment_MissingRoadmap(t *testing.T) {
	handlers := map[string]func([]string) error{
		"comment-add":    taskCommentAdd,
		"comment-list":   taskCommentList,
		"comment-edit":   taskCommentEdit,
		"comment-remove": taskCommentRemove,
	}
	for name, run := range handlers {
		t.Run(name, func(t *testing.T) {
			err := run([]string{"1", "--type", "NOTE", "--body", "text"})
			if err == nil {
				t.Fatal("want an error for a missing -r, got nil")
			}
			if !errors.Is(err, utils.ErrNoRoadmap) {
				t.Errorf("error must wrap utils.ErrNoRoadmap (exit 3); got %v", err)
			}
		})
	}
}

// TestTaskComment_UnknownFlagRejected verifies that an unrecognised flag is exit
// code 2 on every subcommand, including comment-remove, which accepts no flag of
// its own — so `--type` is unknown there.
func TestTaskComment_UnknownFlagRejected(t *testing.T) {
	const roadmap = "comment-unknown-flag"
	setupCommentRoadmap(t, roadmap)

	cases := map[string][]string{
		"comment-add":                    {"-r", roadmap, "1", "--type", "NOTE", "--body", "text", "--foo"},
		"comment-list":                   {"-r", roadmap, "1", "--foo"},
		"comment-edit":                   {"-r", roadmap, "1", "--type", "NOTE", "--foo"},
		"comment-remove":                 {"-r", roadmap, "1", "--foo"},
		"comment-remove takes no --type": {"-r", roadmap, "1", "--type", "NOTE"},
	}
	handlers := map[string]func([]string) error{
		"comment-add":                    taskCommentAdd,
		"comment-list":                   taskCommentList,
		"comment-edit":                   taskCommentEdit,
		"comment-remove":                 taskCommentRemove,
		"comment-remove takes no --type": taskCommentRemove,
	}

	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			err := handlers[name](args)
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

// TestTaskCommentList_OldestFirstAndTypeFilter verifies the ordering contract and
// the filter, on the printed JSON rather than on the query, and verifies that a
// task with nothing recorded prints an empty array rather than null.
func TestTaskCommentList_OldestFirstAndTypeFilter(t *testing.T) {
	const roadmap = "comment-list-order"
	database := setupCommentRoadmap(t, roadmap)

	addComment(t, roadmap, 1, "FINDING", "First: the boundary second is accepted by the parser.")
	addComment(t, roadmap, 1, "DECISION", "Second: compare with !Before(exp).")
	addComment(t, roadmap, 1, "FINDING", "Third: the handler refuses what the parser accepted.")
	addComment(t, roadmap, 2, "NOTE", "A comment on the other task, which must never appear.")

	out := captureStdout(t, func() {
		if err := taskCommentList([]string{"-r", roadmap, "1"}); err != nil {
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
	if !(first < second && second < third) {
		t.Errorf("comments are not oldest first (offsets %d, %d, %d):\n%s", first, second, third, out)
	}
	if strings.Contains(out, "other task") {
		t.Errorf("comment-list leaked another task's comments:\n%s", out)
	}

	// The stored order must match, so the contract does not depend on the printer.
	stored := listComments(t, database, 1)
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
		if err := taskCommentList([]string{"-r", roadmap, "1", "--type", "FINDING"}); err != nil {
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
		if err := taskCommentList([]string{"-r", roadmap, "1", "--type", "HYPOTHESIS"}); err != nil {
			t.Fatalf("comment-list --type HYPOTHESIS: %v", err)
		}
	})
	if strings.TrimSpace(empty) != "[]" {
		t.Errorf("a filter matching nothing must print an empty array, got %q", empty)
	}
}

// TestTaskCommentList_EmptyLogAndUnknownTask verifies that a task with no
// comments prints [] with no error, while an unknown task is exit code 4.
func TestTaskCommentList_EmptyLogAndUnknownTask(t *testing.T) {
	const roadmap = "comment-list-empty"
	setupCommentRoadmap(t, roadmap)

	out := captureStdout(t, func() {
		if err := taskCommentList([]string{"-r", roadmap, "2"}); err != nil {
			t.Fatalf("comment-list on a task with no comments: %v", err)
		}
	})
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("want an empty array for a task with no comments, got %q", out)
	}

	err := taskCommentList([]string{"-r", roadmap, "4242"})
	if !errors.Is(err, utils.ErrNotFound) {
		t.Fatalf("unknown task: error must wrap utils.ErrNotFound (exit 4); got %v", err)
	}
	if want := "resource not found: task 4242 not found"; err.Error() != want {
		t.Errorf("message\n got: %q\nwant: %q", err.Error(), want)
	}
}

// TestTaskCommentList_WritesNoAuditEntry pins that listing is a read: no audit
// row of any comment operation appears after it.
func TestTaskCommentList_WritesNoAuditEntry(t *testing.T) {
	const roadmap = "comment-list-no-audit"
	database := setupCommentRoadmap(t, roadmap)

	addComment(t, roadmap, 1, "NOTE", "A comment to have something to list.")
	before := countAudit(t, database, models.OpTaskCommentCreate, 1)

	_ = captureStdout(t, func() {
		if err := taskCommentList([]string{"-r", roadmap, "1"}); err != nil {
			t.Fatalf("comment-list: %v", err)
		}
	})

	for _, op := range []models.AuditOperation{
		models.OpTaskCommentCreate, models.OpTaskCommentUpdate, models.OpTaskCommentDelete,
	} {
		want := 0
		if op == models.OpTaskCommentCreate {
			want = before
		}
		if got := countAudit(t, database, op, 1); got != want {
			t.Errorf("comment-list changed the %s audit count: got %d, want %d", op, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// comment-edit
// ---------------------------------------------------------------------------

// TestTaskCommentEdit_TypeOnly verifies a type-only edit: the type changes, the
// body and created_at do not, updated_at is stamped, the audit entry is written
// against the parent task — and standard input is NOT read, so a type-only edit
// never waits for input (SPEC precedence rule 2).
func TestTaskCommentEdit_TypeOnly(t *testing.T) {
	const roadmap = "comment-edit-type-only"
	database := setupCommentRoadmap(t, roadmap)

	addComment(t, roadmap, 2, "PROGRESS", "Boundary comparison replaced; regression test pending.")
	before := listComments(t, database, 2)[0]

	const untouched = "A BODY ON STANDARD INPUT THAT A TYPE-ONLY EDIT MUST NOT READ"
	var err error
	leftover := withStdin(t, untouched, func() {
		err = taskCommentEdit([]string{"-r", roadmap, itoa(before.ID), "--type", "UPDATE"})
	})
	if err != nil {
		t.Fatalf("comment-edit --type: %v", err)
	}
	if leftover != untouched {
		t.Errorf("standard input was read although --type was present; leftover = %q", leftover)
	}

	after := getComment(t, database, before.ID)
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
	if n := countAudit(t, database, models.OpTaskCommentUpdate, 2); n != 1 {
		t.Errorf("TASK_COMMENT_UPDATE audit entries against task 2 = %d, want 1", n)
	}
}

// TestTaskCommentEdit_BodyFromStdinIsAValidEdit verifies the flagless form: with
// neither --type nor --body, the body on standard input IS the change, so the
// command is an edit and not a no-op.
func TestTaskCommentEdit_BodyFromStdinIsAValidEdit(t *testing.T) {
	const roadmap = "comment-edit-stdin-body"
	database := setupCommentRoadmap(t, roadmap)

	addComment(t, roadmap, 1, "FINDING", "The original finding text.")
	original := listComments(t, database, 1)[0]

	const revised = "The revised finding: the boundary second is accepted by the parser and refused by the handler."
	var err error
	leftover := withStdin(t, "\n"+revised+"\n", func() {
		err = taskCommentEdit([]string{"-r", roadmap, itoa(original.ID)})
	})
	if err != nil {
		t.Fatalf("flagless comment-edit reading stdin: %v", err)
	}
	if leftover != "" {
		t.Errorf("standard input must be read to EOF; %q left unread", leftover)
	}

	after := getComment(t, database, original.ID)
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

// TestTaskCommentEdit_TypeAndBody verifies that both columns change in one call.
func TestTaskCommentEdit_TypeAndBody(t *testing.T) {
	const roadmap = "comment-edit-type-and-body"
	database := setupCommentRoadmap(t, roadmap)

	addComment(t, roadmap, 1, "HYPOTHESIS", "Maybe the clock skew allowance is too small.")
	original := listComments(t, database, 1)[0]

	const body = "Confirmed by a table-driven test with a frozen clock at exactly exp."
	if err := taskCommentEdit([]string{
		"-r", roadmap, itoa(original.ID), "--type", "TEST", "--body", body,
	}); err != nil {
		t.Fatalf("comment-edit --type --body: %v", err)
	}

	after := getComment(t, database, original.ID)
	if after.Type != models.CommentTest || after.Body != body {
		t.Errorf("after the edit: type = %q, body = %q; want TEST and %q", after.Type, after.Body, body)
	}
}

// TestTaskCommentEdit_NoChangeRequested verifies the one case that requests
// nothing at all — no --type, no --body, nothing usable on standard input — and
// its own pinned message, which differs from the comment-add wording because the
// two subcommands are missing different things.
func TestTaskCommentEdit_NoChangeRequested(t *testing.T) {
	const roadmap = "comment-edit-no-change"
	database := setupCommentRoadmap(t, roadmap)

	addComment(t, roadmap, 1, "NOTE", "Unchanged by the calls below.")
	original := listComments(t, database, 1)[0]

	const wantMsg = "required parameter missing: at least one of --type or --body is required"
	for _, stdin := range []string{"", "   \n\t"} {
		var err error
		withStdin(t, stdin, func() {
			err = taskCommentEdit([]string{"-r", roadmap, itoa(original.ID)})
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

	after := getComment(t, database, original.ID)
	if after.UpdatedAt != nil {
		t.Error("a rejected edit must not stamp updated_at")
	}
	if n := countAudit(t, database, models.OpTaskCommentUpdate, 1); n != 0 {
		t.Errorf("a rejected edit wrote %d audit entries, want 0", n)
	}
}

// TestTaskCommentEdit_UnknownCommentAndIDSpaces verifies the exit-4 condition on
// an unknown comment id, and that the two comment id spaces are independent: an
// id that exists in sprint_comments only is not found here.
func TestTaskCommentEdit_UnknownCommentAndIDSpaces(t *testing.T) {
	const roadmap = "comment-edit-unknown-id"
	database := setupCommentRoadmap(t, roadmap)

	err := taskCommentEdit([]string{"-r", roadmap, "9999", "--type", "NOTE"})
	if !errors.Is(err, utils.ErrNotFound) {
		t.Fatalf("error must wrap utils.ErrNotFound (exit 4); got %v", err)
	}
	if want := "resource not found: task comment 9999 not found"; err.Error() != want {
		t.Errorf("message\n got: %q\nwant: %q", err.Error(), want)
	}

	// Seed a sprint comment with no task comment carrying the same id, then prove
	// the task family does not see it.
	sprintID, sprintCommentID := seedSprintComment(t, database)
	if _, gerr := database.GetSprintComment(context.Background(), sprintCommentID); gerr != nil {
		t.Fatalf("the seeded sprint comment must exist: %v", gerr)
	}
	if _, gerr := database.GetTaskComment(context.Background(), sprintCommentID); gerr == nil {
		t.Fatalf("test premise broken: task comment %d exists, so the id spaces cannot be told apart",
			sprintCommentID)
	}

	err = taskCommentEdit([]string{"-r", roadmap, itoa(sprintCommentID), "--type", "NOTE"})
	if !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("a sprint comment id must be not-found in the task family; got %v", err)
	}
	err = taskCommentRemove([]string{"-r", roadmap, itoa(sprintCommentID)})
	if !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("comment-remove must not reach sprint_comments; got %v", err)
	}

	// The sprint comment survived both calls untouched.
	surviving, gerr := database.GetSprintComment(context.Background(), sprintCommentID)
	if gerr != nil {
		t.Fatalf("the sprint comment was affected by a task-family call: %v", gerr)
	}
	if surviving.SprintID != sprintID || surviving.UpdatedAt != nil {
		t.Errorf("sprint comment altered: sprint_id = %d, updated_at = %v", surviving.SprintID, surviving.UpdatedAt)
	}
}

// seedSprintComment creates one sprint and one comment on it, returning both ids.
// It writes through the same transactional shape the sprint family's own handlers
// use — the insert and its SPRINT_COMMENT_CREATE audit entry in one transaction —
// so this fixture cannot drift from the production write path. It deliberately does
// not call sprintCommentAdd: a task-family test must not depend on the sprint
// family's command surface to state a fact about task_comments.
func seedSprintComment(t *testing.T, database *db.DB) (sprintID, commentID int) {
	t.Helper()

	ctx := context.Background()
	sprintID, err := database.CreateSprint(ctx, &models.Sprint{
		Title:       "Expiry hardening",
		Description: "Close the JWT boundary-second defect and lock it behind a regression test.",
		Status:      models.SprintPending,
		CreatedAt:   utils.NowISO8601(),
	})
	if err != nil {
		t.Fatalf("creating the sprint fixture: %v", err)
	}

	now := utils.NowISO8601()
	comment := &models.SprintComment{
		SprintID:  sprintID,
		Type:      models.CommentProgress,
		Body:      "The expiry task is closed; the regression test is still open.",
		CreatedAt: now,
	}
	if err := database.WithTransaction(func(tx *sql.Tx) error {
		id, insertErr := db.InsertSprintCommentTx(tx, comment)
		if insertErr != nil {
			return insertErr
		}
		commentID = id
		return db.LogAuditTx(tx, models.OpSprintCommentCreate, models.EntitySprint, sprintID, now)
	}); err != nil {
		t.Fatalf("inserting the sprint comment fixture: %v", err)
	}
	return sprintID, commentID
}

// ---------------------------------------------------------------------------
// comment-remove
// ---------------------------------------------------------------------------

// TestTaskCommentRemove_DeletesRowAndKeepsAudit verifies that the row goes, the
// siblings stay, the delete is recorded against the parent task, and the audit
// entries of the removed comment outlive it.
func TestTaskCommentRemove_DeletesRowAndKeepsAudit(t *testing.T) {
	const roadmap = "comment-remove-row"
	database := setupCommentRoadmap(t, roadmap)

	addComment(t, roadmap, 2, "FINDING", "The comment that will be removed.")
	addComment(t, roadmap, 2, "DECISION", "The comment that must survive.")
	comments := listComments(t, database, 2)
	if len(comments) != 2 {
		t.Fatalf("fixture: stored comments = %d, want 2", len(comments))
	}
	doomed, surviving := comments[0], comments[1]

	if err := taskCommentRemove([]string{"-r", roadmap, itoa(doomed.ID)}); err != nil {
		t.Fatalf("comment-remove: %v", err)
	}

	left := listComments(t, database, 2)
	if len(left) != 1 || left[0].ID != surviving.ID {
		t.Fatalf("after the remove: %d comments left, want only comment %d", len(left), surviving.ID)
	}
	if _, err := database.GetTaskComment(context.Background(), doomed.ID); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("the removed comment is still readable: %v", err)
	}
	if n := countAudit(t, database, models.OpTaskCommentDelete, 2); n != 1 {
		t.Errorf("TASK_COMMENT_DELETE audit entries against task 2 = %d, want 1", n)
	}
	// The audit trail of the removed comment outlives the row.
	if n := countAudit(t, database, models.OpTaskCommentCreate, 2); n != 2 {
		t.Errorf("TASK_COMMENT_CREATE audit entries = %d, want 2 (the delete must not erase history)", n)
	}

	// Removing it a second time is a not-found condition, not a silent success.
	if err := taskCommentRemove([]string{"-r", roadmap, itoa(doomed.ID)}); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("a second remove must be exit 4; got %v", err)
	}
}

// TestTaskCommentRemove_TakesExactlyOneID verifies that a comma-separated list is
// not a valid id here: comment-remove is a single-id command, so the batch form
// accepted by `task remove` is rejected as a malformed id.
func TestTaskCommentRemove_TakesExactlyOneID(t *testing.T) {
	const roadmap = "comment-remove-single-id"
	database := setupCommentRoadmap(t, roadmap)

	addComment(t, roadmap, 1, "NOTE", "First comment, must survive.")
	addComment(t, roadmap, 1, "NOTE", "Second comment, must survive.")
	comments := listComments(t, database, 1)

	err := taskCommentRemove([]string{"-r", roadmap, itoa(comments[0].ID) + "," + itoa(comments[1].ID)})
	if err == nil {
		t.Fatal("a comma-separated id list must be rejected")
	}
	if !errors.Is(err, utils.ErrInvalidInput) {
		t.Errorf("error must wrap utils.ErrInvalidInput (exit 2); got %v", err)
	}
	if n := len(listComments(t, database, 1)); n != 2 {
		t.Errorf("comments left = %d, want 2 (nothing may be deleted)", n)
	}
}

// ---------------------------------------------------------------------------
// Audit: always the parent task, never the comment
// ---------------------------------------------------------------------------

// TestTaskComment_AuditTargetsTheParentTask pins the audit contract for all three
// mutations at once, using a fixture where the comment id and the task id differ,
// so an implementation that logged the comment's own id would fail.
func TestTaskComment_AuditTargetsTheParentTask(t *testing.T) {
	const roadmap = "comment-audit-parent"
	database := setupCommentRoadmap(t, roadmap)

	// Comment on task 2 first, so comment id 1 belongs to task 2: the two numbers
	// are different, which is what makes the assertion meaningful.
	addComment(t, roadmap, 2, "FINDING", "A finding recorded against the second task.")
	comment := listComments(t, database, 2)[0]
	if comment.ID == comment.TaskID {
		t.Fatalf("fixture is not discriminating: comment id and task id are both %d", comment.ID)
	}

	if err := taskCommentEdit([]string{"-r", roadmap, itoa(comment.ID), "--type", "DECISION"}); err != nil {
		t.Fatalf("comment-edit: %v", err)
	}
	if err := taskCommentRemove([]string{"-r", roadmap, itoa(comment.ID)}); err != nil {
		t.Fatalf("comment-remove: %v", err)
	}

	for _, op := range []models.AuditOperation{
		models.OpTaskCommentCreate, models.OpTaskCommentUpdate, models.OpTaskCommentDelete,
	} {
		if n := countAudit(t, database, op, comment.TaskID); n != 1 {
			t.Errorf("%s against the parent task %d = %d entries, want 1", op, comment.TaskID, n)
		}
		if n := countAudit(t, database, op, comment.ID); n != 0 {
			t.Errorf("%s was recorded against the comment id %d (%d entries); it must name the parent task",
				op, comment.ID, n)
		}
	}

	// entity_type is TASK for every comment operation; no SPRINT row appears.
	sprintType := string(models.EntitySprint)
	entries, err := database.GetAuditEntries(context.Background(), &db.AuditFilter{EntityType: &sprintType})
	if err != nil {
		t.Fatalf("querying SPRINT audit entries: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(string(e.Operation), "COMMENT") {
			t.Errorf("comment operation %s was recorded with entity_type SPRINT", e.Operation)
		}
	}
}

// TestTaskComment_AcceptedInEveryStatus pins that a comment is accepted whatever
// the task's status is, including COMPLETED, and that commenting never changes it.
func TestTaskComment_AcceptedInEveryStatus(t *testing.T) {
	const roadmap = "comment-any-status"
	database := setupCommentRoadmap(t, roadmap)

	// The task lifecycle is BACKLOG -> SPRINT -> DOING -> TESTING -> COMPLETED,
	// and SPRINT is reachable only through sprint add-tasks, never through
	// taskSetStatus. A sprint is therefore part of the fixture rather than an
	// aside: without it the walk cannot leave BACKLOG at all.
	_ = captureStdout(t, func() {
		if err := sprintCreate([]string{
			"-r", roadmap,
			"-t", "Expiry hardening",
			"-d", "Close the JWT boundary-second defect and lock it behind a regression test.",
		}); err != nil {
			t.Fatalf("creating the sprint fixture: %v", err)
		}
	})

	// Commenting at every stop of the walk.
	steps := []struct {
		status models.TaskStatus
		move   func() error
	}{
		{models.StatusBacklog, nil},
		{models.StatusSprint, func() error { return sprintAddTasks([]string{"-r", roadmap, "1", "1"}) }},
		{models.StatusDoing, func() error { return taskSetStatus([]string{"-r", roadmap, "1", "DOING"}) }},
		{models.StatusTesting, func() error { return taskSetStatus([]string{"-r", roadmap, "1", "TESTING"}) }},
		{models.StatusCompleted, func() error {
			return taskSetStatus([]string{"-r", roadmap, "1", "COMPLETED", "--summary", "Boundary second now expires."})
		}},
	}
	for _, s := range steps {
		if s.move != nil {
			if err := s.move(); err != nil {
				t.Fatalf("moving the task to %s: %v", s.status, err)
			}
		}

		_ = captureStdout(t, func() {
			if err := taskCommentAdd([]string{
				"-r", roadmap, "1", "--type", "PROGRESS", "--body", "Recorded while the task was " + string(s.status) + ".",
			}); err != nil {
				t.Fatalf("comment-add while %s: %v", s.status, err)
			}
		})

		task, err := database.GetTask(context.Background(), 1)
		if err != nil {
			t.Fatalf("reading the task back: %v", err)
		}
		if task.Status != s.status {
			t.Errorf("commenting changed the status: got %s, want %s", task.Status, s.status)
		}
	}

	if n := len(listComments(t, database, 1)); n != len(steps) {
		t.Errorf("stored comments = %d, want %d (one per status)", n, len(steps))
	}
}
