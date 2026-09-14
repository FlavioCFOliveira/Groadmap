// Package aihelp — gates on the per-subcommand exit_codes array.
//
// # What changed, and why it needs a gate at this end
//
// `commands[].subcommands[].exit_codes` used to be an array of integers.
// A bare list of integers tells a caller that a subcommand can fail and
// nothing about when: the top-level catalogue resolves 6 to
// EXIT_INVALID_DATA, "Invalid input data (validation failure: dates,
// ranges, enums)", which is true of every subcommand that validates
// anything and therefore actionable for none. An agent reading
// `[0, 3, 4, 6]` on `sprint start` learns that the call may fail with a
// validation error, and cannot learn that the validation in question is
// that another sprint is already OPEN.
//
// It is now an array of {code, conditions} objects, ordered ascending by
// code, one entry per code, with the conditions written per subcommand
// (SPEC/DATA_FORMATS.md § Field reference: per-subcommand exit code entry).
//
// registry_test.go pins the same invariants at the DECLARATION, over
// commands.AppRegistry. This file pins them on the EMITTED DOCUMENT, which
// is what an agent reads, so a projection bug in the generator — a dropped
// conditions slice, a re-ordered array, a code the emitter invented — fails
// here rather than reaching a consumer. Neither file is redundant: one
// watches the source, the other watches what is published from it.
package aihelp

import (
	"strings"
	"testing"
)

// walkSubcommands calls fn for every subcommand entry of the whole-CLI
// contract, with a label that reads as the invocation ("sprint start").
// Every gate in this file is total over the surface by construction: a
// subcommand added to the registry is walked the moment it is declared,
// with no list here to keep in step.
func walkSubcommands(t *testing.T, fn func(label string, entry map[string]any)) {
	t.Helper()

	m := unmarshalAsMap(t, generateOrFatal(t, ScopeAll()))
	families, ok := m["commands"].([]any)
	if !ok || len(families) == 0 {
		t.Fatalf("commands is missing or empty: %v", m["commands"])
	}

	seen := 0
	for _, rawFamily := range families {
		family, ok := rawFamily.(map[string]any)
		if !ok {
			t.Fatalf("a commands entry is not an object: %v", rawFamily)
		}
		familyName, _ := family["name"].(string)
		subs, ok := family["subcommands"].([]any)
		if !ok {
			t.Fatalf("%s: subcommands is not an array: %v", familyName, family["subcommands"])
		}
		for _, rawSub := range subs {
			sub, ok := rawSub.(map[string]any)
			if !ok {
				t.Fatalf("%s: a subcommands entry is not an object: %v", familyName, rawSub)
			}
			subName, _ := sub["name"].(string)
			seen++
			fn(familyName+" "+subName, sub)
		}
	}

	// A walk that found nothing would let every assertion below pass
	// vacuously. The floor is deliberately loose — it guards against an
	// empty or broken traversal, not against the surface growing or
	// shrinking by one subcommand, which is not this file's business.
	if seen < 40 {
		t.Fatalf("walked only %d subcommands; the traversal is broken and every gate over it is vacuous", seen)
	}
}

// catalogueCodes reads the codes the contract's own top-level exit_codes
// catalogue publishes. It is read from the emitted document rather than
// restated, so the two halves of one document are compared against each
// other and not against a third copy that could drift from both.
func catalogueCodes(t *testing.T) map[int]bool {
	t.Helper()

	m := unmarshalAsMap(t, generateOrFatal(t, ScopeAll()))
	entries, ok := m["exit_codes"].([]any)
	if !ok || len(entries) == 0 {
		t.Fatalf("top-level exit_codes is missing or empty: %v", m["exit_codes"])
	}
	out := make(map[int]bool, len(entries))
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("a top-level exit_codes entry is not an object: %v", raw)
		}
		code, ok := entry["code"].(float64)
		if !ok {
			t.Fatalf("a top-level exit_codes entry carries no numeric code: %v", entry)
		}
		out[int(code)] = true
	}
	return out
}

// subcommandExitCodes reads one subcommand's exit_codes array, failing the
// test rather than returning a partial view when the shape is wrong.
func subcommandExitCodes(t *testing.T, label string, entry map[string]any) []map[string]any {
	t.Helper()

	raw, present := entry["exit_codes"]
	if !present {
		t.Fatalf("%s: exit_codes key missing", label)
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("%s: exit_codes is not an array: %v", label, raw)
	}
	if len(list) == 0 {
		t.Fatalf("%s: exit_codes is empty; every subcommand emits at least 0", label)
	}
	out := make([]map[string]any, 0, len(list))
	for i, rawEntry := range list {
		obj, ok := rawEntry.(map[string]any)
		if !ok {
			t.Fatalf("%s: exit_codes[%d] = %v, want an object carrying code and conditions; the array of "+
				"bare integers is what schema_version 2.0.0 replaced", label, i, rawEntry)
		}
		out = append(out, obj)
	}
	return out
}

