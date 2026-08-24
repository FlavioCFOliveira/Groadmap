// Package commands — the Comment Positional Argument Contract, read against all
// EIGHT comment subcommands at once.
//
// SPEC/COMMANDS.md § Comment Positional Argument Contract pins one rule for the
// four `task` comment subcommands and the four `sprint` ones: each takes exactly
// one positional argument, and a second or later one is REFUSED with exit code 2
// rather than ignored. Before task #184 every one of the eight accepted a stray
// positional and exited 0 — `task comment-remove 1 99` deleted comment 1 and
// reported success, because the shared flag parser COLLECTS positionals into
// ParseResult.Args and returns them without complaint, and no caller inspected
// that slice.
//
// The rule is enforced in ONE place (parseCommentArgs in comments.go) and reaches
// all eight subcommands through the four shared bodies, so this suite is one
// cross-family table rather than eight copies: a regression in the shared helper
// must fail here for every entry, and a family that stopped routing through the
// helper must fail here for its own four.
//
// What the suite asserts, beyond the error class:
//
//   - the exact pinned message, `unexpected argument "X"`, naming only the FIRST
//     leftover token in command-line order;
//   - that NOTHING happened — no comment added, changed, deleted or listed, and
//     no audit entry written — which is the part that made the defect destructive
//     rather than merely untidy;
//   - the ORDER the SPEC pins: the refusal precedes `--type` value validation
//     (so exit 2 beats what would be exit 6), precedes the read of standard
//     input, and precedes opening the roadmap database (so exit 2 beats what
//     would be exit 4);
//   - the two behaviours the contract deliberately leaves alone: a leftover token
//     beginning with `-` stays an UNKNOWN FLAG (rule 5), and `--body -1` still
//     supplies the body `-1` (rule 5's single exception);
//   - that the valid single-id forms of all eight still succeed.
package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

// commentPositionalSeed is the state every test in this file starts from: one
// task carrying one comment, and one sprint carrying one comment, both with id 1
// in their own table.
//
// The two families share a single roadmap deliberately. The contract is
// cross-family, so a test that refuses a stray positional on `task
// comment-remove` and then proves the SPRINT comment is equally untouched is
// reading one rule, not two.
type commentPositionalSeed struct {
	database *db.DB
	roadmap  string
}

// setupCommentPositionalRoadmap creates a hermetic roadmap under a temporary HOME
// and seeds the task, the sprint, and one comment on each.
//
// Hermetic on purpose: ~/.roadmaps resolves under the test's own HOME, so a run
// never touches (or depends on) the developer's real roadmaps.
func setupCommentPositionalRoadmap(t *testing.T, name string) commentPositionalSeed {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	database, cleanup := setupTestTaskRoadmap(t, name)
	t.Cleanup(cleanup)

	if err := taskCreate([]string{
		"-r", name,
		"-t", "Fix JWT boundary-second expiry",
		"-fr", "A token whose exp is the current second must be refused",
		"-tr", "Compare with !time.Now().Before(exp) instead of time.Now().After(exp)",
		"-ac", "A unit test covers the exact boundary second",
	}); err != nil {
		t.Fatalf("seeding the task: %v", err)
	}

	_ = captureStdout(t, func() {
		if err := sprintCreate([]string{
			"-r", name,
			"-t", "Expiry hardening",
			"-d", "Close the JWT boundary-second defect and lock it behind a regression test.",
		}); err != nil {
			t.Fatalf("seeding the sprint: %v", err)
		}
	})

	addComment(t, name, 1, "FINDING", seededTaskCommentBody)
	addSprintComment(t, name, 1, "DECISION", seededSprintCommentBody)

	return commentPositionalSeed{database: database, roadmap: name}
}

// The seeded bodies are named so an assertion can say "unchanged" by value rather
// than by a repeated string literal.
const (
	seededTaskCommentBody   = "time.Now().After(exp) is false at equality, so the boundary second is accepted."
	seededSprintCommentBody = "The boundary-second defect is settled in this sprint, behind a regression test."
)

