// Package commands — the `rmp graph client` subcommand.
//
// This file is deliberately thin, and its thinness is the point.
// SPEC/GRAPH.md § The Bolt Client requires this subcommand and the client half of
// server resolution to be ONE implementation rather than two: the same
// connection, the same statement, the same retry, and the same mapping of
// protocol values onto JSON that `rmp graph execute` uses when a roadmap is
// served. What a command-line surface adds over that shared client is what is
// here — reading the statement, choosing an exit code, and refusing to fall back.
package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// printGraphClientHelp prints the help for rmp graph client.
//
// SPEC/HELP.md § Graph family help specifics, item 7, places one obligation on
// this help that the generic template does not cover, and it is the obligation
// that distinguishes this subcommand from `execute`: it must state that a roadmap
// with no server listening is a FAILURE and not a fall back onto the store, and
// it must name the socket it resolves. The two subcommands take the same
// statement sources and the same --socket flag and differ only in whether an
// unanswered socket is a fallback or a failure, so that is the one thing an agent
// choosing between them needs told.
func printGraphClientHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp graph client -r <roadmap> [-q <cypher>] [--socket <path>]

Send one Cypher statement to a running graph server over its Unix domain socket
and print the result. It reads and writes alike: the server does not examine the
statement any more than execute does, so a statement that creates, changes,
deletes or alters the schema is executed and committed.

client requires a server. It resolves the socket -- ~/.roadmaps/<name>/graph.sock
unless --socket names another -- and, with nothing listening there, it fails with
exit code 1. It does NOT open the store. That is the whole difference between
this subcommand and execute, which resolves the same socket and has a second path
to fall back on; a subcommand that quietly became execute would report a success
that says nothing about whether a server was reached.

The output is byte for byte what rmp graph execute writes for the same statement
against the same graph, so a caller may parse one shape and change nothing when a
server is started or stopped.

A serialisation conflict is retried, not reported. Two clients writing to the
same nodes at once is ordinary inside a server, and the store detects the
collision rather than preventing it; the losing statement committed nothing, so
it is re-sent under the retry policy and a failure is reported only once that
policy or the statement time budget is exhausted.

The statement runs under the same 5s time budget execute runs under. The server
enforces it; this command keeps a later deadline of its own purely as a backstop
against a server that answers nothing, so a statement that committed just before
the budget expired is never reported as one that wrote nothing.

Required:
  -r, --roadmap <name>    Target roadmap. It selects the graph the statement
                          runs against and, unless --socket overrides it, the
                          socket the statement is sent to
  -q, --query <cypher>    Cypher statement; read from stdin when this flag is
                          absent. Supplying neither is an error (exit code 2)

Optional:
      --socket <path>     Unix domain socket of the server. Default
                          ~/.roadmaps/<name>/graph.sock, the same derivation
                          graph serve uses. Write it when the server was
                          started with the same flag
  -h, --help              Show this help message

Output (stdout JSON):
  With result columns:      {"columns": [...], "rows": [[...], ...]}
  Without result columns:   {"ok": true}
  The same shapes, and the same bytes, that rmp graph execute writes.

Exit codes:
  0   The statement was sent to a server, ran, and its result was written
  1   No server is listening for the roadmap; or a server could not be reached
      through the socket; or the connection was lost, or went unanswered, after
      the statement was sent; or the statement failed to parse or execute in
      the engine; or it exhausted the 5s statement time budget, where the
      Cypher was valid and the store healthy: nothing was written, so the
      remedy is to narrow the statement -- add a label, an indexed property
      filter, or a LIMIT -- or split it into smaller statements; or a value the
      server returned could not be mapped onto the published result shape
  2   No query supplied, --socket given with an empty value, or a positional
      argument was given
  3   No roadmap selected
  4   Roadmap not found
  6   Query longer than the maximum length of 1048576 bytes

Examples:
  rmp graph client -r myproject --query "MATCH (n:Spec) RETURN n.key"
  rmp graph client -r myproject --query "CREATE (n:Spec {key:'auth'})"
  echo "MATCH (n) RETURN count(n)" | rmp graph client -r myproject
  rmp graph client -r myproject --socket /run/user/1000/myproject-graph.sock -q "SHOW INDEXES"
`)
}

// runGraphClient is the implementation of `rmp graph client`.
//
// The order of the checks is `graph execute`'s, and the two are read in the same
// sequence for the same reason: the roadmap selector first (exit 3), then the
// flags (exit 2), then the statement (exit 2 or 6), then the roadmap's existence
// (exit 4), and only then the socket. A refused invocation therefore connects to
// nothing.
//
// It resolves the graph directory although it never opens it. The resolution is
// what produces exit code 4 for a roadmap that does not exist, and that code is
// published for this subcommand exactly as it is for `execute`
// (SPEC/COMMANDS.md § Client Exit Codes). Without it, `-r missing --socket <a
// server's socket>` would send a statement to a graph the caller did not name and
// report success.
//
// # The one thing it does not do
//
// It does not fall back. Two of the four resolution states — no socket, and a
// socket file nothing is listening behind — are the direct path for `execute` and
// are a failure here, reported with the line
// SPEC/COMMANDS.md § Graph Server Socket Error Lines publishes for a roadmap
// nothing is serving. That is a contract and not a limitation: this subcommand's
// answer means "a server ran this", and a subcommand that opened the store when
// no server answered would give the same answer to a different question
// (SPEC/GRAPH.md § The Bolt Client).
func runGraphClient(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	socketFlag, remaining, err := extractSocketFlag(remaining)
	if err != nil {
		return err
	}

	query, err := readQuery(remaining)
	if err != nil {
		return err
	}

	if _, err := resolveGraphDir(roadmapName); err != nil {
		return err
	}

	socket, err := graphSocketInForce(roadmapName, socketFlag)
	if err != nil {
		return err
	}

	state, err := resolveGraphServer(socket)
	if err != nil {
		return err
	}
	if !state.Served() {
		// The two definite negatives are one condition for this subcommand:
		// there is nothing to send the statement to. The leftover socket file a
		// killed server left is neither an error in itself nor removed here — it
		// is the next server's business.
		return graphNoServerListening(socket)
	}

	output, err := runOnGraphServer(socket, query)
	if err != nil {
		return err
	}
	return utils.PrintJSON(output)
}
