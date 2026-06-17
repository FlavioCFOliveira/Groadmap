// Package aihelp — contract generator.
//
// Generate is the only public entry point. It walks the singleton
// command registry (internal/commands.AppRegistry) and the static
// data declared in this package, applies the scope filter requested
// by the caller, and returns pretty-printed JSON ready for stdout.
//
// The generator is deliberately allocation-conscious but not
// optimised: the contract is emitted at most once per process
// invocation, so clarity outranks micro-performance. The hot path of
// the binary is unaffected by anything in this file.
package aihelp

import (
	"encoding/json"
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ContractInfo carries the binary identity into the generator. It is
// injected by the caller (cmd/rmp/main.go in the runtime path, or
// test code in the unit-test path) so this package never imports
// cmd/rmp and the version constant lives in exactly one place.
type ContractInfo struct {
	// ToolName is the canonical binary name; expected: "rmp".
	ToolName string
	// DisplayName is the human-readable product name; expected: "Groadmap".
	DisplayName string
	// BinaryVersion is the bare semver string of the binary (no
	// "Groadmap version " prefix). E.g. "1.3.0".
	BinaryVersion string
	// Description is the one-sentence summary of what the tool does.
	Description string
}

// Scope selects which subtree of the CLI is emitted. The zero value
// is whole-CLI (no filtering applied).
type Scope struct {
	// Command, when non-empty, restricts `commands` to the single
	// command family with that canonical name (or alias).
	Command string
	// Subcommand, when non-empty, further restricts the selected
	// family's `subcommands` array to that single subcommand (canonical
	// name or alias). Ignored when Command is empty.
	Subcommand string
}

// ScopeAll returns the whole-CLI scope. Equivalent to a zero-value
// Scope; named for call-site readability.
func ScopeAll() Scope { return Scope{} }

// ScopeCommand returns a scope restricted to one command family.
func ScopeCommand(name string) Scope { return Scope{Command: name} }

// ScopeSubcommand returns a scope restricted to a single subcommand
// under one command family.
func ScopeSubcommand(command, subcommand string) Scope {
	return Scope{Command: command, Subcommand: subcommand}
}

// Generate builds the AI Agent Contract and returns it as
// pretty-printed JSON with a trailing newline. The output is
// deterministic for a given binary+scope (no timestamps, no
// process-id, no map iteration leakage — json.MarshalIndent sorts map
// keys lexicographically).
//
// Errors are returned only when scope filtering names a command or
// subcommand that does not exist in the registry; this maps to exit
// code 2 (EXIT_MISUSE) when surfaced by the CLI wiring in a later
// task. Marshalling errors are surfaced as wrapped utils.ErrDatabase
// equivalents — but in practice json.MarshalIndent on the Contract
// type tree cannot fail, so the marshalling branch is unreachable in
// production.
func Generate(scope Scope, info ContractInfo) ([]byte, error) {
	reg := commands.AppRegistry()

	commandsField, err := buildCommands(reg, scope)
	if err != nil {
		// Scope-resolution failure is a misuse, not an emission: do
		// NOT flip the WasInvoked sentinel. The downstream hint
		// emitters (tasks #5/#6) should still attach the agent hint
		// to the resulting error so the agent can correct the scope.
		return nil, err
	}

	// Past this point the contract IS being emitted. Flip the sentinel
	// BEFORE marshalling so that any failure between here and stdout
	// still suppresses the agent hint on the error path — the agent
	// is interacting with the contract surface and should not also see
	// "run --ai-help" as guidance.
	markInvoked()

	contract := Contract{
		SchemaVersion: SchemaVersion,
		Tool: Tool{
			Name:          info.ToolName,
			DisplayName:   info.DisplayName,
			BinaryVersion: info.BinaryVersion,
			Description:   info.Description,
		},
		Conventions: staticConventions(),
		ExitCodes:   staticExitCodes(),
		Enums:       buildEnums(reg),
		GlobalFlags: buildFlagList(reg.Globals),
		Commands:    commandsField,
		// Curated catalogues mandated by SPEC/DATA_FORMATS.md § AI Agent
		// Contract. See workflows.go / pitfalls.go for the canonical
		// entries and their rationale.
		CommonWorkflows: staticWorkflows(),
		Pitfalls:        staticPitfalls(),
	}

	out, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("ai-help: marshal contract: %w", err)
	}
	// Trailing newline per SPEC/COMMANDS.md § AI Help "Output (stdout JSON)".
	out = append(out, '\n')
	return out, nil
}

