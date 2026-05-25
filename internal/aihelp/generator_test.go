// Package aihelp — unit tests for the AI Agent Contract generator.
//
// The generator is pure (no I/O, no goroutines, no clock) so every
// test in this file is a single Generate call followed by structural
// assertions on the returned bytes. Tests are grouped by acceptance
// criterion from the task description so a future reviewer can map
// failures back to spec requirements directly.
package aihelp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
)

// testInfo returns a canonical ContractInfo for use across the test
// suite. The values mirror cmd/rmp/main.go but are duplicated here so
// the tests do not depend on the binary entry point.
func testInfo() ContractInfo {
	return ContractInfo{
		ToolName:      "rmp",
		DisplayName:   "Groadmap",
		BinaryVersion: "1.3.0",
		Description:   "CLI for managing technical roadmaps in SQLite.",
	}
}

// generateOrFatal is a tiny helper that calls Generate and aborts the
// test on error. Removes a half-dozen identical err-checks from each
// test below.
func generateOrFatal(t *testing.T, scope Scope) []byte {
	t.Helper()
	out, err := Generate(scope, testInfo())
	if err != nil {
		t.Fatalf("Generate(%+v) returned error: %v", scope, err)
	}
	return out
}

// unmarshalAsMap parses the generator output into a generic map for
// schema-level inspection. Returns the parsed map.
func unmarshalAsMap(t *testing.T, out []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\noutput:\n%s", err, out)
	}
	return m
}

// ------------------------------------------------------------------
// Acceptance criterion 1: valid JSON, all required top-level keys.
// ------------------------------------------------------------------

func TestGenerate_TopLevelKeysPresent(t *testing.T) {
	out := generateOrFatal(t, ScopeAll())
	m := unmarshalAsMap(t, out)

	required := []string{
		"schema_version",
		"tool",
		"conventions",
		"exit_codes",
		"enums",
		"global_flags",
		"commands",
		"common_workflows",
		"pitfalls",
	}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			t.Errorf("top-level key %q is missing", k)
		}
	}
}

func TestGenerate_ToolBlock(t *testing.T) {
	out := generateOrFatal(t, ScopeAll())
	m := unmarshalAsMap(t, out)
	tool, ok := m["tool"].(map[string]any)
	if !ok {
		t.Fatalf("tool field has wrong type: %T", m["tool"])
	}
	for k, want := range map[string]string{
		"name":           "rmp",
		"display_name":   "Groadmap",
		"binary_version": "1.3.0",
	} {
		if got, _ := tool[k].(string); got != want {
			t.Errorf("tool.%s = %q, want %q", k, got, want)
		}
	}
	if desc, _ := tool["description"].(string); desc == "" {
		t.Error("tool.description must be non-empty")
	}
}

func TestGenerate_ConventionsBlock(t *testing.T) {
	out := generateOrFatal(t, ScopeAll())
	m := unmarshalAsMap(t, out)
	c, ok := m["conventions"].(map[string]any)
	if !ok {
		t.Fatalf("conventions field has wrong type: %T", m["conventions"])
	}
	for _, k := range []string{
		"stdout_on_success", "stderr_on_error", "json_indent",
		"charset", "locale", "datetime_format", "datetime_example",
		"roadmap_flag", "list_separator", "ai_agent_env_var",
	} {
		if _, ok := c[k]; !ok {
			t.Errorf("conventions.%s missing", k)
		}
	}
	// json_indent must be 2 per SPEC.
	if v, _ := c["json_indent"].(float64); v != 2 {
		t.Errorf("conventions.json_indent = %v, want 2", v)
	}
	// ai_agent_env_var must be the documented triplet.
	env, _ := c["ai_agent_env_var"].(map[string]any)
	if env["name"] != "AI_AGENT" || env["enable_value"] != "1" {
		t.Errorf("ai_agent_env_var = %v, want AI_AGENT/1", env)
	}
}

