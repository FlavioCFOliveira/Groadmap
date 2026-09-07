package graphjson

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/cypher/exec"
)

// Regression tests for SPEC/DATA_FORMATS.md § Graph Plan Node.
//
// The rules these pin are the ones a plausible implementation gets wrong. Every
// one of them is a case where a zero and an absence mean different things, and
// where Go's `omitempty` on a plain int64 would collapse the two -- publishing
// "nobody counted this" as "this was counted and came to zero", which is exactly
// the distinction the engine's tri-state db-hits figure exists to draw.

// TestPlan_CountedZeroIsPublished is the sharp end of the pointer fields. An
// operator whose storage accesses WERE counted and came to zero must publish
// `dbHits: 0`; the key carries a measurement and dropping it would report that
// no measurement was taken.
func TestPlan_CountedZeroIsPublished(t *testing.T) {
	node := &exec.PlanNode{Name: "NodeByLabelScan", DbHits: 0, DbHitsKnown: true}

	got := marshal(t, Plan(node, true))

	if !strings.Contains(got, `"dbHits": 0`) {
		t.Errorf("a counted zero must publish `dbHits: 0`; § Graph Plan Node rule 8 "+
			"distinguishes it from an uncounted figure, and omitting it reports a "+
			"measurement that was never taken.\ngot: %s", got)
	}
}

// TestPlan_UncountedDbHitsIsOmitted is the other half of rule 8, and the half
// that matters most: an operator nobody counted must carry no key at all, never
// a zero standing in for one.
func TestPlan_UncountedDbHitsIsOmitted(t *testing.T) {
	node := &exec.PlanNode{Name: "Filter", DbHits: 0, DbHitsKnown: false}

	got := marshal(t, Plan(node, true))

	if strings.Contains(got, "dbHits") {
		t.Errorf("an uncounted figure must publish NO dbHits key; publishing 0 would "+
			"give a caller a measurement nobody took (§ Graph Plan Node rule 8).\ngot: %s", got)
	}
}

// TestPlan_RowsRemovedZeroIsPublished pins the asymmetry against rule 8 that
// rule 10 states must not be "harmonised". A filter that rejected nothing is a
// finding in its own right, so the zero is published.
func TestPlan_RowsRemovedZeroIsPublished(t *testing.T) {
	node := &exec.PlanNode{Name: "Filter", RowsRemovedByFilter: 0, RowsRemovedByFilterKnown: true}

	got := marshal(t, Plan(node, true))

	if !strings.Contains(got, `"rowsRemovedByFilter": 0`) {
		t.Errorf("an operator with a rejection mechanism that rejected nothing must "+
			"publish 0 -- it is exactly what a reader of a slow plan wants to see "+
			"(§ Graph Plan Node rule 10).\ngot: %s", got)
	}
}

// TestPlan_RowsRemovedOmittedWithoutMechanism is rule 10's other half: an
// operator that cannot reject anything has no figure to have.
func TestPlan_RowsRemovedOmittedWithoutMechanism(t *testing.T) {
	node := &exec.PlanNode{Name: "Project", RowsRemovedByFilterKnown: false}

	got := marshal(t, Plan(node, true))

	if strings.Contains(got, "rowsRemovedByFilter") {
		t.Errorf("an operator with no rejection mechanism must carry no key "+
			"(§ Graph Plan Node rule 10).\ngot: %s", got)
	}
}

// TestPlan_ExplainTreeCarriesNoMeasurement pins rule 7. An EXPLAIN executed
// nothing, so a `rows` of 0 on it would read as an operator that produced no
// rows rather than one that never ran.
func TestPlan_ExplainTreeCarriesNoMeasurement(t *testing.T) {
	node := &exec.PlanNode{
		Name: "NodeByLabelScan",
		// Deliberately populated: the mapping must drop these because the tree is
		// an estimate, not because the engine left them empty.
		Rows: 44, Time: 815402, DbHits: 44, DbHitsKnown: true,
		RowsRemovedByFilter: 3, RowsRemovedByFilterKnown: true,
	}

	got := marshal(t, Plan(node, false))

	for _, key := range []string{"rows", "timeNs", "dbHits", "rowsRemovedByFilter"} {
		if strings.Contains(got, `"`+key+`"`) {
			t.Errorf("a plan tree must carry no measured key, and it carries %q; an "+
				"EXPLAIN measured nothing (§ Graph Plan Node rule 7).\ngot: %s", key, got)
		}
	}
}

