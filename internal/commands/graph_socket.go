// Package commands — the socket half the three graph subcommands share.
//
// `graph execute`, `graph client` and `graph serve` all name a socket, and the
// first two both send statements to whatever answers on it. This file holds what
// is common to them: the `--socket` flag, the derivation of the path in force,
// and the lines SPEC/COMMANDS.md § Graph Server Socket Error Lines publishes for
// a failure that belongs to the socket rather than to the roadmap, the statement,
// or the store.
//
// It holds the lines ONCE. Each is published as a single sentence, and three of
// the error tables in SPEC/COMMANDS.md refer to this section rather than each
// carrying a copy; the code mirrors that arrangement, because two copies of a
// published line are two things to keep in step with one specification.
//
// The RULE those lines report is not here and is not this package's: which of
// the four states a socket is in is decided by internal/graphclient, the one
// realisation SPEC/ARCHITECTURE.md module 9 fixes. What is here is what a
// COMMAND-LINE surface does with each state — a message and an exit code — which
// is exactly the part the web interface answers differently.
package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// backstopLine is the caller-side deadline as it appears in the published
// unanswered line.
//
// It is a LITERAL and not an interpolation, which SPEC/COMMANDS.md § Graph Server
// Socket Error Lines requires in as many words: "7.5s is the backstop itself
// rendered as a duration, a fixed value and not one the binary interpolates".
// The published line is therefore the same characters whatever a test has done to
// the budget, which is what makes it comparable in full.
//
// TestGraphSocket_BackstopLineMatchesTheDeclaredBudget is what keeps the literal
// honest: it renders the default declaration and fails if the two part company.
const backstopLine = "7.5s"

// extractSocketFlag pulls --socket and its value out of args and returns
// everything it did not consume.
//
// It refuses NOTHING else, and that is the difference between it and
// readSocketFlag. `graph serve` takes no other flag and can refuse whatever is
// left where it stands; `graph execute` and `graph client` take --query as well
// and read a statement out of what remains, so the socket flag has to be removed
// from the arguments without an opinion about the rest.
//
// A flag supplied with an empty — or whitespace-only — value is a MISSING
// parameter and not a validation failure: it names no socket at all, which is the
// same condition as writing the flag with nothing after it. The value itself is
// never trimmed, because a path is not text a user reads back and a path may
// legitimately end in a space.
func extractSocketFlag(args []string) (string, []string, error) {
	var value string
	var found bool
	rest := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		if args[i] != socketFlagLong {
			rest = append(rest, args[i])
			continue
		}
		// The value is missing when there is no following token, or when the
		// next token is itself a flag: the same rule --query is read under, so
		// the two flags cannot disagree about what "supplied with no value"
		// means.
		if i+1 >= len(args) || isFlagLike(args[i+1]) {
			return "", nil, fmt.Errorf("%w: %s", utils.ErrRequired, socketFlagLong)
		}
		value = args[i+1]
		found = true
		i++
	}

	if found && strings.TrimSpace(value) == "" {
		return "", nil, fmt.Errorf("%w: %s", utils.ErrRequired, socketFlagLong)
	}
	return value, rest, nil
}

// graphSocketInForce returns the absolute path of the socket an invocation
// resolves: the value of --socket when one was given, and the path derived from
// the roadmap otherwise.
//
// All three subcommands derive it identically and through this one call, so a
// caller and a server that name the same roadmap name the same socket without
// either being told a path (SPEC/GRAPH.md § Socket Path and Permissions, rule 1;
// § Server Resolution, rule 10).
//
// It is made absolute for two reasons that pull the same way: `graph serve`
// PUBLISHES the value in its startup object, where a relative path would be
// meaningless to a reader who does not know the working directory; and every
// error line below names the socket, where the same is true.
func graphSocketInForce(roadmapName, socketFlag string) (string, error) {
	path := socketFlag
	if path == "" {
		derived, err := graphclient.SocketPath(roadmapName)
		if err != nil {
			return "", err
		}
		path = derived
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%w: cannot resolve the socket path %s: %v", utils.ErrDatabase, path, err)
	}
	return abs, nil
}

// resolveGraphServer probes the socket in force and reports whether a server is
// answering there.
//
// The probe is internal/graphclient's, unchanged and unwrapped: this function
// exists to turn the ONE failing state into the line a command-line surface
// prints for it, and to leave the other three to the caller, which reports each
// differently — `graph execute` falls back on the two negatives and `graph client`
// fails on them.
func resolveGraphServer(socket string) (graphclient.State, error) {
	state, probeErr := graphclient.Resolve(context.Background(), socket)
	if state == graphclient.StateUnreachable {
		return state, graphSocketUnreachable(socket, probeErr)
	}
	return state, nil
}