// TestGenerate_SubcommandExitCodesAreObjects is the shape gate: every
// element is an object with a numeric `code` and an array `conditions`,
// and neither key is optional. A consumer written against the published
// shape indexes both without checking, so an entry that carried only one
// of them would be a contract that lies about its own schema version.
func TestGenerate_SubcommandExitCodesAreObjects(t *testing.T) {
	walkSubcommands(t, func(label string, entry map[string]any) {
		for i, obj := range subcommandExitCodes(t, label, entry) {
			if _, ok := obj["code"].(float64); !ok {
				t.Errorf("%s: exit_codes[%d].code is missing or not a number: %v", label, i, obj["code"])
			}
			if _, ok := obj["conditions"].([]any); !ok {
				t.Errorf("%s: exit_codes[%d].conditions is missing or not an array: %v", label, i, obj["conditions"])
			}
			// The name and the meaning belong to the top-level catalogue and
			// to nowhere else. Repeating either per subcommand would be the
			// same sentence in fifty-odd places, which is a sentence that
			// will come to disagree with itself (rule 2).
			for _, forbidden := range []string{"name", "meaning", "sentinel"} {
				if _, present := obj[forbidden]; present {
					t.Errorf("%s: exit_codes[%d] carries %q; the code's name and meaning are published once, "+
						"in the top-level exit_codes catalogue, and resolved from there by code",
						label, i, forbidden)
				}
			}
		}
	})
}

// TestGenerate_SubcommandExitCodesAscendAndAppearOnce pins rule 1: the
// array is keyed by code, ascending, and a code appears exactly once. Two
// conditions that produce the same code are two elements of one entry's
// conditions, never two entries carrying the same code — otherwise a
// consumer that maps code to conditions silently keeps whichever entry it
// read last.
func TestGenerate_SubcommandExitCodesAscendAndAppearOnce(t *testing.T) {
	walkSubcommands(t, func(label string, entry map[string]any) {
		prev := -1
		seen := map[int]bool{}
		for i, obj := range subcommandExitCodes(t, label, entry) {
			code, ok := obj["code"].(float64)
			if !ok {
				continue // reported by the shape gate
			}
			n := int(code)
			if n <= prev {
				t.Errorf("%s: exit_codes[%d] is code %d after code %d; the array must ascend by code",
					label, i, n, prev)
			}
			prev = n
			if seen[n] {
				t.Errorf("%s: code %d appears more than once; a second condition for one code is a second "+
					"element of that code's conditions", label, n)
			}
			seen[n] = true
		}
		if !seen[0] {
			t.Errorf("%s: exit_codes carries no entry for 0; every subcommand can succeed", label)
		}
	})
}

// TestGenerate_EveryExitCodeCarriesANonEmptyCondition pins rule 4: every
// code carries at least one condition, 0 included. The condition for 0
// states what success means for that subcommand — what was created,
// changed or returned — so a caller reading only this array can tell an
// empty success from a silent failure.
func TestGenerate_EveryExitCodeCarriesANonEmptyCondition(t *testing.T) {
	walkSubcommands(t, func(label string, entry map[string]any) {
		for i, obj := range subcommandExitCodes(t, label, entry) {
			code, _ := obj["code"].(float64)
			conditions, ok := obj["conditions"].([]any)
			if !ok {
				continue // reported by the shape gate
			}
			if len(conditions) == 0 {
				t.Errorf("%s: exit_codes[%d] (code %v) carries no condition; a code that says nothing about "+
					"itself is the bare integer this shape replaced", label, i, code)
			}
			for j, rawCond := range conditions {
				cond, ok := rawCond.(string)
				if !ok {
					t.Errorf("%s: exit_codes[%d].conditions[%d] is not a string: %v", label, i, j, rawCond)
					continue
				}
				if strings.TrimSpace(cond) == "" {
					t.Errorf("%s: exit_codes[%d].conditions[%d] (code %v) is empty", label, i, j, code)
				}
			}
		}
	})
}

