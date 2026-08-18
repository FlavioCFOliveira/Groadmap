// Package aihelp — contract tests for the comment surface.
//
// These tests cover the publication of the eight comment subcommands and the
// two comment-type enums on the machine-readable contract
// (SPEC/DATA_FORMATS.md § enums map entry, § Scope filtering;
// SPEC/HELP.md § Comment subcommand help specifics item 1).
//
// Two of them are deliberately generic rather than comment-specific, because
// they close a CLASS of defect rather than one instance:
//
//   - TestGenerate_EveryReferencedEnumHasValues fails for ANY enum key a flag
//     or positional argument names while enumValues() has no case for it. That
//     is exactly how TaskCommentType and SprintCommentType shipped as
//     {"values": []}: the registry named them, the static catalogue did not
//     know them, and no test looked at anything but a hard-coded list of the
//     enums that happened to exist at the time.
//   - TestGenerate_EveryEnumValueCarriesADescription is its sibling one level
//     finer: it fails for ANY published value whose description is empty. That
//     is how AuditOperation shipped 29 values and 0 descriptions while the
//     values gate above was green.
//   - TestGenerate_CommentSubcommandsAgreeAcrossScopes compares the emitted
//     subcommand objects across the three scopes instead of trusting the
//     filter, so a scope that drops or rewrites a field is a failure.
package aihelp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// commentSubcommands is the per-family list of the four comment subcommands,
// paired with the enum key its -y/--type flag must name.
var commentSubcommands = []struct {
	family string
	enum   string
	subs   []string
}{
	{family: "task", enum: "TaskCommentType", subs: []string{"comment-add", "comment-list", "comment-edit", "comment-remove"}},
	{family: "sprint", enum: "SprintCommentType", subs: []string{"comment-add", "comment-list", "comment-edit", "comment-remove"}},
}

// contractEnums extracts the enums map of a generated contract.
func contractEnums(t *testing.T, out []byte) map[string]any {
	t.Helper()
	m := unmarshalAsMap(t, out)
	enums, ok := m["enums"].(map[string]any)
	if !ok {
		t.Fatalf("enums wrong type: %T", m["enums"])
	}
	return enums
}

// enumValueList returns the (value, description) pairs of one enum, failing
// the test when the enum is missing or malformed.
func enumValueList(t *testing.T, enums map[string]any, name string) []struct{ value, description string } {
	t.Helper()
	def, ok := enums[name].(map[string]any)
	if !ok {
		t.Fatalf("enums.%s missing from contract (present: %v)", name, enumNamesOf(enums))
	}
	raw, ok := def["values"].([]any)
	if !ok {
		t.Fatalf("enums.%s.values wrong type: %T", name, def["values"])
	}
	out := make([]struct{ value, description string }, 0, len(raw))
	for i, r := range raw {
		entry, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("enums.%s.values[%d] wrong type: %T", name, i, r)
		}
		v, ok := entry["value"].(string)
		if !ok {
			t.Fatalf("enums.%s.values[%d].value missing or not a string", name, i)
		}
		d, ok := entry["description"].(string)
		if !ok {
			t.Fatalf("enums.%s.values[%d].description key missing (it must always be present)", name, i)
		}
		out = append(out, struct{ value, description string }{v, d})
	}
	return out
}

func enumNamesOf(enums map[string]any) []string {
	names := make([]string, 0, len(enums))
	for k := range enums {
		names = append(names, k)
	}
	sortStrings(names)
	return names
}

// ------------------------------------------------------------------
// Class gates: no referenced enum may serialise with an empty value
// list, and no published value may serialise without a description.
// ------------------------------------------------------------------

// TestGenerate_EveryReferencedEnumHasValues walks the enum names the registry
// actually references and requires each to carry at least one value in the
// contract. It is derived from the registry, not from a hard-coded list, so a
// newly referenced enum with no enumValues() case fails here without anyone
// having to remember to extend the test.
func TestGenerate_EveryReferencedEnumHasValues(t *testing.T) {
	reg := commands.AppRegistry()
	referenced := collectEnumNames(reg)
	if len(referenced) == 0 {
		t.Fatal("registry references no enums at all; the walk is broken")
	}

	enums := contractEnums(t, generateOrFatal(t, ScopeAll()))

	for _, name := range referenced {
		values := enumValueList(t, enums, name)
		if len(values) == 0 {
			t.Errorf("enums.%s is referenced by a flag or positional argument but serialises with an "+
				"empty values list; add a case for it to enumValues() in static.go", name)
		}
	}

	// The converse: the contract must not publish an enum nothing references,
	// which would be dead weight an agent could reach but never use.
	for _, name := range enumNamesOf(enums) {
		found := false
		for _, r := range referenced {
			if r == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("enums.%s is published but referenced by no flag or positional argument", name)
		}
	}
}