// graphSocketUnreachable is the published line for a socket that answered and
// yielded no server.
//
// It is a FAILURE and never a fall back. A socket that answers may belong to a
// server holding the store's lock for its process lifetime, so a caller that
// opened the store on this observation would wait its whole wait budget and then
// fail — which is the outcome resolution exists to prevent
// (SPEC/GRAPH.md § Server Resolution, rule 2).
func graphSocketUnreachable(socket string, cause error) error {
	return fmt.Errorf("%w: graph server unreachable at %s: %v", utils.ErrDatabase, socket, cause)
}

// graphNoServerListening is the published line for `graph client` against a
// roadmap nothing is serving.
//
// It covers the socket that does not exist and the socket file a killed server
// left behind alike, because the two are one condition for that subcommand: there
// is nothing to send the statement to. `graph execute` never reaches this line —
// for it the same two states are the direct path.
func graphNoServerListening(socket string) error {
	return fmt.Errorf("%w: no graph server is listening on %s", utils.ErrDatabase, socket)
}

// graphConnectionLost is the published line for a connection that died after the
// statement had been sent.
//
// It deliberately does not claim that nothing was written. A commit is durable
// before it is acknowledged, so a connection lost between the two leaves the
// outcome genuinely unknown, and a line that said "nothing was written" would be
// false in exactly the case a caller most needs the truth
// (SPEC/GRAPH.md § Server Resolution, rule 4).
func graphConnectionLost(socket string) error {
	return fmt.Errorf("%w: the connection to the graph server at %s was lost; "+
		"the statement's outcome is unknown", utils.ErrDatabase, socket)
}

// graphWriteConflict is the published line for a statement that lost a
// serialisation conflict on every attempt of the retry policy.
//
// It is `rmp`'s own text from end to end, and that is the whole point of it. A
// conflict used to arrive on the parse/execution line, so contention and an
// invalid statement printed the same words and the only thing separating them
// was the engine's diagnostic tail — which SPEC/COMMANDS.md deliberately declines
// to specify and a caller therefore cannot lawfully match. The two conditions
// demand opposite courses: run it again, or correct it. Treating contention as a
// bad statement stops work that was right; treating a bad statement as contention
// re-runs a write that may not be idempotent
// (SPEC/GRAPH.md § Concurrency Inside the Server, rule 9).
//
// It DOES claim that nothing was written, unlike graphConnectionLost, and the
// claim is true rather than optimistic: the conflict is detected before anything
// is applied, so the losing transaction committed nothing (rule 5).
//
// The remedy it names is the measured one. Spreading writes across distinct
// nodes REMOVES the failure rather than moving the threshold at which it starts:
// holding sixteen writers and varying only the number of nodes they touch, the
// policy was exhausted on 0.33% of statements against one node, 0.03% against
// four, and none at all against eight or more (rule 8).
// The budget it names is read from the policy rather than written out. It
// renders "2.5s", the figure SPEC/COMMANDS.md publishes, and it is a FIXED value
// in the sense that section requires — nothing about an invocation can move it,
// because backoff.Total() sums constants — so the line is the same characters on
// every path and is comparable in full. Reading it keeps this line and
// internal/web's, which renders the same quantity the same way, from claiming a
// budget the policy does not spend.
func graphWriteConflict() error {
	return fmt.Errorf("%w: graph write conflict: another writer committed first on every attempt "+
		"within the %s retry budget; nothing was written. The statement is valid — run it again, "+
		"and spread concurrent writes across distinct nodes.", utils.ErrDatabase, backoff.Total())
}

// graphServerSilent is the published line for a server that stayed connected and
// did not answer inside the caller's backstop deadline.
//
// The connection is intact here and the server is alive; it is simply not
// answering, which is what a statement the budget cut mid-write looks like from
// outside, because the engine's undo replay runs past the deadline by a factor
// nothing bounds (SPEC/GRAPH.md § Statement Time Budget). The outcome is unknown
// for the same reason a lost connection's is, and the caller does not fall back
// to the store.
func graphServerSilent(socket string) error {
	return fmt.Errorf("%w: the graph server at %s did not answer within %s; "+
		"the statement's outcome is unknown", utils.ErrDatabase, socket, backstopLine)
}

