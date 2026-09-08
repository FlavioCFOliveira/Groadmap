// Package commands — graph family handler.
//
// No rmp graph invocation opens the GoGraph store rooted at
// ~/.roadmaps/<name>/graph/. `rmp graph serve` is the only process that opens
// one, and `rmp graph client` is the only subcommand that runs a statement,
// which it sends to that server over a Unix domain socket
// (SPEC/GRAPH.md § The Dedicated Graph Server).
//
// What remains in this file is therefore everything the family shares and
// nothing that executes: the family help, the roadmap-to-graph-directory
// resolution both subcommands perform, the creation `serve` performs on it, the
// two sources a Cypher statement may come from and the bound on its length, and
// the mapping of engine values onto the published JSON — which is applied to a
// result that crossed the protocol, in graph_socket.go, rather than to one
// produced here.
package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphjson"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
	"github.com/FlavioCFOliveira/Groadmap/internal/terminal"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// graphQueryResult is the JSON shape returned by read subcommands and
// by write subcommands whose query contains a RETURN clause.
// The field order here is the JSON KEY order: encoding/json emits struct fields
// as declared, and SPEC/DATA_FORMATS.md § Graph Query Result publishes the shape
// with columns and rows first and the plan last, which is the order a reader
// needs -- the plan describes a result, so it follows it. fieldalignment would
// have the two pointers lead, saving 16 bytes of pointer-scan prefix on a struct
// built once per invocation and discarded; the published order is worth more
// than that, and the saving is claimed on graphclient.Result instead, which has
// no published order to give up.
//
//nolint:govet // fieldalignment: field order is the published JSON key order.
type graphQueryResult struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`

	// Plan carries the ESTIMATED plan of a statement written with the EXPLAIN
	// prefix and Profile the MEASURED plan of one written with PROFILE. At most
	// one is ever non-nil and both are nil for an unprefixed statement, whose
	// output is therefore unchanged byte for byte
	// (SPEC/DATA_FORMATS.md § Graph Query Result, SPEC/GRAPH.md § Query Plans).
	Plan    *graphjson.PlanNode `json:"plan,omitempty"`
	Profile *graphjson.PlanNode `json:"profile,omitempty"`

	// Counters names what the statement changed, and is nil — so the key is
	// absent — for every statement that changed nothing, which is what a read
	// and an EXPLAIN both are. It is written LAST because the member is additive:
	// columns and rows keep their meanings and their positions, so an existing
	// parser needs no change (SPEC/DATA_FORMATS.md § Graph Query Counters,
	// rule 3). It never appears beside plan or profile, and neither surface has
	// to arrange that: an EXPLAIN applies nothing and the engine refuses a
	// PROFILE of a writing statement (rule 5).
	Counters *graphjson.Counters `json:"counters,omitempty"`
}

// graphOKResult is the JSON shape returned by write subcommands whose
// query has no RETURN clause.
//
// The field order is the JSON KEY order here too, for the same reason it is on
// graphQueryResult: `ok` keeps its meaning, its value and its position, and the
// counters follow it (SPEC/DATA_FORMATS.md § Graph Write Result;
// § Graph Query Counters, rule 3). fieldalignment would have the pointer lead,
// which would publish `counters` before `ok`; the published order is worth more
// than the pointer-scan prefix of a struct built once per invocation, exactly as
// it is on graphQueryResult above.
//
//nolint:govet // fieldalignment: field order is the published JSON key order.
type graphOKResult struct {
	OK bool `json:"ok"`

	// Counters is nil for a statement that changed nothing, which is what a
	// MERGE that matched and a DELETE that matched no row both are. Such a
	// statement therefore still publishes exactly {"ok": true} — the bytes it
	// published before this member existed.
	Counters *graphjson.Counters `json:"counters,omitempty"`
}

// maxQueryBytes is the maximum length of a Cypher query: 1 MiB, which is
// 1048576 bytes (SPEC/GRAPH.md § Maximum Query Length). A longer query is
// refused with utils.ErrValidation (exit code 6) and the message
// "query exceeds maximum length of 1048576 bytes", whichever of the two sources
// carried it.
//
// BYTES, not characters, and the difference from the comment body's 4096-CHARACTER
// cap is deliberate. A comment body is stored text whose length the author reads
// back, so it is counted in the units it was written in. A query is an
// instruction that is executed and discarded, never stored, and the harm this
// maximum exists against is memory, which is counted in bytes. A 1 MiB query
// written in multi-byte characters therefore carries fewer than 1048576
// characters, and that is the intended reading.
//
// The size is generous on purpose: a graph bootstrap script carrying hundreds of
// MERGE statements stays far below it, while the unbounded read this replaces
// needed 256 MiB of input to reach 867 MB of resident memory and 15.9 s of wall
// time. A maximum someone reaches doing ordinary work is a maximum that gets
// widened later, and widening a published limit is worse than choosing it well
// once; 64 KiB was considered and declined for exactly that reason.
const maxQueryBytes = 1 << 20

// queryTooLongError builds the refusal for a query that exceeds
// maxQueryBytes. It wraps utils.ErrValidation, so the refusal carries exit code
// 6 — NOT the exit code 2 that a missing query carries. The two are different
// classes and SPEC/GRAPH.md § Standard Input That Supplies No Query forbids
// collapsing them: supplying no query at all is a missing required parameter,
// while supplying a query the command refuses to accept is a validation failure.
func queryTooLongError() error {
	return fmt.Errorf("%w: query exceeds maximum length of %d bytes", utils.ErrValidation, maxQueryBytes)
}

// printGraphHelp prints the family-level help for rmp graph.
//
// SPEC/HELP.md § Graph family help specifics, item 5, requires the family help to
// list every subcommand with a verb-first description AND to make the distinction
// between them explicit in one sentence rather than leaving it to be inferred
// from the summaries. The sentence below is that one.
//
// The same item forbids this help to name `execute`, and the prohibition is not
// pedantry about a withdrawn feature: naming it, even to say that it no longer
// resolves, puts in front of an agent a token the dispatcher answers with exit
// code 127. The five names withdrawn before it are out for the same reason, and
// the sentence that used to list them went with them
// (SPEC/GRAPH.md acceptance criterion 74).
func printGraphHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp graph <subcommand> -r <roadmap> [-q <cypher>]

Operate the knowledge graph of a roadmap using Cypher. The graph is stored
under ~/.roadmaps/<name>/graph/. The two subcommands differ in one thing: serve
opens that graph and makes it available over a Unix domain socket until it is
stopped, and client sends a statement to a running server and prints what comes
back.

Using the graph begins by starting a server. serve is the only process that
opens the store, and starting one against a roadmap that has never had a graph
is what creates it; client is the only way to run a statement, and it needs a
running server for every one. With nothing listening, client fails and opens
nothing.

client runs any statement the engine accepts -- a read, a write, a deletion, a
schema change, a schema listing -- and rmp does not examine the statement or
refuse it for what it does. The statement comes from --query, or from standard
input when that flag is absent; supplying neither is an error.

--socket names the socket a subcommand uses: the path serve binds, and the path
client connects to. It defaults to the same path derived from the roadmap on
both, so a server started without the flag is reached without it.

Commands:
  serve     Serve the roadmap knowledge graph over a Unix domain socket
  client    Send one Cypher statement to a running server and print its result

Options:
  -r, --roadmap <name>    REQUIRED. Target roadmap
  -q, --query <cypher>    client. Cypher statement; read from stdin when this
                          flag is absent
      --socket <path>     Both. Socket bound by serve and connected to by
                          client; default ~/.roadmaps/<name>/graph.sock
  -h, --help              Show this help message

Output (stdout JSON):
  Statement that produces result columns:
    {"columns": [...], "rows": [[...], ...], "plan": <plan node, EXPLAIN only>, "profile": <plan node, PROFILE only>, "counters": <write counters, only when the statement changed the graph>}
  Statement that produces none:
    {"ok": true, "counters": <write counters, only when the statement changed the graph>}
  Server startup:
    {"socket": "<path>"}

  A statement written with the EXPLAIN or PROFILE prefix carries its plan and
  always produces the columns shape, whether or not it declares a column.
  EXPLAIN runs nothing and reports the plan the engine would run; PROFILE
  runs the statement and reports what the run measured.

  A statement that changed the graph adds a counters block naming what it
  changed: nodesCreated, nodesDeleted, relationshipsCreated,
  relationshipsDeleted, propertiesWritten, labelsAdded, labelsRemoved,
  indexesAdded, indexesRemoved, constraintsAdded, constraintsRemoved. A counter
  that is zero is left out, and a statement that changed nothing -- every read,
  a MERGE that matched, a DELETE that matched no row -- carries no counters key
  at all.

Exit codes:
  0   Success
  1   For serve, the graph store could not be created, opened or recovered, its
      lock could not be taken, or the socket could not be bound; for client, no
      server is listening, or a Cypher parse/execution error, or a valid
      statement cancelled for running past the 5s statement time budget, which
      writes nothing -- narrow the statement, or split it; for either, a server
      that could not be reached through the socket
  2   No query supplied, --socket with an empty value, or a positional argument
      was given
  3   No roadmap selected
  4   Roadmap not found
  6   Query longer than the maximum length of 1048576 bytes
  127 Unknown subcommand

Examples:
  rmp graph serve -r myproject
  rmp graph client -r myproject --query "MATCH (n:Spec) RETURN n.key"
  rmp graph client -r myproject --query "CREATE (n:Spec {key:'auth'})"
  echo "MATCH (n) RETURN count(n)" | rmp graph client -r myproject
`)
}

