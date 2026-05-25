// Package commands — AI-agent discovery banner.
//
// The banner is the SPEC-mandated first line of every plain-text help
// output. Its purpose is to make the machine-readable AI Agent
// Contract (`rmp --ai-help`) discoverable to LLM agents that first
// reach for the standard `--help` surface.
//
// SPEC references:
//   - SPEC/HELP.md § AI agent banner — canonical wording and placement.
//   - SPEC/COMMANDS.md § AI Help, "Discoverability requirements" rule 1.
//
// Single point of truth: every help printer invocation in this package
// (see registry.go DispatchFamily) routes through PrintAIBanner /
// invokeHelpPrinter so the banner cannot drift between commands.
// Adding a new help printer requires no edit here — as long as the
// new printer is registered via HelpPrinter and invoked through the
// dispatch path, the banner is prepended for free.
//
// The banner is intentionally NOT printed on:
//   - the AI Agent Contract path (`rmp --ai-help`, `rmp ai-help`,
//     `rmp <cmd> --ai-help`): that path bypasses HelpPrinter entirely
//     and emits JSON via internal/aihelp.Generate.
//   - `rmp --version`: handled directly in cmd/rmp/main.go without
//     touching this package.

package commands

import (
	"fmt"
	"io"
	"os"
)

// AIBannerLine is the exact, SPEC-mandated banner string. Exported so
// tests in this package and elsewhere can assert byte-equality without
// re-typing the literal (a single source of truth prevents accidental
// drift between the test assertion and the production output).
//
// The backticks are part of the literal SPEC wording. There is exactly
// one blank line between the banner and the help body — see
// WriteAIBanner for the placement rule.
const AIBannerLine = "AI agents: run `rmp --ai-help` for a machine-readable command contract."

// WriteAIBanner writes the AI-agent discovery banner followed by a
// single blank line. The caller writes the existing help body
// immediately after.
//
// Output shape (exactly three lines, with the last being the start of
// the help body that the caller appends):
//
//	AI agents: run `rmp --ai-help` for a machine-readable command contract.
//	<blank>
//	<existing help body...>
//
// Errors from w.Write are ignored: the banner is best-effort and a
// failure here (broken pipe, closed stdout) will manifest on the next
// write inside the help printer itself, which is already the
// authoritative error site.
func WriteAIBanner(w io.Writer) {
	// Use a single Write to minimise the number of syscalls and to
	// guarantee the banner cannot be interleaved with the body when
	// two goroutines race on the same writer (not a real scenario for
	// the CLI today, but cheap to keep correct).
	fmt.Fprintln(w, AIBannerLine)
	fmt.Fprintln(w)
}

// invokeHelpPrinter is the single dispatcher used by registry.go to
// run a plain-text help printer. It prepends the AI-agent discovery
// banner on stdout, then invokes the printer (which writes to stdout
// via fmt.Println / fmt.Printf, the established convention in this
// package).
//
// Centralising the call here means the banner cannot drift between
// commands: adding a new HelpPrinter automatically inherits the
// banner the moment its dispatch path goes through this function.
func invokeHelpPrinter(printer func()) {
	if printer == nil {
		return
	}
	WriteAIBanner(os.Stdout)
	printer()
}
