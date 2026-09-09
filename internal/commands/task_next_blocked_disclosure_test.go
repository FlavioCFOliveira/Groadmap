// Package commands — the disclosure `task next` owes its caller.
//
// # The claim being corrected
//
// `task next` answers "what do I do next" by reading sprint membership and
// task status, and nothing else. A task whose declared dependencies are not
// yet COMPLETED is returned exactly like any other, at its own planned
// rank, and nothing in the returned object marks it as blocked. The refusal
// arrives later and from a different command: the transition to COMPLETED
// is rejected with exit code 6 while a declared dependency is still open
// (SPEC/STATE_MACHINE.md § Dependency Guard).
//
// A caller who is not told this assumes the opposite — that a listing named
// "next" returns work that can be started AND finished — takes the first
// task, and learns otherwise at the last transition, with the work already
// done. SPEC/HELP.md § Task family help specifics therefore requires the
// statement in BOTH the plain-text help and the machine-readable contract,
// and requires both to name `task blockers` as the command that answers
// what this one does not, so the reader is left with a route and not only
// with a warning.
//
// # Why both surfaces are gated, and separately
//
// They have different readers and neither is derived from the other. A
// human reads the help printer's body; an agent driving the CLI from the
// contract reads no other prose about this subcommand, so a description
// that describes only the ordering is the description that produced the
// wrong belief. A gate on one surface would let the other lose the
// sentence silently.
package commands

import (
	"bytes"
	"strings"
	"testing"
)

// requiredDisclosure is what each surface must convey, expressed as the
// substrings a statement of it cannot avoid. Each case names WHY it is
// required, so a future edit that removes one is told what it is breaking
// rather than only that a string went missing.
var requiredDisclosure = []struct {
	needle string
	why    string
}{
	{
		needle: "task blockers",
		why: "the reader must be left with the command that answers the readiness question this one " +
			"does not (SPEC/HELP.md § Task family help specifics)",
	},
	{
		needle: "COMPLETED",
		why: "the statement has to name the status a dependency must reach before the blocked task can " +
			"be closed; without it the reader cannot tell what 'blocked' means here",
	},
}

// TestTaskNextHelp_DisclosesThatBlockedTasksAreReturned gates the
// plain-text help body.
func TestTaskNextHelp_DisclosesThatBlockedTasksAreReturned(t *testing.T) {
	var buf bytes.Buffer
	WriteHelpBodyTo(&buf, printTaskNextHelp)
	body := buf.String()

	if strings.TrimSpace(body) == "" {
		t.Fatal("task next help body is empty; every assertion below would pass vacuously")
	}

	// The negative claim itself: the listing does NOT filter on
	// dependencies. Accepted in either of the two ways the surfaces word
	// it, because the help and the contract are prose written for different
	// readers and are not required to agree letter for letter.
	if !strings.Contains(body, "NOT filtered by dependencies") &&
		!strings.Contains(body, "not filtered by dependencies") {
		t.Errorf("the task next help does not state that the listing is not filtered by dependencies.\n"+
			"A caller who is not told assumes the opposite, starts a blocked task, and is refused only at "+
			"the transition to COMPLETED.\nbody:\n%s", body)
	}

	for _, want := range requiredDisclosure {
		if !strings.Contains(body, want.needle) {
			t.Errorf("the task next help does not mention %q: %s", want.needle, want.why)
		}
	}
}

// TestTaskNextContract_DescriptionDisclosesThatBlockedTasksAreReturned
// gates the registry description, which is what the AI Agent Contract
// publishes verbatim for this subcommand. It reads the live registry rather
// than a copy, so the value under test is the one the emitter serialises.
func TestTaskNextContract_DescriptionDisclosesThatBlockedTasksAreReturned(t *testing.T) {
	cmd := AppRegistry().FindCommand("task")
	if cmd == nil {
		t.Fatal("no task family in the registry")
	}
	sub := cmd.FindSubcommand("next")
	if sub == nil {
		t.Fatal("no next subcommand under task in the registry")
	}
	description := sub.Description
	if strings.TrimSpace(description) == "" {
		t.Fatal("task next publishes an empty description")
	}

	if !strings.Contains(description, "NOT filtered by dependencies") &&
		!strings.Contains(description, "not filtered by dependencies") {
		t.Errorf("the task next contract description does not state that the listing is not filtered by "+
			"dependencies. An agent driving the CLI from the contract reads no other prose about this "+
			"subcommand, so a description that describes only the ordering is the description that "+
			"produced the wrong belief.\ndescription = %q", description)
	}

	for _, want := range requiredDisclosure {
		if !strings.Contains(description, want.needle) {
			t.Errorf("the task next contract description does not mention %q: %s", want.needle, want.why)
		}
	}

	// `task blockers` must be a command that exists, or the route the
	// statement offers goes nowhere.
	if cmd.FindSubcommand("blockers") == nil {
		t.Error("the disclosure names `task blockers`, which the registry does not resolve")
	}
}
