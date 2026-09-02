// Package commands — graph family handler.
//
// Each rmp graph invocation is a short-lived process that opens the
// GoGraph store rooted at ~/.roadmaps/<name>/graph/, runs exactly one
// Cypher query, commits any write, and exits. The store is not held
// open across invocations and is independent of the SQLite database.
package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
	"github.com/FlavioCFOliveira/Groadmap/internal/terminal"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// graphQueryResult is the JSON shape returned by read subcommands and
// by write subcommands whose query contains a RETURN clause.
type graphQueryResult struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

// graphOKResult is the JSON shape returned by write subcommands whose
// query has no RETURN clause.
type graphOKResult struct {
	OK bool `json:"ok"`
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
func printGraphHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp graph <subcommand> -r <roadmap> [-q <cypher>]

Operate the knowledge graph of a roadmap using Cypher. The graph is stored
under ~/.roadmaps/<name>/graph/ and is created on first use. The three
subcommands differ in one thing: execute runs a statement against the roadmap
graph, serve makes that graph available over a socket until it is stopped, and
client sends a statement to a running server. create, query, update, delete and
search are not subcommand names of rmp graph and do not resolve.

execute and client each run any statement the engine accepts -- a read, a write,
a deletion, a schema change, a schema listing -- and rmp does not examine the
statement or refuse it for what it does. The statement comes from --query, or
from standard input when that flag is absent; supplying neither is an error.

A running server is used automatically. When a server is serving the selected
roadmap, execute sends its statement to that server instead of opening the
store, with no flag and no configuration, and the result and the exit code are
the same either way. client always requires one and fails when none answers;
execute opens the store instead. --socket names the socket an invocation
resolves and neither forces nor forbids a server.

serve holds one roadmap graph open and answers Cypher statements over a Unix
domain socket until it is stopped, so a caller pays one store open instead of
one per invocation. It runs no statement of its own and creates no graph
directory that does not already exist.

Commands:
  execute   Run one Cypher statement against the roadmap knowledge graph
  serve     Serve the roadmap knowledge graph over a Unix domain socket
  client    Send one Cypher statement to a running server and print its result

Options:
  -r, --roadmap <name>    REQUIRED. Target roadmap
  -q, --query <cypher>    execute and client. Cypher statement; read from stdin
                          when this flag is absent
      --socket <path>     All three. Socket bound by serve and resolved by
                          execute and client; default
                          ~/.roadmaps/<name>/graph.sock
  -h, --help              Show this help message

Output (stdout JSON):
  Statement that produces result columns:
    {"columns": [...], "rows": [[...], ...]}
  Statement that produces none:
    {"ok": true}
  Server startup:
    {"socket": "<path>"}

Exit codes:
  0   Success
  1   Graph store unavailable or Cypher parse/execution error; also a valid
      statement cancelled for running past the 5s statement time budget, which
      writes nothing -- narrow the statement, or split it; also, for client, no
      server listening, and for any of the three a server that could not be
      reached through the socket
  2   No query supplied, --socket with an empty value, or a positional argument
      was given
  3   No roadmap selected
  4   Roadmap not found
  6   Query longer than the maximum length of 1048576 bytes
  127 Unknown subcommand

Examples:
  rmp graph execute -r myproject --query "MATCH (n:Spec) RETURN n.key"
  rmp graph execute -r myproject --query "CREATE (n:Spec {key:'auth'})"
  echo "MATCH (n) RETURN count(n)" | rmp graph execute -r myproject
  rmp graph serve -r myproject
  rmp graph client -r myproject --query "MATCH (n:Spec) RETURN n.key"
`)
}

// printGraphExecuteHelp prints the help for rmp graph execute.
//
// The graph-specific behaviours SPEC/HELP.md § Graph family help specifics
// requires are all stated below: where the statement comes from, that execute
// runs any statement without examining it, and that the schema DDL and the schema
// listings run through this same subcommand. In particular the help MUST NOT
// describe any statement as rejected before execution on the ground of its
// operation class, because none is.
//
// Item 6 adds one more, and it is the one an agent cannot infer: a running server
// takes the statement AUTOMATICALLY, with no flag and no configuration, and
// --socket names the socket the invocation resolves rather than switching between
// the two paths. The help states both halves deliberately. An agent told only
// that it is automatic would have no way to reach a server on a non-default
// socket; an agent told only that there is a flag would write it on every
// invocation.
func printGraphExecuteHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp graph execute -r <roadmap> [-q <cypher>] [--socket <path>]

Run one Cypher statement against the roadmap knowledge graph, and print what it
returns. A statement that changes the graph runs inside a single transaction and
is persisted durably before the process exits.

Where the statement runs is resolved, not chosen. When a graph server is serving
the selected roadmap, the statement is sent to that server instead of the store
being opened -- automatically, with no flag and no configuration -- and the
result, the output shape and the exit code are the same either way. With nothing
listening, the store is opened directly under its exclusive lock, which is what
every invocation did before a server existed.

execute accepts every statement the engine accepts and runs it as given:
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

Required:
  -r, --roadmap <name>    Target roadmap
  -q, --query <cypher>    Cypher statement; read from stdin when this flag is
                          absent. Supplying neither is an error (exit code 2)

Optional:
      --socket <path>     Socket this invocation resolves. Default
                          ~/.roadmaps/<name>/graph.sock, the same derivation
                          graph serve and graph client use. It names which
                          socket is looked at and nothing else: it does not
                          force a server, does not forbid one, and does not
                          select the store. Write it only when the server was
                          started with the same flag
  -h, --help              Show this help message

Output (stdout JSON):
  With result columns:      {"columns": [...], "rows": [[...], ...]}
  Without result columns:   {"ok": true}
  A statement carrying a RETURN clause produces columns and one without it does
  not; SHOW INDEXES and SHOW CONSTRAINTS produce columns although they carry no
  RETURN clause.

Exit codes:
  0   Success
  1   Graph store unavailable, or a Cypher parse or execution error, including
      a schema statement the engine refused: a duplicate create, a drop of an
      object that does not exist, an unsupported definition, or a constraint
      the data does not satisfy. Also a statement cancelled for running past
      the 5s statement time budget, where the Cypher was valid and the store
      healthy: the transaction rolls back and nothing is written, so the remedy
      is to narrow the statement -- add a label, an indexed property filter, or
      a LIMIT -- or split it into smaller statements. Also a socket that
      answers but yields no server, and a connection lost or unanswered after
      the statement was sent: neither falls back to the store
  2   No query supplied, --socket given with an empty value, or a positional
      argument was given: a bare Cypher statement on the command line is
      refused, not executed
  3   No roadmap selected
  4   Roadmap not found
  6   Query longer than the maximum length of 1048576 bytes

Examples:
  rmp graph execute -r myproject --query "MATCH (n:Spec) RETURN n.key"
  rmp graph execute -r myproject --query "CREATE (n:Spec {key:'auth'})"
  rmp graph execute -r myproject --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"
  echo "MATCH (n) RETURN count(n)" | rmp graph execute -r myproject
  rmp graph execute -r myproject --socket /run/user/1000/myproject-graph.sock -q "SHOW INDEXES"
`)
}

