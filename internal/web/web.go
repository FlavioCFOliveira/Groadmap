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

// Run is the registry handler for `rmp web`. It parses args, prints help
// when requested, and otherwise starts the long-lived server. Run returns
// nil after a graceful SIGINT/SIGTERM shutdown (exit 0) and a sentinel-
// wrapped error on a startup failure, which cmd/rmp/main.go maps to the
// matching exit code.
func Run(args []string) error {
	opts, showHelp, err := parseArgs(args)
	if err != nil {
		return err
	}
	if showHelp {
		PrintHelp()
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

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Split --flag=value once so both forms share one code path.
		name, inlineVal, hasInline := splitFlag(arg)

		switch name {
		case "-h", "--help", "help":
			return options{}, true, nil

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
				return options{}, false, fmt.Errorf("%w: unknown flag: %s", utils.ErrInvalidInput, arg)
			}
			return options{}, false, fmt.Errorf("%w: unexpected argument: %s", utils.ErrInvalidInput, arg)
		}
	}

	return opts, false, nil
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

// PrintHelp writes the `rmp web` help text to stdout. The text follows the
// skeleton in SPEC/HELP.md § Web command help specifics and makes explicit
// the three behaviours an agent cannot infer from the generic template:
// no -r/--roadmap flag, read-only and loopback-only by default (with
// --host 0.0.0.0 as the explicit network-exposure opt-in), and the long-lived
// process that runs until interrupted.
func PrintHelp() {
	fmt.Print(`Usage: rmp web [options]

Start a read-only web interface for the roadmaps under ~/.roadmaps/.
The browser lists every roadmap and lets you view its tasks, sprints,
and knowledge graph. The web interface never writes; the rmp CLI
remains the sole write path. rmp web does not take -r/--roadmap: it
lists all roadmaps and you select one in the browser.

The interface binds loopback (127.0.0.1) by default, so it is reachable
only from the local machine; to expose it on the network pass the
explicit opt-in --host 0.0.0.0 (all interfaces), which also prints a
network-exposure warning to stderr. --host overrides the bind host; --port
overrides the port. Unlike every other command, rmp web starts a server that
keeps running until interrupted (Ctrl+C / SIGINT or SIGTERM); on startup it
prints the served URL and, unless --no-open is given, opens your default
browser at it.

Options:
  --host <address>   Bind host. Default 127.0.0.1 (loopback, local machine
                     only). Use --host 0.0.0.0 to expose on the network.
  --port <number>    Bind port 0-65535. Default 8787; falls back to an
                     ephemeral port if 8787 is in use and --port is not set.
  --no-open          Do not launch a browser; just print the served URL.
  -h, --help         Show this help message

Output (stdout JSON):
  On startup: {"url": "http://127.0.0.1:8787"} (reflects the bound host/port)

Exit codes:
  0   Server started and was stopped by Ctrl+C / SIGINT / SIGTERM
  1   Host/port could not be bound, or the data directory was unreadable
  2   Unknown flag or unexpected argument
  6   --port out of range 0-65535 or not an integer

Examples:
  rmp web
  rmp web --port 9000
  rmp web --host 127.0.0.1 --port 9000
  rmp web --no-open
`)
}
