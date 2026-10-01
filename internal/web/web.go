package web

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// Default bind address and port for the web interface. The default host is
// 127.0.0.1 (loopback), so the interface is reachable only from the local
// machine; exposing it on the network is the explicit opt-in --host 0.0.0.0
// (or any non-loopback address), which also prints a network-exposure warning
// to stderr at startup. The default port is 8787 with an ephemeral fallback
// when it is busy (see SPEC/WEB.md § Bind Address and Port Selection).
const (
	defaultHost = "127.0.0.1"
	defaultPort = 8787

	minPort = 0
	maxPort = 65535
)

// options carries the resolved command-line configuration for one
// `rmp web` invocation.
type options struct {
	// host is the bind host (default 127.0.0.1, loopback only).
	host string
	// port is the requested bind port (default 8787).
	port int
	// portExplicit reports whether --port was given. It governs the
	// ephemeral-port fallback: only the default port (not an explicit
	// --port) falls back when busy (SPEC/WEB.md rule 3 and 4).
	portExplicit bool
	// noOpen suppresses the browser launch when true.
	noOpen bool
}

// Run is the registry handler for `rmp web`. It parses args, calls printHelp
// when they request help, and otherwise starts the long-lived server. Run
// returns nil after a graceful SIGINT/SIGTERM shutdown (exit 0) and a
// sentinel-wrapped error on a startup failure, which cmd/rmp/main.go maps to
// the matching exit code.
//
// The help is supplied by the caller rather than printed from here because
// its `Exit codes:` block is rendered from the command registry, which this
// package cannot import (SPEC/HELP.md § Agreement with the contract).
func Run(args []string, printHelp func()) error {
	opts, showHelp, err := parseArgs(args)
	if err != nil {
		return err
	}
	if showHelp {
		if printHelp != nil {
			printHelp()
		}
		return nil
	}
	return serve(opts)
}

// parseArgs parses the `rmp web` argument vector. It supports both
// `--flag value` and `--flag=value` forms. The returned showHelp is true
// when a help token was seen; in that case opts and err are zero/nil.
//
// Error sentinels are chosen to land on the SPEC exit codes
// (SPEC/COMMANDS.md § Web Interface, Exit Codes):
//   - utils.ErrRequired   -> exit 2 (a flag is missing its value)
//   - utils.ErrInvalidInput -> exit 2 (unknown flag / unexpected argument)
//   - utils.ErrValidation -> exit 6 (--port out of range or non-integer)
func parseArgs(args []string) (opts options, showHelp bool, err error) {
	opts = options{host: defaultHost, port: defaultPort}

	// No flag is repeatable, and no value of a repeated flag is validated
	// (SPEC/COMMANDS.md § Repeated Flags): the repetition is found before the
	// loop below reads any value.
	if help, rerr := refuseRepeatedFlags(args); rerr != nil || help {
		return options{}, help, rerr
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// A help token is the whole token and nothing else: `--help=1` and
		// `-h=` are unknown flags, never help (SPEC/HELP.md § Help tokens). The
		// comparison therefore reads arg, before the split below removes an
		// "=value" tail.
		if arg == "-h" || arg == "--help" || arg == "help" {
			return options{}, true, nil
		}

		// Split --flag=value once so both forms share one code path.
		name, inlineVal, hasInline := splitFlag(arg)

		switch name {
		case "--no-open":
			if hasInline {
				return options{}, false, fmt.Errorf("%w: --no-open does not take a value", utils.ErrInvalidInput)
			}
			opts.noOpen = true

		case "--host":
			val, next, verr := flagValue(name, inlineVal, hasInline, args, i)
			if verr != nil {
				return options{}, false, verr
			}
			i = next
			opts.host = val

		case "--port":
			val, next, verr := flagValue(name, inlineVal, hasInline, args, i)
			if verr != nil {
				return options{}, false, verr
			}
			i = next
			p, perr := strconv.Atoi(strings.TrimSpace(val))
			if perr != nil {
				return options{}, false, fmt.Errorf("%w: --port must be an integer between %d and %d (got %q)", utils.ErrValidation, minPort, maxPort, val)
			}
			if p < minPort || p > maxPort {
				return options{}, false, fmt.Errorf("%w: --port must be an integer between %d and %d (got %d)", utils.ErrValidation, minPort, maxPort, p)
			}
			opts.port = p
			opts.portExplicit = true

		default:
			if strings.HasPrefix(arg, "-") {
				// The line names the flag without its "=value" tail, as every
				// command's unknown-flag line does (SPEC/COMMANDS.md
				// § Positional Arguments, rule 5): name is arg up to the first "=".
				return options{}, false, fmt.Errorf("%w: unknown flag: %s", utils.ErrInvalidInput, name)
			}
			return options{}, false, fmt.Errorf("%w: unexpected argument: %s", utils.ErrInvalidInput, arg)
		}
	}

	return opts, false, nil
}

