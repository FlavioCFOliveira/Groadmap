// Package commands — the dispatch-failure error.
//
// A dispatch failure is the case in which rmp cannot resolve a name it
// was given to a command or to a subcommand. SPEC/HELP.md
// § Recovery help after a dispatch failure defines exactly two classes:
//
//	Unresolved command      rmp nadadisto        Error: unknown command: nadadisto
//	Unresolved subcommand   rmp task nadadisto   Error: unknown task subcommand: nadadisto
//
// Both exit 127, both write the help for the level at which the name
// could not be resolved to stderr after the error line, and both write
// nothing to stdout.
//
// Why a dedicated type instead of fmt.Errorf with a sentinel:
//
//   - The message must NOT carry a sentinel prefix. fmt.Errorf("%w: …",
//     ErrUnknownCommand, …) would render "unknown command: unknown task
//     subcommand: nadadisto". Error() here returns the message alone,
//     the same contract utils.MessageError follows.
//
//   - The error has to carry the family whose subcommand failed to
//     resolve, so the error path can render that family's help without
//     re-deriving it from the message text.
package commands

import (
	"io"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// DispatchError reports a name that could not be resolved to a command
// or to a subcommand of a command. It unwraps to utils.ErrUnknownCommand,
// which the exit-code mapping in cmd/rmp turns into 127.
type DispatchError struct {
	// Family is the command whose subcommand could not be resolved. It
	// is nil when the unresolved name is a top-level command, because in
	// that case there is no family to render: the recovery help is the
	// global help body, which lives in the binary's main package.
	Family *Command
	// Name is the token that could not be resolved, echoed verbatim in
	// the error line.
	Name string
	// msg is the rendered error line body, built once at construction.
	msg string
}

// Error returns the SPEC-mandated message with no sentinel prefix.
func (e *DispatchError) Error() string { return e.msg }

// Unwrap exposes the sentinel so errors.Is(err, utils.ErrUnknownCommand)
// holds and the exit-code mapping finds it.
func (e *DispatchError) Unwrap() error { return utils.ErrUnknownCommand }

// RecoveryHelp writes the family help body to w, omitting the AI-agent
// banner, and reports whether it wrote anything.
//
// It returns false for an unresolved top-level command: the recovery
// help for that class is the global help body, which this package does
// not own. The caller writes it instead.
func (e *DispatchError) RecoveryHelp(w io.Writer) bool {
	if e.Family == nil || e.Family.HelpPrinter == nil {
		return false
	}
	e.Family.WriteHelpBody(w)
	return true
}

// NewUnknownCommandError builds the dispatch failure for a first
// non-flag token that names no command and no command alias.
//
// It is exported because the top-level lookup runs in cmd/rmp, against
// the registry this package exposes.
func NewUnknownCommandError(name string) *DispatchError {
	return &DispatchError{Name: name, msg: "unknown command: " + name}
}

// newUnknownSubcommandError builds the dispatch failure for a token that
// names no subcommand and no subcommand alias of the family that did
// resolve.
func newUnknownSubcommandError(family *Command, name string) *DispatchError {
	return &DispatchError{
		Family: family,
		Name:   name,
		msg:    "unknown " + family.Name + " subcommand: " + name,
	}
}