// createGraphDir brings the graph store directory into being at 0700.
//
// It has exactly ONE caller and is meant to: `rmp graph serve` is the only thing
// that creates a roadmap's graph, because it is the only thing that opens one
// (SPEC/GRAPH.md § Server Startup, step 1). `rmp graph client` calls
// resolveGraphDir and stops at the resolution — it runs its statement in the
// server's store, so a directory created here would be an empty second store
// beside a graph that is already open.
//
// The mode is set twice on purpose. MkdirAll applies the process umask to the
// permission bits it is given, so a umask of 022 would leave 0755 behind; the
// explicit Chmod is what makes the 0700 CLAUDE.md § 10 fixes for the
// ~/.roadmaps tree a property of the directory rather than of the environment
// that created it.
func createGraphDir(graphDir string) error {
	if mkErr := os.MkdirAll(graphDir, 0700); mkErr != nil {
		return fmt.Errorf("%w: creating graph directory: %v", utils.ErrGraphStore, mkErr)
	}
	if chErr := os.Chmod(graphDir, 0700); chErr != nil { // #nosec G302 -- 0700 on a DIRECTORY is mandated by SPEC (CLAUDE.md §10: 0700 for the ~/.roadmaps tree); gosec G302 false-positives on directory permissions
		return fmt.Errorf("%w: setting graph directory permissions: %v", utils.ErrGraphStore, chErr)
	}
	return nil
}