// TestGenerate_EveryEnumValueCarriesADescription is the sibling of the gate
// above, one level finer. That one fails when a referenced enum publishes no
// values at all; this one fails when a published value carries no description,
// which is the same defect one degree less visible — the agent sees the value
// exists and still cannot tell what it means, or how it differs from the value
// next to it.
//
// It is registry-derived for the same reason: the enums it checks are the ones
// the CLI actually references, so a newly referenced enum is covered the moment
// a flag names it, with nobody having to extend a list here.
//
// The shipped instance was AuditOperation, which published 29 values and 0
// descriptions while every other enum described all of its own. The argument in
// static.go was that the operation names speak for themselves; they do not.
// SPRINT_REORDER_TASKS and SPRINT_TASK_MOVE_POSITION are not distinguishable by
// name, and the six comment operations do not say by name that they are recorded
// against the parent entity rather than against the comment.
func TestGenerate_EveryEnumValueCarriesADescription(t *testing.T) {
	reg := commands.AppRegistry()
	referenced := collectEnumNames(reg)
	if len(referenced) == 0 {
		t.Fatal("registry references no enums at all; the walk is broken")
	}

	enums := contractEnums(t, generateOrFatal(t, ScopeAll()))

	// Counting, and reporting the count, is what keeps a silent regression to
	// "checked nothing" visible: a gate that inspected zero values would
	// otherwise pass exactly like a gate that inspected all of them.
	checked := 0
	for _, name := range referenced {
		values := enumValueList(t, enums, name)
		for i, v := range values {
			checked++
			if strings.TrimSpace(v.description) == "" {
				t.Errorf("enums.%s.values[%d=%s].description is empty; every value the contract publishes "+
					"must carry its description from the SPEC, because the contract is the only thing an "+
					"agent reading it has. Add the value to enumDescriptions in static.go",
					name, i, v.value)
			}
		}
	}
	if checked == 0 {
		t.Fatalf("no enum values were inspected across %d referenced enums, so this gate measured nothing",
			len(referenced))
	}
	t.Logf("checked %d values across %d referenced enums", checked, len(referenced))
}

// ------------------------------------------------------------------
// The two comment-type enums.
// ------------------------------------------------------------------

// TestGenerate_CommentTypeEnumsMatchModels pins both comment-type enums to
// the canonical slices in internal/models — same values, same order — and
// requires a non-empty description on every value.
func TestGenerate_CommentTypeEnumsMatchModels(t *testing.T) {
	enums := contractEnums(t, generateOrFatal(t, ScopeAll()))

	cases := []struct {
		name  string
		types []models.CommentType
	}{
		{"TaskCommentType", models.ValidTaskCommentTypes},
		{"SprintCommentType", models.ValidSprintCommentTypes},
	}

	for _, tc := range cases {
		values := enumValueList(t, enums, tc.name)
		if len(values) != len(tc.types) {
			t.Errorf("enums.%s has %d values, want %d (from internal/models)", tc.name, len(values), len(tc.types))
			continue
		}
		for i, want := range tc.types {
			if values[i].value != string(want) {
				t.Errorf("enums.%s.values[%d].value = %q, want %q (declaration order must match internal/models)",
					tc.name, i, values[i].value, want)
			}
			if values[i].description == "" {
				t.Errorf("enums.%s.values[%d=%s].description is empty; every comment type carries the "+
					"description from SPEC/MODELS.md § Comment Type", tc.name, i, values[i].value)
			}
		}
	}
}

