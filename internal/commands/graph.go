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
	"github.com/FlavioCFOliveira/Groadmap/internal/cypherguard"
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

Manage the knowledge graph for a roadmap using Cypher queries.
Each subcommand validates that the supplied query matches its operation class
before executing it (guard-rail enforcement). When --query is absent the query
is read from standard input.

Subcommands:
  create   Execute a CREATE / MERGE query (adds nodes or edges)
  query    Execute a read-only MATCH ... RETURN or SHOW query
  update   Execute a SET / REMOVE query (mutates existing elements)
  delete   Execute a DELETE / DETACH DELETE query (removes nodes or edges)
  search   Execute a read-only traversal query (variable-length paths, etc.)

Options:
  -r, --roadmap <name>    Target roadmap (required)
  -q, --query <cypher>    Cypher query string; reads stdin when absent
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
  127 Unknown subcommand

Examples:
  rmp graph create -r myproject --query "CREATE (n:Spec {key:'auth'})"
  rmp graph query  -r myproject --query "MATCH (n:Spec) RETURN n.key"
  echo "MATCH (n) RETURN count(n)" | rmp graph query -r myproject
`)
}

func printGraphCreateHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp graph create -r <roadmap> [-q <cypher>]

Execute a CREATE or MERGE query against the roadmap's knowledge graph.
The query MUST contain CREATE and/or MERGE clauses and MUST NOT contain
SET, REMOVE, DELETE, or DETACH DELETE.

Required:
  -r, --roadmap <name>    Target roadmap
  -q, --query <cypher>    Cypher query; read from stdin when this flag is absent

Optional:
  -h, --help              Show this help message

Output (stdout JSON):
  Without a RETURN clause:  {"ok": true}
  With a RETURN clause:     {"columns": [...], "rows": [[...], ...]}

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
	fmt.Fprint(helpDst(), `Usage: rmp graph query -r <roadmap> [-q <cypher>]

Execute a read-only MATCH ... RETURN query against the roadmap's knowledge
graph. The query MUST NOT contain any writing clause.

Schema introspection is also accepted: SHOW INDEXES and SHOW CONSTRAINTS (and
their singular aliases), optionally followed by a YIELD / WHERE / RETURN
projection. They list the registered schema and change nothing.

Required:
  -r, --roadmap <name>    Target roadmap
  -q, --query <cypher>    Cypher query; read from stdin when this flag is absent

Optional:
  -h, --help              Show this help message

Output (stdout JSON):
  {"columns": [...], "rows": [[...], ...]}

Exit codes:
  0   Success
  1   Graph store unavailable or Cypher execution error
  2   No query supplied
  3   No roadmap selected
  4   Roadmap not found
  6   Query contains a writing clause (guard-rail rejection)

Examples:
  rmp graph query -r myproject --query "MATCH (n:Spec) RETURN n.key"
  rmp graph query -r myproject --query "SHOW INDEXES"
  echo "MATCH (n) RETURN count(n)" | rmp graph query -r myproject
`)
}

func printGraphUpdateHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp graph update -r <roadmap> [-q <cypher>]

Execute a SET or REMOVE query against the roadmap's knowledge graph.
The query MUST contain SET and/or REMOVE clauses and MUST NOT contain
CREATE, MERGE, DELETE, or DETACH DELETE.

Required:
  -r, --roadmap <name>    Target roadmap
  -q, --query <cypher>    Cypher query; read from stdin when this flag is absent

Optional:
  -h, --help              Show this help message

Output (stdout JSON):
  Without a RETURN clause:  {"ok": true}
  With a RETURN clause:     {"columns": [...], "rows": [[...], ...]}

Exit codes:
  0   Success
  1   Graph store unavailable or Cypher execution error
  2   No query supplied
  3   No roadmap selected
  4   Roadmap not found
  6   Query class mismatch (guard-rail rejection)

Examples:
  rmp graph update -r myproject --query "MATCH (n:Spec {key:'auth'}) SET n.status='done'"
  echo "MATCH (n:Spec {key:'auth'}) REMOVE n.status" | rmp graph update -r myproject
`)
}

func printGraphDeleteHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp graph delete -r <roadmap> [-q <cypher>]

Execute a DELETE or DETACH DELETE query against the roadmap's knowledge
graph. The query MUST contain DELETE and/or DETACH DELETE and MUST NOT
contain CREATE, MERGE, SET, or REMOVE.

