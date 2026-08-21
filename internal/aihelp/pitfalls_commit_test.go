package aihelp

import (
	"strings"
	"testing"
)

// commitMandatoryTargets maps a `task stat` target status to the flag that
// SPEC/COMMANDS.md makes mandatory on the transition into it, in both spellings.
var commitMandatoryTargets = map[string][]string{
	"DOING":     {"--commit-open", "-co"},
	"COMPLETED": {"--commit-close", "-cc"},
}

// TestPitfallCorrectExamplesCarryTheMandatoryCommitFlag guards the half of the
// machine-readable contract that a registry test cannot see. A pitfall's
// CorrectExample is, by construction, the line an agent is told to copy, so a
// stale one is worse than a stale ordinary example: it teaches the mistake it
// exists to prevent. Four of them went stale the moment the commit flags became
// mandatory, and nothing failed.
//
// WrongExample is deliberately exempt: a bare transition is a legitimate thing
// for a pitfall to display as the mistake.
func TestPitfallCorrectExamplesCarryTheMandatoryCommitFlag(t *testing.T) {
	for _, p := range staticPitfalls() {
		if p.CorrectExample == "" {
			continue
		}
		if flag, target, ok := missingCommitFlag(p.CorrectExample); !ok {
			t.Errorf("pitfall %q: CorrectExample transitions to %s without %s, so following it exits 6: %s",
				p.ID, target, flag, p.CorrectExample)
		}
	}
}

// TestMissingCommitHashPitfallIsCatalogued pins the pitfall SPEC/DATA_FORMATS.md
// requires the contract to publish, so an agent reading the catalogue learns the
// rule before tripping over it.
func TestMissingCommitHashPitfallIsCatalogued(t *testing.T) {
	const want = "missing_commit_hash_on_transition"
	for _, p := range staticPitfalls() {
		if p.ID != want {
			continue
		}
		if p.CorrectExample == "" || p.WrongExample == "" {
			t.Fatalf("pitfall %q must carry both a wrong and a correct example", want)
		}
		// The whole point of the pitfall is that the agent, not rmp, obtains the
		// hash. An example that does not show where the hash comes from leaves
		// the reader to guess that rmp might supply it.
		if !strings.Contains(p.CorrectExample, "git rev-parse") {
			t.Errorf("pitfall %q: CorrectExample should show the agent obtaining the hash itself, got %q",
				want, p.CorrectExample)
		}
		return
	}
	t.Fatalf("pitfall %q is absent from the catalogue; SPEC/DATA_FORMATS.md requires it", want)
}

// missingCommitFlag reports ok=false, with the offending flag and target status,
// when cmdLine drives `task stat` into a status that mandates a commit flag and
// omits it.
func missingCommitFlag(cmdLine string) (flag, target string, ok bool) {
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

// isTaskStatInvocation tolerates a leading shell assignment such as the
// `result=$(rmp ...)` form the catalogue uses.
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
		if strings.Trim(f, "()\"';") == want {
			return true
		}
	}
	return false
}