// resolveGraphDir validates roadmapName, confirms the roadmap exists, and
// returns the path of its graph store directory WITHOUT creating anything.
//
// It is what BOTH graph subcommands need and all that `graph client` needs: the
// resolution is what produces exit code 4 for a roadmap that does not exist, on
// a subcommand that then opens nothing. `graph serve` follows it with
// createGraphDir; nothing else in the CLI does.
func resolveGraphDir(roadmapName string) (string, error) {
	roadmapDir, valErr := utils.GetRoadmapDir(roadmapName)
	if valErr != nil {
		// A classification is stated once, by whoever owns the failure.
		//
		// GetRoadmapDir refuses a name through utils.ValidateRoadmapName, and
		// every branch of that function already carries utils.ErrValidation
		// together with the sentinel naming WHICH rule the name broke
		// (reserved, hyphen-leading, too long, bad characters). Restating the
		// classification here therefore added nothing to the chain and cost the
		// reader a second prefix: `rmp graph execute -r CON`, on the statement
		// subcommand since withdrawn, rendered
		// "validation error: validation error: ...", and the roadmap-name
		// messages SPEC/COMMANDS.md § Roadmap Name Validation publishes WITHOUT
		// a sentinel gained one on the graph paths alone. Every other command
		// family returns this error untouched, which is why only graph diverged
		// (task #325).
		//
		// Returning it unchanged keeps both %w-carried sentinels reachable —
		// the classification for the exit code, the specific rule for a caller
		// that must discriminate — which is the property task #290 established
		// here and which this must not undo.
		if errors.Is(valErr, utils.ErrValidation) {
			return "", valErr
		}
		// The other way GetRoadmapDir fails is an unresolvable home directory,
		// which carries no classification at all. That one is still classified
		// here, so its exit code is unchanged.
		return "", fmt.Errorf("%w: %w", utils.ErrValidation, valErr)
	}

	dbPath := filepath.Join(roadmapDir, utils.DBFileName)
	if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
		return "", fmt.Errorf("%w: roadmap %q not found", utils.ErrNotFound, roadmapName)
	}

	return filepath.Join(roadmapDir, "graph"), nil
}

