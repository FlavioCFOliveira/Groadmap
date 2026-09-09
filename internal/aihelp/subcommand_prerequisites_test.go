// Package aihelp — gates on the subcommand-level `prerequisites` key.
//
// # The rule and why it is worth a gate
//
// Every array-typed field of the contract serialises as `[]` when it is
// empty, never as null — with exactly one carve-out. The `prerequisites`
// of a `subcommands` entry is OMITTED when the subcommand has no
// precondition of its own (SPEC/DATA_FORMATS.md § Subcommand
// prerequisites, rule 2).
//
// Rule 2 is the point of the field. A key that is present and empty on
// every subcommand is worse than a key that is absent, because it is read
// as an assertion: a consumer that found `"prerequisites": []` on
// `sprint start` concluded the subcommand had no precondition, and the
// conclusion was false — only one sprint may be OPEN at a time, and the
// prose description said so where no structured consumer looks. An
// always-empty field invites exactly the trust it cannot honour.
//
// The carve-out is narrow, and the narrowness is half of what is gated
// here: the `prerequisites` of a `commands` entry and of a
// `common_workflows` entry keep the general rule and are ALWAYS present,
// `[]` when empty. A change that widened the carve-out to them would break
// consumers that index those keys unconditionally, and would do it
// quietly.
package aihelp

import (
	"strings"
	"testing"
)

// TestGenerate_SubcommandPrerequisitesAreAbsentOrNonEmpty is the shape
// gate. Three states are possible for the key and only two are legal:
// absent (no precondition of its own), or present and carrying at least
// one non-empty string. Present-and-empty is the state the rule exists to
// forbid.
func TestGenerate_SubcommandPrerequisitesAreAbsentOrNonEmpty(t *testing.T) {
	present, absent := 0, 0

	walkSubcommands(t, func(label string, entry map[string]any) {
		raw, ok := entry["prerequisites"]
		if !ok {
			absent++
			return
		}
		present++

		list, ok := raw.([]any)
		if !ok {
			t.Errorf("%s: prerequisites is present but not an array: %v", label, raw)
			return
		}
		if len(list) == 0 {
			t.Errorf("%s: prerequisites is published as []; a subcommand with no precondition of its own "+
				"omits the key entirely, because an always-empty key is read as an assertion that there is "+
				"no precondition — and that assertion is what the field exists to stop being false", label)
			return
		}
		for i, rawItem := range list {
			item, ok := rawItem.(string)
			if !ok {
				t.Errorf("%s: prerequisites[%d] is not a string: %v", label, i, rawItem)
				continue
			}
			if strings.TrimSpace(item) == "" {
				t.Errorf("%s: prerequisites[%d] is empty", label, i)
			}
		}
	})

	// Both states must actually occur. If every subcommand carried the key
	// the gate above would pass while the carve-out did nothing; if none
	// did, the field would have been withdrawn rather than made optional.
	if present == 0 {
		t.Error("no subcommand publishes prerequisites; the key would then assert nothing anywhere and the " +
			"carve-out would be a withdrawal rather than an omission rule")
	}
	if absent == 0 {
		t.Error("every subcommand publishes prerequisites; the omission rule is then inert, and the gate " +
			"above cannot tell an omitted key from a key nobody omits")
	}
}

// anyContains reports whether any element of a JSON string array carries
// the given substring. It replaces a join-then-search, which the linter
// reads as a concatenation in a loop and which was never needed: the
// question is per element.
func anyContains(list []any, needle string) bool {
	for _, item := range list {
		if s, ok := item.(string); ok && strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// TestGenerate_SprintStartPublishesItsOwnPrerequisite pins the entry the
// SPEC names as the reason the rule exists. `sprint start` has a genuine
// precondition — only one sprint may be OPEN at a time — which was stated
// in prose and nowhere a structured consumer could reach.
//
// The prose description keeps that sentence: the fact was MOVED into
// `prerequisites`, not moved OUT of the description
// (SPEC/DATA_FORMATS.md § Subcommand prerequisites, "The information
// exists already and MUST be moved, not invented").
func TestGenerate_SprintStartPublishesItsOwnPrerequisite(t *testing.T) {
	entry := onlySubcommand(t, unmarshalAsMap(t, generateOrFatal(t, ScopeAll())), "sprint", "start")

	raw, ok := entry["prerequisites"]
	if !ok {
		t.Fatal("sprint start publishes no prerequisites; only one sprint can be OPEN at a time, and that " +
			"is a condition on state the caller must bring about before invoking")
	}
	list, _ := raw.([]any)

	if !anyContains(list, "OPEN") {
		t.Errorf("sprint start prerequisites do not mention the OPEN sprint rule: %v", list)
	}

	// The prose keeps it too. A "move" that emptied the description would
	// leave a human reader worse off than before.
	description, _ := entry["description"].(string)
	if !strings.Contains(description, "Only one sprint can be OPEN at a time") {
		t.Errorf("sprint start description no longer states the OPEN-sprint rule; the fact is published in "+
			"BOTH places by design — the field makes it reachable without reading the prose, and does not "+
			"replace the prose. description = %q", description)
	}
}

// TestGenerate_FamilyAndWorkflowPrerequisitesAreAlwaysPresent pins the
// boundary of the carve-out. Only the subcommand-level key is optional;
// the `commands` and `common_workflows` levels keep the general
// empty-array rule, and a consumer indexing either without a presence
// check must stay correct.
func TestGenerate_FamilyAndWorkflowPrerequisitesAreAlwaysPresent(t *testing.T) {
	m := unmarshalAsMap(t, generateOrFatal(t, ScopeAll()))

	families, _ := m["commands"].([]any)
	if len(families) == 0 {
		t.Fatal("commands is empty")
	}
	emptyFamilySeen := false
	for _, rawFamily := range families {
		family, _ := rawFamily.(map[string]any)
		name, _ := family["name"].(string)
		raw, ok := family["prerequisites"]
		if !ok {
			t.Errorf("commands[%s].prerequisites is absent; the carve-out is for the SUBCOMMAND level and "+
				"does not reach the family, which keeps the general empty-array rule", name)
			continue
		}
		list, ok := raw.([]any)
		if !ok {
			t.Errorf("commands[%s].prerequisites is not an array: %v", name, raw)
			continue
		}
		if len(list) == 0 {
			emptyFamilySeen = true
		}
	}
	// At least one family genuinely has none, so the always-present half of
	// this gate is exercised against a real empty array rather than only
	// against populated ones.
	if !emptyFamilySeen {
		t.Error("no command family publishes an empty prerequisites array; the always-present rule is then " +
			"untested against the case it governs")
	}

	workflows, _ := m["common_workflows"].([]any)
	if len(workflows) == 0 {
		t.Fatal("common_workflows is empty")
	}
	for _, rawFlow := range workflows {
		flow, _ := rawFlow.(map[string]any)
		name, _ := flow["name"].(string)
		raw, ok := flow["prerequisites"]
		if !ok {
			t.Errorf("common_workflows[%s].prerequisites is absent; workflow prerequisites keep the general "+
				"empty-array rule and are always present", name)
			continue
		}
		if _, ok := raw.([]any); !ok {
			t.Errorf("common_workflows[%s].prerequisites is not an array: %v", name, raw)
		}
	}
}
