package cypherguard

// relread.go — the relationship-read direction check (SPEC/GRAPH.md
// § Relationship Read Direction).
//
// # The defect this exists to refuse
//
// GoGraph reconstructs a bound relationship's TYPE and ENDPOINTS by probing the
// stored topology rather than by being told which way the traversal walked.
// buildRelationshipValueFromRow receives the (srcCol, edgeCol, dstCol) triplet
// in PATTERN order and recovers the storage direction with a fallback probe:
// `if !HasEdge(src,dst) && HasEdge(dst,src) then swap`. The probe is right
// whenever the pair carries an edge one way only. On a pair that carries edges
// BOTH ways the first conjunct is false, no swap fires, and the reverse leg of
// the traversal is hydrated from the FORWARD pair — reporting the forward
// edge's type and the pattern's orientation for an edge that is stored the
// other way round.
//
// The consequences are not confined to a mislabelled column, which is why this
// check is keyed on any expression use rather than on projection alone:
//
//   - `RETURN type(e)` names the wrong relationship, and the reverse edge's own
//     type never appears in the result at all.
//   - `RETURN startNode(e).key, endNode(e).key` reports the pattern's
//     orientation, the exact reverse of what storage holds.
//   - `WHERE type(e) = '…'` is evaluated by the engine against the corrupted
//     value, so a row that genuinely matches is silently DROPPED. Nothing
//     reaches the caller to be inspected.
//   - `SET n.p = type(e)` persists the corrupted value to disk while the
//     statement reports success, so the defect escapes the read path entirely.
//
// # Why refusal, and why it cannot be a correction
//
// Correcting the value in Groadmap's own result assembly was measured first and
// is impossible. For `type(e)` and `startNode(e).key` the consumer receives a
// bare expr.StringValue carrying no relationship identity, so there is nothing
// to correct from; only the whole-value `RETURN e` form still carries the edge
// handle. Correcting that form alone would make a single row contradict itself
// — `RETURN e, type(e)` draws both from the same corrupted source — and would
// still leave the dropped-row and persisted-value cases untouched.
//
// # Why the pattern's direction, and not the data
//
// This mirrors the Relationship Write Direction rule in relwrite.go, and rests
// on the same doctrine: whether an undirected or incoming pattern behaves
// correctly depends on the data it meets, not on the query, and that cannot be
// the guarantee. A read that is right today becomes wrong the moment a second
// edge is added between the same two nodes, with no change to the query and no
// diagnostic at the point of failure. The direction of the pattern is the
// stable fact, and it is the only one this check reads.
//
// Refusal costs no reach. Every edge remains readable through an outgoing
// pattern anchored on EITHER endpoint, and an undirected traversal is recovered
// exactly by the union of its two outgoing legs — both verified to report the
// true stored type and orientation on a pair that carries edges both ways.
//
// # What is deliberately NOT refused
//
// Only the shapes that are actually misresolved are refused:
//
//   - An OUTGOING pattern is always resolved correctly, whatever the data.
//   - An ANONYMOUS relationship (`(a)-[:TYPE]-(b)`, no variable) never has a
//     relationship value built for it, so nothing can be misreported.
//   - A VARIABLE-LENGTH relationship (`-[e*1..2]-`) is hydrated by
//     resolveHopRel, which is TOLD the traversal direction instead of probing
//     for it, and reports the true type and orientation on a two-way pair.
//   - A named PATH variable (`MATCH p=(a)-[e]-(x) RETURN p`) is rendered
//     through that same correct resolver, so returning the path is safe; only a
//     direct use of the relationship variable is not.
//   - A bare `DELETE e` names the relationship as a delete TARGET rather than as
//     an expression. The engine resolves that edge itself rather than through the
//     endpoint columns and removes the right one, so a DELETE item is not an
//     expression use. The exemption is of the CLAUSE, not of the delete command:
//     a `WHERE type(e) = '...'` that decides WHICH edges are deleted is an
//     ordinary expression use, is refused, and must be — the engine evaluates the
//     corrupted type, drops the row, and the destructive statement reports
//     success having removed nothing.
//   - A SET/REMOVE TARGET is owned by the Relationship Write Direction rule and
//     is not reported twice.

