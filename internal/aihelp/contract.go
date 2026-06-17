// Package aihelp builds the AI Agent Contract, the machine-readable
// JSON description of the rmp CLI surface. The contract is the response
// payload of `rmp --ai-help` (and equivalent forms) and is consumed by
// AI agents to drive the CLI without recourse to any other document.
//
// The package has two layers:
//
//  1. The contract type tree (this file). Each Go struct mirrors a
//     subtree of the JSON schema defined in SPEC/DATA_FORMATS.md § AI
//     Agent Contract; JSON struct tags drive marshalling.
//
//  2. The generator (generator.go). Generate(scope, info) walks the
//     declarative command registry exposed by internal/commands, the
//     enum catalogues exposed by internal/models, and the static
//     conventions/exit-codes blocks defined in this package, and
//     returns a `[]byte` of pretty-printed JSON ready for stdout.
//
// SchemaVersion ("1.0.0") is the version of the contract schema
// itself, not of the rmp binary. Bump it only when the *shape* of the
// JSON changes in a way that breaks existing AI-agent consumers
// (renamed field, removed key, changed type). The binary version
// flows through the contract via ContractInfo.BinaryVersion, injected
// by the CLI entry point so this package stays decoupled from
// cmd/rmp/main.go.
//
// Two top-level fields, `common_workflows` and `pitfalls`, carry the
// curated catalogues mandated by SPEC/DATA_FORMATS.md § AI Agent
// Contract — six workflows and twelve pitfalls. The canonical entries
// and their rationale live in workflows.go and pitfalls.go.
package aihelp

// SchemaVersion is the semantic version of the AI Agent Contract
// schema. Independent of the rmp binary version. Matches the value
// declared in SPEC/DATA_FORMATS.md § AI Agent Contract.
const SchemaVersion = "1.0.0"

// Contract is the top-level JSON document returned by `rmp --ai-help`.
// Field order in the struct is intentional: it matches the canonical
// presentation order from SPEC/DATA_FORMATS.md § Top-level shape so
// json.Marshal produces a payload that reads top-down in the same
// order the SPEC documents it.
type Contract struct {
	SchemaVersion string `json:"schema_version"`
	Tool          Tool   `json:"tool"`
	// Conventions and ExitCodes are stable across all scopes.
	Conventions Conventions `json:"conventions"`
	ExitCodes   []ExitCode  `json:"exit_codes"`
	// Enums is keyed by enum name (e.g. "TaskStatus"). The map is
	// rendered with deterministic key order by the generator via
	// json.Marshal's lexicographic sort.
	Enums map[string]EnumDefinition `json:"enums"`
	// GlobalFlags lists every top-level flag (--help, --version,
	// --ai-help). Per the SPEC, scope filtering does NOT trim this
	// slice.
	GlobalFlags []FlagEntry `json:"global_flags"`
	// Commands is the only scope-filtered field. Whole-CLI invocations
	// include every command family in declaration order; narrower
	// scopes contain exactly one command (and optionally one
	// subcommand under it).
	Commands []CommandEntry `json:"commands"`
	// CommonWorkflows is the curated workflow catalogue mandated by
	// SPEC/DATA_FORMATS.md § AI Agent Contract. Populated by the
	// generator from staticWorkflows() in workflows.go. Always non-nil
	// so JSON renders `[]` rather than `null` should the catalogue
	// ever be emptied.
	CommonWorkflows []Workflow `json:"common_workflows"`
	// Pitfalls is the curated mistakes catalogue mandated by the same
	// SPEC section. Populated from staticPitfalls() in pitfalls.go.
	Pitfalls []Pitfall `json:"pitfalls"`
}

// Tool identifies the binary that produced the contract.
type Tool struct {
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	BinaryVersion string `json:"binary_version"`
	Description   string `json:"description"`
}

// Conventions documents cross-cutting invariants the agent must honour.
// The values are static for this binary and live as package constants
// in conventions.go; this struct only declares the JSON projection.
type Conventions struct {
	// String fields first (16 bytes each = ptr + len).
	StdoutOnSuccess string `json:"stdout_on_success"`
	StderrOnError   string `json:"stderr_on_error"`
	Charset         string `json:"charset"`
	Locale          string `json:"locale"`
	DatetimeFormat  string `json:"datetime_format"`
	DatetimeExample string `json:"datetime_example"`
	ListSeparator   string `json:"list_separator"`
	// Composite struct fields next.
	RoadmapFlag   RoadmapFlag   `json:"roadmap_flag"`
	AIAgentEnvVar AIAgentEnvVar `json:"ai_agent_env_var"`
	// Scalar last to minimise pointer-scan prefix.
	JSONIndent int `json:"json_indent"`
}

