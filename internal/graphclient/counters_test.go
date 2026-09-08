package graphclient

// Package graphclient — the write-counter inversion's tests.
//
// This file's subject is the step counters.go owns: turning the Bolt statistics
// map back into the ENGINE's [exec.QueryCounters], so that the single mapping in
// internal/graphjson publishes it. It is deliberately the mirror of plan.go's
// tests: what needs proving is the inverse mapping, because the step from the
// engine representation to the published JSON is shared with the web graph data
// endpoint and has no second opinion in it.
//
// The one property that could silently diverge between the two surfaces is the
// property figure — the wire carries the engine's two property counters already
// summed, and nothing on this side can un-sum them — so it is proved here
// explicitly, by composing this inversion with the shared mapping and comparing
// the result against what the same shared mapping produces from the engine's own
// unfolded pair.

import (
	"context"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/bolt/packstream"
	"github.com/FlavioCFOliveira/GoGraph/bolt/proto"
	"github.com/FlavioCFOliveira/GoGraph/cypher/exec"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphjson"
)

// wireStats builds the statistics map the way the server builds it: only the
// non-zero counters, every one an integer, plus the boolean contains-updates
// that is written whenever the map is written at all.
//
// It takes the ENGINE's counters and performs the server's own fold, so a test
// that starts from a statement's real effects gets the bytes that statement
// would really put on the wire. The pointer is the linter's requirement on a
// 96-byte struct, not a claim that the argument is written to.
func wireStats(c *exec.QueryCounters) map[string]packstream.Value {
	stats := map[string]packstream.Value{}
	put := func(key string, n int64) {
		if n != 0 {
			stats[key] = n
		}
	}
	put(statNodesCreated, c.NodesCreated)
	put(statNodesDeleted, c.NodesDeleted)
	put(statRelationshipsCreated, c.RelationshipsCreated)
	put(statRelationshipsDeleted, c.RelationshipsDeleted)
	// The fold the protocol imposes: one properties counter, no counterpart for
	// a removal.
	put(statPropertiesSet, c.PropertiesSet+c.PropertiesRemoved)
	put(statLabelsAdded, c.LabelsAdded)
	put(statLabelsRemoved, c.LabelsRemoved)
	put(statIndexesAdded, c.IndexesAdded)
	put(statIndexesRemoved, c.IndexesRemoved)
	put(statConstraintsAdded, c.ConstraintsAdded)
	put(statConstraintsRemoved, c.ConstraintsRemoved)
	if len(stats) == 0 {
		// The server omits the map entirely rather than sending an empty one, so
		// a statement that changed nothing has no `stats` key at all.
		return nil
	}
	stats["contains-updates"] = true
	return stats
}

// pullSuccessWith builds the terminal PULL success carrying metadata beside the
// has_more the stream always ends with.
func pullSuccessWith(extra map[string]packstream.Value) *proto.Success {
	metadata := map[string]packstream.Value{"has_more": false}
	for k, v := range extra {
		metadata[k] = v
	}
	return &proto.Success{Metadata: metadata}
}

// TestCountersOf_NoStatisticsMapMeansTheStatementChangedNothing is the
// compatibility half. The server omits the map entirely for a read and for a
// write that applied nothing, so the absence must reach the caller as nil rather
// than as an all-zero struct that would publish an empty block.
func TestCountersOf_NoStatisticsMapMeansTheStatementChangedNothing(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]packstream.Value
	}{
		{"a read: the terminal success carries only has_more", map[string]packstream.Value{"has_more": false}},
		{"metadata with no stats key at all", map[string]packstream.Value{}},
		{"a stats key of the wrong type is not a statistics map", map[string]packstream.Value{"stats": int64(3)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countersOf(tc.in); got != nil {
				t.Fatalf("an absent statistics map means the statement changed nothing, "+
					"which must reach the published mapping as nil.\ngot: %+v", got)
			}
		})
	}
}

