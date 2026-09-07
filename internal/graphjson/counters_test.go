package graphjson

// Tests for the ONE mapping from the engine's write-effect counters to the
// published `counters` object (SPEC/DATA_FORMATS.md § Graph Query Counters).
//
// Three properties are load-bearing and each has its own test here, because each
// is a thing an implementation gets wrong differently:
//
//  1. WHEN the block appears at all. Rule 1 says present if and only if the
//     statement changed something, and the half that is easy to break is the
//     absence: an over-eager mapping publishes an empty object for a read, and
//     every existing consumer's bytes change.
//  2. WHICH members appear inside it. Rule 2 omits a zero, which is the opposite
//     of what the neighbouring plan node does, so a copy of plan.go's pointer
//     discipline would publish ten zeroes.
//  3. WHAT the values are, including the one member that is a SUM.
//
// The key ORDER is asserted against literal bytes rather than a key set, because
// the order is part of the published shape and encoding/json takes it from the
// declaration order of the struct: a field moved to keep the linter happy would
// change the bytes both surfaces write, and nothing else in the tree would
// notice.

import (
	"encoding/json"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/cypher/exec"
)

// TestCountersOf_AStatementThatChangedNothingPublishesNoBlock is the
// compatibility half of rule 1, and the one that protects every existing
// consumer: a read and a write that applied nothing must both map to nil, so the
// caller omits the key and produces exactly the bytes it produced before the
// member existed.
func TestCountersOf_AStatementThatChangedNothingPublishesNoBlock(t *testing.T) {
	cases := []struct {
		name string
		in   *exec.QueryCounters
	}{
		{
			// What the engine reports for a statement with no write surface at
			// all: a MATCH, and an EXPLAIN of anything, which executes nothing.
			name: "a read reports no counters at all",
			in:   nil,
		},
		{
			// What the engine reports for a write whose effects came to nothing:
			// a MERGE that matched, a DELETE whose pattern matched no row. The
			// struct exists and every counter is zero.
			name: "a write that applied nothing reports an all-zero struct",
			in:   &exec.QueryCounters{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CountersOf(tc.in); got != nil {
				t.Fatalf("a statement that changed nothing must map to nil, so the "+
					"`counters` key is absent and the output is unchanged in every "+
					"byte (SPEC/DATA_FORMATS.md § Graph Query Counters, rule 1).\ngot: %+v", got)
			}
		})
	}
}

// TestCountersOf_CarriesEveryCounterItsOwnValue guards against a mapping that
// compiles with two members crossed over. Every value is distinct, so a swap
// cannot pass.
func TestCountersOf_CarriesEveryCounterItsOwnValue(t *testing.T) {
	got := CountersOf(&exec.QueryCounters{
		NodesCreated:         1,
		NodesDeleted:         2,
		RelationshipsCreated: 3,
		RelationshipsDeleted: 4,
		PropertiesSet:        5,
		PropertiesRemoved:    6,
		LabelsAdded:          7,
		LabelsRemoved:        8,
		IndexesAdded:         9,
		IndexesRemoved:       10,
		ConstraintsAdded:     11,
		ConstraintsRemoved:   12,
	})
	if got == nil {
		t.Fatal("a statement that changed something must publish its counters")
	}
	want := &Counters{
		NodesCreated:         1,
		NodesDeleted:         2,
		RelationshipsCreated: 3,
		RelationshipsDeleted: 4,
		// 5 + 6: the two engine counters the protocol carries as one.
		PropertiesWritten:  11,
		LabelsAdded:        7,
		LabelsRemoved:      8,
		IndexesAdded:       9,
		IndexesRemoved:     10,
		ConstraintsAdded:   11,
		ConstraintsRemoved: 12,
	}
	if *got != *want {
		t.Errorf("the mapping crossed or dropped a counter.\ngot:  %+v\nwant: %+v", *got, *want)
	}
}

// TestCountersOf_PropertiesWrittenIsTheSumOfTheTwoEngineCounters isolates the
// one member that is arithmetic rather than a copy.
//
// The three cases matter separately: an assignment alone, a removal alone, and
// one of each in a single statement. A mapping that read only PropertiesSet
// passes the first and fails the other two, and a statement that both assigns
// and removes is exactly what SPEC/GRAPH.md acceptance criterion 63 requires the
// two surfaces to be compared on.
func TestCountersOf_PropertiesWrittenIsTheSumOfTheTwoEngineCounters(t *testing.T) {
	cases := []struct {
		name    string
		set     int64
		removed int64
		want    int64
	}{
		{"a SET that wrote one property", 1, 0, 1},
		{"a REMOVE of one property, folded under the same key", 0, 1, 1},
		{"a statement that assigned one and removed one", 1, 1, 2},
		{"a statement that assigned one and removed two", 1, 2, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CountersOf(&exec.QueryCounters{PropertiesSet: tc.set, PropertiesRemoved: tc.removed})
			if got == nil {
				t.Fatal("a property write is a change, so the block must be published")
			}
			if got.PropertiesWritten != tc.want {
				t.Errorf("propertiesWritten carries assignments and removals as ONE figure "+
					"(SPEC/DATA_FORMATS.md § Graph Query Counters, rule 4): "+
					"set=%d removed=%d must publish %d, got %d",
					tc.set, tc.removed, tc.want, got.PropertiesWritten)
			}
		})
	}
}

