// Package commands — destination control for plain-text help bodies.
//
// Every `print*Help` function in this package writes its body through
// helpDst() rather than straight to stdout. The indirection exists for
// exactly one reason: the recovery help that follows a dispatch failure
// must go to **stderr**, not stdout, because a failing invocation writes
// zero bytes to stdout (SPEC/HELP.md § Stdout silence on failure).
//
// The alternative designs were both rejected:
//
//   - Swapping the process-global os.Stdout around the printer call.
//     That mutates a shared, cross-package global from production code,
//     is not safe against any concurrent writer, and needs an os.Pipe
//     plus a draining goroutine to read the bytes back.
//
//   - Changing HelpPrinter to func(io.Writer). That rewrites all 68
//     registry entries and every call site for a capability only the
//     six family printers actually exercise.
//
// The writer indirection keeps HelpPrinter's signature intact, adds no
// syscalls on the ordinary `--help` path, and localises the whole
// mechanism in this file.
package commands

import (
	"io"
	"os"
)

// helpOut is the destination for the plain-text help bodies printed by
// the print*Help functions. A nil value means "os.Stdout", resolved at
// call time by helpDst.
//
// It is deliberately not initialised to os.Stdout at package load. The
// test suite redirects help output by assigning to os.Stdout (see
// captureStdout), which a value captured at init would not observe.
// Resolving lazily keeps both redirection mechanisms working.
//
// Mutation is confined to WriteHelpBodyTo, which saves and restores the
// previous value around a single synchronous printer call. The CLI
// dispatches one command per process, so no two help bodies are ever
// rendered concurrently.
var helpOut io.Writer

// helpDst returns the writer a help printer must write its body to.
func helpDst() io.Writer {
	if helpOut != nil {
		return helpOut
	}
	return os.Stdout
}

// WriteHelpBodyTo runs printer with its output redirected to w, and
// WITHOUT the AI-agent banner that invokeHelpPrinter prepends on the
// stdout path.
//
// Omitting the banner is a requirement, not an optimisation. The banner
// and the trailing AI-agent hint carry the same sentence, and every
// failing invocation already ends with the hint; emitting the banner
// here would put that sentence on stderr twice and reintroduce the
// duplication this mechanism exists to remove (SPEC/HELP.md § Recovery
// help after a dispatch failure).
//
// The previous destination is restored even if printer panics, so a
// failure inside one help body cannot silently redirect every later one.
func WriteHelpBodyTo(w io.Writer, printer func()) {
	if w == nil || printer == nil {
		return
	}
	prev := helpOut
	helpOut = w
	defer func() { helpOut = prev }()
	printer()
}

// WriteHelpBody writes this command's family help body to w, omitting
// the AI-agent banner. It is the recovery help for an unresolved
// subcommand of this family.
func (c *Command) WriteHelpBody(w io.Writer) {
	WriteHelpBodyTo(w, c.HelpPrinter)
}