// buildCommands applies the scope filter and projects the matching
// Command/Subcommand entries. Whole-CLI scope returns every family in
// declaration order; command-scope returns one family; subcommand-
// scope returns one family containing exactly one subcommand.
func buildCommands(reg *commands.Registry, scope Scope) ([]CommandEntry, error) {
	if scope.Command == "" {
		out := make([]CommandEntry, 0, len(reg.Commands))
		for i := range reg.Commands {
			out = append(out, buildCommand(&reg.Commands[i], ""))
		}
		return out, nil
	}

	cmd := reg.FindCommand(scope.Command)
	if cmd == nil {
		return nil, fmt.Errorf("%w: unknown command for --ai-help scope: %q", utils.ErrInvalidInput, scope.Command)
	}

	if scope.Subcommand == "" {
		return []CommandEntry{buildCommand(cmd, "")}, nil
	}

	// Subcommand-scope: validate the subcommand exists before filtering.
	if sub := cmd.FindSubcommand(scope.Subcommand); sub == nil {
		return nil, fmt.Errorf("%w: unknown %s subcommand for --ai-help scope: %q", utils.ErrInvalidInput, cmd.Name, scope.Subcommand)
	}
	return []CommandEntry{buildCommand(cmd, scope.Subcommand)}, nil
}

// buildCommand projects one commands.Command into a CommandEntry. Both
// branching families and single-action leaf commands carry their
// actions in the Subcommands array (SPEC/DATA_FORMATS.md § Single-action
// commands): the contract is NOT flattened. For a leaf command the
// registry stores its single action with an empty Name; the contract
// repeats the command's own name on that one subcommand so traversal is
// uniform. When filterSubcommand is non-empty (subcommand-scope), the
// produced Subcommands array contains only the matching subcommand.
func buildCommand(c *commands.Command, filterSubcommand string) CommandEntry {
	entry := CommandEntry{
		Name:          c.Name,
		Aliases:       emptySliceIfNil(c.Aliases),
		Summary:       c.Summary,
		Description:   c.Description,
		Prerequisites: emptySliceIfNil(c.Prerequisites),
		Subcommands:   make([]SubcommandEntry, 0, len(c.Subcommands)),
	}

	for i := range c.Subcommands {
		s := &c.Subcommands[i]
		// Single-action leaf: the registry leaves Name empty; surface
		// the command's own name so commands[].subcommands[] is
		// uniformly traversable and never carries an anonymous entry.
		name := s.Name
		if !c.HasSubcommand && name == "" {
			name = c.Name
		}
		if filterSubcommand != "" && !subcommandMatches(s, filterSubcommand) {
			continue
		}
		entry.Subcommands = append(entry.Subcommands, buildSubcommand(s, name))
	}
	return entry
}

// subcommandMatches reports whether the given subcommand answers to
// the supplied name token (canonical or alias). Mirrors the matching
// logic used by Command.FindSubcommand without re-routing through it
// (we already have the resolved *Subcommand pointer; avoid the
// second name-walk by inlining the comparison).
func subcommandMatches(s *commands.Subcommand, name string) bool {
	if s.Name == name {
		return true
	}
	for _, a := range s.Aliases {
		if a == name {
			return true
		}
	}
	return false
}

// buildSubcommand projects one commands.Subcommand. name overrides the
// emitted name (used to repeat a single-action command's own name on
// its lone, empty-named action); pass s.Name for ordinary subcommands.
// Every array-typed field is rendered as `[]` when empty, never `null`
// (SPEC/DATA_FORMATS.md § Empty-array serialization).
func buildSubcommand(s *commands.Subcommand, name string) SubcommandEntry {
	so := buildSuccessOutput(s.Output)
	se := buildSideEffects(s.SideEffects)
	entry := SubcommandEntry{
		Name:                  name,
		Aliases:               emptySliceIfNil(s.Aliases),
		Summary:               s.Summary,
		Description:           s.Description,
		Usage:                 s.Usage,
		PositionalArguments:   buildPositionalList(s.Positional),
		Flags:                 buildFlagList(s.Flags),
		MutualExclusionGroups: emptyGroupsIfNil(s.MutexGroups),
		StdoutOnSuccess:       so,
		SideEffects:           se,
		Idempotent:            s.Idempotent,
		ExitCodes:             s.ExitCodes,
		Prerequisites:         emptySliceIfNil(s.Prerequisites),
		Examples:              buildExampleList(s.Examples),
	}
	if s.ReadsStdin {
		entry.ReadsStdin = boolPtr(true)
	}
	return entry
}