// refuseRepeatedFlags reads args left to right as parseArgs does and refuses
// the second occurrence of --no-open, --host or --port, in either the separate
// or the joined form, with the repeated-flag line — unless a help token
// follows it, because a help token is served before any flag is refused
// (SPEC/COMMANDS.md § Repeated Flags), in which case it reports help. It
// examines no value. It stops, refusing nothing, at the first token the main
// loop answers on its own — a help token, an unrecognised flag or an
// unexpected argument — so that token keeps its own outcome.
func refuseRepeatedFlags(args []string) (help bool, err error) {
	var seen utils.FlagOccurrences
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-h" || arg == "--help" || arg == "help" {
			return false, nil
		}
		name, _, hasInline := splitFlag(arg)
		switch name {
		case "--no-open", "--host", "--port":
		default:
			return false, nil
		}
		if rerr := seen.Note(name, arg); rerr != nil {
			if helpTokenAhead(args[i+1:]) {
				return true, nil
			}
			return false, rerr
		}
		if name != "--no-open" && !hasInline && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
		}
	}
	return false, nil
}

// helpTokenAhead reports whether args carries a help token in a token
// position: every position except the value of a separately written --host or
// --port, which is the flag's value unless it begins with "-"
// (SPEC/HELP.md § Help tokens).
func helpTokenAhead(args []string) bool {
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if tok == "-h" || tok == "--help" || tok == "help" {
			return true
		}
		if (tok == "--host" || tok == "--port") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
		}
	}
	return false
}

// splitFlag splits a "--flag=value" token into ("--flag", "value", true).
// A token without '=' (or a bare "-"/"--") returns (token, "", false).
// Only the first '=' is treated as the separator so values may contain '='.
func splitFlag(arg string) (name, value string, hasInline bool) {
	if !strings.HasPrefix(arg, "-") {
		return arg, "", false
	}
	if idx := strings.IndexByte(arg, '='); idx >= 0 {
		return arg[:idx], arg[idx+1:], true
	}
	return arg, "", false
}

// flagValue resolves the value for a value-bearing flag. With an inline
// value (--flag=value) it returns that value and leaves the index
// unchanged. Otherwise it consumes the next argument and returns the
// advanced index. A missing value is an ErrRequired error (exit 2).
func flagValue(name, inlineVal string, hasInline bool, args []string, i int) (val string, nextIndex int, err error) {
	if hasInline {
		return inlineVal, i, nil
	}
	if i+1 >= len(args) {
		return "", i, fmt.Errorf("%w: %s requires a value", utils.ErrRequired, name)
	}
	return args[i+1], i + 1, nil
}

// HelpText returns the `rmp web` help text, carrying exitCodes as its
// `Exit codes:` block. The block is rendered by the caller from the command
// registry, which this package cannot import, so that the help and the AI
// Agent Contract draw every code and condition from one source
// (SPEC/HELP.md § Agreement with the contract). The text follows the
// skeleton in SPEC/HELP.md § Web command help specifics and makes explicit
// the three behaviours an agent cannot infer from the generic template:
// no -r/--roadmap flag; read-only pages with the knowledge-graph query bar as
// the exception that runs the Cypher typed, writes included, and a loopback
// bind by default, so no other machine can connect (with --host 0.0.0.0 as the
// explicit network-exposure opt-in); and the long-lived process that runs until
// interrupted.
func HelpText(exitCodes string) string {
	return `Usage: rmp web [options]

Start a web interface for the roadmaps under ~/.roadmaps/. The browser
lists every roadmap and lets you view its tasks, sprints, and knowledge
graph. Every page is read-only and no roadmap database is ever written.
The knowledge-graph query bar is the exception: it runs the Cypher you
type, including statements that write or delete, and it is not
authenticated. rmp web does not take -r/--roadmap: it lists all roadmaps
and you select one in the browser.

The interface binds loopback (127.0.0.1) by default, so no other machine
can connect to it; to expose it on the network pass the explicit opt-in
--host 0.0.0.0 (all interfaces), which also prints a network-exposure
warning to stderr. --host overrides the bind host; --port overrides the
port. Unlike every other command, rmp web starts a server that keeps
running until interrupted (Ctrl+C / SIGINT or SIGTERM); on startup it prints
the served URL and, unless --no-open is given, opens your default browser at
it.

Options:
  --host <address>   Bind host. Default 127.0.0.1 (loopback: no other
                     machine can connect). Use --host 0.0.0.0 to expose
                     on the network.
  --port <number>    Bind port 0-65535. Default 8787; falls back to an
                     ephemeral port if 8787 is in use and --port is not set.
  --no-open          Do not launch a browser; just print the served URL.
  -h, --help         Show this help message

Output (stdout JSON):
  On startup: {"url": "http://127.0.0.1:8787"} (reflects the bound host/port)

` + exitCodes + `Examples:
  rmp web
  rmp web --port 9000
  rmp web --host 127.0.0.1 --port 9000
  rmp web --no-open
`
}