import (
	"github.com/FlavioCFOliveira/GoGraph/cypher/ast"
	"github.com/FlavioCFOliveira/GoGraph/cypher/parser"
)

// RelReadReference is one relationship variable whose VALUE a query reads while
// it is bound by a pattern the engine does not resolve reliably.
type RelReadReference struct {
	// Variable is the relationship variable named in the pattern and read by
	// the expression, for example "e" in `MATCH (a)-[e]-(x) RETURN type(e)`.
	Variable string
	// Direction is the orientation of the binding pattern: either
	// DirectionIncoming or DirectionUndirected. DirectionOutgoing is never
	// reported, because it is the case the engine resolves correctly.
	Direction Direction
}

// MisreadRelationshipReferences reports every relationship variable that query
// reads in an expression while being bound by an incoming (`<-[e]-`) or
// undirected (`-[e]-`) fixed-length relationship pattern. The returned slice is
// in the order the query first reads the variables, deduplicated, and empty
// when the query is safe.
//
// An expression use is any read of the variable's VALUE: a projection in
// RETURN or WITH (including a `RETURN *` star projection, which projects it), a
// function such as type(e) or startNode(e), a property access e.key, a WHERE
// predicate, an ORDER BY / SKIP / LIMIT expression, or the right-hand side of a
// SET. A binding occurrence in the pattern itself is not a use, nor is a
// SET/REMOVE target, nor a DELETE item — but a WHERE predicate that gates a
// DELETE is, so a bare `DELETE e` is admitted while
// `WHERE type(e) = '...' DELETE e` is not.
//
// Detection uses the engine's own parser rather than a pattern-matching
// approximation over the query text, so the directions it reads are exactly the
// directions the engine will plan, and an arrow inside a string literal or a
// comment cannot trip it. A query the parser rejects yields no references: it
// cannot execute either, and letting it through means the engine reports the
// syntax error itself instead of this check masking it with a different one.
func MisreadRelationshipReferences(query string) []RelReadReference {
	parsed, err := parser.Parse(query)
	if err != nil {
		return nil
	}

	var parts []*ast.SingleQuery
	switch t := parsed.(type) {
	case *ast.SingleQuery:
		parts = []*ast.SingleQuery{t}
	case *ast.MultiQuery:
		parts = t.Parts
	default:
		return nil
	}

	var out []RelReadReference
	seen := make(map[string]struct{})
	for _, part := range parts {
		// Each UNION branch carries its own scope, so it is scanned with its
		// own binding and use sets. Sharing them across branches would let a
		// `RETURN *` in one branch report a variable bound in another, which
		// the engine keeps apart.
		s := &readScan{bindings: make(map[string]Direction), useSeen: make(map[string]struct{})}
		s.singleQuery(part)
		for _, name := range s.resolveUses() {
			if _, dup := seen[name]; dup {
				continue
			}
			dir, bound := s.bindings[name]
			if !bound || dir == DirectionOutgoing {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, RelReadReference{Variable: name, Direction: dir})
		}
	}
	return out
}

// readScan accumulates, for one query branch, the relationship variables bound
// by a fixed-length pattern and the variables read by an expression.
type readScan struct {
	bindings  map[string]Direction
	useSeen   map[string]struct{}
	bindOrder []string
	uses      []string
	// star records that the branch's final RETURN is a `RETURN *` projection,
	// which projects every bound variable — including relationship variables
	// that no expression names explicitly.
	star bool
}