// openGraphStore validates that roadmapName exists, resolves the graph
// directory, and creates it on first use with 0700 permissions. It
// returns the graphDir path. The caller is responsible for opening the
// GoGraph store after this call.
//
// Creating the directory is what makes `rmp graph execute` work against a
// roadmap that has never had a graph. It is deliberately NOT shared with
// `rmp graph serve`, which creates no graph directory that does not already
// exist (SPEC/COMMANDS.md § Serve, "What the server does not do"); that
// subcommand calls resolveGraphDir and stops at the resolution.
func openGraphStore(roadmapName string) (graphDir string, err error) {
	graphDir, err = resolveGraphDir(roadmapName)
	if err != nil {
		return "", err
	}
	if err := createGraphDir(graphDir); err != nil {
		return "", err
	}
	return graphDir, nil
}

// createGraphDir brings the graph store directory into being at 0700, and is the
// half of openGraphStore that a SERVED invocation must not perform.
//
// `rmp graph execute` creates the directory on first use because a statement has
// to have somewhere to run. When a server answers, the statement runs in that
// server's store and this process opens nothing, so creating a directory here
// would leave an empty store beside a graph that is already open — and it would
// do so in the one case where the store is guaranteed to exist already
// (SPEC/GRAPH.md § Server Resolution: on the served path the caller does not open
// the store).
func createGraphDir(graphDir string) error {
	if mkErr := os.MkdirAll(graphDir, 0700); mkErr != nil {
		return fmt.Errorf("%w: creating graph directory: %v", utils.ErrDatabase, mkErr)
	}
	if chErr := os.Chmod(graphDir, 0700); chErr != nil { // #nosec G302 -- 0700 on a DIRECTORY is mandated by SPEC (CLAUDE.md §10: 0700 for the ~/.roadmaps tree); gosec G302 false-positives on directory permissions
		return fmt.Errorf("%w: setting graph directory permissions: %v", utils.ErrDatabase, chErr)
	}
	return nil
}

