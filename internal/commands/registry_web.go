// Package commands — web command registry entry.
package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/web"
)

// printWebHelp writes the `rmp web` help through helpDst. The text is the web
// package's; its `Exit codes:` block is rendered here, from this command's
// registry entry, because the web package cannot import the registry
// (SPEC/HELP.md § Agreement with the contract).
func printWebHelp() {
	fmt.Fprint(helpDst(), web.HelpText(exitCodesBlock("web", "")))
}

// runWeb is the dispatch adapter for `rmp web`. web is a leaf command
// (HasSubcommand: false), so DispatchFamily routes the raw args straight to
// this handler and never runs the hasHelpFlag short-circuit it runs for a
// family's subcommands, which is the path that inserts the SPEC AI-agent
// banner. The handler therefore applies that same predicate to its whole
// argument list itself, as HandleStats does, and routes printWebHelp through
// invokeHelpPrinter so the banner is emitted uniformly, wherever the help
// token is written in a token position (SPEC/HELP.md § Help levels, § Help
// tokens, § AI agent banner). Which tokens are flag values is read from this
// command's own registry declaration, so `rmp web --host help` binds a host
// named `help` rather than writing the help.
//
// The check used to read args[0] alone. A help token written after another
// flag, as in `rmp web --no-open --help`, then fell through to web.Run, whose
// own parser serves the help by calling the printer it is handed directly,
// without the banner (rmp task 475).
//
// Keeping this wrapper here — rather than in the web package — preserves the
// commands -> web dependency direction and keeps the banner string a
// single-source commands-package concern; printWebHelp stays banner-free.
func runWeb(args []string) error {
	if hasHelpFlag(leafSubcommand("web"), args) {
		return invokeHelpPrinter(printWebHelp)
	}
	return web.Run(args, printWebHelp)
}

// buildWebCommand registers the `rmp web` command. Unlike every other
// roadmap-scoped family, web takes NO -r/--roadmap flag: it lists all
// roadmaps and the user selects one in the browser (SPEC/COMMANDS.md
// § Roadmap Selection (Always Required), the web exemption). web is a leaf
// command (HasSubcommand: false) with a single empty-name Subcommand whose
// Handler adapts the web package's Run and whose HelpPrinter renders the
// web package's help text with this entry's exit codes.
func buildWebCommand() Command {
	return Command{
		Name:          "web",
		Summary:       "Start a web interface for the roadmaps under ~/.roadmaps/.",
		Description:   "Starts a long-lived HTTP server embedded in the rmp binary that presents every roadmap under ~/.roadmaps/ as server-rendered HTML and an interactive knowledge-graph visualisation. Every page is read-only and no roadmap database is written; the knowledge-graph query bar is the exception: it runs the Cypher typed, including statements that write or delete, with no authentication. It binds loopback 127.0.0.1 by default, so no other machine can connect to it; pass --host 0.0.0.0 to expose it on the network (which prints a network-exposure warning to stderr). It serves only requests that name a host the bind allows and that a browser did not make on behalf of another site (WEB.md § Security and Constraints, rules 13 and 14), answering any other with 403. It serves GET/HEAD only, prints the served URL, opens a browser unless --no-open is given, and runs until interrupted (Ctrl+C / SIGINT / SIGTERM). It does not take -r/--roadmap.",
		HelpPrinter:   printWebHelp,
		HasSubcommand: false,
		Subcommands: []Subcommand{
			{
				Name:        "",
				Summary:     "Start the web interface.",
				Description: "Resolves the bind host/port (default 127.0.0.1:8787, with an ephemeral-port fallback when 8787 is busy and --port was not given), serves the read-only routes, prints the served URL as a JSON object, and runs until SIGINT/SIGTERM.",
				Usage:       "rmp web [--host <address>] [--port <number>] [--no-open]",
				HelpPrinter: printWebHelp,
				Handler:     runWeb,
				// `rmp web` publishes its own refusal for an excess positional
				// argument — "unexpected argument: X", with a colon and no
				// quotes (SPEC/COMMANDS.md § Web Interface). The shared
				// enforcement point defers so that wording survives.
				PublishesOwnArityRefusal: true,
				Flags: []Flag{
					{Long: "--host", Type: "string", Default: "127.0.0.1", Description: "Bind host. Default 127.0.0.1 (loopback only), so no other machine can connect to it. Use --host 0.0.0.0 to bind all interfaces and expose the interface on the network (which prints a network-exposure warning to stderr)."},
					{Long: "--port", Type: "integer", HasRange: true, RangeMin: 0, RangeMax: 65535, Default: "8787", Description: "Bind port 0-65535. Default 8787; falls back to an ephemeral port if 8787 is in use and --port is not set."},
					{Long: "--no-open", Type: "boolean", Description: "Do not launch a browser; just print the served URL."},
					helpFlag(),
				},
				Output: SuccessOutput{
					Kind:    "object",
					Schema:  `{"url":"http://host:port"}`,
					Example: `{"url":"http://127.0.0.1:8787"}`,
				},
				SideEffects: SideEffects{
					Database:   "Read-only (no writes, no audit entry).",
					Filesystem: "Read-only; serves embedded assets.",
					Network:    "Serves a local HTTP server on the bound host/port; makes no outbound request.",
				},
				Idempotent: false,
				Prerequisites: []string{
					"When --port names a port explicitly, that port is free on the bind host: an explicit port is never replaced by an ephemeral one, and a busy one is a failure rather than a fallback.",
				},
				ExitCodes: []ExitCodeEntry{
					ec(0, "The server bound its host and port, wrote the served URL to stdout, and served read-only routes until SIGINT or SIGTERM."),
					ec(1,
						"The requested host and port could not be bound: an explicit --port is already in use, or the host is not assignable.",
						"The data directory ~/.roadmaps/ could not be read.",
						"The listener stopped accepting connections after the server had started.",
					),
					ec(2, "An unrecognised flag was supplied, or a positional argument was supplied; this command accepts none."),
					ec(6, "--port falls outside 0-65535, or is not an integer."),
				},
				Examples: []Example{
					{
						Title:  "Start on the default loopback address and port",
						Cmd:    "rmp web",
						Stdout: `{"url":"http://127.0.0.1:8787"}`,
						Exit:   0,
					},
					{
						Title:  "Start without opening a browser",
						Cmd:    "rmp web --no-open",
						Stdout: `{"url":"http://127.0.0.1:8787"}`,
						Exit:   0,
					},
					{
						Title:  "Port out of range",
						Cmd:    "rmp web --port 70000",
						Stderr: "Error: validation error: --port must be an integer between 0 and 65535 (got 70000)",
						Exit:   6,
					},
					{
						Title:  "Unknown flag",
						Cmd:    "rmp web --foo",
						Stderr: "Error: invalid input: unknown flag: --foo",
						Exit:   2,
					},
				},
			},
		},
	}
}
