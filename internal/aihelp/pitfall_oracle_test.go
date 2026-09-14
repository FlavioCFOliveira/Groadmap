// Package aihelp — gates on the pitfall oracle fields.
//
// # What these fields are for
//
// A pitfall used to publish only a wrong example and a correct one. That
// pair has no oracle: a gate reading it could assert "the wrong example
// failed" and nothing more, so an example that failed for an unrelated
// reason passed — which is exactly what `complete_with_open_dependencies`
// did. Its wrong example never reached the dependency guard it exists to
// demonstrate; it was refused first for a missing commit hash, teaching
// the reader a rejection that has nothing to do with the pitfall and
// leaving them believing the dependency guard produced it.
//
// `wrong_exit` and `wrong_stderr` are that oracle (SPEC/DATA_FORMATS.md
// § A wrong_example MUST fail for the reason the pitfall names, and
// § Published Examples Are Executed). Both are MEASURED against the
// compiled binary.
//
// # What this file can and cannot check
//
// It cannot run the binary — that is the end-to-end gate the SPEC's
// § Published Examples Are Executed describes, and it lives in the E2E
// suite. What it can do is stop the fields from silently reverting to the
// state that made them necessary: a missing key, an exit code nobody
// published, a stderr line that has lost the shape a published error line
// has, or the named entry losing the commit hash that is what makes its
// example reach the guard at all.
package aihelp

import (
	"strings"
	"testing"
)

// pitfallEntries reads the pitfalls array out of the emitted contract.
func pitfallEntries(t *testing.T) []map[string]any {
	t.Helper()

	m := unmarshalAsMap(t, generateOrFatal(t, ScopeAll()))
	raw, ok := m["pitfalls"].([]any)
	if !ok || len(raw) == 0 {
		t.Fatalf("pitfalls is missing or empty: %v", m["pitfalls"])
	}
	out := make([]map[string]any, 0, len(raw))
	for i, rawEntry := range raw {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			t.Fatalf("pitfalls[%d] is not an object: %v", i, rawEntry)
		}
		out = append(out, entry)
	}
	return out
}

// pitfallByID indexes the catalogue so a case can name the entry it means
// instead of trusting a position that a later insertion would move.
func pitfallByID(t *testing.T, id string) map[string]any {
	t.Helper()

	for _, entry := range pitfallEntries(t) {
		if got, _ := entry["id"].(string); got == id {
			return entry
		}
	}
	t.Fatalf("no pitfall carries id %q", id)
	return nil
}

// TestGenerate_EveryPitfallCarriesItsOracle is the presence gate: both
// fields exist on every entry, with the published types, and `wrong_exit`
// names a code the contract's own catalogue carries. A pitfall that
// published neither would leave a gate with nothing to compare against,
// which is the state these fields were added to end.
func TestGenerate_EveryPitfallCarriesItsOracle(t *testing.T) {
	catalogue := catalogueCodes(t)

	entries := pitfallEntries(t)
	// The catalogue is curated, so its size is a fact worth knowing when a
	// gate over it stops finding anything. The floor is loose on purpose.
	if len(entries) < 15 {
		t.Fatalf("only %d pitfalls; the traversal is broken and every assertion over it is vacuous", len(entries))
	}

	for i, entry := range entries {
		id, _ := entry["id"].(string)
		if id == "" {
			t.Errorf("pitfalls[%d] carries no id", i)
			continue
		}

		exit, ok := entry["wrong_exit"]
		if !ok {
			t.Errorf("%s: wrong_exit is missing; without it a gate can assert only that the wrong example "+
				"failed, which passes an example that fails for the wrong reason", id)
		} else if code, ok := exit.(float64); !ok {
			t.Errorf("%s: wrong_exit is not a number: %v", id, exit)
		} else if !catalogue[int(code)] {
			t.Errorf("%s: wrong_exit is %d, which the contract's top-level exit_codes catalogue does not "+
				"publish", id, int(code))
		}

		if _, ok := entry["wrong_stderr"]; !ok {
			t.Errorf("%s: wrong_stderr is missing", id)
		} else if _, ok := entry["wrong_stderr"].(string); !ok {
			t.Errorf("%s: wrong_stderr is not a string: %v", id, entry["wrong_stderr"])
		}

		for _, k := range []string{"description", "wrong_example", "correct_example", "reference"} {
			if v, _ := entry[k].(string); strings.TrimSpace(v) == "" {
				t.Errorf("%s: %s is empty", id, k)
			}
		}
	}
}

