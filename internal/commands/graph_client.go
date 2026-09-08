// Package commands — the `rmp graph client` subcommand.
//
// This file is deliberately thin, and its thinness is the point.
// SPEC/GRAPH.md § The Bolt Client requires this subcommand and the web graph
// data endpoint to reach a graph through ONE implementation rather than two: the
// same connection, the same statement, the same retry, and the same mapping of
// protocol values onto JSON. That implementation is internal/graphclient. What a
// command-line surface adds over it is what is here — reading the statement,
// choosing an exit code, and failing rather than opening a store.
//
// This is the only subcommand that runs a Cypher statement, and there is no
// second path behind it: with nothing listening it fails
// (SPEC/COMMANDS.md § Graph Management).
package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// printGraphClientHelp prints the help for rmp graph client.
//
// This is now the subcommand that runs a Cypher statement, so items 1 to 4 of
// SPEC/HELP.md § Graph family help specifics govern this help: where the
// statement comes from, that the statement is run as given and is refused for
// nothing it does, that the schema DDL and the schema listings run through this
// same subcommand, and the statement time budget as a cause of exit code 1.
//
// Item 6 places the obligation that is easiest to discharge halfway. The help
// must say that the statement goes to a running server, that there is no other
// way to run one — no flag, no fallback, no one-shot form — and it must NAME the
// command that starts a server, because a caller who reads only this help is
// otherwise told what fails and not what to do about it. Naming it does a second
// job: starting a server is also what creates a roadmap's graph in the first
// place, so an agent meeting an unserved roadmap is told the one command that
// resolves both conditions. The --socket paragraph documents the flag for what it
// is — the socket this invocation connects to — and not as a switch that selects
// between a server and something else, because there is nothing else.
//
// Item 7 requires the failure to be stated: a roadmap with no server listening is
// exit code 1 and not a fall back onto the store, and the socket it resolves is
// named. No sibling subcommand behaves otherwise any more, and the statement is
// still worth making, because the failure is one an agent will meet often and
// must not read as a defect — a result here is evidence that a server was
// reached, and its absence is evidence that none was.
//
// Item 10 places one more, and it is the one this help is most at risk of
// thinking it has already discharged. The prose paragraph below states that a
// serialisation conflict is retried rather than reported — true, and not the
// obligation: the exit-codes block is where a caller goes to learn why a command
// exited 1, so the exit-code-1 line itself names the exhausted conflict, states
// that nothing was written, and gives the remedy in the published error line's
// own terms. It is the one cause in that line whose remedy is to run the SAME
// statement again rather than to rewrite it or correct it.
//
// The clause does NOT write the retry budget's figure. graphWriteConflict
// renders it from backoff.Total() so that one quantity keeps one expression, and
// a figure spelled out here would be a second expression of it that disagreed
// with the policy silently the moment the policy moved.
func printGraphClientHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp graph client -r <roadmap> [-q <cypher>] [--socket <path>]

Send one Cypher statement to a running graph server over its Unix domain socket
and print the result. This is the only way to run a statement against a roadmap
knowledge graph: there is no flag, no fallback, and no one-shot form. It reads
and writes alike -- the server does not examine the statement at all, so a
statement that creates, changes, deletes or alters the schema is executed and
committed.

client requires a server. It connects to ~/.roadmaps/<name>/graph.sock, unless
--socket names another, and with nothing listening there it fails with exit
code 1. It does NOT open the store, and no subcommand does. Start a server with

  rmp graph serve -r <roadmap>

which is also what creates a roadmap knowledge graph the first time. A result
from this command is evidence that a server ran the statement, and a failure
here is evidence that none was reached; neither is a defect.

client accepts every statement the engine accepts and runs it as given:
  - a read, such as MATCH ... RETURN, including variable-length traversals;
  - a write, such as CREATE, MERGE, SET or REMOVE;
  - a deletion, such as DELETE or DETACH DELETE;
  - schema DDL: CREATE INDEX [name] [IF NOT EXISTS] FOR (n:Label) ON (n.property)
    [OPTIONS {indexType:'hash'|'btree'}], DROP INDEX <name> [IF EXISTS],
    CREATE CONSTRAINT [name] [IF NOT EXISTS] FOR (n:Label)
    REQUIRE n.property IS UNIQUE | IS NOT NULL, DROP CONSTRAINT <name> [IF EXISTS];
  - schema introspection: SHOW INDEXES and SHOW CONSTRAINTS, and their singular
    aliases, each optionally followed by a YIELD / WHERE / RETURN projection.

rmp does not examine the statement and refuses none for what it does: what a
statement reads, writes or deletes is decided by its Cypher alone, so the
guarantee you need about a statement is a guarantee about the text you supply.