// TestGenerate_SubcommandExitCodesResolveAgainstTheCatalogue pins the half
// of the contract that makes rule 2 workable: a subcommand publishes a bare
// code precisely because the reader can resolve it in the top-level
// catalogue of the same document. A code that is not in that catalogue is
// unresolvable, and the omission of the name and the meaning then leaves
// the reader with nothing at all.
func TestGenerate_SubcommandExitCodesResolveAgainstTheCatalogue(t *testing.T) {
	catalogue := catalogueCodes(t)
	walkSubcommands(t, func(label string, entry map[string]any) {
		for i, obj := range subcommandExitCodes(t, label, entry) {
			code, ok := obj["code"].(float64)
			if !ok {
				continue // reported by the shape gate
			}
			if !catalogue[int(code)] {
				t.Errorf("%s: exit_codes[%d] publishes code %d, which the contract's own top-level "+
					"exit_codes catalogue does not carry; the reader has no way to resolve it",
					label, i, int(code))
			}
		}
	})
}

// TestGenerate_ExitCodeConditionsAreNotOneGenericSentence is the gate on
// the defect the whole change exists to remove. A condition that is
// repeated across most of the surface is a sentence true of every
// subcommand, which is actionable for none.
//
// A condition is exempt BY NAME, with its reason beside it, when it is
// produced by a step the subcommand SHARES rather than by anything the
// subcommand does: the roadmap-resolution step every roadmap-scoped
// subcommand runs before its own work, the one arity point every invocation
// passes through, and the one flag parser every subcommand with flags of its
// own builds. In each case the same sentence really is the true one in every
// place it appears — one condition published in many places, not many
// conditions flattened into one sentence. Any other sentence that spreads
// across more than half the surface is a generic condition and fails here.
//
// A category-wide exemption is not available and is not wanted: each entry
// names one sentence, and the staleness arm below removes it the moment the
// contract stops publishing it.
func TestGenerate_ExitCodeConditionsAreNotOneGenericSentence(t *testing.T) {
	// Kept in the wording the registry declares (internal/commands/
	// registry_exit_codes.go). A change to any of these sentences must be
	// made here too, deliberately, rather than silently widening what counts
	// as shared.
	exempt := map[string]string{
		"Neither -r nor --roadmap was supplied.": "the shared roadmap-resolution step, run before " +
			"the subcommand's own work",
		"The roadmap named by -r/--roadmap does not exist.": "the same shared step, one stage later",
		"A token beginning with - names none of this subcommand's flags.": "the one flag parser " +
			"(internal/commands/flags.go), which words this refusal once for the whole CLI",
		"A value-taking flag was written with nothing after it to be its value.":                                                                       "the same parser",
		"A flag whose value must be an integer carries a value that is not one.":                                                                       "the same parser",
		"A positional id is not an integer, which is also how a token beginning with - written in an id's position is refused: it is read as that id.": "the shared id-parsing helper every subcommand that reads a positional id calls",
		"A required positional argument was omitted.": "the subcommand's own arity contract, checked " +
			"against the one Positional declaration the registry carries",
		"More positional arguments were supplied than this subcommand accepts.": "the one arity " +
			"enforcement point (internal/commands/positional_arity.go), on the only path that reaches " +
			"a handler",
	}

	counts := map[string]int{}
	published := map[string]bool{}
	total := 0
	walkSubcommands(t, func(label string, entry map[string]any) {
		total++
		for _, obj := range subcommandExitCodes(t, label, entry) {
			conditions, _ := obj["conditions"].([]any)
			for _, rawCond := range conditions {
				cond, ok := rawCond.(string)
				if !ok {
					continue
				}
				published[cond] = true
				if _, ok := exempt[cond]; ok {
					continue
				}
				counts[cond]++
			}
		}
	})

	// Half the surface. A sentence that appears on more subcommands than
	// that is describing the CLI rather than the subcommand.
	limit := total / 2
	for cond, n := range counts {
		if n > limit {
			t.Errorf("the condition %q is published by %d of %d subcommands; a condition that is true of "+
				"most of the surface tells a caller nothing about the subcommand it is attached to, which "+
				"is the defect the per-subcommand condition exists to remove", cond, n, total)
		}
	}

	// The exemption list must not go stale: a sentence exempted here that
	// no longer appears anywhere is an exemption nobody will re-examine.
	for cond, why := range exempt {
		if !published[cond] {
			t.Errorf("the exemption for %q (%s) names a condition the contract no longer publishes; "+
				"remove it rather than leaving it to cover nothing", cond, why)
		}
	}

	// The gate must still have something to judge. Exempting the shared
	// sentences would be a way of emptying it, so the count of conditions it
	// actually weighs is asserted to stay large: these are the per-subcommand
	// sentences the whole field exists for.
	if len(counts) < 100 {
		t.Errorf("only %d distinct non-exempt conditions were weighed across %d subcommands; the "+
			"exemption list has grown into the category-wide exemption this gate refuses to be",
			len(counts), total)
	}
}