// TestCountersOf_InvertsEveryCounterOntoItsOwnEngineField guards against two
// crossed keys. Every value is distinct, so a swap cannot pass.
func TestCountersOf_InvertsEveryCounterOntoItsOwnEngineField(t *testing.T) {
	got := countersOf(map[string]packstream.Value{
		"stats": map[string]packstream.Value{
			statNodesCreated:         int64(1),
			statNodesDeleted:         int64(2),
			statRelationshipsCreated: int64(3),
			statRelationshipsDeleted: int64(4),
			statPropertiesSet:        int64(5),
			statLabelsAdded:          int64(6),
			statLabelsRemoved:        int64(7),
			statIndexesAdded:         int64(8),
			statIndexesRemoved:       int64(9),
			statConstraintsAdded:     int64(10),
			statConstraintsRemoved:   int64(11),
			"contains-updates":       true,
		},
	})
	if got == nil {
		t.Fatal("a statistics map on the wire means the statement changed something")
	}
	want := exec.QueryCounters{
		NodesCreated:         1,
		NodesDeleted:         2,
		RelationshipsCreated: 3,
		RelationshipsDeleted: 4,
		PropertiesSet:        5,
		// The protocol has no properties-removed, so the inversion leaves it at
		// zero and the shared mapping's sum reproduces the wire's figure exactly.
		PropertiesRemoved:  0,
		LabelsAdded:        6,
		LabelsRemoved:      7,
		IndexesAdded:       8,
		IndexesRemoved:     9,
		ConstraintsAdded:   10,
		ConstraintsRemoved: 11,
	}
	if *got != want {
		t.Errorf("the inversion crossed or dropped a counter.\ngot:  %+v\nwant: %+v", *got, want)
	}
	// contains-updates is derived rather than carried, and the derivation must
	// agree with the flag the server sent.
	if !got.ContainsUpdates() {
		t.Error("ContainsUpdates is derived from the counters, so a map carrying any " +
			"non-zero counter must derive true — which is what the wire's " +
			"contains-updates asserted")
	}
}

// TestCountersOf_AnAbsentCounterIsZeroAndNotAnAbsence is the difference between
// this file and plan.go beside it, and it is worth stating because the two
// inversions look alike and mean opposite things.
//
// The server sends only the non-zero counters, so an absent key here means the
// effect did not happen. A plan node's absent dbHits means NOBODY COUNTED, which
// is why plan.go carries known-flags and this file carries none.
func TestCountersOf_AnAbsentCounterIsZeroAndNotAnAbsence(t *testing.T) {
	got := countersOf(map[string]packstream.Value{
		"stats": map[string]packstream.Value{
			statIndexesAdded:   int64(1),
			"contains-updates": true,
		},
	})
	if got == nil {
		t.Fatal("a statistics map carrying one counter still means the statement changed something")
	}
	want := exec.QueryCounters{IndexesAdded: 1}
	if *got != want {
		t.Errorf("every counter the server did not send is zero.\ngot:  %+v\nwant: %+v", *got, want)
	}
}

// TestCountersOf_TheFoldedPropertyFigureAgreesWithTheDirectPath is the identity
// SPEC/DATA_FORMATS.md § Graph Client Result rule 6 requires, proved where it
// could actually break.
//
// The direct path holds the engine's two property counters unfolded and sums
// them in the shared mapping. The served path receives them already summed and
// sums the pair (sum, 0) in the same shared mapping. The two must arrive at one
// figure — and at one whole object — for every combination of the two effects,
// including a statement that both assigns and removes, which is the case
// SPEC/GRAPH.md acceptance criterion 63 names as the one a pure SET would not
// have caught.
func TestCountersOf_TheFoldedPropertyFigureAgreesWithTheDirectPath(t *testing.T) {
	cases := []struct {
		name    string
		engine  exec.QueryCounters
		wantSum int64
	}{
		{"a SET of one property", exec.QueryCounters{PropertiesSet: 1}, 1},
		{"a REMOVE of one property", exec.QueryCounters{PropertiesRemoved: 1}, 1},
		{
			"a statement that both assigns and removes in one pass",
			exec.QueryCounters{PropertiesSet: 1, PropertiesRemoved: 1},
			2,
		},
		{
			"a write with properties beside other effects",
			exec.QueryCounters{NodesCreated: 1, PropertiesSet: 2, PropertiesRemoved: 1, LabelsAdded: 1},
			3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// What a surface holding the engine's own counters publishes: them,
			// straight into the shared mapping.
			direct := graphjson.CountersOf(&tc.engine)
			// What `rmp graph client` publishes: the same counters folded by the
			// server onto the wire, inverted here, then through the SAME mapping.
			served := graphjson.CountersOf(countersOf(map[string]packstream.Value{
				"stats": wireStats(&tc.engine),
			}))

			if direct == nil || served == nil {
				t.Fatalf("both paths must publish counters for a statement that wrote a "+
					"property.\ndirect: %+v\nserved: %+v", direct, served)
			}
			if *direct != *served {
				t.Fatalf("the two surfaces must publish the same object, key for key and "+
					"value for value (SPEC/DATA_FORMATS.md § Graph Client Result, "+
					"rule 6).\nexecute: %+v\nclient:  %+v", *direct, *served)
			}
			if direct.PropertiesWritten != tc.wantSum {
				t.Errorf("propertiesWritten must be the sum of the two engine counters, "+
					"want %d, got %d", tc.wantSum, direct.PropertiesWritten)
			}
		})
	}
}

