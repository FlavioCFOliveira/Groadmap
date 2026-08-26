package aihelp

// Package aihelp — SPEC-versus-generator gate for
// SPEC/DATA_FORMATS.md § `enums` map entry.
//
// WHAT THIS FIXES. The § `enums` map entry examples once showed an `AuditOperation`
// definition carrying `"catalogue_reference": "DATABASE.md § audit Table"`. The
// generator has never emitted that key. The defect lived in an example rather than
// in normative text, which is what made it worth a gate: an agent written from the
// example looks up a key that never arrives, and the lookup fails SILENTLY, because
// a missing key is indistinguishable from an enum that simply carries no reference.
// The key was removed and the real rule published in its place: an enum definition
// has exactly two members, `values` on every enum and `state_machine_reference` on
// `TaskStatus` and `SprintStatus` alone. This file is acceptance criterion 4 of that
// work — "a regression test fails if the example regains a key the generator does
// not emit" — widened, because the same cycle also aligned the example TEXT to the
// generator, so keys and values can both be compared.
//
// WHY IT CANNOT GO GREEN ON THE DRIFT IT CHASES. Neither side is restated here.
// The published side is parsed out of SPEC/DATA_FORMATS.md at test time, so an
// edited example reaches the expectation immediately; the emitted side is generated
// in-process from the live registry and static catalogue, so it is what the binary
// actually ships. A test that pinned a copy of either side would pass through
// exactly the divergence it exists to detect. Every region this gate needs — the
// section, the example blocks, the sentence carrying the rule — is fatal when it
// cannot be located, so a SPEC restructure produces a red test rather than a gate
// that quietly measures nothing.
//
// THE COMPARISON IS TOTAL IN BOTH DIRECTIONS, at three levels:
//
//   - Enum definition members. The member keys of an example definition must equal
//     the member keys the generator emits for that enum. A key the example gains
//     and the generator does not emit fails (the original defect); a key the
//     generator emits and the example omits fails as well (the silent regression
//     the defect describes, in the other direction — an agent that never learns a
//     member exists cannot tell it is missing).
//   - Enum values. Every value the example shows must be published by the
//     generator, with the same member keys and the same member values, byte for
//     byte, including `entity_type`, `legacy` and `description`.
//   - The rule itself. Which of the eight published enums carry a reference member
//     is read out of the SPEC sentence that publishes it and checked against the
//     generator, with the two named lists required to partition the published
//     enums exactly.
//
// POLICY FOR PARTIAL EXAMPLES. An example may show a strict SUBSET of an enum's
// values, and one does: `AuditOperation` has 43 values and the example shows four.
// Requiring the example to list all 43 would turn a worked example into a second
// copy of the catalogue, so the value comparison is keyed by value name — every
// value SHOWN must exist and match exactly, and no value may be shown that the
// contract does not publish; a value the example does not show is not a failure.
// Subsetting is bounded on three sides so it cannot degrade into a gate that
// checks nothing: an example must show at least one value, the values it shows
// must appear in the generator's declaration order (so an example cannot reorder
// the enum), and the run fails outright if no value was compared at all. Member
// keys are NOT subject to this policy: they are compared for equality, because a
// definition has few enough members that showing them is free, and a member
// omitted from an example is precisely the failure this file exists to catch.

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const (
	// dataFormatsRelPath locates the contract specification from the module root.
	dataFormatsRelPath = "SPEC/DATA_FORMATS.md"
	// enumsSectionHeading is the section whose examples and rule this gate reads,
	// and enumsSectionName is the same section as a failure message renders it.
	enumsSectionHeading = "#### `enums` map entry"
	enumsSectionName    = "`enums` map entry"
	// jsonFenceOpen and jsonFenceClose delimit a worked example inside it.
	jsonFenceOpen  = "```json"
	jsonFenceClose = "```"
)

// backtickSpan matches one `...` span, the markup the section uses for every enum
// name and every member name it publishes.
var backtickSpan = regexp.MustCompile("`([^`]+)`")