// isFlagLike reports whether tok is a command-line flag rather than a query
// value. A flag-like token begins with "--" (a long flag) or with a single
// "-" immediately followed by an ASCII letter (a short flag such as "-q").
//
// A token that begins with "-" immediately followed by an ASCII digit or a
// decimal point — a negative numeric literal such as "-1" or "-0.5" — is NOT
// flag-like; it is a legitimate query value passed through to the engine, which
// validates it on its own Cypher-validity merits (SPEC/GRAPH.md precedence
// rule 4). A bare "-" is neither a flag nor a numeric literal and is likewise
// treated as a value. The check is byte-wise to stay allocation-free on the
// argument-parsing path.
func isFlagLike(tok string) bool {
	if strings.HasPrefix(tok, "--") {
		return true
	}
	// A single leading '-' is a short flag only when an ASCII letter follows it.
	if len(tok) >= 2 && tok[0] == '-' {
		c := tok[1]
		return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	return false
}

// readQuery extracts the Cypher query from args. It consumes --query / -q from
// args and returns the trimmed query string, or reads the query from standard
// input when the flag is absent. An empty or whitespace-only result is returned
// as ErrRequired.
//
// Both sources are subject to maxQueryBytes, and the check happens here rather
// than deeper in: an over-long query is never handed to the engine and never
// reaches an opened store (SPEC/GRAPH.md § Maximum Query Length rule 3). It is
// the ONLY condition on which Groadmap refuses a statement's content
// (SPEC/GRAPH.md § Error Handling and Exit Codes, rule 1).
func readQuery(args []string) (string, error) {
	var queryVal string
	var queryFound bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--query", "-q":
			// SPEC/GRAPH.md precedence rule 4: when --query is present but its
			// value is missing — there is no following token, or the next token
			// is itself flag-like (so no value was supplied) — the command fails
			// with exit 2 rather than silently falling back to stdin (finding
			// #26) or swallowing the following flag as the value (finding #27).
			// A flag-like token begins with "--" or with "-"+letter; a leading
			// dash alone is no longer treated as absent, because "-1"/"-0.5" are
			// negative numeric literals — legitimate query values handed to the
			// engine for validation (finding #81). Empty or whitespace-only
			// values are caught by the TrimSpace check below.
			if i+1 >= len(args) || isFlagLike(args[i+1]) {
				return "", fmt.Errorf("%w: no query supplied", utils.ErrRequired)
			}
			queryVal = args[i+1]
			queryFound = true
			i++
		default:
			// Graph queries are supplied ONLY via --query or stdin (SPEC/GRAPH.md
			// § Cypher Input Source and Precedence); there are no other graph
			// flags and no positional query. Reject anything else as malformed
			// input (exit 2) instead of silently ignoring it, matching the
			// cross-cutting unknown-flag rule in SPEC/ARCHITECTURE.md (finding #28).
			// Only genuine flags ("--…" or "-"+letter) are reported as an
			// unknown flag; a bare token such as "-1" is a stray positional, not
			// a flag, so it is reported as an unexpected argument (finding #81).
			// Both map to ErrInvalidInput (exit 2).
			if isFlagLike(args[i]) {
				return "", fmt.Errorf("%w: unknown flag: %s", utils.ErrInvalidInput, args[i])
			}
			return "", fmt.Errorf("%w: unexpected argument %q (graph queries use --query or stdin)", utils.ErrInvalidInput, args[i])
		}
	}

	if queryFound {
		// The maximum applies to BOTH sources (SPEC/GRAPH.md § Maximum Query
		// Length rule 2), so the same text never passes at one door and fails at
		// the other. A cap enforced only on the standard-input path would refuse
		// in one place what it accepts in the other, which is not a maximum. The
		// count is taken over the bytes AS SUPPLIED, which is why it precedes the
		// trim of precedence rule 5.
		if len(queryVal) > maxQueryBytes {
			return "", queryTooLongError()
		}
		q := strings.TrimSpace(queryVal)
		if q == "" {
			return "", fmt.Errorf("%w: no query supplied", utils.ErrRequired)
		}
		return q, nil
	}

	// --query absent: the query comes from standard input.
	return readQueryStdin(os.Stdin)
}

