// Package commands — web command registry entry.
package commands

import "github.com/FlavioCFOliveira/Groadmap/internal/web"

// runWeb is the dispatch adapter for `rmp web`. web is a leaf command
// (HasSubcommand: false), so DispatchFamily routes the raw args straight to
// this handler and bypasses the family-level help path that prepends the
// SPEC AI-agent banner. Mirroring HandleStats, the leaf handler must detect
// the help token itself and route web.PrintHelp through invokeHelpPrinter so
// the banner is emitted uniformly (SPEC/HELP.md § AI agent banner). Keeping
// this wrapper here — rather than in the web package — preserves the
// commands -> web dependency direction and keeps the banner string a
// single-source commands-package concern; web.PrintHelp stays banner-free.
func runWeb(args []string) error {
	if len(args) > 0 && isHelpToken(args[0]) {
		invokeHelpPrinter(web.PrintHelp)
		return nil
	}
	return web.Run(args)
}

// buildWebCommand registers the `rmp web` command. Unlike every other
// roadmap-scoped family, web takes NO -r/--roadmap flag: it lists all
// roadmaps and the user selects one in the browser (SPEC/COMMANDS.md
// § Roadmap Selection (Always Required), the web exemption). web is a leaf
// command (HasSubcommand: false) with a single empty-name Subcommand whose
// Handler and HelpPrinter live in the web package.
func buildWebCommand() Command {
	return Command{
		Name:          "web",
		Summary:       "Start a read-only web interface for the roadmaps under ~/.roadmaps/.",
		Description:   "Starts a long-lived HTTP server embedded in the rmp binary that presents every roadmap under ~/.roadmaps/ as read-only HTML and an interactive knowledge-graph visualisation. It binds loopback 127.0.0.1 by default, so it is reachable only from the local machine; pass --host 0.0.0.0 to expose it on the network (which prints a network-exposure warning to stderr). It serves GET/HEAD only, prints the served URL, opens a browser unless --no-open is given, and runs until interrupted (Ctrl+C / SIGINT / SIGTERM). It does not take -r/--roadmap and never writes; the CLI remains the sole write path.",
		HelpPrinter:   web.PrintHelp,
		HasSubcommand: false,
		Subcommands: []Subcommand{
			{
				Name:        "",
				Summary:     "Start the read-only web interface.",
				Description: "Resolves the bind host/port (default 127.0.0.1:8787, with an ephemeral-port fallback when 8787 is busy and --port was not given), serves the read-only routes, prints the served URL as a JSON object, and runs until SIGINT/SIGTERM.",
				Usage:       "rmp web [--host <address>] [--port <number>] [--no-open]",
				HelpPrinter: web.PrintHelp,
				Handler:     runWeb,
				Flags: []Flag{
					{Long: "--host", Type: "string", Default: "127.0.0.1", Description: "Bind host. Default 127.0.0.1 (loopback only), reachable solely from the local machine. Use --host 0.0.0.0 to bind all interfaces and expose the read-only interface on the network (which prints a network-exposure warning to stderr)."},
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
				ExitCodes:  []int{0, 1, 2, 6},
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