// RoadmapFlag documents the shared -r / --roadmap flag once at the
// contract root, in addition to its per-subcommand inclusion under
// commands[].subcommands[].flags[]. Per-subcommand inclusion makes the
// flag self-evident to flag walkers; this root entry exists so the
// agent learns that the flag is *globally required except for a small
// allow-list*, a rule that does not appear on any single flag entry.
type RoadmapFlag struct {
	Short       string `json:"short"`
	Long        string `json:"long"`
	RequiredFor string `json:"required_for"`
}

// AIAgentEnvVar documents the AI_AGENT environment variable contract
// defined in SPEC/COMMANDS.md § AI Help (Discoverability rule 3).
type AIAgentEnvVar struct {
	Name        string `json:"name"`
	EnableValue string `json:"enable_value"`
	Effect      string `json:"effect"`
}

// ExitCode mirrors one entry in the Exit Code Standards table from
// SPEC/ARCHITECTURE.md § Exit Codes. The Sentinel field is omitted for
// codes (0, 130) that are not produced by wrapping a sentinel error.
type ExitCode struct {
	Name     string `json:"name"`
	Meaning  string `json:"meaning"`
	Sentinel string `json:"sentinel,omitempty"`
	Code     int    `json:"code"`
}

// EnumDefinition is the value-list for one enum referenced by any flag
// or positional argument in the registry. StateMachineReference is
// omitted (rendered absent) for enums that have no associated state
// machine.
type EnumDefinition struct {
	StateMachineReference string      `json:"state_machine_reference,omitempty"`
	Values                []EnumValue `json:"values"`
}

// EnumValue is one member of an enum. Description is best-effort: it
// is populated from SPEC/MODELS.md § Enums where the SPEC supplies a
// description, and empty otherwise. The field is always present in the
// JSON (never omitted) so consumers can rely on its key.
type EnumValue struct {
	Value       string `json:"value"`
	Description string `json:"description"`
}

// FlagEntry is the JSON projection of internal/commands.Flag. The
// SPEC distinguishes "null" (key present, value `null`) from "absent"
// (key omitted) for several fields; we use pointer types and
// omitempty to express this distinction:
//
//   - Short, Default, Enum: rendered as null when empty (pointer-to-string)
//   - Range, MinLength, MaxLength, MutuallyExclusiveWith: rendered as
//     absent when not applicable (omitempty + pointer or zero-test)
type FlagEntry struct {
	// Pointer fields first (8 bytes each, GC-scanned).
	Short         *string `json:"short"`
	Default       *string `json:"default"`
	Enum          *string `json:"enum"`
	Range         *Range  `json:"range,omitempty"`
	MinLength     *int    `json:"min_length,omitempty"`
	MaxLength     *int    `json:"max_length,omitempty"`
	StdinFallback *bool   `json:"stdin_fallback,omitempty"`
	// Strings and slices next (header-typed: 16/24 bytes).
	Long                  string   `json:"long"`
	Type                  string   `json:"type"`
	Description           string   `json:"description"`
	MutuallyExclusiveWith []string `json:"mutually_exclusive_with,omitempty"`
	// Scalar (1 byte) last to keep pointer prefix tight.
	Required bool `json:"required"`
}

// Range is the bounded-integer constraint used by flag entries. Max is
// a pointer so an unbounded-above integer (RangeMin set, no RangeMax)
// omits the `max` key entirely rather than serialising the misleading
// `"max": 0`. See generator.buildFlag for the omission rule.
// Range field order is intentional: JSON emits `min` before `max` to match
// SPEC/DATA_FORMATS.md examples (Implementation Notes: maintain field order
// as defined in examples). The fieldalignment win (8 bytes) is irrelevant —
// Range is emitted at most once per flag per process.
//
//nolint:govet // fieldalignment: SPEC-mandated JSON field order wins.
type Range struct {
	Min int  `json:"min"`
	Max *int `json:"max,omitempty"`
}

