// Package commands — AI-agent discovery banner.
//
// The banner is the SPEC-mandated line that every plain-text help output
// carries immediately after its single `Usage:` line. Its purpose is to make
// the machine-readable AI Agent Contract (`rmp --ai-help`) discoverable to LLM
// agents that first reach for the standard `--help` surface.
//
// SPEC references:
//   - SPEC/HELP.md § AI agent banner — canonical wording and placement.
//   - SPEC/COMMANDS.md § AI Help, "Discoverability requirements" rule 1.
//
// Single point of truth: no help printer writes the banner. Every printer
// writes only its body, through helpDst, and WriteHelpWithAIBanner is the one
// place that inserts the banner into a rendered body. Both stdout help paths
// reach it: invokeHelpPrinter, which every family, subcommand and leaf help
// dispatch in this package goes through, and the global help printer in
// cmd/rmp. Adding a new help printer therefore requires no edit here: as long
// as the new printer is registered via HelpPrinter, writes a `Usage:` line, and
// is invoked through the dispatch path, the banner is inserted for free.
//
// Why the body is rendered first and the banner inserted afterwards, rather
// than written in the middle of the printer's output: the banner's position is
// defined by the body's content (the line after `Usage:`), and the body is
// owned by more than sixty independent printers. Inserting into the rendered
// text keeps the placement rule in one function instead of in every printer,
// and it makes the recovery help (SPEC/HELP.md § Recovery help after a
// dispatch failure) the same rendered body with nothing inserted, so the two
// cannot differ by anything but the banner line.
//
// The banner is intentionally NOT printed on:
//   - the AI Agent Contract path (`rmp --ai-help`, `rmp ai-help`,
//     `rmp <cmd> --ai-help`): that path bypasses HelpPrinter entirely
//     and emits JSON via internal/aihelp.Generate.
//   - `rmp --version`: handled directly in cmd/rmp/main.go without
//     touching this package.
//   - the recovery help written to stderr after a dispatch failure, which
//     renders the body through WriteHelpBodyTo and inserts nothing.

package commands

import (
	"bytes"
	"errors"
	"io"
	"os"
)

// AIBannerLine is the exact, SPEC-mandated banner sentence. It is also the
// sentence of the AI-agent hint written to stderr (SPEC/HELP.md § Stderr part
// order, § AI_AGENT environment variable): cmd/rmp passes this constant to the
// emitters in internal/aihelp, so the banner and the hint have one source.
// Exported so tests in this package and elsewhere can assert byte-equality
// without re-typing the literal.
//
// The backticks are part of the literal SPEC wording.
const AIBannerLine = "AI agents usage: run `rmp --ai-help` for a machine-readable command contract."

// usageLinePrefix opens the one line of every plain-text help that the banner
// follows (SPEC/HELP.md § Help structure template, item 1). It is matched at
// the start of a line only, so a mention of the word elsewhere in a line
// cannot move the banner.
const usageLinePrefix = "Usage:"

// errHelpWithoutUsageLine reports a help body that has no line beginning with
// usageLinePrefix. It marks a defect in a help printer, not a condition of the
// invocation, so it wraps no classification sentinel: cmd/rmp maps it to the
// general-error exit code 1.
var errHelpWithoutUsageLine = errors.New(`help text has no line starting with "Usage:", so the AI agent banner has no place`)

// insertAIBanner returns a copy of body with AIBannerLine inserted as the line
// immediately after the first line that begins with "Usage:". Every other
// byte of body is kept, in order, so the blank line that followed the `Usage:`
// line now follows the banner and no other line moves (SPEC/HELP.md § AI agent
// banner, Placement).
//
// A body with no such line has no place for the banner. That is a defect in
// the help printer, not a condition a caller can recover from, so it is
// reported as an error and no output is produced: a help without its banner is
// never written. The registry-wide tests in banner_test.go render every
// registered printer through this function, so the defect fails the test
// suite before it can reach a binary.
func insertAIBanner(body []byte) ([]byte, error) {
	at := usageLineEnd(body)
	if at < 0 {
		return nil, errHelpWithoutUsageLine
	}

	out := make([]byte, 0, len(body)+len(AIBannerLine)+1)
	out = append(out, body[:at]...)
	out = append(out, '\n')
	out = append(out, AIBannerLine...)
	out = append(out, body[at:]...)
	return out, nil
}

// usageLineEnd returns the offset just past the text of the first line of body
// that begins with usageLinePrefix, that is, the offset of that line's
// terminating newline, or len(body) when the line is the last one and carries
// no newline. It returns -1 when no line begins with the prefix.
func usageLineEnd(body []byte) int {
	for start := 0; start <= len(body); {
		end := bytes.IndexByte(body[start:], '\n')
		if end < 0 {
			end = len(body)
		} else {
			end += start
		}
		if bytes.HasPrefix(body[start:end], []byte(usageLinePrefix)) {
			return end
		}
		start = end + 1
	}
	return -1
}

// WriteHelpWithAIBanner renders a help body with render, inserts the AI-agent
// banner after its `Usage:` line, and writes the result to w in one Write.
//
// It is the single place that places the banner on a stdout help: the family,
// subcommand and leaf helps reach it through invokeHelpPrinter, and the global
// help reaches it from cmd/rmp, whose body this package does not own.
//
// The body is rendered into a buffer first, so a body without a `Usage:` line
// writes nothing at all and the error is returned for the caller's failure
// path. Errors from w.Write are ignored, as they always were on the help path:
// stdout is best-effort there, and a closed stdout surfaces through the
// process's own SIGPIPE handling rather than through this function.
func WriteHelpWithAIBanner(w io.Writer, render func(io.Writer)) error {
	if w == nil || render == nil {
		return nil
	}
	var body bytes.Buffer
	render(&body)
	help, err := insertAIBanner(body.Bytes())
	if err != nil {
		return err
	}
	_, _ = w.Write(help) // best-effort, see the function comment
	return nil
}

// invokeHelpPrinter is the single dispatcher used by registry.go, and by the
// `stats` and `web` leaf handlers, to run a plain-text help printer on stdout.
// It renders the printer's body through WriteHelpBodyTo, which diverts
// helpDst to the buffer WriteHelpWithAIBanner supplies, and writes that body
// with the banner inserted after its `Usage:` line.
//
// os.Stdout is resolved at call time, so a test that redirects os.Stdout
// observes the output.
//
// Centralising the call here means the banner cannot drift between commands:
// adding a new HelpPrinter automatically inherits the banner the moment its
// dispatch path goes through this function. The returned error is the one
// insertAIBanner reports for a body without a `Usage:` line; the dispatch path
// returns it, so the defect fails the invocation instead of writing a help
// without its banner.
func invokeHelpPrinter(printer func()) error {
	if printer == nil {
		return nil
	}
	return WriteHelpWithAIBanner(os.Stdout, func(w io.Writer) {
		WriteHelpBodyTo(w, printer)
	})
}