// readQueryStdin obtains the Cypher query from src, which is the process's
// standard input, when --query is absent (SPEC/GRAPH.md § Cypher Input Source
// and Precedence, rules 2 and 3).
//
// A TERMINAL IS REFUSED WITHOUT BEING READ AT ALL. That ordering is the whole
// point of this function and is part of the contract rather than an accident of
// how fast the check runs: an invocation that forgot the flag, with a terminal on
// standard input, must fail at once instead of waiting for a query nobody is
// going to type. The failure it closes was observed, not imagined — such an
// invocation printed nothing and never returned, and was killed after roughly
// forty minutes.
//
// The refusal is ErrRequired (exit code 2), the same class and the same message
// an empty or whitespace-only stream reaches. It is NOT the ErrValidation (exit
// code 6) that an over-long query carries: supplying no query at all is a missing
// required parameter, while supplying one the command refuses to accept is a
// validation failure. SPEC/GRAPH.md § Standard Input That Supplies No Query
// forbids collapsing the two.
func readQueryStdin(src *os.File) (string, error) {
	if terminal.IsTerminal(src) {
		return "", fmt.Errorf("%w: no query supplied", utils.ErrRequired)
	}
	return readQueryStream(src)
}

// readQueryStream reads a Cypher query from an open, non-terminal stream under a
// HARD BOUND and applies the rules its length and content are subject to.
//
// # Why this is not io.ReadAll
//
// The previous implementation drained the stream to EOF, so a hostile or runaway
// writer decided how much this process buffered: 256 MiB offered to the
// statement subcommand of the day (`rmp graph execute`, since withdrawn; the
// read this guards is now `rmp graph client`'s) produced 867 MB of peak resident
// memory and 15.9 s of wall
// time, all of it spent on a "query" that was never going to be accepted (the
// time went into the engine's parse attempt over 256 MB of input; a literal-
// masking pass that no longer exists took the rest). That is CWE-400 / CWE-789 — an allocation
// sized by whoever is writing — against a command whose largest acceptable input
// is 1 MiB.
//
// The read therefore stops at maxQueryBytes+1 bytes. That one extra byte already
// settles the verdict, because no later byte can bring the count back down: if it
// arrives, the query is over the maximum and is refused; if the stream ends
// first, everything it carried is in hand. Peak memory is the buffer, and it does
// not grow with the amount the writer sends. A producer still writing when the
// command exits sees the usual broken pipe; the bound is a promise about what
// this process consumes and retains, not about what the producer manages to push
// into the operating system's pipe buffer.
//
// The buffer is allocated once at its full size rather than grown by io.ReadAll,
// which reaches roughly 1.3x the data through its doubling and copies every byte
// again at each growth. One allocation of a known size keeps the peak exactly
// what the maximum promises, and 1 MiB is nothing beside the graph store this
// command is about to open.
//
// One difference from the comment body's bounded read is deliberate and must not
// be "aligned" away (SPEC/GRAPH.md § Bounded Standard-Input Read). That read
// looks past its cap for trailing whitespace, so its verdict is exactly the
// verdict a read-to-EOF implementation would reach after trimming. This one does
// not: the maximum counts the bytes standard input supplies, so 1048576 bytes of
// Cypher followed by trailing whitespace is refused even though trimming that
// whitespace would have brought it to the maximum. A query's length is not a
// value anybody reads back, and a producer that pads a megabyte of Cypher with
// more whitespace is not a case worth reading further for.
func readQueryStream(src io.Reader) (string, error) {
	buf := make([]byte, maxQueryBytes+1)
	n, err := io.ReadFull(src, buf)
	// io.ReadFull reports the ordinary outcome — a stream shorter than the buffer
	// — as io.EOF (nothing arrived) or io.ErrUnexpectedEOF (something did), so
	// neither is a failure here. Anything else is a genuine I/O failure of the
	// process rather than bad user input, and maps to exit code 1.
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("%w: reading query from stdin: %v", utils.ErrIO, err)
	}

	if n > maxQueryBytes {
		return "", queryTooLongError()
	}

	// The trim happens AFTER the length check (SPEC/GRAPH.md § Cypher Input
	// Source and Precedence, rule 5). A stream that carries only whitespace
	// therefore trims to nothing and is refused as a missing parameter, exactly
	// as an empty stream is.
	q := strings.TrimSpace(string(buf[:n]))
	if q == "" {
		return "", fmt.Errorf("%w: no query supplied", utils.ErrRequired)
	}
	return q, nil
}

