// Package commands — the single shared enforcement point for positional
// argument arity.
//
// # The rule this file implements
//
// SPEC/COMMANDS.md § Positional Arguments states that every command
// declares the maximum number of positional arguments it accepts, that the
// declaration lives with that command's flag definitions, and that ONE
// shared enforcement point compares what an invocation supplied against
// that declaration. An invocation that supplies more is refused with exit
// code 2 and the line
//
//	Error: invalid input: unexpected argument "X"
//
// where X is the FIRST offending token, echoed as the user supplied it.
//
// # Why the check lives here and not in each command
//
// The declaration is Subcommand.Positional in the registry
// (internal/commands/registry_*.go), alongside Subcommand.Flags: one place
// describes a command's whole argument surface, and every consumer — the
// dispatcher below and the machine-readable contract `rmp --ai-help`
// publishes — reads the arity from that same declaration.
//
// Enforcement is reached by construction, from the declaration alone:
// Command.DispatchFamily is the ONLY place in the binary that invokes a
// subcommand handler, so calling checkPositionalArity there covers every
// command that exists and every command that will ever be added. The
// alternative — a helper each handler remembers to call — was rejected
// deliberately: a check every call site must remember to perform is a check
// some call site will not perform, which is exactly how the eleven commands
// this rule closes came to accept and silently discard a stray token.
//
// # How a positional argument is told apart from a flag's value
//
// To count positional arguments, the tokens that are flags and the tokens
// that are the VALUE of a flag must first be set aside. The rule follows the
// flag parser's own, so the count is over the tokens the parser would leave
// in ParseResult.Args:
//
//   - A token that begins with "-" is a flag, never a positional argument
//     (SPEC/COMMANDS.md § Positional Arguments, rule 5). An unrecognised one
//     is the handler's to reject, with its own message and the same exit
//     code 2.
//   - A "--flag=value" carries its own value; a boolean flag takes none;
//     every other flag takes the token after it, unless that token is itself
//     "-"-prefixed and is not a negative integer — which is exactly when the
//     parser refuses to read it as a value.
//
// The flag table is the subcommand's own Flags declaration, so the whole
// classification comes from the same registry entry the arity does. A flag
// missing from that declaration is answered by the general rule above rather
// than by a guess, which is what keeps an unknown flag's operand from being
// counted as an excess positional argument and reported as one.
package commands

import (
	"fmt"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// roadmapFlagLong and roadmapFlagShort are the roadmap selector. Every
// parser in the package consumes it and its value ahead of the
// subcommand's own flags, and it is declared on most — but not all —
// registry entries, so the classification below recognises it
// unconditionally rather than relying on the per-subcommand table.
const (
	roadmapFlagLong  = "--roadmap"
	roadmapFlagShort = "-r"
)

// checkPositionalArity refuses an invocation that supplies more positional
// arguments than sub declares. args are the tokens the handler will
// receive, i.e. everything after the command and subcommand names.
//
// It returns nil when the invocation is within its declared arity, and a
// utils.ErrInvalidInput error naming the first offending token otherwise.
// The refusal happens before the handler runs, so before the roadmap
// database or the graph store is opened and before standard input is read:
// a refused invocation creates nothing, changes nothing, and deletes
// nothing (SPEC/COMMANDS.md § Positional Arguments, rule 6).
//
// Two classes of invocation are passed through untouched:
//
//   - A help request. A help token anywhere in args means the reader asked
//     for the help body, which every level already serves with exit 0; the
//     arity rule governs work, not documentation.
//   - A subcommand that publishes its own refusal wording
//     (Subcommand.PublishesOwnArityRefusal). Deferring keeps the lines
//     SPEC/COMMANDS.md publishes for `graph`, `web`, and `ai-help` exactly
//     as they are instead of overriding them from here.
func checkPositionalArity(sub *Subcommand, args []string) error {
	if sub == nil || sub.PublishesOwnArityRefusal || hasHelpFlag(args) {
		return nil
	}

	maxPositional := len(sub.Positional)
	seen := 0

	for i := 0; i < len(args); i++ {
		tok := args[i]

		if !strings.HasPrefix(tok, "-") {
			seen++
			if seen > maxPositional {
				return fmt.Errorf("%w: unexpected argument %q", utils.ErrInvalidInput, tok)
			}
			continue
		}

		next, hasNext := "", i+1 < len(args)
		if hasNext {
			next = args[i+1]
		}
		if consumesFollowingValue(sub, tok, next, hasNext) {
			// Skip the operand so a flag value is never counted as a
			// positional argument. A missing operand is the handler's
			// error to report, not this one's.
			i++
		}
	}

	return nil
}

// consumesFollowingValue reports whether tok, a "-"-prefixed token, takes
// next — the token written after it — as its value. hasNext distinguishes
// "no token follows" from "the token that follows is the empty string",
// which is a value a user can legitimately supply.
//
// A GNU-style "--flag=value" carries its own value and consumes nothing, and
// a flag written last has nothing to consume. The roadmap selector always
// takes a value, and a flag the subcommand declares boolean never takes one.
//
// Every other flag — declared value-taking or not declared at all — takes
// the following token only when that token is a value, which is the flag
// parser's own test: a "-"-prefixed token is a flag and is never read as the
// preceding flag's operand, the sole exception being a negative integer.
// Consuming a following flag unconditionally would swallow it and push its
// operand into the positional count, turning a missing-value or unknown-flag
// report into a spurious unexpected-argument one.
func consumesFollowingValue(sub *Subcommand, tok, next string, hasNext bool) bool {
	if strings.Contains(tok, "=") || !hasNext {
		return false
	}
	if tok == roadmapFlagShort || tok == roadmapFlagLong {
		// The roadmap selector takes whatever follows it, without looking:
		// that is what every parser in the package does with it.
		return true
	}
	for i := range sub.Flags {
		f := &sub.Flags[i]
		if tok == f.Long || (f.Short != "" && tok == f.Short) {
			if f.Type == "boolean" {
				return false
			}
			break
		}
	}
	// A declared value-taking flag and an undeclared one are answered the
	// same way; see the doc comment for why.
	return !strings.HasPrefix(next, "-") || isNegativeInteger(next)
}
