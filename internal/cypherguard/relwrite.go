package cypherguard

// relwrite.go — the relationship-write direction check (SPEC/GRAPH.md
// § Relationship Write Direction).
//
// # The defect this exists to refuse
//
// GoGraph's read path and its write path disagree about which endpoint pair a
// relationship bound by a reverse traversal belongs to, and the disagreement is
// silent.
//
// [exec.Expand] emits the triplet (srcCol, edgeCol, dstCol) in PATTERN order:
// for the reverse leg of a traversal its advanceRevEdge returns the anchor node
// as src and the stored SOURCE as dst, so the columns describe the edge the way
// the pattern walked it, not the way storage holds it. The read path knows this
// and corrects for it — buildRelationshipValueFromRow probes
// `!HasEdge(src,dst) && HasEdge(dst,src)` and swaps — so `RETURN type(e)` and
// `startNode(e)` report the true edge. The write path does not: SetProperty's
// resolveRelBinding takes those same two columns verbatim as the endpoint pair
// and calls SetEdgeProperty(src, dst), which lands on a pair that has no edge.
// lpg's setEdgePropertyInfo answers a missing pair with `return nil` — a
// documented no-op — so nothing is written, nothing is reported, and the
// statement still commits.
//
// The engine's own write-effect counters do not help: a reverse-leg SET that
// wrote nothing still reports PropertiesSet=1, because the counter is
// incremented by the operator, above the storage layer that dropped the write.
// So there is no post-hoc signal to test, and the condition must be refused
// before execution or not at all.
//
// # Why refusal, and why keyed on the pattern's direction
//
// Only an OUTGOING relationship pattern is written correctly. An incoming
// pattern (`<-[e]-`) never writes. An undirected pattern (`-[e]-`) writes on the
// rows whose edge happens to run forwards and silently skips the rest, which is
// the worst of the three: it reports success having applied the change to some
// of the matched edges. The project's documented provenance idiom
// (`MATCH (n {key:…})-[e]-(x) SET e.last_commit = …`) is exactly that shape, so
// the partial case is the common one rather than the corner.
//
// Refusal costs nothing in reach: every edge remains writable through an
// outgoing pattern anchored on EITHER endpoint, so an incoming edge is stamped
// by `MATCH (x)-[e]->(n {key:…}) SET …` rather than by reversing the arrow.
// Nothing becomes unreachable; the operator writes the traversal the engine
// actually honours.
//
// A `WITH <relvar>` between the MATCH and the SET also happens to repair the
// write today, because the projection it forces rebuilds a direction-corrected
// RelationshipValue that SetProperty resolves by endpoints rather than by
// columns. It is deliberately NOT exempted here. That behaviour is an
// unspecified consequence of projection materialisation — the engine elides
// projections it judges unnecessary — so exempting it would carve a hole that
// fails OPEN the day the elision widens, reintroducing the silent no-op through
// the one shape this check had blessed. The direction of the pattern is the
// stable fact, and it is the only one this check reads.

import (
	"github.com/FlavioCFOliveira/GoGraph/cypher/ast"
	"github.com/FlavioCFOliveira/GoGraph/cypher/parser"
)

// Direction names the orientation of the relationship pattern that bound a
// variable, in the words the operator wrote it.
type Direction string

const (
	// DirectionOutgoing is `-[e]->`, the only orientation the engine writes.
	DirectionOutgoing Direction = "outgoing"
	// DirectionIncoming is `<-[e]-`.
	DirectionIncoming Direction = "incoming"
	// DirectionUndirected is `-[e]-`.
	DirectionUndirected Direction = "undirected"
)

// RelWriteTarget is one relationship variable that a SET or REMOVE clause
// targets through a pattern the engine does not write.
type RelWriteTarget struct {
	// Variable is the relationship variable named in the pattern and in the
	// SET/REMOVE item, for example "e" in `MATCH (b)-[e]-(x) SET e.k = 1`.
	Variable string
	// Direction is the orientation of the binding pattern: either
	// DirectionIncoming or DirectionUndirected. DirectionOutgoing is never
	// reported, because it is the writable case.
	Direction Direction
}

// UnwritableRelationshipTargets reports every relationship variable that a SET
// or REMOVE clause in query targets while being bound by a pattern whose
// direction the engine does not write — an incoming (`<-[e]-`) or undirected
// (`-[e]-`) relationship pattern. The returned slice is in the order the
// SET/REMOVE items name the variables, deduplicated, and empty when the query
// is safe.
//
// Detection uses the engine's own parser rather than a pattern-matching
// approximation over the query text, so the directions it reads are exactly the
// directions the engine will plan. A query the parser rejects yields no targets:
// it cannot execute either, and letting it through means the engine reports the
// syntax error itself instead of this check masking it with a different one.
//
// Only the SET/REMOVE TARGET is inspected. A relationship variable that a query
// merely reads — `MATCH (b)-[e]-(x) SET x.seen = true`, or a right-hand side
// like `SET n.t = type(e)` — is not reported, because the write lands on a node
// and nodes are resolved by identifier, not by endpoint pair.
func UnwritableRelationshipTargets(query string) []RelWriteTarget {
	parsed, err := parser.Parse(query)
	if err != nil {
		return nil
	}

	bindings := make(map[string]Direction)
	var targets []string
	collectQuery(parsed, bindings, &targets)

	var out []RelWriteTarget
	seen := make(map[string]struct{}, len(targets))
	for _, name := range targets {
		if _, dup := seen[name]; dup {
			continue
		}
		dir, bound := bindings[name]
		if !bound || dir == DirectionOutgoing {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, RelWriteTarget{Variable: name, Direction: dir})
	}
	return out
}

