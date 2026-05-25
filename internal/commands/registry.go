// Package commands — declarative command registry.
//
// This file defines the data shape that lets a single Go-level value
// describe every command, every subcommand, every flag, every exit
// code, every example, and every side effect of the rmp CLI surface.
//
// Two consumers read from the registry:
//
//  1. The runtime dispatch (Find / Dispatch in this file, called from
//     cmd/rmp/main.go) routes a parsed argv to the handler stored in
//     the matching Subcommand.Handler. Help requests at any level are
//     served by the registered HelpPrinter.
//
//  2. The future AI-contract emitter (task 2 in the --ai-help sprint
//     sequence; see SPEC/ARCHITECTURE.md § AI Agent Contract Generation
//     and SPEC/DATA_FORMATS.md § AI Agent Contract) walks the registry
//     and serialises it to JSON without re-querying the dispatch code.
//
// The non-duplication invariant required by SPEC/ARCHITECTURE.md is
// preserved by routing every name lookup (command, alias, subcommand,
// subcommand alias) through Registry.Find / Command.findSubcommand.
// Names and aliases live exclusively here; no parallel switch statement
// hard-codes them.
//
// Why the verbatim HelpPrinter is kept as a function pointer instead of
// being rendered from the structured fields: this refactor's acceptance
// criterion is byte-identical --help output. Rendering 25+ help texts
// from a template carries a high risk of cosmetic drift (tabs vs
// spaces, line wrapping). Holding the printer as a function pointer
// keeps the registry the single resolution / metadata source while
// preserving every byte of the human help body. A later task in the
// AI-help sprint may collapse the printers into a renderer once the
// structured metadata has demonstrably matched the existing text.
package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// Argument describes a positional argument of a subcommand.
type Argument struct {
	// Name is the placeholder shown in the usage line, without angle
	// brackets (e.g. "task-ids", "sprint-id").
	Name string
	// Type is one of "string", "integer", "csv:integer", "enum".
	Type string
	// Enum, when non-empty, names the enum the value must belong to
	// (key into the contract's top-level enums map). Only meaningful
	// when Type is "enum".
	Enum string
	// Description is a one-sentence description for help and contract.
	Description string
	// Required reports whether the argument must be supplied.
	Required bool
}

// Flag describes a single CLI flag.
//
// Field names and types mirror the AI-contract flag entry defined in
// SPEC/DATA_FORMATS.md § AI Agent Contract so the future contract
// emitter is a straight serialisation rather than a translation.
type Flag struct {
	// Long is the long flag including the "--" prefix.
	Long string
	// Short is the short flag including the "-" prefix, or "" when no
	// short form exists.
	Short string
	// Type is one of "string", "integer", "boolean", "enum", "date",
	// "list:string", "list:integer".
	Type string
	// Default is the textual default value rendered as-is into help;
	// "" means no default.
	Default string
	// Enum, when non-empty, names the enum the value must belong to
	// (key into the contract's top-level enums map). Only meaningful
	// when Type is "enum".
	Enum string
	// Description is a one-sentence description.
	Description string
	// MutuallyExclusiveWith lists long flag names that cannot be
	// combined with this one. Placed last among pointer-bearing
	// fields to minimise the GC pointer-scan prefix.
	MutuallyExclusiveWith []string
	// RangeMin / RangeMax bound numeric values. Use HasRange to tell
	// "no range" apart from "0..0".
	RangeMin int
	RangeMax int
	// MinLength / MaxLength bound string lengths. Zero means unbounded.
	MinLength int
	MaxLength int
	// HasRange marks RangeMin/RangeMax as meaningful.
	HasRange bool
	// Required reports whether the flag must be supplied.
	Required bool
}

// Example is one invocation example for help and the AI contract.
type Example struct {
	// Title is a short identifier of the scenario.
	Title string
	// Cmd is the exact rmp invocation, including placeholders.
	Cmd string
	// Stdout is the literal stdout produced.
	Stdout string
	// Stderr is the literal stderr produced.
	Stderr string
	// Exit is the exit code observed.
	Exit int
}