// TestCounters_AZeroIsOmittedAndTheBlockIsNeverEmpty is rule 2, asserted on the
// published bytes rather than on the struct: the rule is about what reaches the
// caller.
func TestCounters_AZeroIsOmittedAndTheBlockIsNeverEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   *exec.QueryCounters
		want string
	}{
		{
			// The headline case of SPEC/GRAPH.md acceptance criterion 61:
			// CREATE (:Widget {serial:'A-1', batch:7}) is three members and no
			// fourth, so a published zero fails here.
			name: "a labelled node with two properties",
			in:   &exec.QueryCounters{NodesCreated: 1, PropertiesSet: 2, LabelsAdded: 1},
			want: `{"nodesCreated":1,"propertiesWritten":2,"labelsAdded":1}`,
		},
		{
			// A DETACH DELETE of a connected node. No property figure: deleting
			// an element counts no property removal, and an implementation that
			// counted the deleted element's properties would show one here.
			name: "a detach delete of a connected node",
			in:   &exec.QueryCounters{NodesDeleted: 1, RelationshipsDeleted: 1},
			want: `{"nodesDeleted":1,"relationshipsDeleted":1}`,
		},
		{
			name: "a schema statement that registered one index",
			in:   &exec.QueryCounters{IndexesAdded: 1},
			want: `{"indexesAdded":1}`,
		},
		{
			name: "a SET that wrote one property",
			in:   &exec.QueryCounters{PropertiesSet: 1},
			want: `{"propertiesWritten":1}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := json.Marshal(CountersOf(tc.in))
			if err != nil {
				t.Fatalf("marshalling the published counters: %v", err)
			}
			if string(encoded) != tc.want {
				t.Errorf("the published block must carry only the counters that are "+
					"non-zero, in the specified order "+
					"(SPEC/DATA_FORMATS.md § Graph Query Counters, rule 2).\n"+
					"got:  %s\nwant: %s", encoded, tc.want)
			}
		})
	}
}

// TestCounters_KeyOrderIsTheSpecifiedOrder pins the whole key set and its order
// in one literal.
//
// encoding/json emits struct fields in DECLARATION order and both surfaces
// marshal this same struct, so this literal is the published key order of both.
// A field reordered — by a linter, by a merge, by a well-meant alphabetisation —
// changes the bytes `rmp graph execute` and `rmp graph client` write, and this
// is the only place that would say so.
func TestCounters_KeyOrderIsTheSpecifiedOrder(t *testing.T) {
	// Every counter is non-zero so that none is omitted and the full order is
	// visible. The values are distinct so a reordering cannot coincidentally
	// reproduce the expected bytes.
	all := CountersOf(&exec.QueryCounters{
		NodesCreated:         1,
		NodesDeleted:         2,
		RelationshipsCreated: 3,
		RelationshipsDeleted: 4,
		PropertiesSet:        5,
		LabelsAdded:          6,
		LabelsRemoved:        7,
		IndexesAdded:         8,
		IndexesRemoved:       9,
		ConstraintsAdded:     10,
		ConstraintsRemoved:   11,
	})
	const want = `{"nodesCreated":1,"nodesDeleted":2,"relationshipsCreated":3,` +
		`"relationshipsDeleted":4,"propertiesWritten":5,"labelsAdded":6,` +
		`"labelsRemoved":7,"indexesAdded":8,"indexesRemoved":9,` +
		`"constraintsAdded":10,"constraintsRemoved":11}`

	encoded, err := json.Marshal(all)
	if err != nil {
		t.Fatalf("marshalling the published counters: %v", err)
	}
	if string(encoded) != want {
		t.Errorf("the published key set and its order are fixed by "+
			"SPEC/DATA_FORMATS.md § Graph Query Counters and are the same on both "+
			"surfaces, because both marshal this struct.\ngot:  %s\nwant: %s", encoded, want)
	}
}

// TestCountersOf_AnySingleNonZeroCounterPublishesTheBlock is the other half of
// rule 1: whichever effect a statement had, the block appears. It walks each
// engine counter in turn so that a mapping which forgot one — and therefore
// reported "changed nothing" for the statement that produced it — fails on that
// counter by name.
func TestCountersOf_AnySingleNonZeroCounterPublishesTheBlock(t *testing.T) {
	cases := map[string]exec.QueryCounters{
		"NodesCreated":         {NodesCreated: 1},
		"NodesDeleted":         {NodesDeleted: 1},
		"RelationshipsCreated": {RelationshipsCreated: 1},
		"RelationshipsDeleted": {RelationshipsDeleted: 1},
		"PropertiesSet":        {PropertiesSet: 1},
		"PropertiesRemoved":    {PropertiesRemoved: 1},
		"LabelsAdded":          {LabelsAdded: 1},
		"LabelsRemoved":        {LabelsRemoved: 1},
		"IndexesAdded":         {IndexesAdded: 1},
		"IndexesRemoved":       {IndexesRemoved: 1},
		"ConstraintsAdded":     {ConstraintsAdded: 1},
		"ConstraintsRemoved":   {ConstraintsRemoved: 1},
	}
	for name, counters := range cases {
		t.Run(name, func(t *testing.T) {
			got := CountersOf(&counters)
			if got == nil {
				t.Fatalf("a statement whose only effect was %s changed something, so "+
					"its counters must be published", name)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshalling the published counters: %v", err)
			}
			if string(encoded) == "{}" {
				t.Errorf("%s reached the block but published no member, so the block "+
					"is empty — which rule 2 says it never is", name)
			}
		})
	}
}