// The two sentences that publish the reference rule. They are matched over the
// whole section flattened to a single line, so the rule may be rewrapped or moved
// within the section without breaking the gate, while a rewording that drops the
// rule is fatal.
//
//	universalMemberSentence -> "`values` is carried by every enum."
//	referenceMemberSentence -> "`state_machine_reference` is carried by `TaskStatus`
//	                            and `SprintStatus`, and by no other enum: `A`, `B`,
//	                            ... and `F` each carry `values` alone."
var (
	universalMemberSentence = regexp.MustCompile("`([a-z_]+)` is carried by every enum\\.")
	// The two capture groups holding the enum lists exclude the full stop on
	// purpose. Without that the first alternative the engine finds spans the
	// sentence boundary, swallowing "every enum. `state_machine_reference` is
	// carried by" into the list and reading the reference member as an enum name.
	referenceMemberSentence = regexp.MustCompile("`([a-z_]+)` is carried by ([^.]+?), and by no other enum: ([^.]+?) each carry `([a-z_]+)` alone\\.")
)

// exampleEnum is one enum definition lifted out of a worked example.
type exampleEnum struct {
	def  map[string]any
	name string
	line int
}

// enumsSection returns the lines of § `enums` map entry together with the 1-based
// file line number of its heading. A section this gate cannot find is a broken
// gate, not a passing one.
func enumsSection(t *testing.T) ([]string, int) {
	t.Helper()

	lines := strings.Split(readRepoFile(t, dataFormatsRelPath), "\n")

	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == enumsSectionHeading {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s no longer contains the heading %q; the gate has lost the section it reads",
			dataFormatsRelPath, enumsSectionHeading)
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if depth := headingDepth(lines[i]); depth > 0 && depth <= 4 {
			end = i
			break
		}
	}
	return lines[start+1 : end], start + 2
}

// headingDepth returns the number of leading '#' characters of a markdown heading,
// or 0 when the line is not one. A '#' run must be followed by a space to open a
// heading, which keeps a fenced comment line from closing the section early.
func headingDepth(line string) int {
	depth := 0
	for depth < len(line) && line[depth] == '#' {
		depth++
	}
	if depth == 0 || depth >= len(line) || line[depth] != ' ' {
		return 0
	}
	return depth
}

// readEnumExamples parses every ```json block of the section into the enum
// definitions it publishes. Each block is a fragment of the `enums` map — a key
// and its definition — so it is wrapped in braces before decoding.
func readEnumExamples(t *testing.T) []exampleEnum {
	t.Helper()

	lines, firstLine := enumsSection(t)

	var out []exampleEnum
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != jsonFenceOpen {
			continue
		}
		closeAt := -1
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == jsonFenceClose {
				closeAt = j
				break
			}
		}
		if closeAt < 0 {
			t.Fatalf("%s:%d: a %s block in § %s is never closed",
				dataFormatsRelPath, firstLine+i, jsonFenceOpen, enumsSectionName)
		}

		body := strings.Join(lines[i+1:closeAt], "\n")
		var block map[string]any
		if err := json.Unmarshal([]byte("{"+body+"}"), &block); err != nil {
			t.Fatalf("%s:%d: the example block does not decode as an `enums` map fragment: %v\n%s",
				dataFormatsRelPath, firstLine+i, err, body)
		}

		names := make([]string, 0, len(block))
		for name := range block {
			names = append(names, name)
		}
		sortStrings(names)
		for _, name := range names {
			def, ok := block[name].(map[string]any)
			if !ok {
				t.Fatalf("%s:%d: example enum %s is %T, want a JSON object",
					dataFormatsRelPath, firstLine+i, name, block[name])
			}
			out = append(out, exampleEnum{name: name, def: def, line: firstLine + i})
		}
		i = closeAt
	}

	if len(out) == 0 {
		t.Fatalf("%s § %s publishes no %s example; the gate parses nothing and would pass vacuously",
			dataFormatsRelPath, enumsSectionName, jsonFenceOpen)
	}
	return out
}

// memberKeys returns the sorted member names of one JSON object.
func memberKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