// SideEffects describes the side effects of a subcommand.
type SideEffects struct {
	// Database describes DB writes in plain language. "Read-only." when
	// none.
	Database string
	// Filesystem describes filesystem writes. "None." when none.
	Filesystem string
	// Network describes network access. Always "None." for Groadmap.
	Network string
}

// SuccessOutput documents what the subcommand prints to stdout on
// success.
type SuccessOutput struct {
	// Kind is "object", "array", or "empty". "empty" means mutating
	// commands that return no body.
	Kind string
	// Schema is a free-form description of the success payload shape
	// (e.g. "task object", "array of audit-entry objects").
	Schema string
	// Example is a one-line example payload; "" for Kind=="empty".
	Example string
}

// Subcommand is one subcommand under a top-level Command family.
type Subcommand struct {
	// Handler is the runtime entry point; it receives the args after
	// the subcommand token (i.e. for `rmp task list -r foo`, Handler
	// receives `[-r foo]`).
	Handler func(args []string) error
	// HelpPrinter prints the verbatim help text for this subcommand.
	// Captured as a function pointer to guarantee byte-identical help
	// output across the refactor (see the package doc).
	HelpPrinter func()

	// Name is the canonical subcommand name (e.g. "list").
	Name string
	// Aliases are alternate names that route to the same handler.
	Aliases []string
	// Summary is a one-line description for the family-help command
	// listing.
	Summary string
	// Description is a longer free-form description.
	Description string
	// Usage is the one-line usage signature.
	Usage string
	// Positional describes positional arguments in order.
	Positional []Argument
	// Flags lists every flag accepted by this subcommand (excluding
	// the shared global -r / --roadmap and -h / --help which live on
	// the Command or Registry levels).
	Flags []Flag
	// MutexGroups lists groups of mutually-exclusive flags. Each inner
	// slice contains long flag names of which at most one may be
	// supplied.
	MutexGroups [][]string
	// Output documents stdout-on-success.
	Output SuccessOutput
	// SideEffects documents DB / FS / network side effects.
	SideEffects SideEffects
	// ExitCodes lists every exit code this subcommand can emit, in
	// ascending order. Always includes 0.
	ExitCodes []int
	// Prerequisites lists agent-visible preconditions.
	Prerequisites []string
	// Examples lists worked examples; the AI contract requires at
	// least one success and one failure example per subcommand.
	Examples []Example
	// Idempotent reports whether repeated invocations with the same
	// arguments leave the system in the same state.
	Idempotent bool
}

// Command is a top-level command family (roadmap, task, sprint, ...).
type Command struct {
	// HelpPrinter prints the family-level verbatim help text.
	HelpPrinter func()

	// Name is the canonical command name (e.g. "task").
	Name string
	// Aliases are alternate names that route to the same family.
	Aliases []string
	// Summary is a one-line description shown in the global help.
	Summary string
	// Description is a longer free-form description.
	Description string
	// Prerequisites lists agent-visible preconditions applying to the
	// whole family (e.g. "An existing roadmap selected via -r").
	Prerequisites []string
	// Subcommands lists every subcommand under this family.
	Subcommands []Subcommand
	// HasSubcommand is false for leaf commands like `stats` that take
	// no subcommand token. Such commands have exactly one entry in
	// Subcommands (with Name == "") whose Handler is the family
	// handler.
	HasSubcommand bool
}

// Registry is the single source of truth for the CLI surface.
type Registry struct {
	Commands []Command
	// Globals are flags recognised at the top level (--help, --version,
	// later --ai-help). Family-level and subcommand-level handlers
	// route their own --help / -h independently.
	Globals []Flag
}

// matchName reports whether name matches the canonical name or any of
// the aliases.
func matchName(canonical string, aliases []string, name string) bool {
	if canonical == name {
		return true
	}
	for _, a := range aliases {
		if a == name {
			return true
		}
	}
	return false
}

