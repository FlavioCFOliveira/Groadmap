// Package commands — the `rmp graph serve` subcommand.
//
// This file holds the CLI's half of the dedicated graph server and nothing else:
// the flags, the roadmap and socket resolution, the startup object on stdout, the
// help, and the exit code. The server's lifecycle — the listener, the engine's
// Bolt server options, the drain and the ordered teardown — belongs to
// internal/graphserve, and the store's lifecycle to internal/graphstore
// (SPEC/ARCHITECTURE.md modules 8 and 10).
package commands

import (
	"fmt"
	"os"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphserve"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// socketFlagLong is the flag that names the socket an invocation resolves. It has
// no short form: `-s` is unclaimed today but reads as nothing in particular, and
// the flag is written rarely — only against a server started with the same flag.
const socketFlagLong = "--socket"

// graphServeResult is the startup object `rmp graph serve` writes to stdout: the
// absolute path of the socket the server bound, so a caller that supplied no
// --socket still learns the path (SPEC/COMMANDS.md § Serve Output).
type graphServeResult struct {
	Socket string `json:"socket"`
}

// printGraphServeHelp prints the help for rmp graph serve.
//
// SPEC/HELP.md § Graph family help specifics places two obligations on this help
// that the generic template does not cover, and both are met below:
//
//   - Item 8: it must state that the command does not complete and exit, that it
//     serves until Ctrl+C or SIGTERM and then drains, checkpoints and exits 0,
//     that it prints the bound socket path as JSON on startup, and that one
//     server runs per roadmap with a second failing rather than queuing.
//   - Item 9: it must state the socket's access model — mode 0600 inside the
//     roadmap's 0700 home, no authentication, and that any caller able to open
//     the socket can read, write, delete and change the schema of that roadmap's
//     graph — and, where it documents --socket, the consequence a user cannot
//     infer: the CLI follows a non-default socket through the same flag and the
//     web interface cannot follow it at all.
func printGraphServeHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp graph serve -r <roadmap> [--socket <path>]

Open the roadmap knowledge graph once, hold it and its advisory store lock for
the life of the process, and answer Cypher statements over a Unix domain socket
until stopped. The protocol is Bolt version 5, served by the graph engine's own
server; rmp defines no protocol of its own.

serve is long-lived. It does not complete and exit like every other command
except rmp web: it keeps running until it receives Ctrl+C (SIGINT) or SIGTERM,
then drains the work in flight, shuts the server down, checkpoints, releases
the lock, removes the socket, and exits 0. On startup it prints the bound
socket path as JSON on stdout.

One server per roadmap. The roadmap store lock is the interlock: a second
rmp graph serve against the same roadmap cannot take it, fails with exit code
1, and leaves the first server's socket untouched. It does not queue.

serve runs no statement of its own, creates no graph directory that does not
already exist, and never reads or writes a roadmap project.db. It serves one
roadmap; serving several means running several servers, one per roadmap, each
on its own socket.

Access control is the filesystem and there is no other. The socket is created
with mode 0600, set explicitly rather than left to the process umask, inside a
roadmap home directory that is 0700. The server authenticates nobody: any
caller able to open the socket can read, write, delete and change the schema of
that roadmap knowledge graph.

Required:
  -r, --roadmap <name>    Target roadmap. The server serves this one roadmap
                          graph and no other

Optional:
      --socket <path>     Unix domain socket to bind. Default
                          ~/.roadmaps/<name>/graph.sock, derived from the
                          roadmap. A non-default path is followed by the CLI
                          through the same flag and by nothing else: the web
                          interface has no way to receive it, resolves the
                          default path, finds nothing there, and fails against
                          this server lock for as long as it runs
  -h, --help              Show this help message

Output (stdout JSON):
  {"socket": "/home/user/.roadmaps/myproject/graph.sock"}

  Per-statement results go to the client that asked for them, never to this
  command stdout. Two warnings from the engine are expected on stderr at
  startup and are not failures: one for a server running without transport
  security, and one for a server running without authentication.

Exit codes:
  0   The server started, served, and was stopped by SIGINT or SIGTERM
  1   The graph store could not be opened or recovered; or its exclusive lock
      could not be taken within the bounded wait, which is what refuses a
      second server against the same roadmap; or the socket could not be
      bound; or a live server already answers on the resolved socket
  2   Unknown flag, a positional argument, or --socket with an empty value
  3   No roadmap selected
  4   Roadmap not found

Examples:
  rmp graph serve -r myproject
  rmp graph serve -r myproject --socket /run/user/1000/myproject-graph.sock
`)
}

// readSocketFlag extracts --socket from args and refuses everything else.
//
// It returns the empty string when the flag is absent, which the caller reads as
// "derive the default path". The two refusals mirror the ones `graph execute`
// already publishes for its own flag, because the classification is the CLI's and
// not this subcommand's: a flag-like token that is not --socket is an unknown
// flag, and anything else is an unexpected argument.
//
// The unexpected-argument branch is defence in depth rather than the live path.
// `graph serve` declares an arity of zero and does NOT publish a refusal of its
// own, so the shared enforcement point refuses a positional argument with the
// canonical line before this function is ever reached (SPEC/COMMANDS.md
// § Positional Arguments; the paragraph naming `graph serve` as the subcommand
// that publishes the canonical line rather than `graph execute`'s hinted one).
//
// A flag supplied with an empty — or whitespace-only — value is a MISSING
// parameter and not a validation failure: it names no socket at all, which is the
// same condition as writing the flag with nothing after it. The value itself is
// never trimmed, because a path is not text a user reads back and a path may
// legitimately end in a space.
func readSocketFlag(args []string) (string, error) {
	value, rest, err := extractSocketFlag(args)
	if err != nil {
		return "", err
	}
	for _, token := range rest {
		if isFlagLike(token) {
			return "", fmt.Errorf("%w: unknown flag: %s", utils.ErrInvalidInput, token)
		}
		return "", fmt.Errorf("%w: unexpected argument %q", utils.ErrInvalidInput, token)
	}
	return value, nil
}

// announceServeSocket writes the startup object. It is passed to
// internal/graphserve as a callback rather than performed there because the
// output is the CLI's: that package serialises nothing.
func announceServeSocket(socket string) error {
	return utils.PrintJSON(graphServeResult{Socket: socket})
}

// runGraphServe is the implementation of `rmp graph serve`.
//
// The order of the checks is the CLI's ordinary one and is what the published
// exit codes describe: the roadmap selector first (exit 3), then this
// subcommand's own flags (exit 2), then the roadmap's existence (exit 4), and
// only then anything that touches the filesystem (exit 1). A refused invocation
// therefore binds nothing, removes nothing, and takes no lock.
//
// It resolves the graph directory WITHOUT creating it. `rmp graph execute`
// creates one on first use because a statement has to have somewhere to run;
// serving a graph that does not exist has no such justification, and a server
// that materialised an empty store would report a roadmap as served whose graph
// nobody had ever written to (SPEC/COMMANDS.md § Serve).
func runGraphServe(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	socketFlag, err := readSocketFlag(remaining)
	if err != nil {
		return err
	}

	graphDir, err := resolveGraphDir(roadmapName)
	if err != nil {
		return err
	}
	if err := requireGraphStore(roadmapName, graphDir); err != nil {
		return err
	}

	socketPath, err := graphSocketInForce(roadmapName, socketFlag)
	if err != nil {
		return err
	}

	return graphserve.Run(graphserve.Options{
		Announce:    announceServeSocket,
		RoadmapName: roadmapName,
		GraphDir:    graphDir,
		SocketPath:  socketPath,
	})
}

// requireGraphStore refuses to serve a roadmap that has no graph store yet.
//
// The refusal is stated here, before the lock is taken, because the alternative
// is worse in both directions: creating the directory is what the specification
// forbids this subcommand, and letting the open fail on it reports the absence as
// a lock file that could not be opened — which names the wrong thing entirely.
// The line carries the published prefix of the store-failure row, so a reader
// matching that row matches this too (SPEC/COMMANDS.md § Serve Error Cases).
func requireGraphStore(roadmapName, graphDir string) error {
	if info, err := os.Stat(graphDir); err == nil && info.IsDir() {
		return nil
	}
	return fmt.Errorf("%w: graph store unavailable: roadmap %q has no graph store at %s, "+
		"and rmp graph serve creates none", utils.ErrDatabase, roadmapName, graphDir)
}
