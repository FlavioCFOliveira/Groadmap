package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The gates of rule 7 of SPEC/COMMANDS.md § Comment Body Input Source and
// Precedence (rmp task 401): a `comment-edit` that carries --type and no --body
// never takes standard input as the body, and refuses a standard input that
// carries data rather than discarding it in silence. The defect this closes was
// a type-only edit that ignored a body piped to it and reported success for an
// edit it had not made.
//
// Both comment families share the rule, so every case runs against both. The
// terminal half of the rule, which needs a real pseudo-terminal, lives in
// comment_edit_type_only_terminal_linux_test.go.

// typeOnlyEditStdinLine is the published refusal, without the "Error: " prefix.
const typeOnlyEditStdinLine = "invalid input: standard input carries data, but it is not read when " +
	"--type is given; supply the new body with --body"

// typeOnlyEditFamily adapts one comment family to the cases below.
type typeOnlyEditFamily struct {
	// seed creates a roadmap holding one comment and returns its id and the
	// open roadmap database.
	seed func(t *testing.T, roadmap string) (int, *db.DB)
	// state reads the comment's type and whether updated_at is set, and the
	// number of comment-update audit entries against its parent.
	state func(t *testing.T, database *db.DB, commentID int) (typ string, updated bool, audits int)
	edit  func(args []string) error
	name  string
	// newType is a type the family accepts that differs from the seeded one.
	newType string
}

func typeOnlyEditFamilies() []typeOnlyEditFamily {
	return []typeOnlyEditFamily{
		{
			name: "task",
			seed: func(t *testing.T, roadmap string) (int, *db.DB) {
				database := setupCommentRoadmap(t, roadmap)
				addComment(t, roadmap, 2, "PROGRESS", "Boundary comparison replaced; regression test pending.")
				return listComments(t, database, 2)[0].ID, database
			},
			state: func(t *testing.T, database *db.DB, commentID int) (string, bool, int) {
				c := getComment(t, database, commentID)
				return string(c.Type), c.UpdatedAt != nil, countAudit(t, database, models.OpTaskCommentUpdate, 2)
			},
			edit:    taskCommentEdit,
			newType: "DECISION",
		},
		{
			name: "sprint",
			seed: func(t *testing.T, roadmap string) (int, *db.DB) {
				database := setupSprintCommentRoadmap(t, roadmap)
				addSprintComment(t, roadmap, 2, "PROGRESS", "Two of five tasks closed; the migration is still blocked.")
				return listSprintComments(t, database, 2)[0].ID, database
			},
			state: func(t *testing.T, database *db.DB, commentID int) (string, bool, int) {
				c := getSprintComment(t, database, commentID)
				return string(c.Type), c.UpdatedAt != nil, countSprintAudit(t, database, models.OpSprintCommentUpdate, 2)
			},
			edit:    sprintCommentEdit,
			newType: "DECISION",
		},
	}
}

// TestCommentEditTypeOnly_StdinCarryingDataIsRefused drives a type-only edit
// with a body on standard input, and with a line break alone, which is data
// too. Each is refused with the published line and exit code 2, nothing about
// the comment changes and no audit entry is written, and exactly one byte is
// consumed from the stream: the rest is never read.
func TestCommentEditTypeOnly_StdinCarryingDataIsRefused(t *testing.T) {
	for _, fam := range typeOnlyEditFamilies() {
		for _, input := range []struct{ label, content string }{
			{"a body", "The revised finding: the retry ladder needs a jittered cap.\n"},
			{"a line break alone", "\n"},
		} {
			t.Run(fam.name+"/"+input.label, func(t *testing.T) {
				roadmap := "type-only-stdin-" + fam.name + "-" + strings.ReplaceAll(input.label, " ", "-")
				id, database := fam.seed(t, roadmap)
				typeBefore, updatedBefore, auditsBefore := fam.state(t, database, id)

				var err error
				leftover := withStdin(t, input.content, func() {
					err = fam.edit([]string{"-r", roadmap, itoa(id), "--type", fam.newType})
				})
				assertPublishedRefusal(t, fam.name+" comment-edit --type with data on stdin", err,
					utils.ErrInvalidInput, 2, typeOnlyEditStdinLine)
				if leftover != input.content[1:] {
					t.Errorf("the refusal consumed %d byte(s) of standard input, want exactly one: leftover = %q",
						len(input.content)-len(leftover), leftover)
				}

				typeAfter, updatedAfter, auditsAfter := fam.state(t, database, id)
				if typeAfter != typeBefore || updatedAfter != updatedBefore {
					t.Errorf("the refused edit changed the comment: type %s -> %s, updated_at set %v -> %v",
						typeBefore, typeAfter, updatedBefore, updatedAfter)
				}
				if auditsAfter != auditsBefore {
					t.Errorf("the refused edit wrote %d audit entries, want none", auditsAfter-auditsBefore)
				}
			})
		}
	}
}