// assertSeedIntact re-reads both families from the database and fails unless the
// seeded state is EXACTLY as it was left: one comment each, the original body,
// and updated_at still null.
//
// This is the assertion that makes the suite about the defect rather than about
// the error message. A refusal that still deleted, still edited, or still
// inserted would pass an error-class check and fail here.
func (s commentPositionalSeed) assertSeedIntact(t *testing.T, when string) {
	t.Helper()

	taskComments := listComments(t, s.database, 1)
	if len(taskComments) != 1 {
		t.Fatalf("%s: task 1 has %d comments, want exactly the 1 seeded", when, len(taskComments))
	}
	if taskComments[0].Body != seededTaskCommentBody {
		t.Errorf("%s: task comment body changed to %q", when, taskComments[0].Body)
	}
	if taskComments[0].UpdatedAt != nil {
		t.Errorf("%s: task comment was stamped updated_at %q, so it was edited", when, *taskComments[0].UpdatedAt)
	}

	sprintComments := listSprintComments(t, s.database, 1)
	if len(sprintComments) != 1 {
		t.Fatalf("%s: sprint 1 has %d comments, want exactly the 1 seeded", when, len(sprintComments))
	}
	if sprintComments[0].Body != seededSprintCommentBody {
		t.Errorf("%s: sprint comment body changed to %q", when, sprintComments[0].Body)
	}
	if sprintComments[0].UpdatedAt != nil {
		t.Errorf("%s: sprint comment was stamped updated_at %q, so it was edited", when, *sprintComments[0].UpdatedAt)
	}
}

// commentInvocation is one subcommand plus the argument list to hand it. The
// argument list omits the leading "-r <roadmap>", which every case shares and
// which the builders below prepend.
type commentInvocation struct {
	handler func([]string) error
	name    string
}

// allCommentSubcommands is the eight-way table this whole file iterates. Keeping
// it in one place is what makes "all eight" a fact of the suite rather than a
// claim in a comment: a ninth comment subcommand added without a positional rule
// would have to be added here too.
func allCommentSubcommands() []commentInvocation {
	return []commentInvocation{
		{name: "task comment-add", handler: taskCommentAdd},
		{name: "task comment-list", handler: taskCommentList},
		{name: "task comment-edit", handler: taskCommentEdit},
		{name: "task comment-remove", handler: taskCommentRemove},
		{name: "sprint comment-add", handler: sprintCommentAdd},
		{name: "sprint comment-list", handler: sprintCommentList},
		{name: "sprint comment-edit", handler: sprintCommentEdit},
		{name: "sprint comment-remove", handler: sprintCommentRemove},
	}
}

// tailFor returns the flags each subcommand needs after its positional id in
// order to be an OTHERWISE VALID invocation.
//
// The distinction matters: a `comment-add` without `--type` would fail with exit
// code 2 for a reason of its own, and the test would then prove nothing about the
// positional rule. Every case in this suite is one that would SUCCEED were the
// stray token removed, which is exactly the defect's shape.
func tailFor(name string) []string {
	switch name {
	case "task comment-add":
		return []string{"--type", "NOTE", "--body", "A note recorded while the parser was being read."}
	case "sprint comment-add":
		return []string{"--type", "UPDATE", "--body", "An update recorded while the parser was being read."}
	case "task comment-edit":
		return []string{"--type", "DECISION"}
	case "sprint comment-edit":
		return []string{"--type", "PROGRESS"}
	default: // both comment-list forms and both comment-remove forms take no flag here
		return nil
	}
}

// invalidTypeTailFor is tailFor with the type value replaced by one no family
// accepts, so the invocation would fail with exit code 6 if the positional rule
// did not fire first. comment-remove takes no --type at all and is therefore not
// part of the ordering-versus-exit-6 table.
func invalidTypeTailFor(name string) []string {
	switch name {
	case "task comment-add", "sprint comment-add":
		return []string{"--type", "BOGUS", "--body", "A body that must never be stored."}
	case "task comment-list", "sprint comment-list", "task comment-edit", "sprint comment-edit":
		return []string{"--type", "BOGUS"}
	default:
		return nil
	}
}

// argsFor assembles a full argument list: the shared roadmap flag, the positional
// id, whatever extra positional tokens the case supplies, and the subcommand's
// own tail.
func argsFor(roadmap, id string, extras, tail []string) []string {
	args := make([]string, 0, 4+len(extras)+len(tail))
	args = append(args, "-r", roadmap, id)
	args = append(args, extras...)
	args = append(args, tail...)
	return args
}

