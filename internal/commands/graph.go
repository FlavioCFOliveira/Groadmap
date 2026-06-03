// Package commands — graph family handler.
//
// Each rmp graph invocation is a short-lived process that opens the
// GoGraph store rooted at ~/.roadmaps/<name>/graph/, runs exactly one
// Cypher query, commits any write, and exits. The store is not held
// open across invocations and is independent of the SQLite database.
package commands

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
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
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// Writing-clause discriminators used by the per-subcommand guard rails.
var (
	reCreate = regexp.MustCompile(`(?i)\b(CREATE|MERGE)\b`)
	reMutate = regexp.MustCompile(`(?i)\b(SET|REMOVE)\b`)
	reDelete = regexp.MustCompile(`(?i)\b(DELETE|DETACH)\b`)
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

// printGraphHelp prints the family-level help for rmp graph.
func printGraphHelp() {
	fmt.Print(`Usage: rmp graph <subcommand> -r <roadmap> [--query <cypher>]

Manage the knowledge graph for a roadmap using Cypher queries.
Each subcommand validates that the supplied query matches its operation class
before executing it (guard-rail enforcement). When --query is absent the query
is read from standard input.

Subcommands:
  create   Execute a CREATE / MERGE query (adds nodes or edges)
  query    Execute a read-only MATCH ... RETURN query
  update   Execute a SET / REMOVE query (mutates existing elements)
  delete   Execute a DELETE / DETACH DELETE query (removes nodes or edges)
  search   Execute a read-only traversal query (variable-length paths, etc.)

Options:
  -r, --roadmap <name>    Target roadmap (required)
  --query <cypher>        Cypher query string; reads stdin when absent
  -h, --help              Show this help message

Output (stdout JSON):
  Read subcommands and write subcommands with RETURN:
    {"columns": [...], "rows": [[...], ...]}
  Write subcommands without RETURN:
    {"ok": true}

Exit codes:
  0   Success
  1   Graph store unavailable or Cypher execution error
  2   No query supplied
  3   No roadmap selected
  4   Roadmap not found
  6   Query's operation class does not match the subcommand

Examples:
  rmp graph create -r myproject --query "CREATE (n:Spec {key:'auth'})"
  rmp graph query  -r myproject --query "MATCH (n:Spec) RETURN n.key"
  echo "MATCH (n) RETURN count(n)" | rmp graph query -r myproject
`)
}

func printGraphCreateHelp() {
	fmt.Print(`Usage: rmp graph create -r <roadmap> [--query <cypher>]

Execute a CREATE or MERGE query against the roadmap's knowledge graph.
The query MUST contain CREATE and/or MERGE clauses and MUST NOT contain
SET, REMOVE, DELETE, or DETACH DELETE.

Options:
  -r, --roadmap <name>    Target roadmap (required)
  --query <cypher>        Cypher query; reads stdin when absent
  -h, --help              Show this help message

Exit codes:
  0   Success
  1   Graph store unavailable or Cypher execution error
  2   No query supplied
  3   No roadmap selected
  4   Roadmap not found
  6   Query class mismatch (guard-rail rejection)

Examples:
  rmp graph create -r myproject --query "CREATE (n:Spec {key:'auth'})"
  rmp graph create -r myproject --query "CREATE (n:Spec {key:'auth'}) RETURN n"
`)
}

func printGraphQueryHelp() {
	fmt.Print(`Usage: rmp graph query -r <roadmap> [--query <cypher>]

Execute a read-only MATCH ... RETURN query against the roadmap's knowledge
graph. The query MUST NOT contain any writing clause.

Options:
  -r, --roadmap <name>    Target roadmap (required)
  --query <cypher>        Cypher query; reads stdin when absent
  -h, --help              Show this help message

Exit codes:
  0   Success
  1   Graph store unavailable or Cypher execution error
  2   No query supplied
  3   No roadmap selected
  4   Roadmap not found
  6   Query contains a writing clause (guard-rail rejection)

Examples:
  rmp graph query -r myproject --query "MATCH (n:Spec) RETURN n.key"
  echo "MATCH (n) RETURN count(n)" | rmp graph query -r myproject
`)
}

func printGraphUpdateHelp() {
	fmt.Print(`Usage: rmp graph update -r <roadmap> [--query <cypher>]

Execute a SET or REMOVE query against the roadmap's knowledge graph.
The query MUST contain SET and/or REMOVE clauses and MUST NOT contain
CREATE, MERGE, DELETE, or DETACH DELETE.

Options:
  -r, --roadmap <name>    Target roadmap (required)
  --query <cypher>        Cypher query; reads stdin when absent
  -h, --help              Show this help message

Exit codes:
  0   Success
  1   Graph store unavailable or Cypher execution error
  2   No query supplied
  3   No roadmap selected
  4   Roadmap not found
  6   Query class mismatch (guard-rail rejection)

Examples:
  rmp graph update -r myproject --query "MATCH (n:Spec {key:'auth'}) SET n.status='done'"
`)
}

func printGraphDeleteHelp() {
	fmt.Print(`Usage: rmp graph delete -r <roadmap> [--query <cypher>]

Execute a DELETE or DETACH DELETE query against the roadmap's knowledge
graph. The query MUST contain DELETE and/or DETACH DELETE and MUST NOT
contain CREATE, MERGE, SET, or REMOVE.

Options:
  -r, --roadmap <name>    Target roadmap (required)
  --query <cypher>        Cypher query; reads stdin when absent
  -h, --help              Show this help message

Exit codes:
  0   Success
  1   Graph store unavailable or Cypher execution error
  2   No query supplied
  3   No roadmap selected
  4   Roadmap not found
  6   Query class mismatch (guard-rail rejection)

Examples:
  rmp graph delete -r myproject --query "MATCH (n:Spec {key:'auth'}) DETACH DELETE n"
`)
}

func printGraphSearchHelp() {
	fmt.Print(`Usage: rmp graph search -r <roadmap> [--query <cypher>]

Execute a read-only traversal query against the roadmap's knowledge graph.
Variable-length path patterns (e.g. -[*1..3]-) are supported. The query
MUST NOT contain any writing clause.

Options:
  -r, --roadmap <name>    Target roadmap (required)
  --query <cypher>        Cypher query; reads stdin when absent
  -h, --help              Show this help message

Exit codes:
  0   Success
  1   Graph store unavailable or Cypher execution error
  2   No query supplied
  3   No roadmap selected
  4   Roadmap not found
  6   Query contains a writing clause (guard-rail rejection)

Examples:
  rmp graph search -r myproject --query "MATCH p=(a)-[*1..3]-(b) RETURN p"
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
		return "", fmt.Errorf("%w: %v", utils.ErrValidation, valErr)
	}

	dbPath := filepath.Join(roadmapDir, utils.DBFileName)
	if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
		return "", fmt.Errorf("%w: roadmap %q not found", utils.ErrNotFound, roadmapName)
	}

	graphDir = filepath.Join(roadmapDir, "graph")

	if mkErr := os.MkdirAll(graphDir, 0700); mkErr != nil {
		return "", fmt.Errorf("%w: creating graph directory: %v", utils.ErrDatabase, mkErr)
	}
	if chErr := os.Chmod(graphDir, 0700); chErr != nil {
		return "", fmt.Errorf("%w: setting graph directory permissions: %v", utils.ErrDatabase, chErr)
	}

	return graphDir, nil
}

// readQuery extracts the Cypher query from args. It consumes --query /
// -q from args and returns the trimmed query string, or reads all of
// stdin when the flag is absent. An empty or whitespace-only result is
// returned as ErrRequired.
func readQuery(args []string) (string, error) {
	var queryVal string
	var queryFound bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--query", "-q":
			if i+1 < len(args) {
				queryVal = args[i+1]
				queryFound = true
				i++
			}
		}
	}

	if queryFound {
		q := strings.TrimSpace(queryVal)
		if q == "" {
			return "", fmt.Errorf("%w: no query supplied", utils.ErrRequired)
		}
		return q, nil
	}

	// --query absent: read stdin in full.
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("%w: reading query from stdin: %v", utils.ErrDatabase, err)
	}
	q := strings.TrimSpace(string(raw))
	if q == "" {
		return "", fmt.Errorf("%w: no query supplied", utils.ErrRequired)
	}
	return q, nil
}

// validateGuardRail checks that query matches the operation class
// required by subcmd. It returns ErrValidation when the class does not
// match, with a message that names the subcommand and the expected class.
func validateGuardRail(subcmd, allowed, query string) error {
	isWrite := cypher.QueryHasWritingClause(query)

	switch subcmd {
	case "create":
		// Must be write, must have CREATE/MERGE, must not have SET/REMOVE/DELETE.
		if !isWrite || !reCreate.MatchString(query) || reMutate.MatchString(query) || reDelete.MatchString(query) {
			return fmt.Errorf("%w: graph create accepts only CREATE/MERGE queries", utils.ErrValidation)
		}
	case "query", "search":
		// Must be read-only.
		if isWrite {
			return fmt.Errorf("%w: graph %s accepts only %s queries", utils.ErrValidation, subcmd, allowed)
		}
	case "update":
		// Must be write, must have SET/REMOVE, must not have CREATE/MERGE/DELETE.
		if !isWrite || !reMutate.MatchString(query) || reCreate.MatchString(query) || reDelete.MatchString(query) {
			return fmt.Errorf("%w: graph update accepts only SET/REMOVE queries", utils.ErrValidation)
		}
	case "delete":
		// Must be write, must have DELETE/DETACH, must not have CREATE/MERGE/SET/REMOVE.
		if !isWrite || !reDelete.MatchString(query) || reCreate.MatchString(query) || reMutate.MatchString(query) {
			return fmt.Errorf("%w: graph delete accepts only DELETE/DETACH DELETE queries", utils.ErrValidation)
		}
	}
	return nil
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

// graphReadOpts carries the recovery.Options value used for every
// graph store open. Defined once to avoid repeating the codec wiring.
var graphReadOpts = recovery.Options[string, float64]{
	Codec:       txn.NewStringCodec(),
	WeightCodec: txn.NewFloat64WeightCodec(),
}

// walRetryPolicy mirrors the SQLite bounded exponential-backoff specified
// in IMPLEMENTATION.md § Concurrency Model (initial 100ms, max 1s, 5 attempts).
const (
	walRetryInitial  = 100 * time.Millisecond
	walRetryMax      = 1000 * time.Millisecond
	walRetryAttempts = 5
)

// openWALWriter opens the WAL writer at walPath with bounded
// exponential backoff. A persistent failure is returned as
// ErrDatabase; callers must close the returned Writer.
func openWALWriter(walPath string) (*wal.Writer, error) {
	delay := walRetryInitial
	var lastErr error
	for attempt := 0; attempt < walRetryAttempts; attempt++ {
		w, err := wal.Open(walPath)
		if err == nil {
			return w, nil
		}
		lastErr = err
		if attempt < walRetryAttempts-1 {
			time.Sleep(delay)
			delay *= 2
			if delay > walRetryMax {
				delay = walRetryMax
			}
		}
	}
	return nil, fmt.Errorf("%w: graph store unavailable: %v", utils.ErrDatabase, lastErr)
}

// runGraphCreate executes a CREATE/MERGE Cypher query.
func runGraphCreate(args []string) error {
	return runGraphWrite("create", "CREATE/MERGE", args)
}

// runGraphQuery executes a read-only Cypher query.
func runGraphQuery(args []string) error {
	return runGraphRead("query", "read-only", args)
}

// runGraphUpdate executes a SET/REMOVE Cypher query.
func runGraphUpdate(args []string) error {
	return runGraphWrite("update", "SET/REMOVE", args)
}

// runGraphDelete executes a DELETE/DETACH DELETE Cypher query.
func runGraphDelete(args []string) error {
	return runGraphWrite("delete", "DELETE/DETACH DELETE", args)
}

// runGraphSearch executes a read-only traversal Cypher query.
func runGraphSearch(args []string) error {
	return runGraphRead("search", "read-only", args)
}

// runGraphRead is the shared implementation for read subcommands
// (query and search). It opens the store in read-only recovery mode,
// runs the query, and serialises the result.
func runGraphRead(subcmd, allowed string, args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	query, err := readQuery(remaining)
	if err != nil {
		return err
	}

	if err := validateGuardRail(subcmd, allowed, query); err != nil {
		return err
	}

	graphDir, err := openGraphStore(roadmapName)
	if err != nil {
		return err
	}

	res, err := recovery.Open[string, float64](graphDir, graphReadOpts)
	if err != nil {
		return fmt.Errorf("%w: graph store unavailable: %v", utils.ErrDatabase, err)
	}

	engine := cypher.NewEngine(res.Graph)
	ctx := context.Background()
	result, err := engine.Run(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("%w: graph %s failed: %v", utils.ErrDatabase, subcmd, err)
	}
	defer result.Close() //nolint:errcheck

	out, err := serializeGraphResult(result)
	if err != nil {
		return fmt.Errorf("%w: graph %s failed: %v", utils.ErrDatabase, subcmd, err)
	}

	return utils.PrintJSON(out)
}

// checkpointGraph performs the synchronous post-commit checkpoint
// (SPEC/GRAPH.md § Synchronous Checkpoint on Write). It writes a
// self-sufficient full snapshot of the committed graph state under
// graphDir/snapshot/ and then truncates the write-ahead log so the log
// holds only post-snapshot transactions. The snapshot carries the
// node-key mapping (mapper.bin) for string keys, so snapshot + WAL tail
// is enough for recovery to reconstruct the graph.
//
// It MUST be called only after the write transaction has committed
// durably; the caller treats any error here as non-fatal (see FR7).
func checkpointGraph(g *lpg.Graph[string, float64], w *wal.Writer, graphDir string) error {
	// Build a CSR view of the committed in-memory graph for the snapshot.
	cs := csr.BuildFromAdjList(g.AdjList())

	snapDir := filepath.Join(graphDir, "snapshot")
	// WriteSnapshotFullWithMapperCodec assembles in snapDir+".tmp" and
	// renames atomically into snapDir; the codec emits mapper.bin so the
	// snapshot is self-sufficient for string keys.
	if err := snapshot.WriteSnapshotFullWithMapperCodec(snapDir, cs, g, txn.NewStringCodec()); err != nil {
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
	_ = os.Chmod(snapDir, 0700)
	return nil
}

// runGraphWrite is the shared implementation for write subcommands
// (create, update, delete). It opens the WAL store with retry,
// runs the query in a transaction, and serialises the result.
func runGraphWrite(subcmd, allowed string, args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	query, err := readQuery(remaining)
	if err != nil {
		return err
	}

	if err := validateGuardRail(subcmd, allowed, query); err != nil {
		return err
	}

	graphDir, err := openGraphStore(roadmapName)
	if err != nil {
		return err
	}

	res, err := recovery.Open[string, float64](graphDir, graphReadOpts)
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

	engine := cypher.NewEngineWithStore(store)
	ctx := context.Background()
	result, err := engine.RunInTx(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("%w: graph %s failed: %v", utils.ErrDatabase, subcmd, err)
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
			return fmt.Errorf("%w: graph %s failed: %v", utils.ErrDatabase, subcmd, iterErr)
		}
		output = graphOKResult{OK: true}
	} else {
		out, serErr := serializeGraphResult(result)
		if serErr != nil {
			_ = result.Close() //nolint:errcheck // roll back; commit error is moot on iteration failure
			return fmt.Errorf("%w: graph %s failed: %v", utils.ErrDatabase, subcmd, serErr)
		}
		output = out
	}

	// Commit is the durability boundary: Result.Close applies and commits
	// the write transaction and returns the commit error. A commit failure
	// here is a normal write failure (SPEC FR7 §4): no checkpoint runs and
	// the command fails with ErrDatabase (exit 1).
	if cerr := result.Close(); cerr != nil {
		return fmt.Errorf("%w: graph %s commit failed: %v", utils.ErrDatabase, subcmd, cerr)
	}

	// The transaction has committed durably; res.Graph now reflects the new
	// state. Checkpoint synchronously: write a self-sufficient snapshot and
	// truncate the WAL. Per SPEC FR7, a checkpoint failure AFTER a durable
	// commit MUST NOT fail the write: the WAL is intact, recovery still
	// works, and the next write reconciles the snapshot. Surface the failure
	// as a diagnostic on stderr but return success with exit code 0.
	if cperr := checkpointGraph(res.Graph, w, graphDir); cperr != nil {
		fmt.Fprintf(os.Stderr, "Warning: graph checkpoint failed: %v\n", cperr)
	}

	return utils.PrintJSON(output)
}