// resolveGraphDir validates roadmapName, confirms the roadmap exists, and
// returns the path of its graph store directory WITHOUT creating anything.
//
// It is the half of openGraphStore that every graph surface needs, split out
// because one of them must not perform the other half: `rmp graph serve` makes an
// existing graph available and does not bring one into being.
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
		// reader a second prefix: `rmp graph execute -r CON` rendered
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
// writer decided how much this process buffered: 256 MiB offered to
// `rmp graph execute` produced 867 MB of peak resident memory and 15.9 s of wall
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
		return "", fmt.Errorf("%w: reading query from stdin: %v", utils.ErrDatabase, err)
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
func serializeValue(v expr.Value) any {
	if v == nil {
		return nil
	}
	switch v.Kind() {
	case expr.KindNull:
		return nil

	case expr.KindInteger:
		iv, _ := v.(expr.IntegerValue)
		return int64(iv)

	case expr.KindFloat:
		fv, _ := v.(expr.FloatValue)
		f := float64(fv)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return f

	case expr.KindString:
		sv, _ := v.(expr.StringValue)
		return string(sv)

	case expr.KindBool:
		bv, _ := v.(expr.BoolValue)
		return bool(bv)

	case expr.KindDate:
		dv, _ := v.(expr.DateValue)
		return dv.ToTime().UTC().Format("2006-01-02")

	case expr.KindDateTime:
		dtv, _ := v.(expr.DateTimeValue)
		return dtv.T.UTC().Format(time.RFC3339Nano)

	case expr.KindLocalDateTime:
		ldtv, _ := v.(expr.LocalDateTimeValue)
		return ldtv.T.Format("2006-01-02T15:04:05.999999999")

	case expr.KindLocalTime:
		ltv, _ := v.(expr.LocalTimeValue)
		return ltv.String()

	case expr.KindTime:
		tv, _ := v.(expr.TimeValue)
		return tv.String()

	case expr.KindDuration:
		durv, _ := v.(expr.DurationValue)
		return durv.String()

	case expr.KindList:
		lv, _ := v.(expr.ListValue)
		out := make([]any, len(lv))
		for i, elem := range lv {
			out[i] = serializeValue(elem)
		}
		return out

	case expr.KindMap:
		mv, _ := v.(expr.MapValue)
		out := make(map[string]any, len(mv))
		for k, val := range mv {
			out[k] = serializeValue(val)
		}
		return out

	case expr.KindNode:
		nv, _ := v.(expr.NodeValue)
		props := make(map[string]any, len(nv.Properties))
		for k, val := range nv.Properties {
			props[k] = serializeValue(val)
		}
		return map[string]any{
			"id":         nv.ID,
			"labels":     nv.Labels,
			"properties": props,
		}

	case expr.KindRelationship:
		rv, _ := v.(expr.RelationshipValue)
		props := make(map[string]any, len(rv.Properties))
		for k, val := range rv.Properties {
			props[k] = serializeValue(val)
		}
		return map[string]any{
			"id":         rv.ID,
			"type":       rv.Type,
			"startId":    rv.StartID,
			"endId":      rv.EndID,
			"properties": props,
		}

	case expr.KindPath:
		pv, _ := v.(expr.PathValue)
		nodes := make([]any, len(pv.Nodes))
		for i, n := range pv.Nodes {
			nodes[i] = serializeValue(n)
		}
		rels := make([]any, len(pv.Relationships))
		for i, r := range pv.Relationships {
			rels[i] = serializeValue(r)
		}
		return map[string]any{
			"nodes":         nodes,
			"relationships": rels,
		}

	default:
		return v.String()
	}
}

