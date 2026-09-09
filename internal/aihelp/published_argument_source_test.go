// Package aihelp — gate on where a published invocation gets its arguments.
//
// SPEC/DATA_FORMATS.md § Published Examples Are Executed states the rule
// this file enforces: "A published example takes its arguments from the
// contract, not from another program." No published invocation may obtain
// an argument by running something other than `rmp`.
//
// The rule exists because an example built that way is not a claim about
// `rmp` at all. `rmp task stat -r myproject 42 DOING --commit-open $(git
// rev-parse HEAD)` succeeds or fails on whether the machine running it
// happens to stand inside a git checkout, so an execution gate asserting it
// would be asserting the behaviour of git. It was published exactly so, and
// this file is what stops it coming back.
//
// The rule bears on where an argument comes from, and not on shell syntax as
// such. Two things stay legal and are asserted to stay legal below:
//
//   - an operator that sequences, backgrounds or pipes one `rmp` invocation
//     beside another (`&&`, `&`, `;`, `|`), because the outcome is still
//     determined by `rmp` alone;
//   - a substitution that captures the output of `rmp` itself, which is how
//     a pitfall about parsing stdout demonstrates the mistake it names.
package aihelp

import (
	"regexp"
	"strings"
	"testing"
)

// substitutionForms finds every shell construct that runs a command and
// feeds its output back onto the command line: the modern `$( … )` form and
// the legacy backquoted one. The body is captured so the gate can ask which
// program the substitution runs.
var substitutionForms = []*regexp.Regexp{
	regexp.MustCompile(`\$\(([^()]*)\)`),
	regexp.MustCompile("`([^`]*)`"),
}

// substitutionRunsRmp reports whether body — the text inside one
// substitution — invokes `rmp` and nothing else. A substitution whose first
// word is `rmp` captures this CLI's own output, which the rule permits.
func substitutionRunsRmp(body string) bool {
	fields := strings.Fields(body)
	return len(fields) > 0 && fields[0] == "rmp"
}

// TestGenerate_NoPublishedInvocationTakesAnArgumentFromAnotherProgram sweeps
// every invocation the contract publishes — subcommand examples, workflow
// steps, and both pitfall examples — and fails on a substitution that runs
// anything but `rmp`.
//
// It reads the EMITTED contract rather than the Go literals, so an
// invocation that reaches a reader through any of the three surfaces is
// covered whether or not it was written in pitfalls.go.
func TestGenerate_NoPublishedInvocationTakesAnArgumentFromAnotherProgram(t *testing.T) {
	published := contractCommandStrings(t)
	// A floor: the sweep is over a corpus whose size is a fact worth
	// knowing when the traversal quietly stops finding anything.
	if len(published) < 150 {
		t.Fatalf("only %d published invocations reached the sweep; the traversal is broken and every "+
			"assertion below is vacuous", len(published))
	}

	for _, cc := range published {
		for _, form := range substitutionForms {
			for _, match := range form.FindAllStringSubmatch(cc.cmd, -1) {
				if substitutionRunsRmp(match[1]) {
					continue
				}
				t.Errorf(
					"%s takes an argument from a program other than rmp: %q. A published example is a "+
						"claim about rmp, and this one succeeds or fails on the behaviour of something "+
						"else; publish a literal in its place and say in the prose beside it where a "+
						"caller obtains such a value.\n  command: %s",
					cc.label, match[0], cc.cmd,
				)
			}
		}
	}
}

// TestGenerate_TheRuleAdmitsWhatItIsMeantToAdmit keeps the gate above from
// becoming a ban on shell syntax. Both constructs the SPEC names as legal
// are asserted to pass the same predicate the sweep applies, so a later
// tightening that outlawed them would fail here rather than silently
// narrowing the corpus.
func TestGenerate_TheRuleAdmitsWhatItIsMeantToAdmit(t *testing.T) {
	admitted := []string{
		`rmp sprint start -r myproject 7 && rmp task next -r myproject`,
		`rmp graph serve -r myproject & rmp graph client -r myproject --query "RETURN 1"`,
		`cat decision.txt | rmp task comment-add -r myproject 42 --type DECISION`,
		`rmp task edit -r myproject $(rmp task next -r myproject) -p 8`,
	}
	for _, cmd := range admitted {
		for _, form := range substitutionForms {
			for _, match := range form.FindAllStringSubmatch(cmd, -1) {
				if !substitutionRunsRmp(match[1]) {
					t.Errorf("the rule refuses %q inside %q, but the SPEC admits it", match[0], cmd)
				}
			}
		}
	}

	refused := []string{
		`rmp task stat -r myproject 42 DOING --commit-open $(git rev-parse HEAD)`,
		"rmp task stat -r myproject 42 DOING --commit-open `git rev-parse HEAD`",
	}
	for _, cmd := range refused {
		found := false
		for _, form := range substitutionForms {
			for _, match := range form.FindAllStringSubmatch(cmd, -1) {
				if !substitutionRunsRmp(match[1]) {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("the rule admits %q, which takes an argument from git; the sweep above would then "+
				"pass the very example the rule was written for", cmd)
		}
	}
}