// collectQuery walks every branch of a query, recording relationship-variable
// bindings in bindings and SET/REMOVE target variables in targets.
func collectQuery(q ast.Query, bindings map[string]Direction, targets *[]string) {
	switch t := q.(type) {
	case *ast.SingleQuery:
		collectSingleQuery(t, bindings, targets)
	case *ast.MultiQuery:
		// A UNION: each branch carries its own scope, but a variable name can
		// only be unwritable in the branch that binds it that way, and the
		// refusal is per name. Walking every branch into the same maps is
		// therefore sound and cannot miss a branch.
		for _, part := range t.Parts {
			collectSingleQuery(part, bindings, targets)
		}
	}
}

// collectSingleQuery walks one query part's reading, WITH and updating clauses.
func collectSingleQuery(q *ast.SingleQuery, bindings map[string]Direction, targets *[]string) {
	if q == nil {
		return
	}
	for _, rc := range q.ReadingClauses {
		collectClause(rc, bindings, targets)
	}
	// For a parser-built multi-part query the WITH clauses are already embedded
	// in ReadingClauses in document order; q.With repeats them only for an
	// AST built by hand. Walking both is harmless — a binding recorded twice is
	// the same binding — and walking only one would miss the other shape.
	for _, w := range q.With {
		collectClause(w, bindings, targets)
	}
	for _, uc := range q.UpdatingClauses {
		collectClause(uc, bindings, targets)
	}
}

// collectClause dispatches one clause, recursing into FOREACH bodies.
func collectClause(c ast.Clause, bindings map[string]Direction, targets *[]string) {
	switch t := c.(type) {
	case *ast.Match:
		collectPattern(t.Pattern, bindings)
	case *ast.OptionalMatch:
		collectPattern(t.Pattern, bindings)
	case *ast.Create:
		collectPattern(t.Pattern, bindings)
	case *ast.Merge:
		collectPath(t.Pattern, bindings)
	case *ast.Set:
		for _, item := range t.Items {
			if name := rootVariable(item.Target); name != "" {
				*targets = append(*targets, name)
			}
		}
	case *ast.Remove:
		for _, item := range t.Items {
			if name := rootVariable(item.Target); name != "" {
				*targets = append(*targets, name)
			}
		}
	case *ast.Foreach:
		// FOREACH (x IN list | SET e.k = 1) writes through the same operator,
		// so its body must be inspected exactly like a top-level SET.
		for _, body := range t.Body {
			collectClause(body, bindings, targets)
		}
	}
}

// collectPattern records the direction of every named relationship in pattern.
//
// When a name is bound more than once, a non-outgoing binding wins over an
// outgoing one: any binding the engine does not write is enough to make the
// write unreliable, so the check reports the orientation that fails.
func collectPattern(pattern *ast.Pattern, bindings map[string]Direction) {
	if pattern == nil {
		return
	}
	for _, path := range pattern.Paths {
		collectPath(path, bindings)
	}
}

// collectPath records the direction of every named relationship along one path.
func collectPath(path *ast.PathPattern, bindings map[string]Direction) {
	if path == nil {
		return
	}
	for el := path.Head; el != nil; el = el.Next {
		rel := el.Relationship
		if rel == nil || rel.Variable == nil {
			continue
		}
		name := *rel.Variable
		if prev, ok := bindings[name]; ok && prev != DirectionOutgoing {
			continue // already recorded as unwritable; keep the failing one
		}
		bindings[name] = directionOf(rel.Direction)
	}
}

// directionOf maps the AST's relationship direction onto the operator-facing
// vocabulary. ast.RelDirectionNone prints as "none", which describes the AST
// rather than the query, so it is renamed to the word the pattern means.
func directionOf(d ast.RelDirection) Direction {
	switch d {
	case ast.RelDirectionOutgoing:
		return DirectionOutgoing
	case ast.RelDirectionIncoming:
		return DirectionIncoming
	default:
		return DirectionUndirected
	}
}

// rootVariable returns the name of the variable a SET/REMOVE target ultimately
// reads, unwrapping property access so `e.last_commit` yields "e" and a bare
// `e` (from `SET e = {…}`, `SET e += {…}`, or `REMOVE e:Label`) yields "e".
// It returns the empty string for any other expression shape, which the caller
// treats as "no variable to check".
func rootVariable(e ast.Expression) string {
	for {
		switch t := e.(type) {
		case *ast.Variable:
			return t.Name
		case *ast.Property:
			e = t.Receiver
		default:
			return ""
		}
	}
}