// TestGenerate_PitfallStderrMatchesItsExitCode ties the two oracle fields
// to each other. A refused invocation writes a line that begins with the
// published `Error: ` prefix (SPEC/COMMANDS.md § Published Error Strings
// Are Exact); an invocation that is not refused writes no line at all. A
// pitfall claiming a non-zero exit with an empty stderr, or a zero exit
// with a stderr line, has one of the two fields wrong, and which one is
// wrong is not knowable from here — but that they disagree is.
//
// A zero `wrong_exit` is legal and deliberate for the entries whose
// mistake is a wrong BELIEF about a command that succeeds: nothing refuses
// `MATCH (n:Spec) DETACH DELETE n`, and that is the lesson. Those entries
// are identified by their own measured exit code rather than by a list
// here, so a pitfall that changes class changes what is asserted about it
// without anyone editing this file.
func TestGenerate_PitfallStderrMatchesItsExitCode(t *testing.T) {
	refused, unrefused := 0, 0

	for _, entry := range pitfallEntries(t) {
		id, _ := entry["id"].(string)
		exit, ok := entry["wrong_exit"].(float64)
		if !ok {
			continue // reported by the presence gate
		}
		stderr, ok := entry["wrong_stderr"].(string)
		if !ok {
			continue // reported by the presence gate
		}

		if exit == 0 {
			unrefused++
			if stderr != "" {
				t.Errorf("%s: wrong_exit is 0 but wrong_stderr is %q; an invocation that is not refused "+
					"writes no error line", id, stderr)
			}
			continue
		}

		refused++
		if stderr == "" {
			t.Errorf("%s: wrong_exit is %d but wrong_stderr is empty; a refused invocation writes a line, "+
				"and the line is the half of the oracle that tells a WRONG failure from the right one",
				id, int(exit))
			continue
		}
		if !strings.HasPrefix(stderr, "Error: ") {
			t.Errorf("%s: wrong_stderr = %q, which does not begin with the published %q prefix; the field "+
				"carries the COMPLETE first line of stderr, prefix and sentinel included", id, stderr, "Error: ")
		}
		if strings.Contains(stderr, "\n") {
			t.Errorf("%s: wrong_stderr carries a newline; the field is the FIRST line and only that: %q", id, stderr)
		}
	}

	// Both classes must occur, or one arm of the gate above is untested.
	if refused == 0 {
		t.Error("no pitfall publishes a non-zero wrong_exit; the refusal arm of this gate is then vacuous")
	}
	if unrefused == 0 {
		t.Error("no pitfall publishes a zero wrong_exit; the arm that permits a mistake nothing refuses is " +
			"then vacuous, and a later entry of that class has no precedent to follow")
	}
}

// unrefusedPitfalls names, individually and with its reason, every pitfall
// whose wrong example is NOT refused by the binary. Each teaches a wrong
// BELIEF about an invocation that succeeds rather than a rejection, so its
// measured oracle is exit 0 with no stderr line.
//
// The list exists because a zero `wrong_exit` is indistinguishable, from
// the JSON alone, from an entry that lost its oracle: a Go struct whose two
// fields were never populated publishes exit 0 and an empty line, which
// reads as "nothing refuses this". Naming the four that genuinely belong to
// that class turns the silence into a failure — an entry that drops its
// measured oracle joins the class and is reported here, because it is not
// on the list.
//
// A category-wide exemption would be the wrong instrument for the same
// reason the SPEC forbids one for the execution gate: it grows to cover the
// cases nobody looked at.
var unrefusedPitfalls = map[string]string{
	"parse_modification_stdout": "the mistake is parsing the stdout of a command that succeeds and " +
		"deliberately prints nothing; the invocation is accepted",
	"parse_comment_mutation_stdout": "same shape: comment-edit succeeds and prints nothing, and the " +
		"mistake is expecting a body",
	"graph_statement_is_not_checked": "the lesson IS that nothing refuses the statement; a DETACH DELETE " +
		"sent to a running server is executed and committed",
	"graph_schema_two_statements_in_one_query": "the schema parser discards the trailing clause without " +
		"an error and without a notification, and the command prints {\"ok\": true} and exits 0",
}