// buildFlagList projects every flag, applying the null/absent
// distinction documented in SPEC/DATA_FORMATS.md § Field reference:
// flag entry.
func buildFlagList(flags []commands.Flag) []FlagEntry {
	out := make([]FlagEntry, len(flags))
	for i := range flags {
		// Index, not range-by-value: commands.Flag is 160 bytes.
		out[i] = buildFlag(&flags[i])
	}
	return out
}

// buildFlag is the per-flag projection. The pointer fields below are
// the marker for null-vs-omitted: nil means "key present, value
// null"; an empty omitempty field means "key absent from JSON". f is
// taken by pointer because commands.Flag is heavy (160 bytes) and the
// caller already has it addressable.
func buildFlag(f *commands.Flag) FlagEntry {
	entry := FlagEntry{
		Long:                  f.Long,
		Short:                 stringPtrOrNil(f.Short),
		Type:                  f.Type,
		Required:              f.Required,
		Default:               stringPtrOrNil(f.Default),
		Enum:                  stringPtrOrNil(f.Enum),
		Description:           f.Description,
		MutuallyExclusiveWith: nilSliceIfEmpty(f.MutuallyExclusiveWith),
	}
	if f.HasRange {
		// An unbounded-above integer (RangeMin > 0, RangeMax == 0) must
		// NOT serialise as "max": 0, which reads as a zero ceiling.
		// Emit a min-only range by leaving Max nil (omitempty) in that
		// case; otherwise carry the declared upper bound.
		r := &Range{Min: f.RangeMin}
		if f.RangeMax != 0 {
			max := f.RangeMax
			r.Max = &max
		}
		entry.Range = r
	}
	if f.MinLength != 0 {
		v := f.MinLength
		entry.MinLength = &v
	}
	if f.MaxLength != 0 {
		v := f.MaxLength
		entry.MaxLength = &v
	}
	if f.StdinFallback {
		entry.StdinFallback = boolPtr(true)
	}
	return entry
}

// buildPositionalList projects positional arguments.
func buildPositionalList(args []commands.Argument) []PositionalArgument {
	if len(args) == 0 {
		return []PositionalArgument{}
	}
	out := make([]PositionalArgument, len(args))
	for i, a := range args {
		out[i] = PositionalArgument{
			Name:        a.Name,
			Type:        a.Type,
			Required:    a.Required,
			Enum:        stringPtrOrNil(a.Enum),
			Description: a.Description,
		}
	}
	return out
}

// buildSuccessOutput projects stdout-on-success.
func buildSuccessOutput(o commands.SuccessOutput) SuccessOutput {
	return SuccessOutput{
		Kind:    o.Kind,
		Schema:  stringPtrOrNil(o.Schema),
		Example: stringPtrOrNil(o.Example),
	}
}

// buildSideEffects projects the side-effects triplet.
func buildSideEffects(s commands.SideEffects) SideEffects {
	return SideEffects{
		Database:   s.Database,
		Filesystem: s.Filesystem,
		Network:    s.Network,
	}
}

// buildExampleList projects the worked-example list. An empty Examples
// slice yields an empty JSON array (`[]`) rather than `null` so
// consumers always see the same key shape.
func buildExampleList(examples []commands.Example) []ExampleEntry {
	out := make([]ExampleEntry, len(examples))
	for i, e := range examples {
		out[i] = ExampleEntry{
			Title:  e.Title,
			Cmd:    e.Cmd,
			Stdout: e.Stdout,
			Stderr: e.Stderr,
			Exit:   e.Exit,
		}
	}
	return out
}

