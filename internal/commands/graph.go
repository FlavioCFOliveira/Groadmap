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
	"github.com/FlavioCFOliveira/GoGraph/graph/csr"
	"github.com/FlavioCFOliveira/GoGraph/graph/lpg"
	"github.com/FlavioCFOliveira/GoGraph/store/recovery"
	"github.com/FlavioCFOliveira/GoGraph/store/snapshot"
	"github.com/FlavioCFOliveira/GoGraph/store/txn"
	"github.com/FlavioCFOliveira/GoGraph/store/wal"
	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
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
func printGraphHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp graph <subcommand> -r <roadmap> [-q <cypher>]

Operate the knowledge graph of a roadmap using Cypher. The graph is stored
under ~/.roadmaps/<name>/graph/ and is created on first use. There is exactly
one subcommand, execute; create, query, update, delete and search are not
subcommand names of rmp graph and do not resolve. execute runs any statement
the engine accepts -- a read, a write, a deletion, a schema change, a schema
listing -- and rmp does not examine the statement or refuse it for what it
does. The statement comes from --query, or from standard input when that flag
is absent; supplying neither is an error.

Commands:
  execute   Run one Cypher statement against the roadmap knowledge graph

Options:
  -r, --roadmap <name>    REQUIRED. Target roadmap
  -q, --query <cypher>    Cypher statement; read from stdin when this flag is absent
  -h, --help              Show this help message

Output (stdout JSON):
  Statement that produces result columns:
    {"columns": [...], "rows": [[...], ...]}
  Statement that produces none:
    {"ok": true}

Exit codes:
  0   Success
  1   Graph store unavailable or Cypher parse/execution error
  2   No query supplied, or a positional argument was given
  3   No roadmap selected
  4   Roadmap not found
  6   Query longer than the maximum length of 1048576 bytes
  127 Unknown subcommand

Examples:
  rmp graph execute -r myproject --query "MATCH (n:Spec) RETURN n.key"
  rmp graph execute -r myproject --query "CREATE (n:Spec {key:'auth'})"
  echo "MATCH (n) RETURN count(n)" | rmp graph execute -r myproject
`)
}

// printGraphExecuteHelp prints the help for rmp graph execute, the single
// subcommand of the graph family.
//
// The three graph-specific behaviours SPEC/HELP.md § Graph family help
// specifics requires are all stated below: where the statement comes from, that
// execute runs any statement without examining it, and that the schema DDL and
// the schema listings run through this same subcommand. In particular the help
// MUST NOT describe any statement as rejected before execution on the ground of
// its operation class, because none is.
func printGraphExecuteHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp graph execute -r <roadmap> [-q <cypher>]

Run one Cypher statement against the roadmap knowledge graph, and print what it
returns. A statement that changes the graph runs inside a single transaction and
is persisted durably before the process exits.

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
      the data does not satisfy
  2   No query supplied, or a positional argument was given: a bare Cypher
      statement on the command line is refused, not executed
  3   No roadmap selected
  4   Roadmap not found
  6   Query longer than the maximum length of 1048576 bytes

Examples:
  rmp graph execute -r myproject --query "MATCH (n:Spec) RETURN n.key"
  rmp graph execute -r myproject --query "CREATE (n:Spec {key:'auth'})"
  rmp graph execute -r myproject --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"
  echo "MATCH (n) RETURN count(n)" | rmp graph execute -r myproject
`)
}

