package commands

import (
	"strings"
	"testing"
)

// commitMandatoryTargets maps a `task stat` target status to the flag that
// SPEC/COMMANDS.md makes mandatory on the transition into it. Both spellings of
// each flag are listed because the contract and the pitfall catalogue are free
// to use either.
var commitMandatoryTargets = map[string][]string{
	"DOING":     {"--commit-open", "-co"},
	"COMPLETED": {"--commit-close", "-cc"},
}

// TestContractExamplesCarryTheMandatoryCommitFlag is the guard whose absence
// let the AI contract drift out of step with the binary. Before it existed, the
// `task stat` entry advertised `rmp task stat -r myproject 1 DOING` with
// `Exit: 0` for as long as it took someone to notice — an example the binary
// rejects with exit 6, handed to agents as the way to start a task.
//
// Running every example against a live binary would be the stronger guard, but
// the examples name a roadmap and task ids that do not exist, so a literal run
// exits 4 (not found) rather than 0 and proves nothing about the flags. This
// test instead asserts the property that actually broke: every example, and
// every pitfall CorrectExample, that drives `task stat` into DOING or COMPLETED
// and claims success must carry the flag that transition requires.
func TestContractExamplesCarryTheMandatoryCommitFlag(t *testing.T) {
	reg := AppRegistry()
	for _, cmd := range reg.Commands {
		for _, sub := range cmd.Subcommands {
			for _, ex := range sub.Examples {
				if ex.Exit != 0 {
					// A deliberately-rejected example is exactly where a bare
					// transition belongs; skipping them keeps the guard from
					// forbidding the counter-examples the contract needs.
					continue
				}
				if flag, target, ok := missingCommitFlag(ex.Cmd); !ok {
					t.Errorf("%s %s: example %q claims Exit 0 but transitions to %s without %s; the binary rejects it with exit 6",
						cmd.Name, sub.Name, ex.Title, target, flag)
				}
			}
		}
	}
}

// missingCommitFlag reports whether cmdLine is free of the defect. It returns
// ok=false together with the offending flag and target status when the line
// drives `task stat` into a status that mandates a commit flag and omits it.
func missingCommitFlag(cmdLine string) (flag, target string, ok bool) {
	// An example may chain several commands with &&; each is checked on its own,
	// because only one of them may be the offending transition.
	for _, part := range strings.Split(cmdLine, "&&") {
		fields := strings.Fields(part)
		if !isTaskStatInvocation(fields) {
			continue
		}
		for status, spellings := range commitMandatoryTargets {
			if !containsField(fields, status) {
				continue
			}
			present := false
			for _, spelling := range spellings {
				if containsField(fields, spelling) {
					present = true
					break
				}
			}
			if !present {
				return spellings[0], status, false
			}
		}
	}
	return "", "", true
}

// isTaskStatInvocation reports whether fields is an `rmp task stat` (or its
// set-status alias) invocation, tolerating a leading shell assignment such as
// the `result=$(rmp ...)` form the pitfall catalogue uses.
func isTaskStatInvocation(fields []string) bool {
	for i := 0; i+2 < len(fields); i++ {
		if !strings.HasSuffix(fields[i], "rmp") {
			continue
		}
		if fields[i+1] != "task" {
			continue
		}
		if fields[i+2] == "stat" || fields[i+2] == "set-status" {
			return true
		}
	}
	return false
}

func containsField(fields []string, want string) bool {
	for _, f := range fields {
		// Trim the shell noise the pitfall examples carry, so `DOING)` from a
		// `$(...)` substitution still matches the bare status.
		if strings.Trim(f, "()\"';") == want {
			return true
		}
	}
	return false
}