// TestSend_CarriesTheServersWriteCounters drives the whole exchange, so that the
// counters are proved to be read from the message the server really writes them
// to — the SUCCESS that terminates the stream, beside the notifications and the
// plan — rather than from a message that happens to be convenient.
func TestSend_CarriesTheServersWriteCounters(t *testing.T) {
	effects := exec.QueryCounters{NodesCreated: 1, PropertiesSet: 2, LabelsAdded: 1}
	server := startScriptedServer(t, func(request any, runCount int64) exchange {
		return defaultSession(request, func(request any, _ int64) exchange {
			switch request.(type) {
			case *proto.Run:
				// A write with no RETURN clause: no "fields" key at all.
				return exchange{responses: []any{&proto.Success{Metadata: map[string]packstream.Value{
					"qid": int64(-1),
				}}}}
			case *proto.Pull:
				return exchange{responses: []any{
					pullSuccessWith(map[string]packstream.Value{"stats": wireStats(&effects)}),
				}}
			default:
				return ok()
			}
		}, runCount)
	})

	result, err := Send(context.Background(), server.socket, "CREATE (:Widget {serial:'A-1', batch:7})")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.Counters == nil {
		t.Fatal("a statement that changed the graph must carry its counters back to the " +
			"caller; without them `graph client` publishes {\"ok\": true} over a write " +
			"whose effects the server reported")
	}
	if *result.Counters != effects {
		t.Errorf("the counters must be the ones the server reported.\ngot:  %+v\nwant: %+v",
			*result.Counters, effects)
	}
	// And through the shared mapping, the exact object the direct path publishes
	// for the same effects.
	published := graphjson.CountersOf(result.Counters)
	want := graphjson.CountersOf(&effects)
	if published == nil || want == nil || *published != *want {
		t.Errorf("the served result must publish the object the shared mapping publishes for the\n"+
			"engine's own counters.\n"+
			"got:  %+v\nwant: %+v", published, want)
	}
}

// TestSend_AReadCarriesNoCounters is the other half, at the exchange level: a
// server that writes no statistics map leaves the caller with nothing to
// publish, so the output of a read is unchanged in every byte.
func TestSend_AReadCarriesNoCounters(t *testing.T) {
	server := startScriptedServer(t, func(request any, runCount int64) exchange {
		return defaultSession(request, func(request any, _ int64) exchange {
			switch request.(type) {
			case *proto.Run:
				return runSuccess("w.serial")
			case *proto.Pull:
				return exchange{responses: []any{
					&proto.Record{Data: []packstream.Value{"A-1"}},
					pullSuccessWith(nil),
				}}
			default:
				return ok()
			}
		}, runCount)
	})

	result, err := Send(context.Background(), server.socket, "MATCH (w:Widget) RETURN w.serial")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.Counters != nil {
		t.Errorf("a read changes nothing, so no statistics map is sent and no counters "+
			"reach the caller.\ngot: %+v", result.Counters)
	}
	if published := graphjson.CountersOf(result.Counters); published != nil {
		t.Errorf("and the published mapping must omit the block entirely.\ngot: %+v", published)
	}
}
