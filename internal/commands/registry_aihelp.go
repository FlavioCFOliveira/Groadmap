// Package commands — ai-help command registry entry.
//
// `ai-help` is a top-level leaf command whose sole responsibility is
// to emit the AI Agent Contract — the machine-readable JSON document
// described by SPEC/COMMANDS.md § AI Help and SPEC/DATA_FORMATS.md §
// AI Agent Contract. It is functionally equivalent to the global
// `--ai-help` flag: both forms emit the whole-CLI contract.
//
// Why is the runtime handler absent from this declaration?
//
// The contract-emission wiring lives in cmd/rmp/main.go, where it
// runs as an EARLY-PASS scan over os.Args — before any registry
// dispatch happens. The scan must catch every form (`rmp --ai-help`,
// `rmp ai-help`, `rmp <cmd> --ai-help`, `rmp <cmd> <sub> --ai-help`)
// uniformly and must take precedence over `--help`, `-r`, and every
// action flag. Routing `ai-help` through the normal DispatchFamily
// path would mean the JSON contract path lived in two places: the
// early scan (for the flag forms) and a Handler here (for the command
// form). To keep one path of truth, the handler stored here is a
// programmer-error sentinel; in production the early scan in
// cmd/rmp/main.go intercepts every `ai-help` token before the
// dispatch ever reaches this entry.
//
// The registry entry is kept because:
//   - SPEC/COMMANDS.md § Command Aliases Reference lists `ai-help` as
//     a top-level command; absent here the AI contract would describe
//     itself with a missing command.
//   - The Help system can discover the command's existence via the
//     registry walk used by `rmp --help`.
//   - The AI Agent Contract is self-describing: when callers fetch
//     `rmp --ai-help`, the returned `commands` array includes a
//     `ai-help` entry that documents how to fetch the contract again
//     in scope-narrowed forms.

package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// buildAIHelpCommand assembles the leaf-command entry for `ai-help`.
func buildAIHelpCommand() Command {
	return Command{
		Name:          "ai-help",
		Summary:       "Emit the AI Agent Contract (machine-readable JSON describing the CLI).",
		Description:   "Equivalent to the global --ai-help flag. Emits a pretty-printed JSON document that fully describes every command, subcommand, flag, exit code, enum, and example exposed by the binary. Intended for AI agents and automated callers; the contract is the canonical machine-readable surface and is the only documentation an agent needs.",
		HasSubcommand: false,
		HelpPrinter:   printAIHelpHelp,
		Subcommands: []Subcommand{
			{
				Name:        "",
				Summary:     "Emit the whole-CLI AI Agent Contract.",
				Description: "Walks the internal command registry and emits the full contract as pretty-printed JSON on stdout with a trailing newline. Identical payload to `rmp --ai-help`.",
				Usage:       "rmp ai-help",
				HelpPrinter: printAIHelpHelp,
				// Handler is a programmer-error sentinel: the early-pass
				// scan in cmd/rmp/main.go intercepts every `ai-help`
				// token before the dispatcher reaches this entry. If
				// this Handler is ever called it means the wiring was
				// removed or the scan misclassified an argv shape.
				Handler:     aiHelpHandlerUnreachable,
				Flags:       []Flag{helpFlag()},
				Output:      SuccessOutput{Kind: "object", Schema: "AI Agent Contract — see DATA_FORMATS.md § AI Agent Contract."},
				SideEffects: SideEffects{Database: "Read-only.", Filesystem: "None.", Network: "None."},
				Idempotent:  true,
				ExitCodes:   []int{0, 2},
				Examples: []Example{
					{Title: "Emit the whole-CLI contract", Cmd: "rmp ai-help", Exit: 0},
					{Title: "Same payload via the global flag", Cmd: "rmp --ai-help", Exit: 0},
					{Title: "Unexpected positional argument", Cmd: "rmp ai-help foo", Stderr: "Error: ai-help accepts no positional arguments or flags other than --help", Exit: 2},
				},
			},
		},
	}
}

// aiHelpHandlerUnreachable is the programmer-error sentinel installed
// as the runtime Handler for the `ai-help` registry entry. Reaching
// this function means the early-pass scan in cmd/rmp/main.go that is
// the SOLE production path for contract emission has stopped
// intercepting the `ai-help` token. This is a wiring regression, not
// a user error.
//
// Returning ErrInvalidInput here ensures the binary exits 2 with a
// diagnostic instead of panicking, and the error message points the
// reader at the actual fix site.
func aiHelpHandlerUnreachable(_ []string) error {
	return fmt.Errorf("%w: ai-help dispatch reached commands.aiHelpHandlerUnreachable — the early-pass scan in cmd/rmp/main.go should have intercepted this token; fix the wiring there", utils.ErrInvalidInput)
}

// printAIHelpHelp writes the human-readable help for the `ai-help`
// command. Kept minimal: the command itself is the documentation
// surface for AI consumers; the human-help text only needs to tell a
// person how to invoke it and where the JSON shape is defined.
func printAIHelpHelp() {
	fmt.Println(`rmp ai-help - Emit the AI Agent Contract (JSON)

Usage:
  rmp ai-help
  rmp --ai-help

Description:
  Emits a pretty-printed JSON document on stdout that fully describes
  every command, subcommand, flag, exit code, enum, and example
  exposed by the binary. Intended for AI agents and automated callers.

  The two forms above are equivalent and produce byte-identical output.
  Scope-narrowed forms are also available:

    rmp <command> --ai-help              # one command and its subcommands
    rmp <command> <subcommand> --ai-help # one subcommand only

Options:
  -h, --help    Show this help message

Exit Codes:
  0   Contract emitted successfully
  2   Unexpected positional argument or unknown command/subcommand scope

See Also:
  SPEC/COMMANDS.md       § AI Help
  SPEC/DATA_FORMATS.md   § AI Agent Contract`)
}
