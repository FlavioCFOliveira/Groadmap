// Package commands — regression gates for two rules of
// SPEC/HELP.md § Audit family help specifics that both audit help surfaces
// violated before rmp task #266.
//
// The section binds exactly two surfaces: "The `audit` family help and the
// `audit list` subcommand help ... MUST additionally make the rules below
// explicit." Rule 2 requires the LEGACY values to be marked and explained, and
// rule 5 requires `related_entity_id` to be explained as the operation's
// counterpart entity. Neither surface said either thing: `audit --help` had no
// occurrence of "legacy" at all, and neither surface had an occurrence of
// "counterpart", so a reader was left to take `related_entity_id` for a second
// copy of `entity_id` and to pick a LEGACY filter that silently returns nothing.
//
// These gates match on CONCEPTS rather than on sentences. Each rule is satisfied
// by any of several phrasings, so rewording the help for clarity does not break
// the build, while deleting the statement does. A gate that pinned the prose
// verbatim would be a gate on the wording, and the SPEC constrains the meaning.
package commands

import (
	"regexp"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// auditRuleSurfaces are the two surfaces the section binds. `audit history` and
// `audit stats` are deliberately absent: the section names these two.
func auditRuleSurfaces(t *testing.T) map[string]string {
	t.Helper()
	cmd := AppRegistry().FindCommand("audit")
	if cmd == nil {
		t.Fatal("the audit command family is not registered")
	}
	family := captureStdout(t, func() { _ = cmd.DispatchFamily([]string{"--help"}) })
	list := captureStdout(t, func() { _ = cmd.DispatchFamily([]string{"list", "--help"}) })

	if strings.TrimSpace(family) == "" {
		t.Fatal("`rmp audit --help` produced no output")
	}
	if strings.TrimSpace(list) == "" {
		t.Fatal("`rmp audit list --help` produced no output")
	}
	if family == list {
		t.Fatal("`rmp audit --help` and `rmp audit list --help` produced identical output, so one of the " +
			"two surfaces is not being exercised and every assertion below is being made twice about one " +
			"of them")
	}
	return map[string]string{"rmp audit --help": family, "rmp audit list --help": list}
}

// legacyNotWritten and legacyStaysFilterable are the two halves rule 2 demands:
// that no command writes a LEGACY value, and that it stays accepted so the
// entries already carrying it remain findable.
//
// Every pattern here separates words with \s+ rather than a literal space,
// because these surfaces are WRAPPED text: the phrase the rule requires may be
// split across a line break and an indent, and a gate that missed it there
// would demand a particular line width instead of a particular statement.
var (
	legacyNotWritten      = regexp.MustCompile(`(?is)(no\s+command\s+writes|written\s+by\s+no\s+command|not\s+written\s+by\s+any\s+command|never\s+written)`)
	legacyStaysFilterable = regexp.MustCompile(`(?is)((remain|stay)\s+(filterable|reachable|findable))`)
)

// TestAuditHelp_LegacyIsMarkedAndExplained is rule 2 on both bound surfaces.
func TestAuditHelp_LegacyIsMarkedAndExplained(t *testing.T) {
	for label, out := range auditRuleSurfaces(t) {
		if !strings.Contains(out, "LEGACY") {
			t.Errorf("%s: no occurrence of LEGACY. The four values the catalogue still accepts but no "+
				"command writes are listed indistinguishably from the operations in use, so a reader "+
				"filtering for current activity gets an empty result with no explanation", label)
			continue
		}
		if !legacyNotWritten.MatchString(out) {
			t.Errorf("%s: says LEGACY but never says that no command writes those values, which is the "+
				"half of rule 2 that tells a reader why the filter came back empty", label)
		}
		if !legacyStaysFilterable.MatchString(out) {
			t.Errorf("%s: says LEGACY but never says the values stay accepted so the entries already "+
				"carrying them remain filterable, which is the half of rule 2 that explains why a value "+
				"nothing writes is still offered", label)
		}
	}
}

// TestAuditHelp_LegacyMarkingCoversExactlyTheLegacyValues ties rule 2 to the
// declaration rather than to a count. The family help lists the operations, so
// it must mark every declared legacy value and mark nothing else; without this
// the marking could name three of the four and still satisfy the gate above.
func TestAuditHelp_LegacyMarkingCoversExactlyTheLegacyValues(t *testing.T) {
	cmd := AppRegistry().FindCommand("audit")
	if cmd == nil {
		t.Fatal("the audit command family is not registered")
	}
	out := captureStdout(t, func() { _ = cmd.DispatchFamily([]string{"--help"}) })

	// Group label -> the operations printed under it, taken from the rendered
	// block so the assertion is about what a reader sees.
	block := auditOperationBlock()
	legacyLabels := map[string]bool{}
	for _, g := range auditOperationGroups {
		if g.legacy {
			legacyLabels[g.label] = true
		}
	}
	if len(legacyLabels) == 0 {
		t.Fatal("no group in auditOperationGroups is marked legacy, so nothing here can be attributed")
	}

	printedLegacy := map[string]bool{}
	current := ""
	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		for _, g := range auditOperationGroups {
			if strings.HasPrefix(trimmed, g.label) {
				current = g.label
			}
		}
		if !legacyLabels[current] {
			continue
		}
		list := line
		if idx := strings.LastIndex(list, ":"); idx >= 0 {
			list = list[idx+1:]
		}
		for _, f := range strings.FieldsFunc(list, func(r rune) bool { return r == ' ' || r == ',' }) {
			printedLegacy[f] = true
		}
	}

	declaredLegacy := map[string]bool{}
	for _, op := range models.ValidAuditOperations {
		if class, ok := models.ClassifyAuditOperation(op); ok && class.Legacy {
			declaredLegacy[string(op)] = true
		}
	}
	if len(declaredLegacy) == 0 {
		t.Fatal("internal/models declares no legacy operation, so this gate is measuring nothing")
	}

	for op := range declaredLegacy {
		if !printedLegacy[op] {
			t.Errorf("%s is declared LEGACY but is not printed under a LEGACY-labelled group, so `rmp "+
				"audit --help` presents it as an operation still in use", op)
		}
	}
	for op := range printedLegacy {
		if !declaredLegacy[op] {
			t.Errorf("%s is printed under a LEGACY-labelled group but is not declared legacy; a command "+
				"still writes it, and the help is telling readers to stop filtering on it", op)
		}
	}

	// The family help must actually carry the block it is being judged on.
	if !strings.Contains(out, strings.TrimRight(block, "\n")) {
		t.Error("`rmp audit --help` does not contain the rendered operation block, so the attribution " +
			"above describes a string the reader never sees")
	}
}