// printGraphNotifications writes each advisory notification attached to
// result as a plain-text diagnostic line on stderr, one line per
// notification (SPEC/GRAPH.md § Query Notifications as Diagnostics). The
// line carries the notification's severity, its stable machine-readable
// code, and its description. Notifications are advisory: they never change
// the stdout success output or the exit code. A result with no
// notifications writes nothing.
//
// It is surfaced generically: whatever notifications the engine attaches to
// the result are emitted, whatever their code, severity, or category, so the
// behaviour is not tied to any specific notification. The representative line
// for the Cartesian-product warning reads:
//
//	INFORMATION Neo.ClientNotification.Statement.CartesianProductWarning: <description>
func printGraphNotifications(result *cypher.Result) {
	for _, n := range result.Notifications() {
		fmt.Fprintf(os.Stderr, "%s %s: %s\n", n.Severity, n.Code, n.Description)
	}
}

// serializeGraphResult drains result into a graphQueryResult. The
// caller must close the result after this function returns.
func serializeGraphResult(result *cypher.Result) (graphQueryResult, error) {
	cols := result.Columns()
	out := graphQueryResult{
		Columns: cols,
		Rows:    [][]any{},
	}
	for result.Next() {
		rec := result.Record()
		row := make([]any, len(cols))
		for i, col := range cols {
			raw := rec[col]
			if v, ok := raw.(expr.Value); ok {
				row[i] = serializeValue(v)
			} else {
				row[i] = raw
			}
		}
		out.Rows = append(out.Rows, row)
	}
	if err := result.Err(); err != nil {
		return graphQueryResult{}, err
	}
	return out, nil
}

// graphStatementError classifies a failure raised while the statement was
// executing — whether it surfaced from the engine call, from the walk over the
// result, or from the commit — and words it truthfully.
//
// Every case carries utils.ErrDatabase and exit code 1. Exhausting the
// statement time budget is a database failure exactly as a statement the engine
// refuses is: the graph feature introduces no new sentinel error and no new exit
// code, and may not (SPEC/GRAPH.md § Constraints, rule 5; § Schema Failure
// Classes, rule 6). Only the message differs.
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
			"LIMIT — or split it into smaller statements.", utils.ErrDatabase, budget)
	}
	return fmt.Errorf("%w: %s: %v", utils.ErrDatabase, stage, err)
}