// FindCommand returns the Command whose name or alias matches the
// given token, or nil if no command matches.
func (r *Registry) FindCommand(name string) *Command {
	for i := range r.Commands {
		c := &r.Commands[i]
		if matchName(c.Name, c.Aliases, name) {
			return c
		}
	}
	return nil
}

// FindSubcommand returns the Subcommand whose name or alias matches
// the given token, or nil if no subcommand matches.
func (c *Command) FindSubcommand(name string) *Subcommand {
	for i := range c.Subcommands {
		s := &c.Subcommands[i]
		if matchName(s.Name, s.Aliases, name) {
			return s
		}
	}
	return nil
}

// DispatchFamily routes args to the matching subcommand handler. It is
// the registry-driven replacement for the per-family Handle* switches.
// When args is empty, the family help is printed. When the first arg
// is a help token, the family help is printed. When the first arg is
// an unknown subcommand, an ErrInvalidInput error is returned so the
// top-level error handler can render it.
//
// The caller is the leaf command's Handle* function; the registry is
// queried via the embedding *Command. This indirection means
// HandleTask, HandleSprint, etc. share one implementation.
func (c *Command) DispatchFamily(args []string) error {
	if !c.HasSubcommand {
		// Leaf command (e.g. `stats`). The single entry in Subcommands
		// holds the handler; --help is recognised by that handler
		// itself, so we pass the full args through unchanged.
		if len(c.Subcommands) != 1 {
			return fmt.Errorf("%w: command %q has no subcommand dispatcher", utils.ErrInvalidInput, c.Name)
		}
		return c.Subcommands[0].Handler(args)
	}

	if len(args) == 0 {
		// Family help triggered by bare `rmp <family>`. Route through
		// invokeHelpPrinter so the SPEC-mandated AI-agent banner is
		// prepended uniformly (see SPEC/HELP.md § AI agent banner and
		// internal/commands/banner.go).
		invokeHelpPrinter(c.HelpPrinter)
		return nil
	}

	subToken := args[0]
	if isHelpToken(subToken) {
		// Explicit family-help token: `rmp <family> --help` (or `-h` /
		// `help`). Same banner-prepending dispatch as above.
		invokeHelpPrinter(c.HelpPrinter)
		return nil
	}

	sub := c.FindSubcommand(subToken)
	if sub == nil {
		return fmt.Errorf("%w: unknown %s subcommand: %s", utils.ErrInvalidInput, c.Name, subToken)
	}

	// Subcommand-level help: `rmp <family> <sub> --help` (or `help` /
	// `-h` anywhere among the remaining args). Banner is prepended via
	// invokeHelpPrinter, keeping the SPEC banner rule applied at the
	// single dispatch point rather than duplicated across 40+ printers.
	if hasHelpFlag(args[1:]) {
		if sub.HelpPrinter != nil {
			invokeHelpPrinter(sub.HelpPrinter)
			return nil
		}
	}

	return sub.Handler(args[1:])
}

// isHelpToken reports whether arg is one of the recognised help
// tokens.
func isHelpToken(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}

// dispatchFamily resolves a family by canonical name in the singleton
// AppRegistry and routes args through it. It is the shared
// implementation behind every Handle*(args) function exposed by this
// package; centralising it ensures the registry is the only place
// where command-name and alias lookup happen.
func dispatchFamily(canonicalName string, args []string) error {
	cmd := AppRegistry().FindCommand(canonicalName)
	if cmd == nil {
		// This is a programmer error: dispatchFamily is only called by
		// in-package Handle* functions whose canonical names are
		// hard-coded against the registry. A miss here means the
		// registry data lost an entry.
		return fmt.Errorf("%w: family %q is not registered", utils.ErrInvalidInput, canonicalName)
	}
	return cmd.DispatchFamily(args)
}