// The two commands rule 5's counter-example names. Wrap-insensitive for the
// reason given on legacyNotWritten above.
var (
	sprintRemoveTasksMention = regexp.MustCompile(`(?is)sprint\s+remove-tasks`)
	taskStatMention          = regexp.MustCompile(`(?is)task\s+stat`)
)

// counterpartExplained accepts any phrasing that names the OTHER entity of the
// operation, which is what rule 5 requires the key to be explained as.
var counterpartExplained = regexp.MustCompile(`(?i)(counterpart|other entity|opposite entity)`)

// TestAuditHelp_RelatedEntityIDIsExplainedAsTheCounterpart is rule 5 on both
// bound surfaces: the key names the counterpart entity of the operation, it is
// null when there is none, and its presence does not follow from the operation
// name. The last clause is the one that has a concrete example in the SPEC, so
// it is checked against the pair the SPEC names rather than against prose.
func TestAuditHelp_RelatedEntityIDIsExplainedAsTheCounterpart(t *testing.T) {
	for label, out := range auditRuleSurfaces(t) {
		if !strings.Contains(out, "related_entity_id") {
			t.Errorf("%s: never names related_entity_id, so rule 5 has nothing to attach to", label)
			continue
		}
		if !counterpartExplained.MatchString(out) {
			t.Errorf("%s: explains related_entity_id without ever saying it names the COUNTERPART entity "+
				"of the operation. Without that the key reads as a duplicate of entity_id", label)
		}
		if !regexp.MustCompile(`(?is)null\s+when\s+the\s+operation\s+has\s+no`).MatchString(out) {
			t.Errorf("%s: does not state that related_entity_id is null when the operation has no "+
				"counterpart, so an agent treats the absence of a value as an error", label)
		}
		// Rule 5's closing sentence: the key's presence must not be presented
		// as following from the operation name. The SPEC gives the exact
		// counter-example, so both halves of it must appear.
		if !strings.Contains(out, string(models.OpTaskStatusBacklog)) ||
			!sprintRemoveTasksMention.MatchString(out) ||
			!taskStatMention.MatchString(out) {
			t.Errorf("%s: does not carry the counter-example rule 5 requires — %s carries a sprint id "+
				"from `sprint remove-tasks` and null from `task stat` — so the help still lets a reader "+
				"conclude that the operation name decides whether the key is set",
				label, models.OpTaskStatusBacklog)
		}
	}
}

// TestAuditHelp_SevenKeysAreNamedAndNeverOmitted is rule 4, checked here because
// it is the sibling of rule 5 and shares the same failure mode: an agent that
// does not know a key can be null treats the missing value as an error. The two
// nullable keys were also the two missing from the contract's own schema string,
// which acceptance criterion 2 of rmp task #266 fixed.
func TestAuditHelp_SevenKeysAreNamedAndNeverOmitted(t *testing.T) {
	keys := []string{"id", "operation", "entity_type", "entity_id", "performed_at",
		"related_entity_id", "commit_hash"}

	cmd := AppRegistry().FindCommand("audit")
	if cmd == nil {
		t.Fatal("the audit command family is not registered")
	}
	out := captureStdout(t, func() { _ = cmd.DispatchFamily([]string{"--help"}) })

	for _, k := range keys {
		if !strings.Contains(out, k) {
			t.Errorf("`rmp audit --help` does not name the audit-entry key %q", k)
		}
	}
	if !regexp.MustCompile(`(?is)never\s+omitted`).MatchString(out) {
		t.Error("`rmp audit --help` does not state that the two nullable keys are never omitted; an " +
			"agent that expects them to disappear will not handle the null")
	}

	// The same seven keys must be published in the machine-readable schema of
	// `audit list`, which is what an agent reads instead of the help.
	sub := cmd.FindSubcommand("list")
	if sub == nil {
		t.Fatal("`audit list` is not registered")
	}
	for _, k := range keys {
		if !strings.Contains(sub.Output.Schema, k) {
			t.Errorf("the stdout_on_success schema of `audit list` does not publish the key %q; it is "+
				"the only description of the response shape an agent gets.\n  schema: %s",
				k, sub.Output.Schema)
		}
	}
}