// assertUnexpectedArgument fails unless err is the refusal the SPEC pins: exit
// code 2 via utils.ErrInvalidInput, and the exact message naming token.
//
// It also fails when the error is one of the verdicts the refusal must PRE-EMPT,
// so a test that means "exit 2 beat exit 6" cannot pass on an error that merely
// happens to mention the right words.
func assertUnexpectedArgument(t *testing.T, err error, token string) {
	t.Helper()

	if err == nil {
		// Deliberately not fatal: the caller's own state assertions are the ones
		// that show WHAT the missing refusal did, and they must still run.
		t.Errorf("want a refusal for the extra positional argument %q, got nil", token)
		return
	}
	if !errors.Is(err, utils.ErrInvalidInput) {
		t.Errorf("error must wrap utils.ErrInvalidInput (exit 2); got %v", err)
	}
	for _, wrong := range []struct {
		sentinel error
		label    string
	}{
		{utils.ErrValidation, "utils.ErrValidation (exit 6)"},
		{utils.ErrFieldTooLarge, "utils.ErrFieldTooLarge (exit 6)"},
		{utils.ErrNotFound, "utils.ErrNotFound (exit 4)"},
		{utils.ErrNoRoadmap, "utils.ErrNoRoadmap (exit 3)"},
		{utils.ErrRequired, "utils.ErrRequired"},
	} {
		if errors.Is(err, wrong.sentinel) {
			t.Errorf("error must NOT wrap %s; got %v", wrong.label, err)
		}
	}

	want := `unexpected argument "` + token + `"`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("message must contain %q; got %q", want, err.Error())
	}
	// The graph family's parenthetical is specific to a command that takes no
	// positional at all, so it must not have been copied here.
	if strings.Contains(err.Error(), "(") {
		t.Errorf("message must carry no parenthetical; got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// The refusal itself
// ---------------------------------------------------------------------------

// TestCommentPositional_ExtraArgumentRefusedOnEverySubcommand is the core
// regression for task #184: every one of the eight comment subcommands refuses a
// stray second positional argument with exit code 2 and the pinned message, and
// leaves the seeded state exactly as it found it.
//
// Before the fix each of these eight invocations returned nil and acted on the id
// that preceded the stray token — the `comment-remove` rows deleting a comment
// and reporting success.
func TestCommentPositional_ExtraArgumentRefusedOnEverySubcommand(t *testing.T) {
	seed := setupCommentPositionalRoadmap(t, "comment-positional-refused")

	for _, sub := range allCommentSubcommands() {
		t.Run(sub.name, func(t *testing.T) {
			args := argsFor(seed.roadmap, "1", []string{"99"}, tailFor(sub.name))

			out := captureStdout(t, func() {
				assertUnexpectedArgument(t, sub.handler(args), "99")
			})
			if out != "" {
				t.Errorf("stdout must stay empty on a refusal; got %q", out)
			}

			seed.assertSeedIntact(t, sub.name)
		})
	}
}

// TestCommentPositional_RemoveDoesNotDelete is the destructive case stated as its
// own test, because it is the one the defect made silently lossy: `comment-remove
// <id> <stray>` deleted the comment named by <id> and exited 0.
//
// The proof is the comment's survival — read back from the database, not from the
// command's own output — plus the absence of the DELETE audit entry that a real
// deletion would have written.
func TestCommentPositional_RemoveDoesNotDelete(t *testing.T) {
	seed := setupCommentPositionalRoadmap(t, "comment-positional-remove")

	assertUnexpectedArgument(t, taskCommentRemove([]string{"-r", seed.roadmap, "1", "99"}), "99")
	assertUnexpectedArgument(t, sprintCommentRemove([]string{"-r", seed.roadmap, "1", "99"}), "99")

	seed.assertSeedIntact(t, "after both comment-remove refusals")

	if n := countAudit(t, seed.database, models.OpTaskCommentDelete, 1); n != 0 {
		t.Errorf("a refused task comment-remove wrote %d TASK_COMMENT_DELETE audit entries, want 0", n)
	}
	if n := countSprintAudit(t, seed.database, models.OpSprintCommentDelete, 1); n != 0 {
		t.Errorf("a refused sprint comment-remove wrote %d SPRINT_COMMENT_DELETE audit entries, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// The order the refusal happens in
// ---------------------------------------------------------------------------

// TestCommentPositional_RefusalPrecedesTypeValidation pins SPEC rule 6's first
// half: an invocation carrying BOTH an extra positional and an invalid `--type`
// value is exit code 2, not the exit code 6 the invalid type alone would produce.
//
// comment-remove is absent from the table because it accepts no `--type`; its
// ordering is covered by the database test below.
func TestCommentPositional_RefusalPrecedesTypeValidation(t *testing.T) {
	seed := setupCommentPositionalRoadmap(t, "comment-positional-order-type")

	for _, sub := range allCommentSubcommands() {
		tail := invalidTypeTailFor(sub.name)
		if tail == nil {
			continue
		}
		t.Run(sub.name, func(t *testing.T) {
			err := sub.handler(argsFor(seed.roadmap, "1", []string{"99"}, tail))
			assertUnexpectedArgument(t, err, "99")
			if strings.Contains(err.Error(), "BOGUS") {
				t.Errorf("the type value must not have been reached; got %q", err.Error())
			}
		})
	}
	seed.assertSeedIntact(t, "after the invalid-type ordering table")
}

// TestCommentPositional_RefusalPrecedesDatabase pins SPEC rule 6's second half on
// all eight: the refusal happens before the roadmap database is opened, so an
// extra positional beats an id that does not exist (exit 4) and beats a roadmap
// that does not exist (exit 4) alike.
//
// The nonexistent roadmap is the stronger of the two, because it can only be
// discovered by opening something: a refusal that still reported it would prove
// the check ran too late.
func TestCommentPositional_RefusalPrecedesDatabase(t *testing.T) {
	seed := setupCommentPositionalRoadmap(t, "comment-positional-order-db")

	for _, sub := range allCommentSubcommands() {
		t.Run(sub.name+"/unknown id", func(t *testing.T) {
			err := sub.handler(argsFor(seed.roadmap, "99999", []string{"99"}, tailFor(sub.name)))
			assertUnexpectedArgument(t, err, "99")
		})
		t.Run(sub.name+"/unknown roadmap", func(t *testing.T) {
			err := sub.handler(argsFor("no-such-roadmap", "1", []string{"99"}, tailFor(sub.name)))
			assertUnexpectedArgument(t, err, "99")
		})
	}
	seed.assertSeedIntact(t, "after the database ordering table")
}

// TestCommentPositional_RefusalPrecedesStdin pins the remaining half of rule 2:
// standard input is NOT read. The evidence is what is left in the stream
// afterwards — a command that consumed it would leave nothing behind.
//
// Only the four subcommands with a standard-input fallback can be tested this
// way; the other four never read the stream under any argument list.
func TestCommentPositional_RefusalPrecedesStdin(t *testing.T) {
	seed := setupCommentPositionalRoadmap(t, "comment-positional-order-stdin")

	const piped = "A body arriving on standard input that must never be read.\n"

	cases := []struct {
		handler func([]string) error
		name    string
		args    []string
	}{
		// --body absent, so the fallback to standard input is live.
		{name: "task comment-add", handler: taskCommentAdd, args: []string{"-r", seed.roadmap, "1", "99", "--type", "NOTE"}},
		{name: "sprint comment-add", handler: sprintCommentAdd, args: []string{"-r", seed.roadmap, "1", "99", "--type", "UPDATE"}},
		// --type absent too, which is the form comment-edit reads stdin under.
		{name: "task comment-edit", handler: taskCommentEdit, args: []string{"-r", seed.roadmap, "1", "99"}},
		{name: "sprint comment-edit", handler: sprintCommentEdit, args: []string{"-r", seed.roadmap, "1", "99"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			leftover := withStdin(t, piped, func() {
				assertUnexpectedArgument(t, tc.handler(tc.args), "99")
			})
			if leftover != piped {
				t.Errorf("standard input was read: %d of %d bytes consumed", len(piped)-len(leftover), len(piped))
			}
		})
	}
	seed.assertSeedIntact(t, "after the stdin ordering table")
}

// ---------------------------------------------------------------------------
// Which token is named, and where it may sit
// ---------------------------------------------------------------------------

// TestCommentPositional_PositionIndependentAndFirstTokenOnly pins SPEC rules 3
// and 4: the stray token is refused wherever it sits on the command line, because
// what is refused is whatever remains once the flags have been consumed; and when
// several remain, only the FIRST in command-line order is named.
func TestCommentPositional_PositionIndependentAndFirstTokenOnly(t *testing.T) {
	seed := setupCommentPositionalRoadmap(t, "comment-positional-placement")

	t.Run("before the flags", func(t *testing.T) {
		err := taskCommentAdd(argsFor(seed.roadmap, "1", []string{"99"}, tailFor("task comment-add")))
		assertUnexpectedArgument(t, err, "99")
	})

	t.Run("after the flags", func(t *testing.T) {
		args := argsFor(seed.roadmap, "1", nil, tailFor("task comment-add"))
		err := taskCommentAdd(append(args, "99"))
		assertUnexpectedArgument(t, err, "99")
	})

	t.Run("straddling the flags names the first", func(t *testing.T) {
		args := argsFor(seed.roadmap, "1", []string{"alpha"}, tailFor("sprint comment-add"))
		err := sprintCommentAdd(append(args, "omega"))
		assertUnexpectedArgument(t, err, "alpha")
		if strings.Contains(err.Error(), "omega") {
			t.Errorf("only the first leftover may be named; got %q", err.Error())
		}
	})

	t.Run("three leftovers name the first", func(t *testing.T) {
		err := taskCommentRemove([]string{"-r", seed.roadmap, "1", "alpha", "beta", "gamma"})
		assertUnexpectedArgument(t, err, "alpha")
		for _, later := range []string{"beta", "gamma"} {
			if strings.Contains(err.Error(), later) {
				t.Errorf("only the first leftover may be named; got %q", err.Error())
			}
		}
	})

	seed.assertSeedIntact(t, "after the placement table")
}

// ---------------------------------------------------------------------------
// What the contract deliberately leaves alone
// ---------------------------------------------------------------------------

// TestCommentPositional_HyphenPrefixedStaysUnknownFlag pins SPEC rule 5: on these
// subcommands a leftover token beginning with `-` is a FLAG, so it is reported as
// an unknown flag and never as an unexpected argument — digits included, which is
// deliberately unlike the graph family, where `-1` is a query value.
//
// The rule matters because the two refusals share an exit code: without this test
// the wording could drift from "unknown flag: -1" to `unexpected argument "-1"`
// and every exit-code assertion would still pass.
func TestCommentPositional_HyphenPrefixedStaysUnknownFlag(t *testing.T) {
	seed := setupCommentPositionalRoadmap(t, "comment-positional-hyphen")

	for _, sub := range allCommentSubcommands() {
		for _, token := range []string{"-1", "--foo"} {
			t.Run(sub.name+"/"+token, func(t *testing.T) {
				args := argsFor(seed.roadmap, "1", nil, tailFor(sub.name))
				err := sub.handler(append(args, token))
				if err == nil {
					t.Fatalf("want a refusal for the leftover token %q, got nil", token)
				}
				if !errors.Is(err, utils.ErrInvalidInput) {
					t.Errorf("error must wrap utils.ErrInvalidInput (exit 2); got %v", err)
				}
				if !strings.Contains(err.Error(), "unknown flag: "+token) {
					t.Errorf("a %q-prefixed leftover must be reported as an unknown flag; got %q", "-", err.Error())
				}
				if strings.Contains(err.Error(), "unexpected argument") {
					t.Errorf("a %q-prefixed leftover must NOT be reported as an unexpected argument; got %q", "-", err.Error())
				}
			})
		}
	}
	seed.assertSeedIntact(t, "after the hyphen table")
}

// TestCommentPositional_BodyMinusOneStillStored pins rule 5's single exception:
// `--body -1` is not a leftover token at all, it is the body `-1`, and it is
// stored as such. extractCommentBody consumes it before the parser ever sees it,
// which is why the surrounding rule can treat every other `-`-prefixed token as a
// flag without contradicting itself.
func TestCommentPositional_BodyMinusOneStillStored(t *testing.T) {
	seed := setupCommentPositionalRoadmap(t, "comment-positional-body-minus-one")

	_ = captureStdout(t, func() {
		if err := taskCommentAdd([]string{"-r", seed.roadmap, "1", "--type", "NOTE", "--body", "-1"}); err != nil {
			t.Fatalf("task comment-add --body -1: %v", err)
		}
	})
	_ = captureStdout(t, func() {
		if err := sprintCommentAdd([]string{"-r", seed.roadmap, "1", "--type", "UPDATE", "--body", "-1"}); err != nil {
			t.Fatalf("sprint comment-add --body -1: %v", err)
		}
	})

	if got := getComment(t, seed.database, 2).Body; got != "-1" {
		t.Errorf("task comment body = %q, want %q", got, "-1")
	}
	if got := getSprintComment(t, seed.database, 2).Body; got != "-1" {
		t.Errorf("sprint comment body = %q, want %q", got, "-1")
	}

	// The same on the edit path, where the body also reaches extractCommentBody
	// before the parser.
	if err := taskCommentEdit([]string{"-r", seed.roadmap, "1", "--body", "-1"}); err != nil {
		t.Fatalf("task comment-edit --body -1: %v", err)
	}
	if got := getComment(t, seed.database, 1).Body; got != "-1" {
		t.Errorf("edited task comment body = %q, want %q", got, "-1")
	}
}

// TestCommentPositional_SingleIDFormsUnaffected is the other side of the
// regression: the fix refuses a SECOND positional and must not have made the
// FIRST one harder to supply. Every one of the eight valid single-id forms still
// succeeds, and the two mutating pairs still take effect.
func TestCommentPositional_SingleIDFormsUnaffected(t *testing.T) {
	seed := setupCommentPositionalRoadmap(t, "comment-positional-valid-forms")

	const addedTaskBody = "A second finding, recorded through the ordinary single-id form."
	const addedSprintBody = "A second decision, recorded through the ordinary single-id form."

	// comment-add
	_ = captureStdout(t, func() {
		if err := taskCommentAdd([]string{"-r", seed.roadmap, "1", "--type", "TEST", "--body", addedTaskBody}); err != nil {
			t.Fatalf("task comment-add: %v", err)
		}
	})
	_ = captureStdout(t, func() {
		if err := sprintCommentAdd([]string{"-r", seed.roadmap, "1", "--type", "PROGRESS", "--body", addedSprintBody}); err != nil {
			t.Fatalf("sprint comment-add: %v", err)
		}
	})
	if got := len(listComments(t, seed.database, 1)); got != 2 {
		t.Fatalf("task 1 has %d comments after the add, want 2", got)
	}
	if got := len(listSprintComments(t, seed.database, 1)); got != 2 {
		t.Fatalf("sprint 1 has %d comments after the add, want 2", got)
	}

	// comment-list
	taskListing := captureStdout(t, func() {
		if err := taskCommentList([]string{"-r", seed.roadmap, "1"}); err != nil {
			t.Fatalf("task comment-list: %v", err)
		}
	})
	if !strings.Contains(taskListing, addedTaskBody) {
		t.Errorf("task comment-list omitted the added comment; got %q", taskListing)
	}
	sprintListing := captureStdout(t, func() {
		if err := sprintCommentList([]string{"-r", seed.roadmap, "1"}); err != nil {
			t.Fatalf("sprint comment-list: %v", err)
		}
	})
	if !strings.Contains(sprintListing, addedSprintBody) {
		t.Errorf("sprint comment-list omitted the added comment; got %q", sprintListing)
	}

	// comment-edit
	if err := taskCommentEdit([]string{"-r", seed.roadmap, "2", "--type", "DECISION"}); err != nil {
		t.Fatalf("task comment-edit: %v", err)
	}
	if got := getComment(t, seed.database, 2).Type; string(got) != "DECISION" {
		t.Errorf("task comment 2 type = %q, want DECISION", got)
	}
	if err := sprintCommentEdit([]string{"-r", seed.roadmap, "2", "--type", "FINDING"}); err != nil {
		t.Fatalf("sprint comment-edit: %v", err)
	}
	if got := getSprintComment(t, seed.database, 2).Type; string(got) != "FINDING" {
		t.Errorf("sprint comment 2 type = %q, want FINDING", got)
	}

	// comment-remove
	if err := taskCommentRemove([]string{"-r", seed.roadmap, "2"}); err != nil {
		t.Fatalf("task comment-remove: %v", err)
	}
	if got := len(listComments(t, seed.database, 1)); got != 1 {
		t.Errorf("task 1 has %d comments after the remove, want 1", got)
	}
	if err := sprintCommentRemove([]string{"-r", seed.roadmap, "2"}); err != nil {
		t.Fatalf("sprint comment-remove: %v", err)
	}
	if got := len(listSprintComments(t, seed.database, 1)); got != 1 {
		t.Errorf("sprint 1 has %d comments after the remove, want 1", got)
	}
}