// serializeValue converts a single expr.Value into a JSON-compatible
// Go value for inclusion in a graphQueryResult row.
//
// The mapping itself is not here. It has ONE realisation, in
// internal/graphjson, and every surface that publishes a graph value calls it
// (SPEC/DATA_FORMATS.md § One Realisation of the Mapping). What stays in this
// package is the part that is only the CLI's: the {columns, rows} document the
// mapped values are placed in, which serializeGraphResult builds, and the Path
// rendering below, which no other surface publishes.
func serializeValue(v expr.Value) any {
	return graphjson.Value(v, serializePath)
}

// serializePath renders a path as SPEC/DATA_FORMATS.md § Graph element mapping
// requires: an object carrying the nodes it visits and the relationships it
// traverses, each rendered as the element object that same section fixes.
//
// It is the CLI's own row rather than a shared one because the CLI is the only
// surface that publishes a path. The graph data endpoint decomposes a path into
// the elements it contains and publishes no path object at all
// (SPEC/DATA_FORMATS.md § Graph View Data, rule 3), so the Path rendering
// already has one realisation by having one producer; moving it into the shared
// package would buy no identity and would give the endpoint a row it can never
// reach.
//
// It is handed to internal/graphjson as its graphjson.Unmapped, so a path nested
// inside a returned list, map or property bag is rendered here exactly as one
// returned in its own column is. A value of any OTHER kind the shared mapping
// carries no row for is not a path, and falls back to the engine's own string
// form — which is what this command published for such a value before the
// mapping was shared.
func serializePath(v expr.Value) any {
	switch v.Kind() {
	case expr.KindPath:
		pv, _ := v.(expr.PathValue)
		nodes := make([]any, len(pv.Nodes))
		for i := range pv.Nodes {
			nodes[i] = graphjson.Node(pv.Nodes[i], serializePath)
		}
		rels := make([]any, len(pv.Relationships))
		for i := range pv.Relationships {
			rels[i] = graphjson.Relationship(pv.Relationships[i], serializePath)
		}
		return map[string]any{
			"nodes":         nodes,
			"relationships": rels,
		}
	default:
		return v.String()
	}
}

