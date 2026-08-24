package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The model-level counterpart of this file is
// internal/models/error_message_dedup_test.go, which pins the rejection texts
// at the point they are built. This file pins what the USER actually reads,
// which is a different string: a command that delegates to a models.Parse*
// helper re-wraps its error in utils.ErrValidation, so the rendered line is
//
//	validation error: invalid task type: "BOGUS"
//
// and cmd/rmp/main.go prints it as `Error: ` + that.
//
// The defect being guarded: the wrapped model error used to restate its own
// sentinel, so the line read
//
//	Error: validation error: invalid task type: "BOGUS": invalid task type
//
// with the sentinel printed twice. Asserting the full string here — rather than
// strings.Contains, which passes either way and is why the doubling survived so
// long — is what makes a relapse fail.
//
// The `audit` cases were never doubled, because that command used to validate
// its two enums inline — calling models.IsValidAuditOperation /
// models.IsValidEntityType and building the message from a literal — and so
// escaped the defect by escaping the shared code path entirely. The cost was a
// second convention: it named the rejected value unquoted and called the
// operation enum "operation" rather than "audit operation", so a user met two
// different renderings of the same class of mistake depending on which command
// they had typed, and models.ParseAuditOperation / models.ParseEntityType sat
// exported with no caller at all.
//
// `audit list` and `audit history` now reach their enums through those parsers
// like every other command, so the audit cases below are no longer exceptions:
// they carry the quoted value and the full enum name, and they are pinned here
// on exactly the same terms as the rest. The structural guard that keeps them
// there — rather than these literals alone — is
// internal/commands/audit_enum_owner_test.go.

// enumRejection is one command invocation that must be refused for an enum
// value, together with the complete line the user sees.
type enumRejection struct {
	name string
	// run performs the invocation against the roadmap named by the argument.
	run func(roadmap string) error
	// wantMsg is the complete rendered error, asserted with ==.
	wantMsg string
	// wantPhrase is the enum-naming clause that must appear exactly once.
	wantPhrase string
	// wantSentinel is the enum-specific sentinel that models.Parse* raised.
	// It must stay reachable through the command's wrap, so that a caller can
	// DISCRIMINATE which enum was rejected rather than merely classify the
	// failure as invalid input. See TestEnumRejectionsCarrySpecificSentinel.
	wantSentinel error
}