One statement per invocation. There is no ALTER INDEX: changing an index is a
DROP INDEX followed by a CREATE INDEX, as two separate invocations, and the
index is absent between them. An index or a constraint covers a single node
property; removal is by name, and SHOW INDEXES reports the name an unnamed
object was given.

A serialisation conflict is retried, not reported. Two clients writing to the
same nodes at once is ordinary inside a server, and the store detects the
collision rather than preventing it; the losing statement committed nothing, so
it is re-sent under the retry policy and a failure is reported only once that
policy or the statement time budget is exhausted.

The statement runs under a 5s time budget. The server enforces it; this command
keeps a later deadline of its own purely as a backstop against a server that
answers nothing, so a statement that committed just before the budget expired is
never reported as one that wrote nothing.

Required:
  -r, --roadmap <name>    Target roadmap. It selects the graph the statement
                          runs against and, unless --socket overrides it, the
                          socket the statement is sent to
  -q, --query <cypher>    Cypher statement; read from stdin when this flag is
                          absent. Supplying neither is an error (exit code 2)

Optional:
      --socket <path>     Unix domain socket this invocation connects to.
                          Default ~/.roadmaps/<name>/graph.sock, the same
                          derivation graph serve uses. It names which socket is
                          connected to and nothing else: it selects nothing
                          else, because there is nothing else. Write it only
                          when the server was started with the same flag
  -h, --help              Show this help message

Output (stdout JSON):
  With result columns:      {"columns": [...], "rows": [[...], ...], "plan": <plan node, EXPLAIN only>, "profile": <plan node, PROFILE only>, "counters": <write counters, only when the statement changed the graph>}
  Without result columns:   {"ok": true, "counters": <write counters, only when the statement changed the graph>}
  A statement carrying a RETURN clause produces columns and one without it does
  not; SHOW INDEXES and SHOW CONSTRAINTS produce columns although they carry no
  RETURN clause.
  A statement written with the EXPLAIN or PROFILE prefix always produces the
  columns shape so that it can carry its plan, even with no column of its own.
  EXPLAIN runs nothing and reports the plan the engine would run; PROFILE
  runs the statement and reports what the run measured.
  A statement that changed the graph adds a counters block naming what it
  changed -- nodes and relationships created and deleted, properties written,
  labels added and removed, indexes and constraints added and removed. A zero
  counter is left out, and a statement that changed nothing carries no counters
  key at all, so a MERGE that created is distinguishable from one that matched.

Exit codes:
  0   The statement was sent to a server, ran, and its result was written
  1   No server is listening for the roadmap -- start one with rmp graph serve
      -r <roadmap>; or a server could not be reached through the socket; or the
      connection was lost, or went unanswered, after the statement was sent; or
      the statement failed to parse or execute in the engine, including a schema
      statement the engine refused: a duplicate create, a drop of an object that
      does not exist, an unsupported definition, or a constraint the data does
      not satisfy; or it exhausted the 5s statement time budget, where the
      Cypher was valid and the store healthy: nothing was written, so the
      remedy is to narrow the statement -- add a label, an indexed property
      filter, or a LIMIT -- or split it into smaller statements; or every
      attempt of the retry policy lost a serialisation conflict against the
      server, the Cypher again valid and the store again healthy: nothing was
      written, so the remedy is to run the same statement again -- it needs no
      change -- and to spread concurrent writes across distinct nodes; or a
      value the server returned could not be mapped onto the published result
      shape
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
// The order of the checks is the CLI's ordinary one and is what the published
// exit codes describe: the roadmap selector first (exit 3), then the flags
// (exit 2), then the statement (exit 2 or 6), then the roadmap's existence
// (exit 4), and only then the socket. A refused invocation therefore connects to
// nothing.
//
// It resolves the graph directory although it never opens it, and never creates
// it. The resolution is what produces exit code 4 for a roadmap that does not
// exist (SPEC/COMMANDS.md § Client Exit Codes). Without it, `-r missing --socket
// <a server's socket>` would send a statement to a graph the caller did not name
// and report success.
//
// # The one thing it does not do
//
// It does not fall back, and there is nothing left to fall back to. Two of the
// four resolution states — no socket, and a socket file nothing is listening
// behind — are a failure here, reported with the line
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
	// Settled before the probe, and settled the same way whoever chose the path.
	// The refusal is not this subcommand's own: it is the one rule every surface
	// applies, so a path the platform cannot hold fails here exactly as it fails
	// at `graph serve`. What the published line tells the caller is why no server
	// can EVER answer there, rather than that none happens to be listening at the
	// moment (SPEC/GRAPH.md § Socket Path Length, rules 5 and 6;
	// § Server Resolution, rule 12).
	if err := refuseOverLongSocket(socket); err != nil {
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
