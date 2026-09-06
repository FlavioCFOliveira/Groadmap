package graphjson

import "github.com/FlavioCFOliveira/GoGraph/cypher/exec"

// PlanNode is the published JSON shape of one query-plan operator, canonical in
// SPEC/DATA_FORMATS.md § Graph Plan Node. It is the target of the ONE mapping
// this package exists to hold: both surfaces that publish a plan — `rmp graph
// execute` and `rmp graph client` — arrive here, over the engine's own
// [exec.PlanNode], so the byte identity SPEC/DATA_FORMATS.md § Graph Client
// Result requires is a property of the code rather than something a test has to
// keep policing.
//
// # Why five fields are pointers
//
// `omitempty` omits a zero-valued int64, and for four of these keys zero is a
// FINDING rather than an absence. The specification draws a tri-state
// distinction the engine introduced deliberately: a figure that was counted and
// came to zero, a figure nobody counted, and a figure that cannot exist. A plain
// int64 collapses the first two, which is precisely the defect the `dbHits`
// rules exist to prevent (§ Graph Plan Node rules 8 and 10).
//
// So a nil pointer omits the key and a pointer to zero publishes `0`, and which
// one is built is decided by the engine's own known-flags — never by the value.
//
// # Field order is the JSON key order
//
// encoding/json emits struct fields in declaration order, and both surfaces
// marshal this same struct, so the key order cannot differ between them. The
// order below is the order § Graph Plan Node's shape example publishes.
type PlanNode struct {
	Operator            string     `json:"operator"`
	Detail              string     `json:"detail,omitempty"`
	EstimatedRows       *int64     `json:"estimatedRows,omitempty"`
	EstimatedRowsSource string     `json:"estimatedRowsSource,omitempty"`
	Rows                *int64     `json:"rows,omitempty"`
	TimeNs              *int64     `json:"timeNs,omitempty"`
	DbHits              *int64     `json:"dbHits,omitempty"`
	RowsRemovedByFilter *int64     `json:"rowsRemovedByFilter,omitempty"`
	Children            []PlanNode `json:"children,omitempty"`
}

// Plan maps an engine plan tree onto the published shape.
//
// profiled selects which of the two trees is being published, and it is passed
// rather than read off the node because it is a property of the STATEMENT: a
// plan tree comes from [cypher.Result.Plan] and a profile tree from
// [cypher.Result.Profile], and at most one of those is ever non-nil. The four
// measured keys are written only for a profile tree (§ Graph Plan Node rule 7):
// an EXPLAIN executed nothing, so a `rows` of 0 on it would read as an operator
// that produced no rows rather than one that never ran.
//
// It returns nil for a nil node, so a caller may hand it either accessor's
// result unconditionally.
func Plan(n *exec.PlanNode, profiled bool) *PlanNode {
	if n == nil {
		return nil
	}
	out := &PlanNode{
		Operator: n.Name,
		Detail:   n.Detail,
	}

	// The estimate is a prediction made before anything ran, so it belongs to
	// both trees (rule 6). EstimateAbsent covers both "no estimate attributed"
	// and "the backing statistic was absent or stale", and both must omit the
	// pair rather than fabricate a number (rule 5).
	if n.Est.Source != exec.EstimateAbsent {
		out.EstimatedRows = int64Ptr(n.Est.Rows)
		out.EstimatedRowsSource = n.Est.Source.String()
	}

	if profiled {
		out.Rows = int64Ptr(n.Rows)
		out.TimeNs = int64Ptr(n.Time.Nanoseconds())
		// The known-flags, never the values, decide presence. DbHitsKnown false
		// means nobody counted, which publishes no key; RowsRemovedByFilterKnown
		// false means the operator has no rejection mechanism at all. The
		// asymmetry between the two is deliberate and specified (rule 10).
		if n.DbHitsKnown {
			out.DbHits = int64Ptr(n.DbHits)
		}
		if n.RowsRemovedByFilterKnown {
			out.RowsRemovedByFilter = int64Ptr(n.RowsRemovedByFilter)
		}
	}

	if len(n.Children) > 0 {
		out.Children = make([]PlanNode, 0, len(n.Children))
		for i := range n.Children {
			// Execution order is the order the engine built, and it is the one
			// thing the ordering carries (rule 3), so it is preserved as-is.
			out.Children = append(out.Children, *Plan(&n.Children[i], profiled))
		}
	}
	return out
}

// int64Ptr yields a pointer to a copy of v, which is how a counted zero reaches
// the JSON as `0` instead of being omitted. See the PlanNode doc comment.
func int64Ptr(v int64) *int64 { return &v }
