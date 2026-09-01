// Package graphkeys implements the second step of the knowledge graph's node-key
// uniqueness audit: grouping keys by their Unicode NFC form and reporting the
// groups that more than one node carries.
//
// WHY THE AUDIT HAS TWO STEPS, AND WHY THE SECOND ONE IS HERE.
// SPEC/GRAPH.md § Node Key Uniqueness states the invariant — within one knowledge
// graph no two nodes carry the same `key`, two keys being the same key when their
// NFC forms are equal — and states that Groadmap does not enforce it. It is a
// convention the caller honours, so what the product owes is not prevention but
// DETECTION. Step 1 of that detection is a read-only Cypher query the
// specification publishes, run with `rmp graph execute`, returning `id(n)`,
// `labels(n)` and `n.key` for every keyed node. Step 2 cannot be Cypher: GoGraph's
// function registry holds no normalising function, and a query calling one is
// refused as an unknown function. So the grouping happens out here, over the rows
// step 1 returned.
//
// NORMALISATION IS FOR COMPARISON ONLY. Nothing in this package rewrites a key.
// Audit reports what it finds and changes nothing, and every Violation carries the
// stored spellings byte for byte, because only the caller knows which spelling the
// artefact was meant to carry.
//
// WHAT COUNTS AS A VIOLATION. The invariant is broken as soon as two nodes carry
// one key, so Audit reports a group the moment more than one node lands in it. The
// specification's step 2 names the condition the NFC grouping ADDS — a group
// holding more than one distinct byte sequence — because that is precisely the
// case the byte-wise duplicate audit
// (`MATCH (n) WHERE n.key IS NOT NULL RETURN n.key, count(*)`) cannot see: it
// groups on the stored bytes, so two spellings of one key are two groups of one
// and it reports nothing. A group holding one byte sequence and several nodes is
// the other case, the one that audit does see, and it is reported here too rather
// than filtered out: it is the same invariant broken, and an audit that stayed
// silent about it would be reporting a subset of what it had already computed.
// Kind tells the two apart.
package graphkeys

