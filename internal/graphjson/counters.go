package graphjson

import "github.com/FlavioCFOliveira/GoGraph/cypher/exec"

// Counters is the published JSON shape of what one statement changed in the
// graph, canonical in SPEC/DATA_FORMATS.md § Graph Query Counters. It is the
// target of the ONE mapping this package exists to hold, exactly as PlanNode is:
// both surfaces that publish counters — `rmp graph execute` and `rmp graph
// client` — arrive here over the engine's own [exec.QueryCounters], so the byte
// identity SPEC/DATA_FORMATS.md § Graph Client Result requires is a property of
// the code rather than something a test has to keep policing.
//
// # Why these fields are NOT pointers, where PlanNode's are
//
// The neighbouring file does the opposite for a documented reason, so the
// difference is stated here rather than left for a reader to wonder at.
//
// On a plan node, zero is a FINDING: § Graph Plan Node draws a tri-state
// distinction between a figure that was counted and came to zero, a figure
// nobody counted, and a figure that cannot exist, and a plain int64 collapses
// the first two. On a counter there is no such distinction to lose.
// § Graph Query Counters rule 2 is explicit about it — a zero is omitted, an
// absent key reads as zero, and the rule names the asymmetry with the plan node
// as intended: every counter is counted for every statement that reaches this
// block, so an absent key states that the effect it names did not happen and
// there is no second state for a zero to hide. A plain int64 with `omitempty` is
// therefore the exact shape the specification asks for, and a pointer would buy
// a state the specification says does not exist.
//
// # One member covers two engine counters
//
// PropertiesWritten carries the engine's PropertiesSet and PropertiesRemoved as
// a single figure. The constraint that decides it is the protocol's: a served
// result crosses Bolt, whose statistics vocabulary has one properties counter
// and no counterpart for a removal, so the split cannot be recovered on the
// client path at all. Folding on both paths is what keeps the two surfaces
// publishing one object, and the key is named for the sum rather than for an
// assignment so that it does not claim an effect that did not occur
// (§ Graph Query Counters rule 4; SPEC/GRAPH.md § Write Counters: What a Statement Changed, rules 6 to 9).
//
// # Field order is the JSON key order
//
// encoding/json emits struct fields in declaration order, and both surfaces
// marshal this same struct, so the key order cannot differ between them. The
// order below is the order § Graph Query Counters' field-reference table
// publishes.
type Counters struct {
	NodesCreated         int64 `json:"nodesCreated,omitempty"`
	NodesDeleted         int64 `json:"nodesDeleted,omitempty"`
	RelationshipsCreated int64 `json:"relationshipsCreated,omitempty"`
	RelationshipsDeleted int64 `json:"relationshipsDeleted,omitempty"`
	PropertiesWritten    int64 `json:"propertiesWritten,omitempty"`
	LabelsAdded          int64 `json:"labelsAdded,omitempty"`
	LabelsRemoved        int64 `json:"labelsRemoved,omitempty"`
	IndexesAdded         int64 `json:"indexesAdded,omitempty"`
	IndexesRemoved       int64 `json:"indexesRemoved,omitempty"`
	ConstraintsAdded     int64 `json:"constraintsAdded,omitempty"`
	ConstraintsRemoved   int64 `json:"constraintsRemoved,omitempty"`
}

// CountersOf maps the engine's write-effect counters onto the published shape.
//
// It returns nil when there is nothing to publish — a nil argument, which is
// what the engine reports for a statement with no write surface at all, or a
// statement whose effects came to nothing — so a caller may hand it the
// accessor's result unconditionally, exactly as it may hand [Plan] either plan
// accessor's. That single return covers all three classes § Graph Query Counters
// rule 1 names: a read, a write that applied nothing, and an EXPLAIN, which
// executes nothing and therefore has no applied effect to count.
//
// The discriminator is the engine's own ContainsUpdates rather than a test of
// the eleven published members, because it is derived from the twelve engine
// counters and so cannot disagree with them.
//
// The name is CountersOf and not Counters because the published type owns that
// identifier; the -Of suffix is the one internal/graphclient already uses for
// the mappings that construct an engine representation from another form.
func CountersOf(c *exec.QueryCounters) *Counters {
	if c == nil || !c.ContainsUpdates() {
		return nil
	}
	return &Counters{
		NodesCreated:         c.NodesCreated,
		NodesDeleted:         c.NodesDeleted,
		RelationshipsCreated: c.RelationshipsCreated,
		RelationshipsDeleted: c.RelationshipsDeleted,
		// The fold. See the type's doc comment: the sum is what both paths
		// publish, and the client path reaches it with PropertiesRemoved at zero
		// because the protocol already summed the two.
		PropertiesWritten:  c.PropertiesSet + c.PropertiesRemoved,
		LabelsAdded:        c.LabelsAdded,
		LabelsRemoved:      c.LabelsRemoved,
		IndexesAdded:       c.IndexesAdded,
		IndexesRemoved:     c.IndexesRemoved,
		ConstraintsAdded:   c.ConstraintsAdded,
		ConstraintsRemoved: c.ConstraintsRemoved,
	}
}
