// Groadmap — arity of the global switch forms.
//
// This file exists because the six forms it governs never reach the command
// registry. `rmp help`, `rmp --help`, `rmp -h`, `rmp version`,
// `rmp --version`, and `rmp -v` are resolved by the switch in main() BEFORE
// any command lookup happens, so the shared enforcement point that covers
// every registered command (internal/commands/positional_arity.go, called
// from Command.DispatchFamily) cannot see them and does not cover them.
// They are deliberately enforced here instead, and nowhere else.
//
// SPEC/COMMANDS.md § Positional Arity by Command declares a maximum of zero
// positional arguments for all six. The refusal is the canonical one the
// shared point produces — exit code 2 and
//
//	Error: invalid input: unexpected argument "X"
//
// — so a reader sees one rule with two enforcement sites, not two rules.
package main

import (
	"fmt"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// refuseGlobalPositional enforces the arity-0 declaration of the global
// switch forms. rest is everything on the command line after the global
// form itself (os.Args[2:]).
//
// It returns a utils.ErrInvalidInput error naming the first positional
// argument found, or nil when there is none. A "-"-prefixed token is a
// flag and not a positional argument (SPEC/COMMANDS.md § Positional
// Arguments, rule 5), so it is not this function's to refuse.
//
// The call sites are inside the switch in main(), before the help body or
// the version line is written and before any filesystem work, so a refused
// invocation writes zero bytes to stdout and touches nothing.
func refuseGlobalPositional(rest []string) error {
	for _, tok := range rest {
		if strings.HasPrefix(tok, "-") {
			continue
		}
		return fmt.Errorf("%w: unexpected argument %q", utils.ErrInvalidInput, tok)
	}
	return nil
}