import (
	"fmt"
	"sort"

	"github.com/FlavioCFOliveira/Groadmap/internal/unicodenorm"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// Row is one row of the audit's first step: a node that carries a key.
//
// ID is GoGraph's internal identifier, which DATA_FORMATS.md § Graph Query Result
// describes as ephemeral. It is here to tell the nodes of one group apart within a
// single audit, and MUST NOT be recorded as the way to reach a node afterwards.
type Row struct {
	// Key is the node's stored key, byte for byte as the caller supplied it.
	Key string
	// Labels are the node's labels, as step 1 returned them.
	Labels []string
	// ID is the node's ephemeral internal identifier.
	ID int64
}

// Kind distinguishes the two ways a group can hold more than one node.
type Kind uint8

const (
	// KindNormalisation is a group holding more than one distinct byte sequence:
	// one key spelled in several normalisation forms. This is the condition the
	// two-step audit exists for, because the byte-wise duplicate audit is blind
	// to it.
	KindNormalisation Kind = iota + 1
	// KindIdentical is a group holding several nodes that carry one byte
	// sequence. It breaks the same invariant, and the byte-wise duplicate audit
	// reports it as well.
	KindIdentical
)

// String renders a Kind for a report.
func (k Kind) String() string {
	switch k {
	case KindNormalisation:
		return "normalisation"
	case KindIdentical:
		return "identical"
	default:
		return fmt.Sprintf("Kind(%d)", uint8(k))
	}
}

// Violation is one group of nodes whose keys are the same key under NFC.
type Violation struct {
	// NFC is the normalised form the group shares. It is the grouping key, not a
	// value any node stores, and it MUST NOT be written back to the graph.
	NFC string
	// Spellings are the distinct stored byte sequences in the group, sorted, so a
	// report can show what actually differs. A KindIdentical group has exactly
	// one; a KindNormalisation group has more.
	Spellings []string
	// Rows are the nodes carrying the key, in the order step 1 returned them.
	Rows []Row
	// Kind says which of the two conditions this group is.
	Kind Kind
}

// Audit groups rows by the NFC form of their keys and returns every group that
// more than one node lands in.
//
// The result is deterministic for a given input: violations are ordered by their
// NFC form, spellings within a violation are sorted, and rows keep the order step
// 1 returned them in — step 1's own ORDER BY makes that order stable for a given
// graph. An audit that found nothing returns nothing.
//
// The comparison is NFC and nothing else. It is deliberately NOT case folding and
// NOT lower-casing: `SPEC/GRAPH.md` and `SPEC/graph.md` are two different keys for
// two different files, and an audit that called them the same key would report a
// violation where the convention is intact. TestAuditDoesNotTreatCaseAsANormalisation
// pins that, because a fold is exactly the kind of loosening this comparison could
// drift into without anyone noticing.
//
// LIMIT, STATED RATHER THAN HIDDEN. A key holding bytes that are not valid UTF-8
// is not a sequence of code points, and normalising it replaces each such byte
// with U+FFFD. Two different malformed keys can therefore land in one group. That
// is a false positive rather than a missed violation, and Spellings shows the
// caller immediately that the stored bytes differ.
func Audit(rows []Row) []Violation {
	groups := make(map[string][]Row, len(rows))
	order := make([]string, 0, len(rows))
	for _, r := range rows {
		form := unicodenorm.NFC(r.Key)
		if _, seen := groups[form]; !seen {
			order = append(order, form)
		}
		groups[form] = append(groups[form], r)
	}

	sort.Strings(order)

	violations := make([]Violation, 0)
	for _, form := range order {
		members := groups[form]
		if len(members) < 2 {
			continue
		}

		distinct := make(map[string]bool, len(members))
		for _, m := range members {
			distinct[m.Key] = true
		}
		spellings := make([]string, 0, len(distinct))
		for s := range distinct {
			spellings = append(spellings, s)
		}
		sort.Strings(spellings)

		kind := KindIdentical
		if len(spellings) > 1 {
			kind = KindNormalisation
		}

		violations = append(violations, Violation{
			NFC:       form,
			Spellings: spellings,
			Rows:      members,
			Kind:      kind,
		})
	}
	return violations
}

// RowsFrom adapts one Graph Query Result — the columns and rows
// `rmp graph execute` publishes, as DATA_FORMATS.md § Graph Query Result defines
// them — into the rows Audit consumes.
//
// Every failure it returns is carried by utils.ErrInvalidInput, because
// ARCHITECTURE.md § Sentinel Error Catalogue requires every error in the codebase
// to originate from a sentinel, and a result whose shape the audit cannot read is
// malformed input to it.
//
// It resolves the three values BY COLUMN NAME rather than by position, so the
// specification's `ORDER BY` or a reordered RETURN list cannot silently shift a
// key into the label column. A result missing one of the three named columns is
// an error rather than a partial audit, because an audit run over the wrong
// columns is worse than one that did not run.
func RowsFrom(columns []string, rows [][]any) ([]Row, error) {
	index := map[string]int{}
	for i, c := range columns {
		index[c] = i
	}
	for _, want := range []string{"id", "labels", "key"} {
		if _, ok := index[want]; !ok {
			return nil, fmt.Errorf("%w: graph result has no %q column; step 1 must return id, labels and key",
				utils.ErrInvalidInput, want)
		}
	}

	out := make([]Row, 0, len(rows))
	for n, raw := range rows {
		if len(raw) != len(columns) {
			return nil, fmt.Errorf("%w: graph result row %d has %d values for %d columns",
				utils.ErrInvalidInput, n, len(raw), len(columns))
		}
		key, ok := raw[index["key"]].(string)
		if !ok {
			return nil, fmt.Errorf("%w: graph result row %d carries a non-string key (%T)",
				utils.ErrInvalidInput, n, raw[index["key"]])
		}
		id, err := toInt64(raw[index["id"]])
		if err != nil {
			return nil, fmt.Errorf("graph result row %d: %w", n, err)
		}
		out = append(out, Row{Key: key, Labels: toStrings(raw[index["labels"]]), ID: id})
	}
	return out, nil
}

// toInt64 accepts the numeric shapes a graph query result can carry an id in.
func toInt64(v any) (int64, error) {
	switch n := v.(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case float64:
		return int64(n), nil
	default:
		return 0, fmt.Errorf("%w: id is %T, which is not a number", utils.ErrInvalidInput, v)
	}
}

// toStrings accepts the shapes a label list can arrive in. A value that is not a
// list of strings yields no labels rather than an error: labels name the nodes of
// a group in a report and never decide whether a group is a violation, so a
// surprising shape must not stop the audit from reporting one.
func toStrings(v any) []string {
	switch list := v.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
