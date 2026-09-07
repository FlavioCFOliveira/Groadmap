package graphclient

import (
	"github.com/FlavioCFOliveira/GoGraph/bolt/packstream"
	"github.com/FlavioCFOliveira/GoGraph/cypher/exec"
)

// The protocol's own key names for a statement's write effects. They are the
// Bolt statistics names — hyphenated, and drawn from the driver's own summary
// vocabulary — rather than the ones Groadmap publishes, and the difference is
// the point of this file, as it is of plan.go beside it.
//
// Nothing here maps to JSON. This file inverts the protocol encoding onto the
// ENGINE's representation, and the single mapping in internal/graphjson then
// publishes it — which is what makes `client` and `execute` byte-identical by
// construction rather than by assertion (SPEC/DATA_FORMATS.md § Graph Client
// Result, rule 6).
const (
	metaKeyStats = "stats"

	statNodesCreated         = "nodes-created"
	statNodesDeleted         = "nodes-deleted"
	statRelationshipsCreated = "relationships-created"
	statRelationshipsDeleted = "relationships-deleted"
	statPropertiesSet        = "properties-set"
	statLabelsAdded          = "labels-added"
	statLabelsRemoved        = "labels-removed"
	statIndexesAdded         = "indexes-added"
	statIndexesRemoved       = "indexes-removed"
	statConstraintsAdded     = "constraints-added"
	statConstraintsRemoved   = "constraints-removed"
)

// countersOf reads a statement's write effects out of the final success's
// metadata, under the "stats" key the server writes them to.
//
// The map is absent whenever the statement changed nothing: the server omits it
// entirely rather than sending eleven zeroes, so a missing key is the ordinary
// case for a read and for a write that applied nothing alike, and nil is the
// right answer for both. Every counter key is an INTEGER on the wire and only
// the non-zero ones are sent, so an absent counter reads as zero — which is what
// the zero value of the struct being built already says.
//
// # properties-set arrives already folded, and that is what makes the two paths
// agree
//
// The engine counts a property assignment and a property removal separately, but
// the protocol's statistics vocabulary has a single properties counter and no
// counterpart for a removal, so the server sends the SUM of the two under
// properties-set. It is mapped onto PropertiesSet with PropertiesRemoved left at
// zero, and the shared mapping then computes its published propertiesWritten as
// PropertiesSet + PropertiesRemoved — arriving, by construction, at the identical
// figure the direct path arrives at from an unfolded pair. No test has to police
// that identity, because there is no second arithmetic for it to diverge from
// (SPEC/GRAPH.md § Write Counters: What a Statement Changed, "The one figure the protocol carries folded";
// SPEC/DATA_FORMATS.md § Graph Query Counters, rule 4).
//
// # contains-updates is deliberately not inverted
//
// The wire carries it as a BOOLEAN beside the counters, and it is dropped here
// rather than carried, because [exec.QueryCounters.ContainsUpdates] is DERIVED
// from the counters rather than stored: any non-zero effect makes it true. The
// server sends the map only when at least one counter is non-zero, so the
// reconstructed struct derives exactly the answer the wire asserted, and reading
// the flag could only introduce a way for the two to disagree.
func countersOf(metadata map[string]packstream.Value) *exec.QueryCounters {
	stats, ok := metadata[metaKeyStats].(map[string]packstream.Value)
	if !ok {
		return nil
	}
	return &exec.QueryCounters{
		NodesCreated:         statInt(stats, statNodesCreated),
		NodesDeleted:         statInt(stats, statNodesDeleted),
		RelationshipsCreated: statInt(stats, statRelationshipsCreated),
		RelationshipsDeleted: statInt(stats, statRelationshipsDeleted),
		PropertiesSet:        statInt(stats, statPropertiesSet),
		LabelsAdded:          statInt(stats, statLabelsAdded),
		LabelsRemoved:        statInt(stats, statLabelsRemoved),
		IndexesAdded:         statInt(stats, statIndexesAdded),
		IndexesRemoved:       statInt(stats, statIndexesRemoved),
		ConstraintsAdded:     statInt(stats, statConstraintsAdded),
		ConstraintsRemoved:   statInt(stats, statConstraintsRemoved),
	}
}

// statInt reads one counter, yielding zero for a key the server did not send and
// for a value that is not the integer the protocol requires it to be. Zero is
// the faithful reading of an absent counter here — unlike an absent plan figure,
// which means nobody counted — because the server omits a counter exactly when
// its value is zero.
func statInt(stats map[string]packstream.Value, key string) int64 {
	n, _ := stats[key].(int64)
	return n
}