// TestPlan_EstimateIsPublishedForBothTrees pins rule 6: the estimate is a
// prediction made before anything ran, so it belongs to an EXPLAIN and to a
// PROFILE alike, and it is the same number in both. Placing it beside a
// measured `rows` is what makes a bad estimate visible.
func TestPlan_EstimateIsPublishedForBothTrees(t *testing.T) {
	node := &exec.PlanNode{
		Name: "NodeByLabelScan",
		Est:  exec.PlanEstimate{Rows: 44, Source: exec.EstimateExact},
	}

	for _, profiled := range []bool{false, true} {
		got := marshal(t, Plan(node, profiled))
		if !strings.Contains(got, `"estimatedRows": 44`) || !strings.Contains(got, `"estimatedRowsSource": "exact"`) {
			t.Errorf("the estimate pair must be published for profiled=%v "+
				"(§ Graph Plan Node rule 6).\ngot: %s", profiled, got)
		}
	}
}

// TestPlan_AbsentEstimateOmitsBothKeys pins rule 4 and rule 5 together: the pair
// is written as a pair, and an operator the planner attributed no estimate to --
// which includes one whose backing statistic was absent or stale, since the
// engine collapses that to EstimateAbsent -- publishes neither half rather than
// fabricating a number.
func TestPlan_AbsentEstimateOmitsBothKeys(t *testing.T) {
	node := &exec.PlanNode{Name: "Filter", Est: exec.PlanEstimate{Rows: 12, Source: exec.EstimateAbsent}}

	got := marshal(t, Plan(node, false))

	if strings.Contains(got, "estimatedRows") || strings.Contains(got, "estimatedRowsSource") {
		t.Errorf("an absent estimate must publish neither key, even with a non-zero "+
			"Rows on the node (§ Graph Plan Node rules 4 and 5).\ngot: %s", got)
	}
}

// TestPlan_ChildOrderIsPreserved pins rule 3. The order is execution order and
// it is the one thing the ordering carries, so a mapping that sorted or reversed
// it would destroy the information without changing the key set.
func TestPlan_ChildOrderIsPreserved(t *testing.T) {
	node := &exec.PlanNode{Name: "HashJoin", Children: []exec.PlanNode{
		{Name: "BuildSide"},
		{Name: "ProbeSide"},
	}}

	out := Plan(node, false)

	if len(out.Children) != 2 || out.Children[0].Operator != "BuildSide" || out.Children[1].Operator != "ProbeSide" {
		t.Errorf("children must keep execution order, build side before probe side "+
			"(§ Graph Plan Node rule 3).\ngot: %+v", out.Children)
	}
}

// TestPlan_TimeIsWholeNanoseconds pins rule 12. The engine measures nanoseconds
// and the protocol carries that same integer; publishing it unchanged is what
// makes the byte identity between the two surfaces exact by construction rather
// than dependent on two float formatters agreeing.
func TestPlan_TimeIsWholeNanoseconds(t *testing.T) {
	node := &exec.PlanNode{Name: "Project", Time: 1482310 * time.Nanosecond}

	got := marshal(t, Plan(node, true))

	if !strings.Contains(got, `"timeNs": 1482310`) {
		t.Errorf("timeNs must publish the engine's whole-nanosecond figure and derive "+
			"nothing from it (§ Graph Plan Node rule 12).\ngot: %s", got)
	}
}

// TestPlan_NilNodeMapsToNil lets a caller hand either accessor's result over
// unconditionally, which is what keeps the two call sites free of a branch that
// could come to disagree about which tree is which.
func TestPlan_NilNodeMapsToNil(t *testing.T) {
	if got := Plan(nil, false); got != nil {
		t.Errorf("a nil plan node must map to nil, got %+v", got)
	}
}

// TestPlan_KeySetIsClosed pins rule 2. The engine's plan node carries
// information this shape does not publish; an addition to the key set is a
// change to the specification, so it must not arrive by accident.
func TestPlan_KeySetIsClosed(t *testing.T) {
	node := &exec.PlanNode{
		Name: "Filter", Detail: "s.status = 'implemented'",
		Est:  exec.PlanEstimate{Rows: 12, Source: exec.EstimateStats},
		Rows: 9, Time: 1104986, DbHits: 4, DbHitsKnown: true,
		RowsRemovedByFilter: 35, RowsRemovedByFilterKnown: true,
		Children: []exec.PlanNode{{Name: "NodeByLabelScan"}},
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(marshal(t, Plan(node, true))), &decoded); err != nil {
		t.Fatalf("plan node is not valid JSON: %v", err)
	}

	want := map[string]bool{
		"operator": true, "detail": true, "estimatedRows": true,
		"estimatedRowsSource": true, "rows": true, "timeNs": true,
		"dbHits": true, "rowsRemovedByFilter": true, "children": true,
	}
	for key := range decoded {
		if !want[key] {
			t.Errorf("key %q is published but is not one of the nine § Graph Plan Node "+
				"defines; the key set is closed and an addition is a specification "+
				"change (rule 2)", key)
		}
	}
	if len(decoded) != len(want) {
		t.Errorf("a node carrying every figure must publish all nine keys, got %d: %v", len(decoded), decoded)
	}
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