func TestGenerate_ExitCodesCatalogue(t *testing.T) {
	out := generateOrFatal(t, ScopeAll())
	m := unmarshalAsMap(t, out)
	codes, ok := m["exit_codes"].([]any)
	if !ok {
		t.Fatalf("exit_codes wrong type: %T", m["exit_codes"])
	}
	// The SPEC catalogue has 10 entries: 0,1,2,3,4,5,6,126,127,130.
	if len(codes) != 10 {
		t.Fatalf("exit_codes len = %d, want 10", len(codes))
	}
	seen := make(map[int]bool, 10)
	for _, e := range codes {
		entry := e.(map[string]any)
		code := int(entry["code"].(float64))
		seen[code] = true
		if name, _ := entry["name"].(string); name == "" {
			t.Errorf("exit_code %d has empty name", code)
		}
		if meaning, _ := entry["meaning"].(string); meaning == "" {
			t.Errorf("exit_code %d has empty meaning", code)
		}
	}
	for _, want := range []int{0, 1, 2, 3, 4, 5, 6, 126, 127, 130} {
		if !seen[want] {
			t.Errorf("exit_codes missing code %d", want)
		}
	}
}

// ------------------------------------------------------------------
// Acceptance criterion 2: pretty-printed, 2-space indent, trailing
// newline.
// ------------------------------------------------------------------

func TestGenerate_FormattingRules(t *testing.T) {
	out := generateOrFatal(t, ScopeAll())

	// Trailing newline.
	if len(out) == 0 || out[len(out)-1] != '\n' {
		t.Fatal("output does not end with a newline")
	}

	// 2-space indent: every leading whitespace block before a JSON
	// content character must be a multiple of two spaces, and tabs
	// must never appear.
	if bytes.Contains(out, []byte("\t")) {
		t.Error("output must not contain tab characters (indent is 2 spaces)")
	}
	scanner := bytes.Split(out, []byte("\n"))
	for i, line := range scanner {
		var spaces int
		for _, b := range line {
			if b == ' ' {
				spaces++
				continue
			}
			break
		}
		if spaces%2 != 0 {
			t.Errorf("line %d has odd indent (%d spaces): %q", i, spaces, line)
		}
	}
}

// ------------------------------------------------------------------
// Acceptance criterion 3: scope filtering.
// ------------------------------------------------------------------