// generatedEnums returns the `enums` map the binary actually emits, decoded with
// full fidelity (every member of every value, not just value and description).
func generatedEnums(t *testing.T) map[string]any {
	t.Helper()
	return contractEnums(t, generateOrFatal(t, ScopeAll()))
}

// generatedDefinition resolves one enum definition of the emitted contract.
func generatedDefinition(t *testing.T, enums map[string]any, name string) map[string]any {
	t.Helper()

	raw, present := enums[name]
	if !present {
		t.Fatalf("the contract publishes no enum named %s (published: %v)", name, enumNamesOf(enums))
	}
	def, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("enums.%s is %T on the contract, want a JSON object", name, raw)
	}
	return def
}

// objectValues coerces a definition's `values` member into the list of objects it
// must be, on either side of the comparison.
func objectValues(t *testing.T, side, name string, def map[string]any) []map[string]any {
	t.Helper()

	raw, ok := def["values"].([]any)
	if !ok {
		t.Fatalf("%s: enums.%s.values is %T, want an array", side, name, def["values"])
	}
	out := make([]map[string]any, 0, len(raw))
	for i, entry := range raw {
		obj, isObject := entry.(map[string]any)
		if !isObject {
			t.Fatalf("%s: enums.%s.values[%d] is %T, want a JSON object", side, name, i, entry)
		}
		if _, named := obj["value"].(string); !named {
			t.Fatalf("%s: enums.%s.values[%d] carries no string `value` member", side, name, i)
		}
		out = append(out, obj)
	}
	return out
}

// ---------------------------------------------------------------------------
// 1. The examples: same members, same values, in the generator's own order.
// ---------------------------------------------------------------------------

// TestEnumsSpec_ExampleMembersMatchTheGenerator is the criterion at the level the
// defect occurred on. It compares the member keys of every example enum definition
// against the member keys the generator emits, in both directions, so an example
// that regains `catalogue_reference` — or any other key the binary does not emit —
// fails here, and so does an example that drops a member the binary does emit.
func TestEnumsSpec_ExampleMembersMatchTheGenerator(t *testing.T) {
	enums := generatedEnums(t)

	compared := 0
	for _, example := range readEnumExamples(t) {
		published := memberKeys(example.def)
		emitted := memberKeys(generatedDefinition(t, enums, example.name))

		if !reflect.DeepEqual(published, emitted) {
			for _, key := range published {
				if !containsString(emitted, key) {
					t.Errorf("%s:%d: the example for enums.%s carries the member %q, which the generator "+
						"never emits; a consumer written from this example waits for a key that does not "+
						"arrive, and cannot tell the difference between an absent member and an enum that "+
						"legitimately carries none",
						dataFormatsRelPath, example.line, example.name, key)
				}
			}
			for _, key := range emitted {
				if !containsString(published, key) {
					t.Errorf("%s:%d: the generator emits enums.%s.%s and the example omits it; a consumer "+
						"written from this example never learns the member exists",
						dataFormatsRelPath, example.line, example.name, key)
				}
			}
			t.Errorf("%s:%d: enums.%s example members %v, generator members %v",
				dataFormatsRelPath, example.line, example.name, published, emitted)
		}
		compared++
	}

	if compared == 0 {
		t.Fatalf("no example enum definition was compared against the generator, so this gate measured nothing")
	}
	t.Logf("compared the member keys of %d example enum definition(s) against the generator", compared)
}