// buildEnums walks the registry, collects every enum name referenced
// by any flag or positional argument, and returns the value-list
// projection for each. The walk is exhaustive (every command, every
// subcommand, every flag, every positional) so adding a new enum
// reference automatically pulls the enum into the contract.
func buildEnums(reg *commands.Registry) map[string]EnumDefinition {
	names := collectEnumNames(reg)
	out := make(map[string]EnumDefinition, len(names))
	for _, name := range names {
		values := enumValues(name)
		if values == nil {
			// Registry references an enum the static catalogue does
			// not know about. Surface a single-value definition so
			// the JSON consumer at least sees the name; the missing
			// values are a registry/static-data bug worth fixing
			// before release.
			out[name] = EnumDefinition{Values: []EnumValue{}}
			continue
		}
		out[name] = EnumDefinition{
			Values:                buildEnumValues(name, values),
			StateMachineReference: stateMachineRefs[name],
		}
	}
	return out
}

// buildEnumValues attaches the description (from enumDescriptions) to
// each value. Unknown values render with an empty description string
// rather than crashing — the key is always present, value never null.
func buildEnumValues(enumName string, values []string) []EnumValue {
	descs := enumDescriptions[enumName]
	out := make([]EnumValue, len(values))
	for i, v := range values {
		out[i] = EnumValue{Value: v, Description: descs[v]}
	}
	return out
}

// collectEnumNames walks every flag and every positional argument in
// the registry (including globals) and returns the set of referenced
// enum names in sorted order. Sorted output keeps tests stable across
// Go versions.
func collectEnumNames(reg *commands.Registry) []string {
	seen := make(map[string]struct{})
	addFromFlags := func(flags []commands.Flag) {
		// Index-based loop: commands.Flag is 160 bytes; range-by-value
		// would copy each one onto the stack.
		for i := range flags {
			if flags[i].Enum != "" {
				seen[flags[i].Enum] = struct{}{}
			}
		}
	}
	addFromArgs := func(args []commands.Argument) {
		for i := range args {
			if args[i].Enum != "" {
				seen[args[i].Enum] = struct{}{}
			}
		}
	}
	addFromFlags(reg.Globals)
	for i := range reg.Commands {
		c := &reg.Commands[i]
		for j := range c.Subcommands {
			s := &c.Subcommands[j]
			addFromFlags(s.Flags)
			addFromArgs(s.Positional)
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	// Sort for deterministic output. Using simple insertion sort
	// would be fine for the tiny N here, but stdlib sort is clearer.
	sortStrings(out)
	return out
}

// sortStrings sorts a slice of strings in ascending order. Wrapped
// here so the import of "sort" is local to this function; the
// allocation cost of the standard sort is irrelevant given the tiny
// slice size (single-digit enum names).
func sortStrings(s []string) {
	// In-place insertion sort: O(n^2) but n<=10. Avoids the import
	// churn of pulling in "sort" for one call.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// stringPtrOrNil returns &s when s is non-empty, or nil otherwise.
// Used wherever the contract distinguishes "value null" (nil) from a
// real string.
func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nilSliceIfEmpty returns nil for an empty slice and the original
// slice otherwise. Combined with omitempty struct tags, this hides
// trivially-empty OPTIONAL arrays from the JSON. It is used only for
// fields the SPEC marks as "absent when not applicable" (the flag-level
// mutually_exclusive_with field), NOT for the always-present arrays.
func nilSliceIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}

// emptySliceIfNil returns a non-nil empty slice for an empty/nil input
// and the original slice otherwise. It is the inverse intent of
// nilSliceIfEmpty: it guarantees a JSON `[]` (never `null`) for the
// array fields the SPEC mandates as always-present — aliases,
// prerequisites, subcommands, positional_arguments, examples
// (SPEC/DATA_FORMATS.md § Empty-array serialization).
func emptySliceIfNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// emptyGroupsIfNil is the [][]string flavour of emptySliceIfNil for the
// mutual_exclusion_groups field, which the SPEC also mandates as an
// always-present array.
func emptyGroupsIfNil(g [][]string) [][]string {
	return emptySliceIfNil(g)
}

// boolPtr returns a pointer to the given bool value. Used to populate
// optional pointer fields (StdinFallback, ReadsStdin) that follow the
// omitempty/null convention: nil means absent from JSON, non-nil means
// the value is present.
func boolPtr(b bool) *bool { return &b }