func TestGenerate_ScopeAll_ContainsEveryFamily(t *testing.T) {
	out := generateOrFatal(t, ScopeAll())
	m := unmarshalAsMap(t, out)
	cmds := m["commands"].([]any)

	got := make(map[string]bool, len(cmds))
	for _, e := range cmds {
		got[e.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"roadmap", "task", "sprint", "backlog", "audit", "stats"} {
		if !got[want] {
			t.Errorf("ScopeAll commands missing family %q", want)
		}
	}
}

func TestGenerate_ScopeCommand_FiltersToOneFamily(t *testing.T) {
	out := generateOrFatal(t, ScopeCommand("task"))
	m := unmarshalAsMap(t, out)
	cmds := m["commands"].([]any)

	if len(cmds) != 1 {
		t.Fatalf("ScopeCommand(task): expected 1 command, got %d", len(cmds))
	}
	if name, _ := cmds[0].(map[string]any)["name"].(string); name != "task" {
		t.Errorf("ScopeCommand(task): commands[0].name = %q, want %q", name, "task")
	}
	// Subcommands must still contain every task subcommand from the
	// registry (count comparison is enough).
	reg := commands.AppRegistry()
	taskCmd := reg.FindCommand("task")
	subs := cmds[0].(map[string]any)["subcommands"].([]any)
	if len(subs) != len(taskCmd.Subcommands) {
		t.Errorf("subcommand count = %d, want %d (registry)", len(subs), len(taskCmd.Subcommands))
	}

	// Top-level fields preserved.
	for _, k := range []string{"schema_version", "tool", "conventions", "exit_codes", "enums", "global_flags", "common_workflows", "pitfalls"} {
		if _, ok := m[k]; !ok {
			t.Errorf("ScopeCommand strips %q from top level", k)
		}
	}
}

func TestGenerate_ScopeCommand_ResolvedByAlias(t *testing.T) {
	// The task family's alias is "t"; scope filtering must accept aliases.
	out := generateOrFatal(t, ScopeCommand("t"))
	m := unmarshalAsMap(t, out)
	cmds := m["commands"].([]any)
	if len(cmds) != 1 {
		t.Fatalf("ScopeCommand(t alias): expected 1 command, got %d", len(cmds))
	}
	if name, _ := cmds[0].(map[string]any)["name"].(string); name != "task" {
		t.Errorf("alias resolution: commands[0].name = %q, want \"task\"", name)
	}
}

func TestGenerate_ScopeSubcommand_FiltersToOneSubcommand(t *testing.T) {
	out := generateOrFatal(t, ScopeSubcommand("task", "list"))
	m := unmarshalAsMap(t, out)
	cmds := m["commands"].([]any)

	if len(cmds) != 1 {
		t.Fatalf("ScopeSubcommand: expected 1 command, got %d", len(cmds))
	}
	subs := cmds[0].(map[string]any)["subcommands"].([]any)
	if len(subs) != 1 {
		t.Fatalf("ScopeSubcommand: expected 1 subcommand, got %d", len(subs))
	}
	if name, _ := subs[0].(map[string]any)["name"].(string); name != "list" {
		t.Errorf("subcommand name = %q, want \"list\"", name)
	}
}

func TestGenerate_ScopeUnknownCommand_Error(t *testing.T) {
	_, err := Generate(ScopeCommand("not-a-real-family"), testInfo())
	if err == nil {
		t.Fatal("expected error for unknown command scope, got nil")
	}
	if !strings.Contains(err.Error(), "not-a-real-family") {
		t.Errorf("error %q does not mention the offending name", err)
	}
}

func TestGenerate_ScopeUnknownSubcommand_Error(t *testing.T) {
	_, err := Generate(ScopeSubcommand("task", "nonexistent"), testInfo())
	if err == nil {
		t.Fatal("expected error for unknown subcommand scope, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error %q does not mention the offending name", err)
	}
}

// ------------------------------------------------------------------
// Acceptance criterion 4: schema_version is the canonical SPEC value.
// ------------------------------------------------------------------

func TestGenerate_SchemaVersionMatchesSPEC(t *testing.T) {
	out := generateOrFatal(t, ScopeAll())
	m := unmarshalAsMap(t, out)
	got, _ := m["schema_version"].(string)
	// SPEC/DATA_FORMATS.md § AI Agent Contract declares "1.0.0".
	const want = "1.0.0"
	if got != want {
		t.Errorf("schema_version = %q, want %q (SPEC/DATA_FORMATS.md § AI Agent Contract)", got, want)
	}
	if got != SchemaVersion {
		t.Errorf("schema_version = %q, package SchemaVersion const = %q", got, SchemaVersion)
	}
}

// ------------------------------------------------------------------
// Acceptance criterion 5: parses cleanly and round-trips.
// ------------------------------------------------------------------

func TestGenerate_RoundTripEquivalence(t *testing.T) {
	out := generateOrFatal(t, ScopeAll())

	// First unmarshal → map. Re-marshal → bytes. Second unmarshal of
	// the re-marshalled bytes → map. The two maps must be deeply
	// equal. (Direct byte comparison would fail because re-marshal
	// uses a fixed key order and no indent.)
	var first map[string]any
	if err := json.Unmarshal(out, &first); err != nil {
		t.Fatalf("first unmarshal: %v", err)
	}
	reMarshalled, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	var second map[string]any
	if err := json.Unmarshal(reMarshalled, &second); err != nil {
		t.Fatalf("second unmarshal: %v", err)
	}

	// Re-marshal both to canonical (sorted, indent-free) form and
	// byte-compare. json.Marshal sorts map keys lexicographically, so
	// equivalent structures produce identical bytes.
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if !bytes.Equal(a, b) {
		t.Errorf("round-trip lost information.\nfirst:  %s\nsecond: %s", a, b)
	}
}

// ------------------------------------------------------------------
// Bonus structural checks driven by the task description.
// ------------------------------------------------------------------

func TestGenerate_AIHelpFlagInGlobalFlags(t *testing.T) {
	out := generateOrFatal(t, ScopeAll())
	m := unmarshalAsMap(t, out)
	globals, _ := m["global_flags"].([]any)
	for _, raw := range globals {
		entry := raw.(map[string]any)
		if entry["long"] == "--ai-help" {
			if entry["short"] != nil {
				t.Errorf("--ai-help must have short=null, got %v", entry["short"])
			}
			if entry["type"] != "boolean" {
				t.Errorf("--ai-help type = %v, want \"boolean\"", entry["type"])
			}
			if entry["description"] == "" {
				t.Error("--ai-help has empty description")
			}
			return
		}
	}
	t.Error("--ai-help flag not found in global_flags")
}

func TestGenerate_StubsAreEmptyArraysNotNull(t *testing.T) {
	out := generateOrFatal(t, ScopeAll())
	m := unmarshalAsMap(t, out)

	for _, k := range []string{"common_workflows", "pitfalls"} {
		v, ok := m[k]
		if !ok {
			t.Errorf("%s missing", k)
			continue
		}
		if v == nil {
			t.Errorf("%s is null, want []", k)
			continue
		}
		arr, ok := v.([]any)
		if !ok {
			t.Errorf("%s wrong type: %T, want []any", k, v)
			continue
		}
		if len(arr) != 0 {
			t.Errorf("%s has %d entries, want 0 (stub for task #7)", k, len(arr))
		}
	}

	// Double-check the literal JSON form: the key must be followed by
	// "[]" (possibly with whitespace) and never "null".
	for _, k := range []string{"common_workflows", "pitfalls"} {
		needle := []byte("\"" + k + "\": null")
		if bytes.Contains(out, needle) {
			t.Errorf("%s is serialised as null in the JSON body", k)
		}
	}
}

func TestGenerate_EnumsPopulated(t *testing.T) {
	out := generateOrFatal(t, ScopeAll())
	m := unmarshalAsMap(t, out)
	enums, _ := m["enums"].(map[string]any)

	// Every enum name referenced by the registry must appear with at
	// least one value.
	wanted := []string{"TaskStatus", "TaskType", "SprintStatus", "AuditOperation", "AuditEntityType", "TaskSort"}
	for _, name := range wanted {
		def, ok := enums[name].(map[string]any)
		if !ok {
			t.Errorf("enums.%s missing", name)
			continue
		}
		values, _ := def["values"].([]any)
		if len(values) == 0 {
			t.Errorf("enums.%s has empty values", name)
		}
		for i, v := range values {
			entry := v.(map[string]any)
			if _, ok := entry["value"]; !ok {
				t.Errorf("enums.%s.values[%d] missing 'value'", name, i)
			}
			if _, ok := entry["description"]; !ok {
				t.Errorf("enums.%s.values[%d] missing 'description'", name, i)
			}
		}
	}
}

func TestGenerate_EveryRegistryCommandSurfaces(t *testing.T) {
	out := generateOrFatal(t, ScopeAll())
	m := unmarshalAsMap(t, out)
	cmds := m["commands"].([]any)
	reg := commands.AppRegistry()

	if len(cmds) != len(reg.Commands) {
		t.Fatalf("commands count = %d, registry count = %d", len(cmds), len(reg.Commands))
	}

	for i, raw := range cmds {
		entry := raw.(map[string]any)
		name := entry["name"].(string)
		regCmd := &reg.Commands[i]
		if name != regCmd.Name {
			t.Errorf("commands[%d].name = %q, registry = %q", i, name, regCmd.Name)
		}

		if regCmd.HasSubcommand {
			subs := entry["subcommands"].([]any)
			if len(subs) != len(regCmd.Subcommands) {
				t.Errorf("commands[%d=%s].subcommands count = %d, want %d", i, name, len(subs), len(regCmd.Subcommands))
			}
		} else {
			// Leaf family: subcommands key must be absent (omitempty).
			if _, present := entry["subcommands"]; present {
				t.Errorf("leaf command %q must not have subcommands key", name)
			}
			// Leaf-promoted fields must be present.
			for _, k := range []string{"usage", "flags", "stdout_on_success", "side_effects", "idempotent", "exit_codes", "examples"} {
				if _, present := entry[k]; !present {
					t.Errorf("leaf command %q missing promoted field %q", name, k)
				}
			}
		}
	}
}

func TestGenerate_FlagNullVsAbsentDistinction(t *testing.T) {
	// Use the task-list subcommand: --sort has Default="priority" (so
	// "default" must be a string, not null), --status has Enum=
	// "TaskStatus" (so "enum" must be a string), --priority has a
	// range (so "range" must be present), and the global --help has
	// no default and no enum (so both should be null).
	out := generateOrFatal(t, ScopeSubcommand("task", "list"))
	m := unmarshalAsMap(t, out)
	subs := m["commands"].([]any)[0].(map[string]any)["subcommands"].([]any)
	listFlags := subs[0].(map[string]any)["flags"].([]any)

	byLong := make(map[string]map[string]any, len(listFlags))
	for _, f := range listFlags {
		entry := f.(map[string]any)
		byLong[entry["long"].(string)] = entry
	}

	sort := byLong["--sort"]
	if sort["default"] != "priority" {
		t.Errorf("--sort default = %v, want \"priority\"", sort["default"])
	}
	if sort["enum"] != "TaskSort" {
		t.Errorf("--sort enum = %v, want \"TaskSort\"", sort["enum"])
	}

	status := byLong["--status"]
	if status["enum"] != "TaskStatus" {
		t.Errorf("--status enum = %v, want \"TaskStatus\"", status["enum"])
	}
	if status["default"] != nil {
		t.Errorf("--status default = %v, want null", status["default"])
	}

	priority := byLong["--priority"]
	r, ok := priority["range"].(map[string]any)
	if !ok {
		t.Fatalf("--priority range missing or wrong type: %v", priority["range"])
	}
	if r["min"] != float64(0) || r["max"] != float64(9) {
		t.Errorf("--priority range = %v, want min=0 max=9", r)
	}

	help := byLong["--help"]
	if help["default"] != nil {
		t.Errorf("--help default = %v, want null", help["default"])
	}
	if help["enum"] != nil {
		t.Errorf("--help enum = %v, want null", help["enum"])
	}
	if _, present := help["range"]; present {
		t.Errorf("--help must not have range key, got %v", help["range"])
	}
}

func TestGenerate_LeafCommandStatsFlattens(t *testing.T) {
	out := generateOrFatal(t, ScopeCommand("stats"))
	m := unmarshalAsMap(t, out)
	cmds := m["commands"].([]any)
	if len(cmds) != 1 {
		t.Fatalf("stats scope: expected 1 command, got %d", len(cmds))
	}
	entry := cmds[0].(map[string]any)

	// Subcommands must be absent for the leaf family.
	if v, present := entry["subcommands"]; present {
		t.Errorf("leaf command 'stats' has subcommands key: %v", v)
	}
	// Promoted fields must be present.
	if _, ok := entry["usage"].(string); !ok {
		t.Error("stats.usage must be a string")
	}
	flags, ok := entry["flags"].([]any)
	if !ok || len(flags) == 0 {
		t.Error("stats.flags must be a non-empty array (at minimum --roadmap and --help)")
	}
}

func TestGenerate_Deterministic(t *testing.T) {
	// Two consecutive Generate calls must return byte-identical output.
	a := generateOrFatal(t, ScopeAll())
	b := generateOrFatal(t, ScopeAll())
	if !bytes.Equal(a, b) {
		t.Errorf("Generate is non-deterministic")
	}
}