// PositionalArgument projects internal/commands.Argument. Enum is null
// for non-enum arguments.
type PositionalArgument struct {
	Enum        *string `json:"enum"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Required    bool    `json:"required"`
}

// SuccessOutput projects internal/commands.SuccessOutput. Schema and
// Example are nullable because the SPEC permits absence for
// kind=="empty". Schema is rendered as a free-form string; Example is
// embedded as a raw JSON-compatible string (typically a short
// snippet) to match the SPEC's example payloads.
type SuccessOutput struct {
	Schema  *string `json:"schema"`
	Example *string `json:"example"`
	Kind    string  `json:"kind"`
}

// SideEffects projects internal/commands.SideEffects. All three fields
// are always populated; "None." and "Read-only." are the canonical
// no-effect values.
type SideEffects struct {
	Database   string `json:"database"`
	Filesystem string `json:"filesystem"`
	Network    string `json:"network"`
}

// ExampleEntry projects internal/commands.Example. Empty Stdout and
// Stderr are rendered as empty strings (not omitted) so consumers
// always see the four-key shape {title, cmd, stdout, stderr, exit}.
type ExampleEntry struct {
	Title  string `json:"title"`
	Cmd    string `json:"cmd"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Exit   int    `json:"exit"`
}

// SubcommandEntry projects internal/commands.Subcommand. For leaf
// families (e.g. `stats`) the registry stores a single Subcommand with
// Name == ""; the generator surfaces such families as a top-level
// CommandEntry without a Subcommands array, so SubcommandEntry only
// appears under genuine sub-tokens.
type SubcommandEntry struct {
	ReadsStdin            *bool                `json:"reads_stdin,omitempty"`
	SideEffects           SideEffects          `json:"side_effects"`
	StdoutOnSuccess       SuccessOutput        `json:"stdout_on_success"`
	Usage                 string               `json:"usage"`
	Description           string               `json:"description"`
	Summary               string               `json:"summary"`
	Name                  string               `json:"name"`
	Flags                 []FlagEntry          `json:"flags"`
	Prerequisites         []string             `json:"prerequisites"`
	ExitCodes             []int                `json:"exit_codes"`
	MutualExclusionGroups [][]string           `json:"mutual_exclusion_groups"`
	PositionalArguments   []PositionalArgument `json:"positional_arguments"`
	Aliases               []string             `json:"aliases"`
	Examples              []ExampleEntry       `json:"examples"`
	Idempotent            bool                 `json:"idempotent"`
}

// CommandEntry projects internal/commands.Command. Every command —
// branching family or single-action leaf — carries its actions in the
// Subcommands array, so an agent can traverse the whole CLI uniformly
// through commands[].subcommands[] without special-casing leaf
// commands (SPEC/DATA_FORMATS.md § Single-action commands). For a
// single-action command (`ai-help`, `stats`, `web`) the array holds
// exactly one element whose name repeats the command's own name.
//
// Aliases, Prerequisites, and Subcommands are always-present arrays:
// they serialise as `[]` (never `null`) when empty, per
// SPEC/DATA_FORMATS.md § Empty-array serialization.
type CommandEntry struct {
	Name          string            `json:"name"`
	Aliases       []string          `json:"aliases"`
	Summary       string            `json:"summary"`
	Description   string            `json:"description"`
	Prerequisites []string          `json:"prerequisites"`
	Subcommands   []SubcommandEntry `json:"subcommands"`
}

// Workflow is the JSON shape of a `common_workflows` entry. The
// curated list of six workflows mandated by SPEC/DATA_FORMATS.md §
// AI Agent Contract is supplied by staticWorkflows() in workflows.go.
type Workflow struct {
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	ExpectedOutcome string         `json:"expected_outcome"`
	Prerequisites   []string       `json:"prerequisites"`
	Steps           []WorkflowStep `json:"steps"`
}

// WorkflowStep is one step inside a Workflow.
type WorkflowStep struct {
	Command string `json:"command"`
	Purpose string `json:"purpose"`
}

// Pitfall is the JSON shape of a `pitfalls` entry. The curated list
// of twelve pitfalls mandated by SPEC/DATA_FORMATS.md § AI Agent
// Contract is supplied by staticPitfalls() in pitfalls.go.
type Pitfall struct {
	ID             string `json:"id"`
	Description    string `json:"description"`
	WrongExample   string `json:"wrong_example"`
	CorrectExample string `json:"correct_example"`
	Reference      string `json:"reference"`
}