// resolveUses returns the variables this branch reads, in first-read order,
// expanding a `RETURN *` star projection into every relationship variable the
// branch bound. A `WITH *` is deliberately not expanded: it carries bindings
// forward without delivering a value to the caller, and any later use of one is
// recorded on its own merits.
func (s *readScan) resolveUses() []string {
	if !s.star {
		return s.uses
	}
	out := s.uses
	for _, name := range s.bindOrder {
		if _, dup := s.useSeen[name]; dup {
			continue
		}
		s.useSeen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// bind records the direction of one named relationship pattern.
//
// When a name is bound more than once, a non-outgoing binding wins over an
// outgoing one: any binding the engine misresolves is enough to make the read
// unreliable, so the check reports the orientation that fails.
func (s *readScan) bind(name string, dir Direction) {
	if prev, ok := s.bindings[name]; ok {
		if prev != DirectionOutgoing {
			return
		}
		s.bindings[name] = dir
		return
	}
	s.bindings[name] = dir
	s.bindOrder = append(s.bindOrder, name)
}

// addUse records that an expression reads name, keeping first-read order.
func (s *readScan) addUse(name string) {
	if _, dup := s.useSeen[name]; dup {
		return
	}
	s.useSeen[name] = struct{}{}
	s.uses = append(s.uses, name)
}

// singleQuery walks one query branch's reading, WITH, updating and RETURN
// clauses.
func (s *readScan) singleQuery(q *ast.SingleQuery) {
	if q == nil {
		return
	}
	for _, rc := range q.ReadingClauses {
		s.clause(rc)
	}
	// For a parser-built multi-part query the WITH clauses are already embedded
	// in ReadingClauses in document order; q.With repeats them only for an AST
	// built by hand. Walking both is harmless — a binding or use recorded twice
	// is the same one — and walking only one would miss the other shape.
	for _, w := range q.With {
		s.clause(w)
	}
	for _, uc := range q.UpdatingClauses {
		s.clause(uc)
	}
	if q.Return != nil {
		s.projection(q.Return.Projection, true)
	}
}

// clause dispatches one clause, binding the relationships its patterns name and
// recording the variables its expressions read.
func (s *readScan) clause(c ast.Clause) {
	switch t := c.(type) {
	case *ast.Match:
		s.pattern(t.Pattern)
		s.where(t.Where)
	case *ast.OptionalMatch:
		s.pattern(t.Pattern)
		s.where(t.Where)
	case *ast.Create:
		s.pattern(t.Pattern)
	case *ast.Merge:
		s.path(t.Pattern)
		for _, item := range t.OnCreate {
			s.setItem(item)
		}
		for _, item := range t.OnMatch {
			s.setItem(item)
		}
	case *ast.With:
		s.projection(t.Projection, false)
		s.where(t.Where)
	case *ast.Unwind:
		s.expr(t.Expr)
	case *ast.Call:
		for _, arg := range t.Args {
			s.expr(arg)
		}
		s.where(t.Where)
	case *ast.Set:
		for _, item := range t.Items {
			s.setItem(item)
		}
	case *ast.Foreach:
		s.expr(t.Expr)
		for _, body := range t.Body {
			s.clause(body)
		}
	case *ast.Remove:
		// REMOVE names only a TARGET, which the Relationship Write Direction
		// rule owns. Reporting it here too would refuse the same query twice
		// with two different messages.
	case *ast.Delete:
		// A DELETE item names the relationship as a TARGET, not as a value: the
		// engine resolves that edge itself rather than through the endpoint
		// columns, so a bare `DELETE e` removes the right one and is admitted.
		//
		// This exempts the CLAUSE, not the command. The WHERE predicate that may
		// gate the same statement is walked by the Match/OptionalMatch case above
		// like any other, so `WHERE type(e) = '...' DELETE e` is refused — which
		// it must be, because there the corrupted type decides WHICH edges are
		// deleted and the statement reports success having removed nothing.
	}
}

// setItem records the right-hand side of a SET assignment as a read. The TARGET
// is deliberately skipped: writing a relationship through a reverse pattern is
// the Relationship Write Direction rule's subject, and reporting it here as
// well would refuse one query under two contracts.
func (s *readScan) setItem(item *ast.SetItem) {
	if item == nil {
		return
	}
	s.expr(item.Value)
}

// where records the reads in a WHERE predicate.
func (s *readScan) where(w *ast.Where) {
	if w != nil {
		s.expr(w.Predicate)
	}
}

// projection records the reads in a RETURN or WITH projection, including its
// ORDER BY, SKIP and LIMIT expressions. isReturn marks the branch's terminal
// RETURN, the only projection whose `*` form delivers values to the caller.
func (s *readScan) projection(p *ast.Projection, isReturn bool) {
	if p == nil {
		return
	}
	if p.All && isReturn {
		s.star = true
	}
	for _, item := range p.Items {
		if item != nil {
			s.expr(item.Expr)
		}
	}
	for _, sort := range p.OrderBy {
		if sort != nil {
			s.expr(sort.Expr)
		}
	}
	s.expr(p.Skip)
	s.expr(p.Limit)
}

// pattern binds every named relationship in a comma-separated pattern.
func (s *readScan) pattern(p *ast.Pattern) {
	if p == nil {
		return
	}
	for _, path := range p.Paths {
		s.path(path)
	}
}

// path binds every named relationship along one path, and records the reads in
// any inline property maps the path carries.
func (s *readScan) path(path *ast.PathPattern) {
	if path == nil {
		return
	}
	namedRelationships(path, func(name string, rel *ast.RelationshipPattern) {
		// A variable-length relationship is hydrated by the resolver that is
		// TOLD the traversal direction, not by the probing one, and reports the
		// true type and orientation even on a two-way pair. Binding it here
		// would refuse a shape that works.
		if rel.Range != nil {
			return
		}
		s.bind(name, directionOf(rel.Direction))
	})
	for el := path.Head; el != nil; el = el.Next {
		if el.Node != nil {
			s.expr(el.Node.Properties)
		}
		if el.Relationship != nil {
			s.expr(el.Relationship.Properties)
		}
	}
}

// expr records every variable an expression reads, and binds the relationships
// named by any pattern nested inside it.
func (s *readScan) expr(e ast.Expression) {
	if e == nil {
		return
	}
	switch t := e.(type) {
	case *ast.Variable:
		s.addUse(t.Name)
	case *ast.Property:
		s.expr(t.Receiver)
	case *ast.FunctionInvocation:
		for _, arg := range t.Args {
			s.expr(arg)
		}
	case *ast.BinaryOp:
		s.expr(t.Left)
		s.expr(t.Right)
	case *ast.UnaryOp:
		s.expr(t.Operand)
	case *ast.LabelPredicate:
		s.expr(t.Receiver)
	case *ast.CaseExpression:
		s.expr(t.Subject)
		s.expr(t.ElseExpr)
		for _, alt := range t.Alternatives {
			if alt != nil {
				s.expr(alt.Condition)
				s.expr(alt.Consequent)
			}
		}
	case *ast.ListComprehension:
		s.expr(t.Source)
		s.expr(t.Predicate)
		s.expr(t.Projection)
	case *ast.PatternComprehension:
		s.path(t.Pattern)
		s.expr(t.Predicate)
		s.expr(t.Projection)
	case *ast.MapProjection:
		s.expr(t.Subject)
		for _, item := range t.Items {
			if item != nil {
				s.expr(item.Value)
			}
		}
	case *ast.ExistsSubquery:
		s.pattern(t.Pattern)
		s.where(t.Where)
		s.singleQuery(t.Query)
	case *ast.CountSubquery:
		s.pattern(t.Pattern)
		s.where(t.Where)
		s.singleQuery(t.Query)
	case *ast.SubscriptExpr:
		s.expr(t.Expr)
		s.expr(t.Index)
	case *ast.SliceExpr:
		s.expr(t.Expr)
		s.expr(t.From)
		s.expr(t.To)
	case *ast.ReduceExpr:
		s.expr(t.Init)
		s.expr(t.Source)
		s.expr(t.Projection)
	case *ast.ListLiteral:
		for _, elem := range t.Elements {
			s.expr(elem)
		}
	case *ast.MapLiteral:
		for _, val := range t.Values {
			s.expr(val)
		}
	case *ast.PathPattern:
		// A path in an expression position is a BINDING occurrence, exactly
		// like one in a MATCH: it introduces the relationship variable rather
		// than reading its value.
		s.path(t)
	}
}

// namedRelationships calls fn for every named relationship pattern along path,
// in traversal order. It is the single traversal both direction rules share, so
// the read check and the write check cannot drift apart on which relationships
// a pattern names or on the order they are reported in.
func namedRelationships(path *ast.PathPattern, fn func(name string, rel *ast.RelationshipPattern)) {
	if path == nil {
		return
	}
	for el := path.Head; el != nil; el = el.Next {
		rel := el.Relationship
		if rel == nil || rel.Variable == nil {
			continue
		}
		fn(*rel.Variable, rel)
	}
}