// runGraphExecute is the implementation of `rmp graph execute`.
//
// It has TWO paths and resolves between them rather than choosing. When a server
// answers on the socket in force, the statement is sent to it and this process
// opens nothing and takes no lock; when nothing answers, it opens the store under
// the exclusive advisory lock, runs the statement inside a transaction,
// serialises the result, and checkpoints — which is what every invocation did
// before a server existed. The statement, the result, the output shape and the
// exit code are the same either way, and which path carried it is not observable
// (SPEC/GRAPH.md § Server Resolution, rule 6). No flag chooses between them:
// --socket names the socket that is looked at and decides nothing else.
//
// The paragraphs below describe the direct path.
//
// There is ONE execution path here, and it is the transactional one, because
// nothing in Groadmap decides between two: Groadmap does not examine the
// statement, so it cannot learn from it whether it reads or writes
// (SPEC/GRAPH.md § Engine Construction and Lifecycle). Every statement goes to
// the same store-backed engine, built with a transactional store over a
// write-ahead-log writer, which is what the hazard the specification names
// requires — a writing statement run on an engine built WITHOUT a transactional
// store executes against the recovered in-memory graph, commits nothing, and
// still reports success, so the write is lost in silence.
//
// A read-only path used to exist beside this one and was reachable through
// `graph query` and `graph search`. It went with the five subcommand names,
// because the operation-class check was the only thing that could route a
// statement to it (SPEC/COMMANDS.md § Graph Management).
//
// The single engine call is RunAny, and the choice is measured rather than
// stylistic. RunAny is the engine's OWN transactional dispatcher: it routes a
// statement carrying a writing clause to RunInTx and every other statement to
// Run, so a write is still committed atomically through exactly the call the
// specification names, and Groadmap still makes one call for every statement.
// Calling RunInTx directly for everything was tried first and loses a published
// behaviour: at the pinned engine, a Result produced by RunInTx carries NO
// plan-time notifications, while the same statement through Run or RunAny
// carries the Cartesian-product advisory. Measured on GoGraph v0.12.0, against
// this project's own store, with `MATCH (a:Spec), (b:Task) RETURN a.key, b.key`:
// Run and RunAny each returned one notification, RunInTx returned nil. Groadmap
// surfaces exactly what the engine attaches (SPEC/GRAPH.md § Query Notifications
// as Diagnostics), so RunInTx-for-everything would silently withdraw the
// stderr diagnostic that acceptance criterion 21 requires. The specification
// fixes the behaviour and leaves the Go API to the implementation, which is what
// this comment is spending its words on.
func runGraphExecute(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	// --socket is consumed BEFORE the statement is read, because readQuery
	// refuses every token it does not recognise: the two flags are read in
	// sequence rather than by one parser that knows both, so each keeps the
	// refusal SPEC/COMMANDS.md publishes for it.
	socketFlag, remaining, err := extractSocketFlag(remaining)
	if err != nil {
		return err
	}

	query, err := readQuery(remaining)
	if err != nil {
		return err
	}

	// The roadmap's existence is checked on BOTH paths and before either is
	// taken, so exit code 4 stays reachable for a roadmap that does not exist
	// whatever answers on the socket. resolveGraphDir creates nothing: the
	// directory is brought into being further down, on the direct path alone.
	graphDir, err := resolveGraphDir(roadmapName)
	if err != nil {
		return err
	}

	// Resolution decides WHERE the statement runs, and it runs before any lock
	// is taken and before any store is opened, so this invocation takes exactly
	// one of the two paths and never both (SPEC/GRAPH.md § Server Resolution,
	// rule 3). A server holds the store's exclusive lock for its whole process
	// lifetime, and no finite wait can be sized against such a hold — resolving
	// first is what stops a running server from disabling this subcommand
	// against the roadmap it serves.
	socket, err := graphSocketInForce(roadmapName, socketFlag)
	if err != nil {
		return err
	}
	state, err := resolveGraphServer(socket)
	if err != nil {
		// The socket answered and yielded no server. This is a FAILURE and not a
		// fall back: the socket may belong to a server holding the lock, so
		// opening the store here would wait the whole wait budget and then fail
		// (rule 2).
		return err
	}
	if state.Served() {
		output, sendErr := runOnGraphServer(socket, query)
		if sendErr != nil {
			return sendErr
		}
		return utils.PrintJSON(output)
	}

	// Not served: the direct path, which is what every invocation did before a
	// server existed. The graph directory is created here and not above, because
	// a served invocation opens nothing and must bring no store into being.
	if err := createGraphDir(graphDir); err != nil {
		return err
	}

	// The store's whole lifecycle — the exclusive advisory hold, the recovery
	// open, the write-ahead-log writer, the transactional store, the engine over
	// them, and the checkpoint below — belongs to internal/graphstore, which owns
	// the one copy of it. Close releases the log and then the lock, in that
	// order, so the log is never closed outside the hold that covers this store.
	st, err := graphstore.Open(graphDir)
	if err != nil {
		return err
	}
	defer st.Close() //nolint:errcheck // the close error is moot once the commit has been reported

	engine := st.Engine()

	// The whole of the statement's execution runs under the graph store's
	// statement time budget (SPEC/GRAPH.md § Statement Time Budget). The deadline
	// is derived HERE, and not around the whole invocation, because rule 1
	// defines the budget as covering exactly what follows — the run against the
	// engine and the walk over the result that run produces — and nothing else:
	// taking the lock, opening the store, the recovery repair the open performs,
	// and the checkpoint below are not statement execution.
	//
	// The budget is graphlock.StatementBudget, and this call site READS it rather
	// than declaring one of its own. It is the same declaration, carrying the
	// same value, that the web graph data endpoint applies
	// (internal/web.runGraphViewQuery), so the two surfaces cannot come to
	// disagree and the CLI carries no second constant to drift from the first.
	// It lives in the package that owns the lock because the same quantity bounds
	// the VARIABLE part of a lock hold, and the party that has to know how long a
	// hold may lawfully last is the one waiting for it (SPEC/GRAPH.md
	// § Lock Contention).
	//
	// It is read ONCE, into a local, so that the deadline which fires and the
	// message that reports it can never disagree.
	budget := graphlock.StatementBudget
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	// Releasing the timer here keeps the budget strictly per invocation; the
	// checkpoint below does not run under it and is not cancelled by it.
	defer cancel()

	result, err := engine.RunAny(ctx, query, nil)
	if err != nil {
		return graphStatementError(budget, "graph query failed", err)
	}

	// Build the output value first by draining the result. The write
	// transaction is not yet committed here: result.Close() performs the
	// commit and returns its error, so the result MUST be fully consumed
	// and serialised BEFORE Close, not via a deferred Close.
	var output any
	cols := result.Columns()
	if len(cols) == 0 {
		// No RETURN clause: drain to allow the commit and emit {"ok": true}.
		for result.Next() {
		}
		if iterErr := result.Err(); iterErr != nil {
			_ = result.Close() //nolint:errcheck // roll back; commit error is moot on iteration failure
			return graphStatementError(budget, "graph query failed", iterErr)
		}
		output = graphOKResult{OK: true}
	} else {
		out, serErr := serializeGraphResult(result)
		if serErr != nil {
			_ = result.Close() //nolint:errcheck // roll back; commit error is moot on iteration failure
			return graphStatementError(budget, "graph query failed", serErr)
		}
		output = out
	}

	// Surface any advisory notifications attached to the result as stderr
	// diagnostics, after the result is fully drained and the output value is
	// built, but BEFORE Close commits and releases the result. Notifications
	// are parse-time advisories available as soon as RunInTx returns; they
	// never change the stdout success output or the exit code (SPEC FR10).
	printGraphNotifications(result)

	// Commit is the durability boundary: Result.Close applies and commits
	// the write transaction and returns the commit error. A commit failure
	// here is a normal write failure (SPEC FR7 §4): no checkpoint runs and
	// the command fails with ErrDatabase (exit 1).
	if cerr := result.Close(); cerr != nil {
		return graphStatementError(budget, "graph commit failed", cerr)
	}

	// The transaction has committed durably. Checkpoint synchronously: write a
	// self-sufficient snapshot and truncate the WAL. Per SPEC FR7, a checkpoint
	// failure AFTER a durable commit MUST NOT fail the write: the WAL is intact,
	// recovery still works, and the next write reconciles the snapshot. Surface
	// the failure as a diagnostic on stderr but return success with exit code 0.
	//
	// Store.Checkpoint carries the gate: a statement whose transaction appended
	// nothing leaves `snapshot/` and `wal` exactly as it found them, which is what
	// makes an ordinary read cost no snapshot rewrite now that every statement
	// runs here. The decision is the store's, not this call site's, so the two
	// surfaces that take this checkpoint cannot come to disagree about when it
	// runs.
	if _, cperr := st.Checkpoint(); cperr != nil {
		fmt.Fprintf(os.Stderr, "Warning: graph checkpoint failed: %v\n", cperr)
	}

	return utils.PrintJSON(output)
}