// TestGenerate_CommentTypeEnumsAreTwoDisjointKeys enforces the rule that
// makes two keys necessary: flags[].enum is a single key, so publishing one
// shared seven-value enum would offer a sprint the three types a sprint
// rejects (SPEC/DATA_FORMATS.md § enums map entry).
func TestGenerate_CommentTypeEnumsAreTwoDisjointKeys(t *testing.T) {
	enums := contractEnums(t, generateOrFatal(t, ScopeAll()))

	if _, present := enums["CommentType"]; present {
		t.Error("enums.CommentType is published; the contract must expose the two per-entity subsets " +
			"(TaskCommentType, SprintCommentType) and no combined key")
	}

	sprintValues := enumValueList(t, enums, "SprintCommentType")
	got := make(map[string]bool, len(sprintValues))
	for _, v := range sprintValues {
		got[v.value] = true
	}
	// Every task-only value must be absent from the sprint enum. Membership is
	// asked of internal/models so a change to the subsets is reflected here.
	for _, tt := range models.ValidTaskCommentTypes {
		taskOnly := !models.IsValidSprintCommentType(string(tt))
		if taskOnly && got[string(tt)] {
			t.Errorf("enums.SprintCommentType publishes %s, which a sprint comment rejects with exit code 6", tt)
		}
		if !taskOnly && !got[string(tt)] {
			t.Errorf("enums.SprintCommentType omits %s, which a sprint comment accepts", tt)
		}
	}

	// Comments have no state machine, so neither enum may claim one.
	for _, name := range []string{"TaskCommentType", "SprintCommentType"} {
		def := enums[name].(map[string]any)
		if ref, present := def["state_machine_reference"]; present {
			t.Errorf("enums.%s carries state_machine_reference %v; comments have no state machine", name, ref)
		}
	}
}

// TestGenerate_CommentFlagsReachTheirFamilyEnum walks the emitted contract and
// asserts that the -y/--type flag of every comment subcommand names its own
// family's enum key, and that the key it names is present in the same
// contract slice (self-containment, SPEC/DATA_FORMATS.md § Scope filtering).
func TestGenerate_CommentFlagsReachTheirFamilyEnum(t *testing.T) {
	for _, fam := range commentSubcommands {
		for _, sub := range fam.subs {
			out := generateOrFatal(t, ScopeSubcommand(fam.family, sub))
			m := unmarshalAsMap(t, out)
			enums := contractEnums(t, out)

			entry := onlySubcommand(t, m, fam.family, sub)
			flags, _ := entry["flags"].([]any)
			seen := false
			for _, rf := range flags {
				f := rf.(map[string]any)
				name, _ := f["long"].(string)
				if name != "--type" {
					continue
				}
				seen = true
				enumName, ok := f["enum"].(string)
				if !ok {
					t.Errorf("%s %s: --type carries no enum key (got %v)", fam.family, sub, f["enum"])
					continue
				}
				if enumName != fam.enum {
					t.Errorf("%s %s: --type names enum %q, want %q", fam.family, sub, enumName, fam.enum)
				}
				if _, present := enums[enumName]; !present {
					t.Errorf("%s %s: --type names enum %q which is absent from the same contract slice",
						fam.family, sub, enumName)
				}
			}
			// comment-remove has no --type flag; every other comment
			// subcommand must have one.
			if !seen && sub != "comment-remove" {
				t.Errorf("%s %s: no --type flag in the contract", fam.family, sub)
			}
		}
	}
}

// ------------------------------------------------------------------
// Three-scope agreement (task acceptance criterion 1).
// ------------------------------------------------------------------

// onlySubcommand returns the named subcommand entry of the named family from a
// parsed contract, failing when the family or the subcommand is not uniquely
// present.
func onlySubcommand(t *testing.T, m map[string]any, family, sub string) map[string]any {
	t.Helper()
	cmds, ok := m["commands"].([]any)
	if !ok {
		t.Fatalf("commands wrong type: %T", m["commands"])
	}
	var fam map[string]any
	for _, rc := range cmds {
		c := rc.(map[string]any)
		if name, _ := c["name"].(string); name == family {
			if fam != nil {
				t.Fatalf("family %q appears more than once in commands", family)
			}
			fam = c
		}
	}
	if fam == nil {
		t.Fatalf("family %q missing from commands", family)
	}
	subs, ok := fam["subcommands"].([]any)
	if !ok {
		t.Fatalf("%s.subcommands wrong type: %T", family, fam["subcommands"])
	}
	var found map[string]any
	for _, rs := range subs {
		s := rs.(map[string]any)
		if name, _ := s["name"].(string); name == sub {
			if found != nil {
				t.Fatalf("%s %s appears more than once in subcommands", family, sub)
			}
			found = s
		}
	}
	if found == nil {
		t.Fatalf("%s %s missing from subcommands", family, sub)
	}
	return found
}