// TestCommentEditTypeOnly_RefusedBeforeTheCommentIsLookedUp pins the place of
// the check: where the body is resolved, after --type is validated and before
// the comment is looked up, so an id no comment carries is refused for the
// data, with exit code 2, and not as a missing comment.
func TestCommentEditTypeOnly_RefusedBeforeTheCommentIsLookedUp(t *testing.T) {
	for _, fam := range typeOnlyEditFamilies() {
		t.Run(fam.name, func(t *testing.T) {
			roadmap := "type-only-order-" + fam.name
			fam.seed(t, roadmap)

			var err error
			withStdin(t, "A body for a comment that does not exist.", func() {
				err = fam.edit([]string{"-r", roadmap, "99999", "--type", fam.newType})
			})
			assertPublishedRefusal(t, fam.name+" comment-edit on an unknown id", err,
				utils.ErrInvalidInput, 2, typeOnlyEditStdinLine)

			// An invalid --type is refused before standard input is considered.
			withStdin(t, "A body that is never reached.", func() {
				err = fam.edit([]string{"-r", roadmap, "99999", "--type", "NOT_A_TYPE"})
			})
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("an invalid --type with data on stdin = %v, want the type refusal (exit code 6) first", err)
			}
		})
	}
}

// TestCommentEditTypeOnly_ReadFailureIsAnIOFailure pins the fourth branch of
// rule 7: a failure of the read itself is rule 6's failure, exit code 1 and
// rule 6's line, and not a refusal of the data.
func TestCommentEditTypeOnly_ReadFailureIsAnIOFailure(t *testing.T) {
	for _, fam := range typeOnlyEditFamilies() {
		t.Run(fam.name, func(t *testing.T) {
			roadmap := "type-only-readfail-" + fam.name
			id, _ := fam.seed(t, roadmap)

			// A closed file is not a terminal, and reading it fails.
			path := filepath.Join(t.TempDir(), "stdin")
			if err := os.WriteFile(path, []byte("unread"), 0o600); err != nil {
				t.Fatalf("writing the stdin fixture: %v", err)
			}
			closed, err := os.Open(filepath.Clean(path))
			if err != nil {
				t.Fatalf("opening the stdin fixture: %v", err)
			}
			if err := closed.Close(); err != nil {
				t.Fatalf("closing the stdin fixture: %v", err)
			}

			restore := os.Stdin
			os.Stdin = closed
			editErr := fam.edit([]string{"-r", roadmap, itoa(id), "--type", fam.newType})
			os.Stdin = restore

			if !errors.Is(editErr, utils.ErrIO) || errors.Is(editErr, utils.ErrInvalidInput) {
				t.Fatalf("a failed read = %v, want utils.ErrIO (exit code 1) and not the data refusal", editErr)
			}
			const prefix = "I/O error: reading the comment body from standard input: "
			if !strings.HasPrefix(editErr.Error(), prefix) {
				t.Errorf("a failed read printed %q, want the line of rule 6 beginning %q", editErr.Error(), prefix)
			}
		})
	}
}