// TestEnumsSpec_ExampleValuesMatchTheGenerator is the same criterion one level
// finer, and it is what the aligned example text buys: every value an example shows
// must be published by the generator with identical members and identical member
// values. It is what fails if a description in the example drifts from the string
// in static.go, in either direction.
//
// An example may show a subset of an enum's values (see the policy in the file
// header); it may not show a value the contract does not publish, and it may not
// show values in an order the contract does not use.
func TestEnumsSpec_ExampleValuesMatchTheGenerator(t *testing.T) {
	enums := generatedEnums(t)

	compared := 0
	for _, example := range readEnumExamples(t) {
		emitted := objectValues(t, "generator", example.name, generatedDefinition(t, enums, example.name))
		published := objectValues(t, dataFormatsRelPath, example.name, example.def)

		if len(published) == 0 {
			t.Errorf("%s:%d: the example for enums.%s shows no value at all, so it demonstrates nothing "+
				"about the values the contract publishes",
				dataFormatsRelPath, example.line, example.name)
			continue
		}

		previous := -1
		for _, value := range published {
			name, _ := value["value"].(string)

			at := -1
			for i, candidate := range emitted {
				if candidate["value"] == name {
					at = i
					break
				}
			}
			if at < 0 {
				t.Errorf("%s:%d: the example for enums.%s shows the value %q, which the contract does not "+
					"publish", dataFormatsRelPath, example.line, example.name, name)
				continue
			}
			if at <= previous {
				t.Errorf("%s:%d: the example for enums.%s shows %q out of the contract's declaration order "+
					"(it is published at index %d, after a value shown earlier)",
					dataFormatsRelPath, example.line, example.name, name, at)
			}
			previous = at

			if !reflect.DeepEqual(value, emitted[at]) {
				reportValueDrift(t, example, name, value, emitted[at])
			}
			compared++
		}
	}

	if compared == 0 {
		t.Fatalf("no example enum value was compared against the generator, so this gate measured nothing")
	}
	t.Logf("compared %d example enum value(s) against the generator", compared)
}