// canonicalJSON marshals a parsed value back to JSON. encoding/json sorts map
// keys, so the result is a canonical form two scopes can be compared by.
func canonicalJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(b)
}

// TestGenerate_CommentSubcommandsAgreeAcrossScopes compares each comment
// subcommand's contract object as emitted at the three scopes the CLI offers —
// whole CLI, single family, single subcommand — and requires the three to be
// identical. Scope narrowing may only remove OTHER entries; it may never
// change, trim or re-describe the entry it keeps.
func TestGenerate_CommentSubcommandsAgreeAcrossScopes(t *testing.T) {
	whole := unmarshalAsMap(t, generateOrFatal(t, ScopeAll()))

	for _, fam := range commentSubcommands {
		famScoped := unmarshalAsMap(t, generateOrFatal(t, ScopeCommand(fam.family)))
		if got := len(famScoped["commands"].([]any)); got != 1 {
			t.Errorf("%s family scope emitted %d families, want 1", fam.family, got)
		}
		for _, sub := range fam.subs {
			subScoped := unmarshalAsMap(t, generateOrFatal(t, ScopeSubcommand(fam.family, sub)))
			if got := len(subScoped["commands"].([]any)[0].(map[string]any)["subcommands"].([]any)); got != 1 {
				t.Errorf("%s %s subcommand scope emitted %d subcommands, want 1", fam.family, sub, got)
			}

			a := canonicalJSON(t, onlySubcommand(t, whole, fam.family, sub))
			b := canonicalJSON(t, onlySubcommand(t, famScoped, fam.family, sub))
			c := canonicalJSON(t, onlySubcommand(t, subScoped, fam.family, sub))
			if a != b {
				t.Errorf("%s %s differs between whole-CLI and family scope:\n whole: %s\nfamily: %s", fam.family, sub, a, b)
			}
			if a != c {
				t.Errorf("%s %s differs between whole-CLI and subcommand scope:\n whole: %s\n   sub: %s", fam.family, sub, a, c)
			}

			// The narrower scopes must still be self-contained: everything
			// outside `commands` is emitted unchanged.
			for _, field := range []string{"schema_version", "tool", "conventions", "exit_codes", "enums", "global_flags", "common_workflows", "pitfalls"} {
				want := canonicalJSON(t, whole[field])
				if got := canonicalJSON(t, famScoped[field]); got != want {
					t.Errorf("%s family scope altered top-level %s", fam.family, field)
				}
				if got := canonicalJSON(t, subScoped[field]); got != want {
					t.Errorf("%s %s subcommand scope altered top-level %s", fam.family, sub, field)
				}
			}
		}
	}
}

