package commands

import (
	"errors"
	"strings"
	"testing"

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
// The `audit list` cases are deliberately included although they were never
// doubled. That command validates its two enums inline instead of calling
// models.ParseAuditOperation / models.ParseEntityType, so it built its message
// from a literal and escaped the defect. Pinning it here keeps it that way, and
// records that its wording differs from every other enum refusal: it names the
// value unquoted, and calls the operation enum "operation" rather than "audit
// operation".

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
			wantMsg:    `validation error: invalid task type: "BOGUS"`,
			wantPhrase: "invalid task type",
		},
		{
			name: "task list --type",
			run: func(r string) error {
				return HandleTask([]string{"list", "-r", r, "--type", "BOGUS"})
			},
			wantMsg:    `validation error: invalid task type: "BOGUS"`,
			wantPhrase: "invalid task type",
		},
		{
			name: "task edit --type",
			run: func(r string) error {
				return HandleTask([]string{"edit", "-r", r, "1", "-y", "BOGUS"})
			},
			wantMsg:    `validation error: invalid task type: "BOGUS"`,
			wantPhrase: "invalid task type",
		},
		{
			name: "backlog list --type",
			run: func(r string) error {
				return backlogList([]string{"-r", r, "--type", "BOGUS"})
			},
			wantMsg:    `validation error: invalid task type: "BOGUS"`,
			wantPhrase: "invalid task type",
		},
		{
			name: "task list --status",
			run: func(r string) error {
				return HandleTask([]string{"list", "-r", r, "--status", "BOGUS"})
			},
			wantMsg:    `validation error: invalid task status: "BOGUS"`,
			wantPhrase: "invalid task status",
		},
		{
			// A different call site from `task list --status`: the status
			// setter parses the target status through the same helper but
			// wraps it with a second %w rather than with %s.
			name: "task stat <id> <status>",
			run: func(r string) error {
				return HandleTask([]string{"stat", "-r", r, "1", "NOPE"})
			},
			wantMsg:    `validation error: invalid task status: "NOPE"`,
			wantPhrase: "invalid task status",
		},
		{
			name: "sprint list --status",
			run: func(r string) error {
				return HandleSprint([]string{"list", "-r", r, "--status", "BOGUS"})
			},
			wantMsg:    `validation error: invalid sprint status: "BOGUS"`,
			wantPhrase: "invalid sprint status",
		},
		{
			// Never doubled: validated inline, message built from a literal.
			name: "audit list --entity-type",
			run: func(r string) error {
				return HandleAudit([]string{"list", "-r", r, "-e", "BOGUS"})
			},
			wantMsg:    "validation error: invalid entity type: BOGUS",
			wantPhrase: "invalid entity type",
		},
		{
			// Never doubled: validated inline, message built from a literal.
			name: "audit list --operation",
			run: func(r string) error {
				return HandleAudit([]string{"list", "-r", r, "-o", "BOGUS"})
			},
			wantMsg:    "validation error: invalid operation: BOGUS",
			wantPhrase: "invalid operation",
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