// reportValueDrift prints one failing value member by member, so the failure names
// the string to paste rather than two JSON blobs to diff by eye.
func reportValueDrift(t *testing.T, example exampleEnum, value string, published, emitted map[string]any) {
	t.Helper()

	keys := memberKeys(published)
	for _, key := range memberKeys(emitted) {
		if !containsString(keys, key) {
			keys = append(keys, key)
		}
	}
	sortStrings(keys)

	for _, key := range keys {
		specValue, inSpec := published[key]
		genValue, inGen := emitted[key]
		switch {
		case inSpec && !inGen:
			t.Errorf("%s:%d: enums.%s.values[%s] carries %q in the example and the generator never emits it",
				dataFormatsRelPath, example.line, example.name, value, key)
		case !inSpec && inGen:
			t.Errorf("%s:%d: enums.%s.values[%s] omits %q, which the generator emits as %#v",
				dataFormatsRelPath, example.line, example.name, value, key, genValue)
		case !reflect.DeepEqual(specValue, genValue):
			t.Errorf("%s:%d: enums.%s.values[%s].%s\n  example:   %#v\n  generator: %#v",
				dataFormatsRelPath, example.line, example.name, value, key, specValue, genValue)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. The rule the section publishes about all eight enums.
// ---------------------------------------------------------------------------

// referenceRule is the rule read out of § `enums` map entry: the member every enum
// carries, the reference member, and the two exhaustive lists of enum names.
type referenceRule struct {
	universalMember string
	referenceMember string
	withReference   []string
	valuesOnly      []string
}

// readReferenceRule parses the rule out of the section. The section is flattened to
// a single space-separated line first, so the sentences may be rewrapped freely; a
// sentence the gate cannot find is fatal, because the alternative is a gate that
// silently stops asserting the rule it was written for.
func readReferenceRule(t *testing.T) referenceRule {
	t.Helper()

	lines, _ := enumsSection(t)
	flat := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")

	universal := universalMemberSentence.FindStringSubmatch(flat)
	if universal == nil {
		t.Fatalf("%s § %s no longer states which member every enum carries (pattern %q); the gate has "+
			"lost the rule it asserts", dataFormatsRelPath, enumsSectionName, universalMemberSentence)
	}
	reference := referenceMemberSentence.FindStringSubmatch(flat)
	if reference == nil {
		t.Fatalf("%s § %s no longer states which enums carry a reference member (pattern %q); the gate "+
			"has lost the rule it asserts", dataFormatsRelPath, enumsSectionName, referenceMemberSentence)
	}
	if reference[4] != universal[1] {
		t.Fatalf("%s § %s names %q as the member every enum carries and %q as the member the enums "+
			"without a reference carry alone; the two must be the same member",
			dataFormatsRelPath, enumsSectionName, universal[1], reference[4])
	}

	rule := referenceRule{
		universalMember: universal[1],
		referenceMember: reference[1],
		withReference:   backtickNames(reference[2]),
		valuesOnly:      backtickNames(reference[3]),
	}
	if len(rule.withReference) == 0 || len(rule.valuesOnly) == 0 {
		t.Fatalf("%s § %s publishes %d enum(s) with a reference member and %d without; neither list may "+
			"be empty or the rule asserts nothing",
			dataFormatsRelPath, enumsSectionName, len(rule.withReference), len(rule.valuesOnly))
	}
	return rule
}

// backtickNames returns the `...` spans of one fragment, in order.
func backtickNames(fragment string) []string {
	matches := backtickSpan.FindAllStringSubmatch(fragment, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if name := strings.TrimSpace(m[1]); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// TestEnumsSpec_ReferenceRuleHoldsOnTheGenerator asserts the published rule itself
// against the binary: `values` on every enum, `state_machine_reference` on the two
// enums the SPEC names and on no other, and no third member anywhere. Both lists
// are read out of the SPEC rather than written here, so the gate cannot go green on
// a SPEC that has been edited to describe whatever the generator happens to emit —
// that edit moves the expectation and the other direction of the comparison, the
// partition check below, fails instead.
func TestEnumsSpec_ReferenceRuleHoldsOnTheGenerator(t *testing.T) {
	rule := readReferenceRule(t)
	enums := generatedEnums(t)

	// The two lists must partition the published enums: no name in both, no
	// published enum in neither, no listed enum the contract does not publish.
	classified := make(map[string]bool, len(rule.withReference)+len(rule.valuesOnly))
	for _, name := range rule.withReference {
		classified[name] = true
	}
	for _, name := range rule.valuesOnly {
		if classified[name] {
			t.Errorf("%s § %s names %s both as carrying %s and as carrying %s alone",
				dataFormatsRelPath, enumsSectionName, name, rule.referenceMember, rule.universalMember)
			continue
		}
		classified[name] = false
	}
	for _, name := range enumNamesOf(enums) {
		if _, named := classified[name]; !named {
			t.Errorf("the contract publishes the enum %s, which %s § %s does not name on either side of "+
				"the rule; every published enum must be classified or a consumer cannot tell whether the "+
				"enum carries a reference the contract failed to publish",
				name, dataFormatsRelPath, enumsSectionName)
		}
	}

	// Each named enum carries exactly the members the rule gives it.
	for name, carriesReference := range classified {
		want := []string{rule.universalMember}
		if carriesReference {
			want = append(want, rule.referenceMember)
		}
		sortStrings(want)

		got := memberKeys(generatedDefinition(t, enums, name))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the contract emits enums.%s with members %v; %s § %s publishes %v",
				name, got, dataFormatsRelPath, enumsSectionName, want)
		}
	}

	// "An enum definition has exactly two members", and the reference member is the
	// only reference the contract defines: no enum may carry a third member.
	allowed := map[string]bool{rule.universalMember: true, rule.referenceMember: true}
	for _, name := range enumNamesOf(enums) {
		for _, key := range memberKeys(generatedDefinition(t, enums, name)) {
			if !allowed[key] {
				t.Errorf("the contract emits enums.%s.%s; %s § %s defines exactly two enum-definition "+
					"members, %s and %s, and publishing a third widens the contract without saying so",
					name, key, dataFormatsRelPath, enumsSectionName,
					rule.universalMember, rule.referenceMember)
			}
		}
	}

	t.Logf("%s § %s classifies %d enum(s) as carrying %s and %d as carrying %s alone",
		dataFormatsRelPath, enumsSectionName, len(rule.withReference), rule.referenceMember,
		len(rule.valuesOnly), rule.universalMember)
}

// containsString reports whether a sorted or unsorted slice holds one name.
func containsString(haystack []string, needle string) bool {
	for _, candidate := range haystack {
		if candidate == needle {
			return true
		}
	}
	return false
}