// TestGenerate_CommentSubcommandsDescribeThemselvesFully requires every
// comment subcommand to carry the whole subcommand shape: a summary, a
// description, a usage line, the parent-or-comment positional argument, an
// output kind, the side-effects triplet, and an exit-code list including 0.
// A subcommand present but under-described is as unusable to an agent as one
// that is absent (SPEC/DATA_FORMATS.md § subcommands array entry).
func TestGenerate_CommentSubcommandsDescribeThemselvesFully(t *testing.T) {
	whole := unmarshalAsMap(t, generateOrFatal(t, ScopeAll()))

	for _, fam := range commentSubcommands {
		for _, sub := range fam.subs {
			entry := onlySubcommand(t, whole, fam.family, sub)
			label := fam.family + " " + sub

			for _, k := range []string{"summary", "description", "usage"} {
				if v, _ := entry[k].(string); v == "" {
					t.Errorf("%s: %s is empty", label, k)
				}
			}
			for _, k := range []string{"aliases", "positional_arguments", "flags", "mutual_exclusion_groups", "prerequisites", "examples", "exit_codes"} {
				if _, present := entry[k]; !present {
					t.Errorf("%s: %s key missing (arrays are always present, never null)", label, k)
				}
			}
			if _, present := entry["idempotent"]; !present {
				t.Errorf("%s: idempotent key missing", label)
			}

			pos, _ := entry["positional_arguments"].([]any)
			if len(pos) != 1 {
				t.Errorf("%s: %d positional arguments, want exactly 1 (the parent id, or the comment's own id)", label, len(pos))
			}

			out, _ := entry["stdout_on_success"].(map[string]any)
			if kind, _ := out["kind"].(string); kind == "" {
				t.Errorf("%s: stdout_on_success.kind is empty", label)
			}
			se, _ := entry["side_effects"].(map[string]any)
			for _, k := range []string{"database", "filesystem", "network"} {
				if v, _ := se[k].(string); v == "" {
					t.Errorf("%s: side_effects.%s is empty", label, k)
				}
			}

			codes, _ := entry["exit_codes"].([]any)
			if len(codes) == 0 {
				t.Errorf("%s: exit_codes is empty", label)
			}
			hasZero := false
			for _, rc := range codes {
				if n, ok := rc.(float64); ok && n == 0 {
					hasZero = true
				}
			}
			if !hasZero {
				t.Errorf("%s: exit_codes %v does not include 0", label, codes)
			}
		}
	}
}

// ------------------------------------------------------------------
// Curated catalogues: the comment workflow and the two comment pitfalls.
// ------------------------------------------------------------------

// TestGenerate_CommentWorkflowPresent requires the curated working-log
// workflow to exist and to exercise the comment subcommands, so an agent
// discovers not just that the subcommands exist but how they compose over the
// life of a task.
func TestGenerate_CommentWorkflowPresent(t *testing.T) {
	const name = "record_task_working_log"

	flows := staticWorkflows()
	var found *Workflow
	for i := range flows {
		if flows[i].Name == name {
			found = &flows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("common_workflows is missing the %q entry", name)
	}

	// It must reach at least comment-add, comment-edit and comment-list: a
	// workflow that only ever adds shows no way to read the log back.
	want := map[string]bool{"comment-add": false, "comment-edit": false, "comment-list": false}
	for _, step := range found.Steps {
		for sub := range want {
			if strings.Contains(step.Command, "task "+sub) {
				want[sub] = true
			}
		}
	}
	for sub, seen := range want {
		if !seen {
			t.Errorf("workflow %q never uses `task %s`", name, sub)
		}
	}
	if len(found.Prerequisites) == 0 {
		t.Errorf("workflow %q declares no prerequisites", name)
	}
	if found.ExpectedOutcome == "" {
		t.Errorf("workflow %q declares no expected_outcome", name)
	}
}

// TestGenerate_CommentPitfallsPresent requires both curated comment pitfalls:
// the per-entity type trap, and the assumption that every comment subcommand
// prints JSON.
func TestGenerate_CommentPitfallsPresent(t *testing.T) {
	byID := make(map[string]Pitfall)
	for _, p := range staticPitfalls() {
		byID[p.ID] = p
	}

	for _, want := range []string{"task_only_comment_type_on_sprint", "parse_comment_mutation_stdout"} {
		p, present := byID[want]
		if !present {
			t.Errorf("pitfalls is missing the %q entry", want)
			continue
		}
		if p.WrongExample == "" || p.CorrectExample == "" || p.Description == "" || p.Reference == "" {
			t.Errorf("pitfall %q has an empty required field: %+v", want, p)
		}
	}

	// The type pitfall must name a task-only value in its wrong example and a
	// sprint-accepted value in its correct example, otherwise it does not
	// demonstrate the trap it claims to.
	p := byID["task_only_comment_type_on_sprint"]
	taskOnlyShown := false
	for _, tt := range models.ValidTaskCommentTypes {
		if models.IsValidSprintCommentType(string(tt)) {
			continue
		}
		if strings.Contains(p.WrongExample, string(tt)) {
			taskOnlyShown = true
		}
		if strings.Contains(p.CorrectExample, string(tt)) {
			t.Errorf("pitfall %q correct_example uses the task-only type %s, which a sprint rejects", p.ID, tt)
		}
	}
	if !taskOnlyShown {
		t.Errorf("pitfall %q wrong_example names no task-only comment type: %q", p.ID, p.WrongExample)
	}
}