// openGraphStore validates that roadmapName exists, resolves the graph
// directory, and creates it on first use with 0700 permissions. It
// returns the graphDir path and a no-op cleanup func (reserved for
// future use). The caller is responsible for opening the GoGraph store
// after this call.
func openGraphStore(roadmapName string) (graphDir string, err error) {
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

	graphDir = filepath.Join(roadmapDir, "graph")

	if mkErr := os.MkdirAll(graphDir, 0700); mkErr != nil {
		return "", fmt.Errorf("%w: creating graph directory: %v", utils.ErrDatabase, mkErr)
	}
	if chErr := os.Chmod(graphDir, 0700); chErr != nil { // #nosec G302 -- 0700 on a DIRECTORY is mandated by SPEC (CLAUDE.md §10: 0700 for the ~/.roadmaps tree); gosec G302 false-positives on directory permissions
		return "", fmt.Errorf("%w: setting graph directory permissions: %v", utils.ErrDatabase, chErr)
	}

	return graphDir, nil
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

// graphOpenOpts carries the recovery.Options value used for every
// graph store open. Defined once to avoid repeating the codec wiring.
//
// It was named graphReadOpts while a read path existed to distinguish it from
// the write path's options; there is one path now, and one set of options.
var graphOpenOpts = recovery.Options[string, float64]{
	Codec:       txn.NewStringCodec(),
	WeightCodec: txn.NewFloat64WeightCodec(),
}

// openWALWriter opens the WAL writer at walPath under the project's single
// bounded backoff policy (internal/backoff), which owns the attempt count and
// the delay ladder. This site used to keep its own constants and its own loop,
// and the loop disagreed with them; it now has neither.
//
// Every failure is waited on, because the one this retry exists for is
// contention — another process holding the WAL directory lock — and a WAL that
// cannot be opened for any other reason is not distinguishable here anyway. A
// persistent failure is returned as ErrDatabase; callers must close the
// returned Writer.
func openWALWriter(walPath string) (*wal.Writer, error) {
	w, err := backoff.Retry(func() (*wal.Writer, error) { return wal.Open(walPath) }, backoff.Always)
	if err != nil {
		return nil, fmt.Errorf("%w: graph store unavailable: %v", utils.ErrDatabase, err)
	}
	return w, nil
}

// checkpointGraph performs the synchronous post-commit checkpoint
// (SPEC/GRAPH.md § Synchronous Checkpoint on Write). It writes a
// self-sufficient full snapshot of the committed graph state under
// graphDir/snapshot/ and then truncates the write-ahead log so the log
// holds only post-snapshot transactions. The snapshot carries the
// node-key mapping (mapper.bin) for string keys AND the registered schema
// (constraints.bin, indexdefs.bin), so snapshot + WAL tail is enough for
// recovery to reconstruct both the graph and the schema declared over it.
//
// It takes the engine that just executed the statement because the
// specification requires the snapshot to carry "the schema the engine holds
// registered at the moment of the checkpoint", and forbids Groadmap keeping a
// record of its own beside it: the engine is the only party that knows what is
// registered after a statement has run. The engine is therefore a parameter
// rather than the two spec slices, so this function reads them itself, at the
// one moment they are correct, and no caller can hand it a set assembled
// earlier or elsewhere. The committed graph stays a separate parameter because
// the engine exposes no accessor for it, and because the snapshot's subject is
// the graph the store open recovered and the transaction then committed into —
// which is what the caller holds.
//
// The order below is load-bearing. The specs are read, and the snapshot they
// go into is made durable, BEFORE the write-ahead log is truncated: until that
// snapshot exists, the log holds the only record of every CREATE INDEX and
// CREATE CONSTRAINT the graph has seen, and truncating first would destroy the
// schema outright (SPEC/GRAPH.md § Synchronous Checkpoint on Write, step 2).
//
// It MUST be called only after the write transaction has committed
// durably; the caller treats any error here as non-fatal (see FR7).
func checkpointGraph(engine *cypher.Engine, g *lpg.Graph[string, float64], w *wal.Writer, graphDir string) error {
	// Build a CSR view of the committed in-memory graph for the snapshot.
	cs := csr.BuildFromAdjList(g.AdjList())

	// The registered schema, read from the engine that ran the statement and
	// while the write-ahead log is still intact. Either slice may be empty —
	// the common case, a graph with no schema declared over it — and the
	// writer then simply omits the corresponding snapshot component.
	constraints := engine.ConstraintSpecsForSnapshot()
	indexDefs := engine.IndexSpecsForSnapshot()

	snapDir := filepath.Join(graphDir, "snapshot")
	// WriteSnapshotFullWithMapperCodecConstraintsAndIndexDefs assembles in
	// snapDir+".tmp" and renames atomically into snapDir; the codec emits
	// mapper.bin so the snapshot is self-sufficient for string keys, and the
	// two spec slices are what make it self-sufficient for the schema. The
	// plain WriteSnapshotFullWithMapperCodec persists no schema at all, and
	// the truncation below then leaves nothing to recover it from.
	if err := snapshot.WriteSnapshotFullWithMapperCodecConstraintsAndIndexDefs(
		snapDir, cs, g, txn.NewStringCodec(), constraints, indexDefs); err != nil {
		return fmt.Errorf("snapshot write: %w", err)
	}

	// Flush the WAL, then truncate it to bound its growth. Truncation
	// happens only after the snapshot is durable, so no committed data is
	// lost.
	if err := w.Sync(); err != nil {
		return fmt.Errorf("wal sync: %w", err)
	}
	if _, err := w.Truncate(); err != nil {
		return fmt.Errorf("wal truncate: %w", err)
	}

	// Keep the snapshot directory consistent with the 0700 graphDir
	// permissions set in openGraphStore. Best-effort: a failure here does
	// not invalidate the durable snapshot.
	_ = os.Chmod(snapDir, 0700) // #nosec G302 -- 0700 on a DIRECTORY is mandated by SPEC (CLAUDE.md §10: 0700 for the ~/.roadmaps tree); gosec G302 false-positives on directory permissions
	return nil
}

// runGraphExecute is the implementation of `rmp graph execute`, the whole of
// the graph family. It opens the store under the exclusive advisory lock, runs
// the statement it is given inside a transaction, serialises the result, and
// checkpoints.
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

	query, err := readQuery(remaining)
	if err != nil {
		return err
	}

	graphDir, err := openGraphStore(roadmapName)
	if err != nil {
		return err
	}

	// Serialise concurrent writers to prevent the lost-write corruption
	// described in graphlock.AcquireExclusive, and shut out a reader that would
	// otherwise run its recovery repair over this writer's checkpoint. Held for
	// the whole sequence, until after the checkpoint.
	releaseLock, err := graphlock.AcquireExclusive(graphDir)
	if err != nil {
		return err
	}
	defer releaseLock()

	res, err := recovery.Open[string, float64](graphDir, graphOpenOpts)
	if err != nil {
		return fmt.Errorf("%w: graph store unavailable: %v", utils.ErrDatabase, err)
	}

	walPath := filepath.Join(graphDir, "wal")
	w, err := openWALWriter(walPath)
	if err != nil {
		return err
	}
	defer w.Close() //nolint:errcheck

	store := txn.NewStoreWithOptions[string, float64](res.Graph, w, txn.Options[string, float64]{
		Codec:       txn.NewStringCodec(),
		WeightCodec: txn.NewFloat64WeightCodec(),
	})

	// The whole recovery result, not extracted fields: this constructor
	// re-registers the recovered constraints and index definitions AND hydrates
	// each index from the snapshot payload the same open returned, instead of
	// rebuilding it by a full scan of the graph. `rmp` opens the store, runs one
	// statement and exits, so a rebuild would be paid once per command rather
	// than once per process lifetime (SPEC/GRAPH.md § Engine Constructor by
	// Path). res MUST be the result of the open that produced this store, a few
	// lines above: a result from any other open would describe a different graph
	// and neither the engine nor the store could detect the substitution.
	engine := cypher.NewEngineWithStoreAndRecovery(store, res)
	ctx := context.Background()

	// The write-ahead log's durable offset BEFORE the statement runs. The
	// checkpoint below is gated on this growing, because a transaction that
	// appended nothing MUST NOT snapshot and MUST NOT truncate: it would rewrite
	// a full snapshot on every statement, and it would shorten the history a
	// later recovery replays (SPEC/GRAPH.md § What a Statement That Writes
	// Nothing Changes on Disk, rules 2 and 3). The offset is the log's own
	// answer to "did this transaction append", which is the question the
	// specification asks — not a guess made from the statement's text.
	walBefore := w.DurableOffset()

	result, err := engine.RunAny(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("%w: graph query failed: %v", utils.ErrDatabase, err)
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
			return fmt.Errorf("%w: graph query failed: %v", utils.ErrDatabase, iterErr)
		}
		output = graphOKResult{OK: true}
	} else {
		out, serErr := serializeGraphResult(result)
		if serErr != nil {
			_ = result.Close() //nolint:errcheck // roll back; commit error is moot on iteration failure
			return fmt.Errorf("%w: graph query failed: %v", utils.ErrDatabase, serErr)
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
		return fmt.Errorf("%w: graph commit failed: %v", utils.ErrDatabase, cerr)
	}

	// The transaction has committed durably; res.Graph now reflects the new
	// state. Checkpoint synchronously: write a self-sufficient snapshot and
	// truncate the WAL. Per SPEC FR7, a checkpoint failure AFTER a durable
	// commit MUST NOT fail the write: the WAL is intact, recovery still
	// works, and the next write reconciles the snapshot. Surface the failure
	// as a diagnostic on stderr but return success with exit code 0.
	//
	// Gated on the log having grown. A statement whose transaction appended
	// nothing leaves `snapshot/` and `wal` exactly as it found them, which is
	// what makes an ordinary read cost no snapshot rewrite now that every
	// statement runs here.
	if w.DurableOffset() > walBefore {
		if cperr := checkpointGraph(engine, res.Graph, w, graphDir); cperr != nil {
			fmt.Fprintf(os.Stderr, "Warning: graph checkpoint failed: %v\n", cperr)
		}
	}

	return utils.PrintJSON(output)
}