// runOnGraphServer sends one statement to the server at socket and returns the
// value to print for it.
//
// It is the served half of `graph execute` and the whole of `graph client`, and
// the two share it rather than each calling the client for themselves, so that
// the output shape, the notifications and the classification of every failure are
// decided once. SPEC/DATA_FORMATS.md § Graph Client Result requires the bytes the
// two write to be identical; sharing the call is what makes that true of the
// bytes rather than merely asserted about them.
//
// The result is serialised through serializeValue — the SAME function the direct
// path uses — because the client hands back the engine's own value model rather
// than JSON. The published shape is therefore produced by one mapping on both
// paths, and the surface a statement was executed through is not observable in
// it.
func runOnGraphServer(socket, query string) (any, error) {
	result, err := graphclient.Send(context.Background(), socket, query)
	if err != nil {
		return nil, graphServerFailure(socket, err)
	}

	// Notifications are advisory and reach stderr exactly as the engine's own do
	// on the direct path: one plain-text line each, changing neither the stdout
	// output nor the exit code (SPEC/GRAPH.md § Query Notifications as
	// Diagnostics).
	for _, n := range result.Notifications {
		fmt.Fprintf(os.Stderr, "%s %s: %s\n", n.Severity, n.Code, n.Description)
	}

	// The discriminator is the columns and not the RETURN clause, which is what
	// lets a schema introspection produce the listing while a CREATE INDEX
	// produces {"ok": true} (SPEC/DATA_FORMATS.md § Graph Write Result).
	if len(result.Columns) == 0 {
		return graphOKResult{OK: true}, nil
	}

	out := graphQueryResult{Columns: result.Columns, Rows: make([][]any, 0, len(result.Rows))}
	for _, row := range result.Rows {
		cells := make([]any, len(row))
		for i, v := range row {
			cells[i] = serializeValue(v)
		}
		out.Rows = append(out.Rows, cells)
	}
	return out, nil
}

// graphServerFailure words a failure the shared Bolt client classified, in the
// terms a command-line surface publishes.
//
// The two failures that are the STATEMENT's are worded by the same helper the
// direct path uses, deliberately: a statement that failed to parse and a
// statement the budget cut read identically whether they ran in this process or
// in a server, and SPEC/COMMANDS.md publishes one line for each across both
// subcommands. The three that are the CONNECTION's have lines of their own, which
// this file holds.
//
// The exhausted serialisation retry is the fourth line this file holds, and it is
// the one that is neither the connection's nor shared with the direct path: it
// reports contention INSIDE a server, which one invocation running one
// transaction cannot produce (SPEC/COMMANDS.md § Execute Error Cases).
func graphServerFailure(socket string, err error) error {
	var sendErr *graphclient.SendError
	if !errors.As(err, &sendErr) {
		// The client returns nothing else, so this is defence rather than a
		// live path; reporting it as a store failure names the class correctly
		// without inventing a line for a case that does not arise.
		return fmt.Errorf("%w: graph store unavailable: %v", utils.ErrDatabase, err)
	}

	budget := graphStatementBudget()
	switch sendErr.Kind {
	case graphclient.FailureUnreachable:
		return graphSocketUnreachable(socket, sendErr.Cause)
	case graphclient.FailureLost:
		return graphConnectionLost(socket)
	case graphclient.FailureUnanswered:
		return graphServerSilent(socket)
	case graphclient.FailureConflict:
		// Contention, not a bad statement. It is worded here rather than by the
		// direct path's helper because the direct path cannot reach it: one
		// invocation runs one transaction, so there is nothing for it to collide
		// with (SPEC/IMPLEMENTATION.md § Graph Store Concurrency).
		return graphWriteConflict()
	case graphclient.FailureBudget:
		// The published budget line, produced by the one function that owns it.
		// context.DeadlineExceeded is the value that selects it, and it is the
		// truthful one: the server's typed failure reports a statement its own
		// deadline cut.
		return graphStatementError(budget, "graph query failed", context.DeadlineExceeded)
	default:
		// A statement the engine refused and a value the mapping could not
		// represent land on the SAME published line, and deliberately: both are
		// failures of the statement, both carry utils.ErrDatabase and exit code 1,
		// and the diagnostic is what tells them apart. The mapping's own
		// diagnostic names its class in its own words
		// (internal/graphclient.errUnrepresentable), so a second prefix here would
		// say twice what the line already says once.
		return fmt.Errorf("%w: graph query failed: %s", utils.ErrDatabase, sendErr.Diagnostic)
	}
}

// graphStatementBudget reads the statement time budget the served path reports.
//
// It is the SAME declaration the direct path reads and the same one the server
// enforces (internal/graphserve builds its options from it), so a test that moves
// the budget moves the message on every path together and the three can never
// report different figures for one quantity.
func graphStatementBudget() time.Duration { return graphlock.StatementBudget }