// TestGenerate_UnrefusedPitfallsAreNamed holds the class above closed in
// both directions: an entry that publishes exit 0 without being on the list
// has probably lost its measured oracle, and an entry on the list that now
// publishes a refusal is a stale name nobody will re-examine.
func TestGenerate_UnrefusedPitfallsAreNamed(t *testing.T) {
	observed := map[string]bool{}

	for _, entry := range pitfallEntries(t) {
		id, _ := entry["id"].(string)
		exit, ok := entry["wrong_exit"].(float64)
		if !ok || exit != 0 {
			continue
		}
		observed[id] = true
		if _, named := unrefusedPitfalls[id]; !named {
			t.Errorf("%s publishes wrong_exit 0 and is not one of the pitfalls whose wrong example is "+
				"knowingly accepted by the binary. Either its measured oracle was never populated — an "+
				"unset pair reads exactly like this — or it is a new entry of that class and belongs in "+
				"unrefusedPitfalls with the reason it is there.", id)
		}
	}

	for id, why := range unrefusedPitfalls {
		if !observed[id] {
			t.Errorf("%s is listed as a pitfall nothing refuses (%s) but no longer publishes wrong_exit 0; "+
				"remove the entry rather than leaving it to cover nothing", id, why)
		}
	}
}

// TestGenerate_CompleteWithOpenDependenciesReachesTheGuard pins the entry
// the SPEC corrects by name. Its wrong example is refused THREE times over
// on the way to the guard it means to show, and only the third refusal is
// the pitfall's own:
//
//  1. without --commit-close, "--commit-close is required when
//     transitioning to COMPLETED";
//  2. from any status but TESTING, an illegal-transition refusal;
//  3. and only then, with a hash and from TESTING, the dependency guard.
//
// So the example MUST carry a literal hash, and the description MUST state
// the status the example starts from and the open dependency — a single
// command line cannot establish state it does not create, so without the
// stated precondition neither a reader nor a gate can put the example
// where it fails as advertised (SPEC/DATA_FORMATS.md § A wrong_example
// MUST fail for the reason the pitfall names, rule 2).
func TestGenerate_CompleteWithOpenDependenciesReachesTheGuard(t *testing.T) {
	entry := pitfallByID(t, "complete_with_open_dependencies")

	wrong, _ := entry["wrong_example"].(string)
	if !strings.Contains(wrong, "--commit-close ") {
		t.Errorf("wrong_example = %q; without a literal --commit-close hash the invocation is refused for "+
			"the missing flag and never reaches the dependency guard this pitfall is about", wrong)
	}
	// A hash, not a placeholder: a `<hash>` token is refused as a malformed
	// commit hash, which is a fourth rejection that is not this one either.
	if strings.Contains(wrong, "<hash>") {
		t.Errorf("wrong_example = %q; `<hash>` is not a commit hash and is refused as malformed, which is "+
			"again not the refusal the pitfall names", wrong)
	}

	description, _ := entry["description"].(string)
	if !strings.Contains(description, "TESTING") {
		t.Errorf("description does not state the status the example starts from; COMPLETED is legal only "+
			"from TESTING, so from anywhere else the example is refused as an illegal transition. "+
			"description = %q", description)
	}

	// The oracle itself: this is the line the guard prints, and the ids in
	// it are the ids the example names.
	stderr, _ := entry["wrong_stderr"].(string)
	if !strings.Contains(stderr, "incomplete dependencies") {
		t.Errorf("wrong_stderr = %q, which is not the dependency guard's line; if the measured line is "+
			"anything else the example is being stopped by an earlier refusal", stderr)
	}
	if exit, _ := entry["wrong_exit"].(float64); exit != 6 {
		t.Errorf("wrong_exit = %v, want 6 (the dependency guard is a validation refusal)", entry["wrong_exit"])
	}
}