// graphStatementError classifies a failure raised while the statement was
// executing — whether it surfaced from the engine call, from the walk over the
// result, or from the commit — and words it truthfully.
//
// Every case carries utils.ErrGraphEngine and exit code 1. Exhausting the
// statement time budget is an engine failure exactly as a statement the engine
// refuses is -- in both the statement reached the engine and did not complete
// there, which is what that sentinel names, and what tells a reader to act on
// the STATEMENT rather than on the store or the server. The graph feature
// introduces no new exit CODE, and may not (SPEC/GRAPH.md § Constraints, rule 5;
// § Schema Failure Classes, rule 6). Only the message differs.
//
// **A field the engine refuses as too long for the write-ahead log is the third
// message, and it is the only one of the three that ends in the engine's own
// text.** The other two are wholly rmp's, because rmp knows the whole of what
// they report; this one cannot be, because the caller must be told WHICH of the
// statement's fields is at fault and by how much, and only the engine knows that.
// So rmp writes the class, the fact that nothing was written and the action to
// take, and then hands over: the engine's diagnostic follows unchanged,
// untrimmed and LAST, which is where every other line carrying an engine or
// operating-system diagnostic puts it and what lets a test assert the whole of
// rmp's half and none of the engine's. Both halves therefore say the field is
// too long, and that echo is deliberate — trimming it would mean parsing it, and
// a match on the engine's wording fails silently at the next version bump
// (SPEC/GRAPH.md § Field Length Limits, rules 3 and 4).
//
// The class exists because without it the two conditions were separated only by
// that diagnostic tail, which SPEC/GRAPH.md § Error Handling and Exit Codes,
// rule 2, deliberately declines to specify and which a caller therefore cannot
// lawfully match: a caller reading "graph query failed: " had to parse English to
// learn whether to correct the statement's syntax or to shorten one of its
// values. That is the same defect, and the same remedy, as the statement time
// budget above.
//
// The RECOGNITION is graphstore's, not this function's, and it is a sentinel
// rather than a string match. Two neighbouring refusals — an over-long node key
// and an over-large assembled log frame — are genuine length refusals that this
// class MUST NOT absorb, and matching the sentinel and nothing else is what keeps
// them on the ordinary line (SPEC/GRAPH.md § Field Length Limits, rule 10).
//
// **All three arrival points are classified, and the walk is the one that
// matters.** The engine streams a disconnected pattern's tuples as the result is
// iterated, so a Cartesian product's cost is paid during result.Next() and the
// engine call returns a nil error long before the deadline fires. Measured
// against a 44,906-node graph: the run returned no error, the cancellation
// arrived at result.Err() as context.DeadlineExceeded, and the commit path is
// classified with them because a cut that lands there is the same failure
// (SPEC/GRAPH.md § Statement Time Budget, rule 5).
//
// **Unlike the web endpoint, this classifier consults no parent context, and the
// asymmetry is deliberate rather than an omission.** internal/web derives its
// deadline from the REQUEST's context, so context.DeadlineExceeded there may be
// the budget or a parent deadline the client's disconnect brought with it, and
// that endpoint disambiguates by asking whether its parent is still live. A CLI
// invocation's parent is context.Background(), which never carries a client, a
// disconnect, or a deadline of its own, so the only deadline that can fire here
// is the one derived above and errors.Is is sufficient
// (SPEC/GRAPH.md § Statement Time Budget, rule 1).
//
// The budget is passed in rather than read again, so the value that produced the
// deadline is the value the message reports; a test that moves the budget
// therefore gets a truthful message, and in production it renders "5s" and
// matches the line SPEC/COMMANDS.md § Graph Management publishes character for
// character.
func graphStatementError(budget time.Duration, stage string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: graph query exceeded the %s statement time budget; nothing was "+
			"written. Narrow the statement — add a label, an indexed property filter, or a "+
			"LIMIT — or split it into smaller statements.", utils.ErrGraphEngine, budget)
	}
	// THIS BRANCH CANNOT FIRE AT THE PINNED ENGINE, AND IT IS KEPT DELIBERATELY.
	// It is not dead code to tidy away. A statement now reaches a graph only
	// through a server, and the server classifies this refusal as its own fault
	// rather than the caller's and replaces the message, so the condition arrives
	// at the caller through the ordinary parse/execution line below instead. The
	// sentinel, the exit code and the fact that nothing is written are the same
	// either way; only the message differs. SPEC/GRAPH.md § Field Length Limits,
	// rule 13, is canonical for the limitation and for the engine-side change
	// that ends it -- after which this branch produces the line again with no
	// change here. Deleting it would leave SPEC/COMMANDS.md § Client Error Cases
	// publishing a line that nothing in the product can produce.
	if graphstore.CommitRefusedFieldTooLong(err) {
		return fmt.Errorf("%w: graph field too long; nothing was written. Shorten the field the "+
			"engine names: %v", utils.ErrGraphEngine, err)
	}
	return fmt.Errorf("%w: %s: %v", utils.ErrGraphEngine, stage, err)
}