Required:
  -r, --roadmap <name>    Target roadmap
  -q, --query <cypher>    Cypher query; read from stdin when this flag is absent

Optional:
  -h, --help              Show this help message

Output (stdout JSON):
  Without a RETURN clause:  {"ok": true}
  With a RETURN clause:     {"columns": [...], "rows": [[...], ...]}

Exit codes:
  0   Success
  1   Graph store unavailable or Cypher execution error
  2   No query supplied
  3   No roadmap selected
  4   Roadmap not found
  6   Query class mismatch (guard-rail rejection)

Examples:
  rmp graph delete -r myproject --query "MATCH (n:Spec {key:'auth'}) DETACH DELETE n"
  rmp graph delete -r myproject --query "MATCH (:Spec)-[r:DEPENDS_ON]->(:Spec) DELETE r"
`)
}

func printGraphSearchHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp graph search -r <roadmap> [-q <cypher>]

Execute a read-only traversal query against the roadmap's knowledge graph.
Variable-length path patterns (e.g. -[*1..3]-) are supported. The query
MUST NOT contain any writing clause.

Schema introspection is also accepted: SHOW INDEXES and SHOW CONSTRAINTS (and
their singular aliases), optionally followed by a YIELD / WHERE / RETURN
projection. They list the registered schema and change nothing.

Required:
  -r, --roadmap <name>    Target roadmap
  -q, --query <cypher>    Cypher query; read from stdin when this flag is absent

Optional:
  -h, --help              Show this help message

Output (stdout JSON):
  {"columns": [...], "rows": [[...], ...]}

Exit codes:
  0   Success
  1   Graph store unavailable or Cypher execution error
  2   No query supplied
  3   No roadmap selected
  4   Roadmap not found
  6   Query contains a writing clause (guard-rail rejection)

Examples:
  rmp graph search -r myproject --query "MATCH p=(a)-[*1..3]-(b) RETURN p"
  rmp graph search -r myproject --query "MATCH p=(s:Spec)-[:DEPENDS_ON*1..3]->(d:Spec) RETURN p"
  rmp graph search -r myproject --query "SHOW CONSTRAINTS YIELD name RETURN name"
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
		// reader a second prefix: `rmp graph query -r CON` rendered
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
// than deeper in: an over-long query is never masked, never classified by the
// guard rail, never handed to the engine, and never reaches an opened store
// (SPEC/GRAPH.md § Maximum Query Length rule 3).
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
// `rmp graph query` produced 867 MB of peak resident memory and 15.9 s of wall
// time, all of it spent on a "query" that was never going to be accepted (the
// time went into the guard rail's masking pass and the engine's parse attempt,
// both run over 256 MB of input). That is CWE-400 / CWE-789 — an allocation
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

// maskCypherLiterals returns a copy of query with the interior characters of
// Cypher string literals, comments, and backtick-quoted identifiers neutralized
// to spaces, used solely for operation-class classification (SPEC/GRAPH.md
// § Literal-Aware Normalization). It delegates to the shared guard-rail package
// so the CLI and the read-only web endpoint mask identically; see
// cypherguard.MaskLiterals for the full contract.
func maskCypherLiterals(query string) string {
	return cypherguard.MaskLiterals(query)
}

// validateGuardRail checks that query matches the operation class required by
// subcmd. It returns ErrValidation when the class does not match, with a
// message that names the subcommand and the expected class.
//
// The read subcommands carry one further acceptance rule, applied after the
// class rule and only to them: a schema-introspection command is accepted only
// in the keyword spelling the engine routes to its introspection parser
// (SPEC/GRAPH.md § Keyword Spacing in a Schema-Introspection Command). It is a
// rule about which statements the introspection class covers, which is why it
// belongs to the guard rail rather than beside it.
//
// Classification runs on the literal-masked normalization of the query, never
// on the raw string (SPEC/GRAPH.md § Literal-Aware Normalization): a clause
// keyword appearing only inside a string literal, comment, or backtick
// identifier must not affect the guard rail. The original query is still what
// executes against the store. The masking and clause-class detection are owned
// by the shared cypherguard package, so the CLI guard rail and the read-only
// web graph data endpoint apply the exact same classification.
func validateGuardRail(subcmd, allowed, query string) error {
	c := cypherguard.Classify(query)

	// DDL (CREATE INDEX, DROP INDEX, CREATE CONSTRAINT, DROP CONSTRAINT) is a
	// schema-mutating clause class that is outside every subcommand's accepted
	// class (SPEC/GRAPH.md § Per-Subcommand Validation Rules note 5): the read
	// subcommands accept only read-only queries and DDL is not read-only, and
	// the write subcommands each accept only their own data-writing clause.
	// QueryHasWritingClause does not flag DDL (and the two-word CREATE INDEX /
	// CREATE CONSTRAINT forms would otherwise satisfy the create accept check),
	// so DDL is rejected up front for ALL subcommands, with the per-subcommand
	// message that names the class each one does accept.
	if c.DDL {
		switch subcmd {
		case "query", "search":
			return fmt.Errorf("%w: graph %s accepts only %s queries", utils.ErrValidation, subcmd, allowed)
		case "create":
			return fmt.Errorf("%w: graph create accepts only CREATE/MERGE queries", utils.ErrValidation)
		case "update":
			return fmt.Errorf("%w: graph update accepts only SET/REMOVE queries", utils.ErrValidation)
		case "delete":
			return fmt.Errorf("%w: graph delete accepts only DELETE/DETACH DELETE queries", utils.ErrValidation)
		}
	}

	switch subcmd {
	case "create":
		// Must be write, must have CREATE/MERGE, must not have SET/REMOVE/DELETE.
		if !c.Write || !c.Create || c.Mutate || c.Delete {
			return fmt.Errorf("%w: graph create accepts only CREATE/MERGE queries", utils.ErrValidation)
		}
	case "query", "search":
		// Must be read-only.
		if c.Write {
			return fmt.Errorf("%w: graph %s accepts only %s queries", utils.ErrValidation, subcmd, allowed)
		}
		// Schema introspection is accepted only in the spelling the engine routes
		// to its introspection parser: exactly one space between SHOW and the
		// target keyword. Any other separator is refused HERE, with the guard
		// rail's own message, instead of being admitted and left to die in the
		// engine's parser under a diagnostic that lists every clause keyword
		// except SHOW and so reads as though schema introspection were
		// unsupported (SPEC/GRAPH.md § Keyword Spacing in a Schema-Introspection
		// Command; § Per-Subcommand Validation Rules note 8).
		//
		// Placement carries two contracts. It is decided AFTER the class
		// objections above, so a query that both writes and carries a badly
		// spaced SHOW is rejected on its class, matching the precedence the web
		// endpoint applies. And it is confined to this branch, so the write
		// subcommands keep rejecting a SHOW on its class at any spacing (note 6)
		// — their objection is that it carries none of the clauses they accept,
		// which holds for the well-formed spelling too.
		if reason, misspaced := cypherguard.IntrospectSpacingRejection(query); misspaced {
			return fmt.Errorf("%w: graph %s: %s", utils.ErrValidation, subcmd, reason)
		}
	case "update":
		// Must be write, must have SET/REMOVE, must not have CREATE/MERGE/DELETE.
		if !c.Write || !c.Mutate || c.Create || c.Delete {
			return fmt.Errorf("%w: graph update accepts only SET/REMOVE queries", utils.ErrValidation)
		}
	case "delete":
		// Must be write, must have DELETE/DETACH, must not have CREATE/MERGE/SET/REMOVE.
		if !c.Write || !c.Delete || c.Create || c.Mutate {
			return fmt.Errorf("%w: graph delete accepts only DELETE/DETACH DELETE queries", utils.ErrValidation)
		}
	}
	return nil
}

// validateRelationshipWriteDirection rejects a `graph update` query whose SET or
// REMOVE targets a relationship the engine would not write (SPEC/GRAPH.md
// § Relationship Write Direction).
//
// It is a SEPARATE contract from the clause-class guard rail above, not another
// class rule: the query's operation class is already correct — it is a mutating
// write under the subcommand that accepts mutating writes — and what is wrong is
// the ORIENTATION of the pattern that binds the relationship being written. The
// two checks are kept apart so the clause-class classification, which the
// read-only web endpoint shares, is untouched by this write-path-only rule.
//
// Only `update` is checked. `delete` is unaffected: DELETE resolves the edge
// itself rather than through the endpoint columns, and removes a relationship
// bound by a reverse traversal correctly. `create` cannot reach the condition,
// because the clause-class rule above already rejects any CREATE/MERGE query
// that contains SET or REMOVE (so `MERGE … ON MATCH SET …` is not admitted by
// this CLI at all).
//
// Detection runs on the parsed query rather than on the masked text, so a
// relationship arrow inside a string literal or a comment cannot trip it: the
// parser never sees those characters as pattern syntax.
func validateRelationshipWriteDirection(subcmd, query string) error {
	if subcmd != "update" {
		return nil
	}
	unwritable := cypherguard.UnwritableRelationshipTargets(query)
	if len(unwritable) == 0 {
		return nil
	}
	t := unwritable[0]
	return fmt.Errorf(
		"%w: graph update cannot write relationship %q: it is bound by an %s pattern, "+
			"and the engine writes a relationship property only through an outgoing pattern, "+
			"so %s would be skipped while the command still reported success. "+
			"Rewrite the traversal as outgoing: MATCH (source)-[%s]->(target) ... SET %s.<key> = <value>. "+
			"To reach the edges arriving AT a node, anchor the outgoing pattern on that node "+
			"instead of reversing the arrow: MATCH (other)-[%s]->(target {key:'...'}) ... SET %s.<key> = <value>",
		utils.ErrValidation,
		t.Variable, t.Direction, skippedLegOf(t.Direction),
		t.Variable, t.Variable, t.Variable, t.Variable,
	)
}

// skippedLegOf names the edges the engine would drop, for the message
// validateRelationshipWriteDirection builds. An incoming pattern drops every
// edge it matches; an undirected pattern drops only the ones it reaches against
// the stored arrow, which is the incoming half of the traversal.
func skippedLegOf(d cypherguard.Direction) string {
	if d == cypherguard.DirectionUndirected {
		return "the incoming half of that traversal"
	}
	return "the incoming direction"
}

// validateQueryEncoding rejects a query carrying a byte that begins no valid
// UTF-8 sequence, on EVERY graph subcommand (SPEC/GRAPH.md § Cypher Query and
// Property Value Content Rules; SPEC/MODELS.md § Free-Text UTF-8 Encoding
// Constraint, which defines the rule).
//
// It takes no subcommand filter because the rule has none. The engine decodes
// the query to runes before its grammar runs and replaces every byte that
// decodes to no character with U+FFFD, so the statement it executes is not the
// statement the caller wrote — a fact about the QUERY, indifferent to what the
// statement then does. `create` and `update` store a value that was never
// supplied; `query` and `search` compare against a literal that was never
// supplied and report success having matched nothing; and `delete` gated by such
// a literal removes nothing and still reports success. That last one is the
// worst of the three and is why the rule is not confined to the writers: a
// destructive command reporting success having removed nothing is the failure
// shape the caller has no reason to check.
//
// The refusal names the byte and its offset rather than echoing any of the
// query, because the bytes at fault are exactly the ones that must not be
// emitted. Where the byte falls inside a value the query WRITES, it also names
// the property; where it does not — which is always the case for a subcommand
// that writes none — it says so, rather than withholding the naming in silence.
func validateQueryEncoding(subcmd, query string) error {
	r, refused := cypherguard.RefusedQueryEncoding(query)
	if !refused {
		return nil
	}

	const cause = ". The byte %#02x at offset %d of the query begins no valid UTF-8 sequence, " +
		"and the engine replaces every such byte with U+FFFD before it parses the query, so %s"
	if r.Attributed() {
		return fmt.Errorf(
			"%w: graph %s cannot write property %q: %s"+cause+". Supply the query as well-formed UTF-8",
			utils.ErrValidation, subcmd, r.Property, r.Violation.Reason(),
			r.Byte, r.Offset, encodingConsequenceOf(subcmd))
	}
	return fmt.Errorf(
		"%w: graph %s cannot run this query: %s"+cause+". %s. Supply the query as well-formed UTF-8",
		utils.ErrValidation, subcmd, r.Violation.Reason(),
		r.Byte, r.Offset, encodingConsequenceOf(subcmd), unattributedReasonOf(subcmd))
}

// encodingConsequenceOf names what the engine would do with the rewritten
// statement, for the message validateQueryEncoding builds. The consequence is
// what makes the objection concrete, and it differs by subcommand even though
// the rule does not.
func encodingConsequenceOf(subcmd string) string {
	switch subcmd {
	case "delete":
		return "the statement would match on a literal that was never supplied and would report " +
			"success having deleted nothing"
	case "query", "search":
		return "the statement would match on a literal that was never supplied and would report " +
			"success having found nothing"
	default:
		return "the store would hold a value different from the one supplied while the command " +
			"still reported success"
	}
}

// unattributedReasonOf explains why no property is named, in the terms that are
// true for the subcommand at hand. For a subcommand that writes no property
// value there is simply none to name, and saying so is more useful than the
// writer's explanation, which would imply the query had written values the byte
// merely missed.
func unattributedReasonOf(subcmd string) string {
	if writesPropertyValues(subcmd) {
		return "No property value could be attributed to that byte: it falls outside the values " +
			"this query writes, or the query does not parse"
	}
	return "This subcommand writes no property value, so there is no property to name: the byte " +
		"corrupts the literal the query matches on"
}

// writesPropertyValues reports whether subcmd is one of the two that write
// property values, and therefore whether the control-character rule reaches it.
// It is one predicate rather than a condition spelled at each site, so the two
// rules cannot drift apart on which subcommands write.
func writesPropertyValues(subcmd string) bool {
	return subcmd == "create" || subcmd == "update"
}

// validateWrittenPropertyValues rejects a `graph create` or `graph update` query
// that would write a property value carrying a forbidden control character
// (SPEC/GRAPH.md § Cypher Query and Property Value Content Rules;
// SPEC/MODELS.md § Free-Text Control-Character Constraint, which defines it).
//
// # Why this rule stops where validateQueryEncoding does not
//
// The asymmetry is deliberate. The encoding rule objects to a query the engine
// would silently REWRITE, which is a fact about the statement. This one objects
// to a value that would be STORED, and only a write stores one.
//
// The substantive reason is about READS. A control character in a read literal
// is compared against what the graph already holds, and the store can
// legitimately hold one — everything written before this rule existed, and
// anything a computed expression produces, which is outside what any of this can
// see. Refusing such a read would leave that data unreadable rather than merely
// unwritable, which is a loss of reach the rule was never meant to impose.
// `graph delete` is on the same side: it removes nodes and edges, it stores no
// value, and a predicate that names a control character is how an operator
// reaches the entry that carries one.
//
// # The subcommand filter is a SECOND line, and is deliberately redundant today
//
// cypherguard.RefusedWrittenPropertyValue already walks only the positions a
// query WRITES, so on a read or a delete it finds nothing and would refuse
// nothing even without the filter below — the clause-class guard rail admits no
// CREATE/MERGE/SET to any of those three subcommands in the first place. Removing
// the filter would therefore change no behaviour today, which is exactly why it
// is kept and why that is stated here rather than left for someone to discover:
// it puts the boundary on record as a DECISION at the site that enforces it,
// instead of leaving it as a consequence of two other rules that could each
// change on their own. writesPropertyValues is pinned directly by
// TestWritesPropertyValuesNamesOnlyTheTwoWritingSubcommands, so it cannot be
// deleted as dead code without that test being confronted.
//
// The refusal NAMES the offending value by the property it is assigned to, and
// says which rule it breaks in the words every other value in the application is
// refused with — the wording comes from internal/utils, which owns it, rather
// than being spelled again here. The offending bytes are never echoed: the
// refusal names the CODE POINT, which is bounded and safe to print, because
// printing the character itself would emit it into the terminal the rule exists
// to protect.
//
// The caller applies validateQueryEncoding FIRST. That is the order
// SPEC/MODELS.md fixes for the pair and it is not a preference: an invalid byte
// decodes to U+FFFD, which is not a forbidden code point, so this rule would
// answer "fine" for a value the encoding rule refuses.
func validateWrittenPropertyValues(subcmd, query string) error {
	if !writesPropertyValues(subcmd) {
		return nil
	}
	r, refused := cypherguard.RefusedWrittenPropertyValue(query)
	if !refused {
		return nil
	}
	return fmt.Errorf(
		"%w: graph %s cannot write property %q: %s. The value carries %s, which the store would "+
			"hold verbatim and every surface that renders it would carry unchanged - the terminal "+
			"escape-sequence injection (CWE-150) and Trojan Source (CVE-2021-42574) exposure the "+
			"free-text rules close for every other value Groadmap stores, and a Cypher property "+
			"value is subject to the same two rules. Note that the query text alone does not show "+
			"it: Cypher decodes \\b, \\f and \\uXXXX inside a string literal, so a value written "+
			"with an escape carries the character even though the query is pure ASCII. Remove the "+
			"character from the value",
		utils.ErrValidation, subcmd, r.Property, r.Violation.Reason(), r.CodePoint)
}

// validateRelationshipReadDirection rejects a query that READS the value of a
// relationship variable bound by a pattern the engine does not resolve reliably
// (SPEC/GRAPH.md § Relationship Read Direction).
//
// It is the read-side counterpart of validateRelationshipWriteDirection above,
// and the two share one doctrine: whether an undirected or incoming pattern
// behaves correctly depends on the data it meets, not on the query, and that
// cannot be the guarantee. The engine recovers a bound relationship's type and
// endpoints by probing the stored topology, so on a node pair carrying edges in
// BOTH directions the reverse leg of the traversal is hydrated from the forward
// pair — reporting the wrong type, the reversed orientation, dropping rows whose
// WHERE predicate reads that type, and persisting the wrong value when a node
// write derives from it.
//
// It applies to EVERY graph subcommand, because the corrupted value is harmful
// wherever it is read: `query` and `search` deliver it to the caller, `update`'s
// SET right-hand side persists it, and a `delete` whose WHERE predicate reads it
// removes the wrong edges — or, as measured, none at all while reporting
// success.
//
// The exemption is of the DELETE CLAUSE, not of the delete COMMAND. A bare
// `DELETE e` names the relationship as a delete target rather than as an
// expression, and the engine resolves that edge itself rather than through the
// endpoint columns, so it removes the right one and stays accepted. The moment a
// predicate over `type(e)` decides WHICH edges are deleted, the engine evaluates
// the corrupted type, drops the row, and the destructive command reports
// `{"ok": true}` having removed nothing — the worst symptom in this family,
// because the caller has no reason to check. That case is an ordinary expression
// use and is refused like any other; cypherguard draws the line by clause.
//
// Detection runs on the parsed query rather than on the masked text, so a
// relationship arrow inside a string literal or a comment cannot trip it, and
// the refusal happens before the graph store is opened.
func validateRelationshipReadDirection(subcmd, query string) error {
	misread := cypherguard.MisreadRelationshipReferences(query)
	if len(misread) == 0 {
		return nil
	}
	v := misread[0].Variable
	return fmt.Errorf(
		"%w: graph %s cannot read relationship %q: it is bound by an %s pattern, and the engine "+
			"resolves a relationship's type and endpoints by probing the stored direction, so on a "+
			"node pair that carries edges in BOTH directions it reports the forward edge's type and "+
			"orientation for the reverse one: type(%s) names the wrong relationship, "+
			"startNode(%s)/endNode(%s) reverse it, and a predicate over either silently drops the row. "+
			"Rewrite the traversal as outgoing, which resolves correctly whatever the data: anchor it "+
			"on the source with MATCH (source)-[%s]->(target) ... RETURN type(%s), or, to reach the "+
			"edges arriving AT a node, on that node with MATCH (other)-[%s]->(target {key:'...'}) ... "+
			"RETURN type(%s) - do not reverse the arrow. To cover both directions in one read, take "+
			"the union of the two outgoing legs: MATCH (a {key:'...'})-[%s]->(x) RETURN type(%s) AS t, "+
			"x.key AS k UNION ALL MATCH (x)-[%s]->(a {key:'...'}) RETURN type(%s) AS t, x.key AS k",
		utils.ErrValidation, subcmd, v, misread[0].Direction,
		v, v, v, v, v, v, v, v, v, v, v,
	)
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

// graphReadOpts carries the recovery.Options value used for every
// graph store open. Defined once to avoid repeating the codec wiring.
var graphReadOpts = recovery.Options[string, float64]{
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
// (query and search). It opens the store under the shared store lock, releases
// that lock the moment the open returns, then runs the query against the
// in-memory graph the open produced and serialises the result.
//
// The lock is taken because opening the store is not a read-only operation on
// disk: recovery repairs an interrupted checkpoint before it loads anything, so
// an unlocked read could delete or race the staging directory a concurrent
// writer is publishing its snapshot from (SPEC/GRAPH.md § What a Read Changes on
// Disk). It is released at the open because that is the whole of what a read
// touches on disk — see graphlock's package comment for the anti-widening
// clause that governs this.
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

	// Refused before the store is opened, like the clause-class guard rail and
	// its write-side sibling, so a rejected query never reaches the graph
	// (SPEC/GRAPH.md § Relationship Read Direction).
	if err := validateRelationshipReadDirection(subcmd, query); err != nil {
		return err
	}

	// The encoding rule, which binds the reading subcommands exactly as it binds
	// the writing ones (SPEC/GRAPH.md § Cypher Query and Property Value Content
	// Rules). A byte the engine replaces with U+FFFD changes the statement, not
	// only a value it stores: the literal this query matches on is not the
	// literal supplied, so the row that should have matched does not and the
	// command reports success having found nothing.
	//
	// The control-character rule is deliberately NOT applied here. It governs
	// values that are STORED, and a read stores none; refusing a read that names
	// a control character would deny reach to data the store legitimately holds.
	// See validateWrittenPropertyValues for the whole of that reasoning.
	if err := validateQueryEncoding(subcmd, query); err != nil {
		return err
	}

	graphDir, err := openGraphStore(roadmapName)
	if err != nil {
		return err
	}

	// Shared lock, held across the store open ALONE. Released with an explicit
	// call rather than a defer, on both the success and the failure path, so
	// the hold cannot be silently widened to the query by a later edit.
	releaseLock, err := graphlock.AcquireShared(graphDir)
	if err != nil {
		return err
	}
	res, openErr := recovery.Open[string, float64](graphDir, graphReadOpts)
	releaseLock()
	if openErr != nil {
		return fmt.Errorf("%w: graph store unavailable: %v", utils.ErrDatabase, openErr)
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

	// Surface any advisory notifications attached to the result as stderr
	// diagnostics. The result is still open here (the deferred Close runs at
	// return), so its notifications are available. Notifications never change
	// the stdout success output or the exit code (SPEC FR10).
	printGraphNotifications(result)

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
	_ = os.Chmod(snapDir, 0700) // #nosec G302 -- 0700 on a DIRECTORY is mandated by SPEC (CLAUDE.md §10: 0700 for the ~/.roadmaps tree); gosec G302 false-positives on directory permissions
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

	// Refused before the store is opened, like the clause-class guard rail, so
	// a rejected query never reaches the graph (SPEC/GRAPH.md
	// § Relationship Write Direction).
	if err := validateRelationshipWriteDirection(subcmd, query); err != nil {
		return err
	}

	// The write-direction rule owns the SET/REMOVE TARGET; this one owns every
	// relationship VALUE the statement reads — a SET right-hand side such as
	// `SET n.p = type(e)`, which would otherwise persist a misresolved type, and
	// a DELETE gated by `WHERE type(e) = ...`, which would otherwise delete the
	// wrong edges or silently none. Ordered after the write rule so a query that
	// trips both keeps the write-side message, which names the write the caller
	// actually asked for.
	if err := validateRelationshipReadDirection(subcmd, query); err != nil {
		return err
	}

	// Content, decided last among the guard-rail rules and still before the store
	// is opened, so a refused query writes nothing. It is last because the
	// objections above are about what the query IS - its clause class, and the
	// orientation of the patterns it binds - while these are about what the query
	// or a value it carries CONTAINS, which only matters once the statement is
	// otherwise one this subcommand would run (SPEC/GRAPH.md § Cypher Query and
	// Property Value Content Rules).
	//
	// The two calls are in the order SPEC/MODELS.md fixes for the pair, and the
	// order is load-bearing: an invalid byte decodes to U+FFFD, which is not a
	// forbidden code point, so the control-character rule would answer "fine" for
	// a value the encoding rule refuses. They are two calls rather than one
	// because their REACH differs - the encoding rule binds all three write
	// subcommands including `delete`, the control-character rule only the two
	// that write property values - and separate calls put each reach where it is
	// enforced instead of inside a shared helper.
	if err := validateQueryEncoding(subcmd, query); err != nil {
		return err
	}
	if err := validateWrittenPropertyValues(subcmd, query); err != nil {
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