// enumRejectionCases is every command path that refuses an enum value.
func enumRejectionCases() []enumRejection {
	return []enumRejection{
		{
			name: "task create --type",
			run: func(r string) error {
				return HandleTask([]string{
					"create", "-r", r,
					"-t", "Harden the audit writer",
					"-fr", "Reject an entry whose entity id is not positive",
					"-tr", "Enforce the rule in AuditEntry.Validate",
					"-ac", "A non-positive entity id is refused with exit 6",
					"-y", "BOGUS",
				})
			},
			wantMsg:      `validation error: invalid task type: "BOGUS"`,
			wantPhrase:   "invalid task type",
			wantSentinel: models.ErrInvalidTaskType,
		},
		{
			name: "task list --type",
			run: func(r string) error {
				return HandleTask([]string{"list", "-r", r, "--type", "BOGUS"})
			},
			wantMsg:      `validation error: invalid task type: "BOGUS"`,
			wantPhrase:   "invalid task type",
			wantSentinel: models.ErrInvalidTaskType,
		},
		{
			name: "task edit --type",
			run: func(r string) error {
				return HandleTask([]string{"edit", "-r", r, "1", "-y", "BOGUS"})
			},
			wantMsg:      `validation error: invalid task type: "BOGUS"`,
			wantPhrase:   "invalid task type",
			wantSentinel: models.ErrInvalidTaskType,
		},
		{
			name: "backlog list --type",
			run: func(r string) error {
				return backlogList([]string{"-r", r, "--type", "BOGUS"})
			},
			wantMsg:      `validation error: invalid task type: "BOGUS"`,
			wantPhrase:   "invalid task type",
			wantSentinel: models.ErrInvalidTaskType,
		},
		{
			name: "task list --status",
			run: func(r string) error {
				return HandleTask([]string{"list", "-r", r, "--status", "BOGUS"})
			},
			wantMsg:      `validation error: invalid task status: "BOGUS"`,
			wantPhrase:   "invalid task status",
			wantSentinel: models.ErrInvalidTaskStatus,
		},
		{
			// A different call site from `task list --status`: the status
			// setter parses the target status through the same helper but
			// wraps it with a second %w rather than with %s.
			name: "task stat <id> <status>",
			run: func(r string) error {
				return HandleTask([]string{"stat", "-r", r, "1", "NOPE"})
			},
			wantMsg:      `validation error: invalid task status: "NOPE"`,
			wantPhrase:   "invalid task status",
			wantSentinel: models.ErrInvalidTaskStatus,
		},
		{
			// A third --status surface, distinct from `task list --status` and
			// from `task stat`: it belongs to a sprint subcommand and parses
			// the flag after a positional sprint id. The sweep for task #290
			// found it wrapping with %s like the rest, so it is pinned here.
			name: "sprint tasks <id> --status",
			run: func(r string) error {
				return HandleSprint([]string{"tasks", "-r", r, "1", "--status", "BOGUS"})
			},
			wantMsg:      `validation error: invalid task status: "BOGUS"`,
			wantPhrase:   "invalid task status",
			wantSentinel: models.ErrInvalidTaskStatus,
		},
		{
			name: "sprint list --status",
			run: func(r string) error {
				return HandleSprint([]string{"list", "-r", r, "--status", "BOGUS"})
			},
			wantMsg:      `validation error: invalid sprint status: "BOGUS"`,
			wantPhrase:   "invalid sprint status",
			wantSentinel: models.ErrInvalidSprintStatus,
		},
		{
			name: "audit list --entity-type",
			run: func(r string) error {
				return HandleAudit([]string{"list", "-r", r, "-e", "BOGUS"})
			},
			wantMsg:      `validation error: invalid entity type: "BOGUS"`,
			wantPhrase:   "invalid entity type",
			wantSentinel: models.ErrInvalidEntityType,
		},
		{
			name: "audit list --operation",
			run: func(r string) error {
				return HandleAudit([]string{"list", "-r", r, "-o", "BOGUS"})
			},
			wantMsg:      `validation error: invalid audit operation: "BOGUS"`,
			wantPhrase:   "invalid audit operation",
			wantSentinel: models.ErrInvalidAuditOperation,
		},
		{
			// The second audit surface: a positional, not a flag. It shares the
			// entity-type enum with `audit list -e`, so it must share the
			// wording too — that equality is what broke when the two paths
			// validated independently.
			name: "audit history <entity-type>",
			run: func(r string) error {
				return HandleAudit([]string{"history", "-r", r, "BOGUS", "1"})
			},
			wantMsg:      `validation error: invalid entity type: "BOGUS"`,
			wantPhrase:   "invalid entity type",
			wantSentinel: models.ErrInvalidEntityType,
		},
	}
}

// TestEnumRejectionMessagesAreExact pins the user-visible refusal of every
// command path that rejects an enum flag value, and asserts each one still
// chains utils.ErrValidation so cmd/rmp/main.go keeps mapping it to exit code 6
// (SPEC/ARCHITECTURE.md). Message and exit code are asserted together because
// this fix changed the message and must not change the code.
func TestEnumRejectionMessagesAreExact(t *testing.T) {
	roadmap := "testenumdedup"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	for _, tc := range enumRejectionCases() {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(roadmap)
			if err == nil {
				t.Fatalf("want a rejection, got nil")
			}
			if got := err.Error(); got != tc.wantMsg {
				t.Errorf("rendered error differs\n got: %q\nwant: %q", got, tc.wantMsg)
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("error must wrap utils.ErrValidation so handleError returns exit 6; got %v", err)
			}
		})
	}
}

// TestEnumRejectionsNamePhraseOnce states the property behind the exact
// assertions directly: the clause naming the rejected enum appears once in the
// line, never twice. It keeps holding if the surrounding wording is later
// changed for an unrelated reason — which the == assertions, being exact,
// cannot do on their own.
func TestEnumRejectionsNamePhraseOnce(t *testing.T) {
	roadmap := "testenumdedupphrase"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	for _, tc := range enumRejectionCases() {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(roadmap)
			if err == nil {
				t.Fatalf("want a rejection, got nil")
			}
			if n := strings.Count(err.Error(), tc.wantPhrase); n != 1 {
				t.Errorf("%q must appear exactly once in the line the user reads, appeared %d times\n line: %q",
					tc.wantPhrase, n, err.Error())
			}
		})
	}
}
