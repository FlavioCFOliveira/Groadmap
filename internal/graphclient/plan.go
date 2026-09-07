package graphclient

import (
	"time"

	"github.com/FlavioCFOliveira/GoGraph/bolt/packstream"
	"github.com/FlavioCFOliveira/GoGraph/cypher/exec"
)

// The protocol's own key names for a plan node. They are the Bolt plan-metadata
// names rather than the ones Groadmap publishes, and the difference is the point
// of this file: the wire nests four fields under `args` and carries the duration
// in nanoseconds under `time`, while the published shape is flat and names its
// keys differently (SPEC/DATA_FORMATS.md § Graph Plan Node).
//
// Nothing here maps to JSON. This file inverts the protocol encoding onto the
// ENGINE's representation, and the single mapping in internal/graphjson then
// publishes it — which is what makes `client` and `execute` byte-identical by
// construction rather than by assertion (§ Graph Client Result).
const (
	planKeyOperatorType = "operatorType"
	planKeyArgs         = "args"
	planKeyChildren     = "children"
	planKeyDbHits       = "dbHits"
	planKeyRows         = "rows"
	planKeyTime         = "time"

	planArgDetails             = "Details"
	planArgRowsRemoved         = "RowsRemovedByFilter"
	planArgEstimatedRows       = "EstimatedRows"
	planArgEstimatedRowsSource = "EstimatedRowsSource"
)

// planOf reads the plan tree out of the final success's metadata under key, which
// is "plan" for an EXPLAIN and "profile" for a PROFILE. At most one of the two is
// ever present, and neither is present for an unprefixed statement, so a nil
// return is the ordinary case.
func planOf(metadata map[string]packstream.Value, key string) *exec.PlanNode {
	raw, ok := metadata[key].(map[string]packstream.Value)
	if !ok {
		return nil
	}
	return planNodeOf(raw)
}

// planNodeOf inverts one protocol plan node onto the engine's [exec.PlanNode].
//
// Absence is carried faithfully, because absence is information here: a `dbHits`
// the server did not write means nobody counted the figure, and turning it into a
// zero would republish as a measurement something that was never measured. The
// known-flags this sets are what the published mapping reads to decide whether a
// key appears at all (SPEC/DATA_FORMATS.md § Graph Plan Node rules 8 and 10).
func planNodeOf(m map[string]packstream.Value) *exec.PlanNode {
	node := &exec.PlanNode{}
	node.Name, _ = m[planKeyOperatorType].(string)

	if args, ok := m[planKeyArgs].(map[string]packstream.Value); ok {
		node.Detail, _ = args[planArgDetails].(string)

		// The estimate pair is written as a pair by the server and is read as
		// one: a source without a number, or a number without a source, is not a
		// state the wire produces, and inventing either half here would defeat
		// rule 4.
		if rows, ok := args[planArgEstimatedRows].(int64); ok {
			if source, ok := args[planArgEstimatedRowsSource].(string); ok {
				node.Est = exec.PlanEstimate{Rows: rows, Source: estimateSourceOf(source)}
			}
		}

		if removed, ok := args[planArgRowsRemoved].(int64); ok {
			node.RowsRemovedByFilter = removed
			node.RowsRemovedByFilterKnown = true
		}
	}

	if hits, ok := m[planKeyDbHits].(int64); ok {
		node.DbHits = hits
		node.DbHitsKnown = true
	}
	if rows, ok := m[planKeyRows].(int64); ok {
		node.Rows = rows
		node.Profiled = true
	}
	if ns, ok := m[planKeyTime].(int64); ok {
		// The wire carries whole nanoseconds and a Duration IS whole
		// nanoseconds, so this conversion loses nothing and the published
		// timeNs is the same integer the server measured.
		node.Time = time.Duration(ns)
	}

	if children, ok := m[planKeyChildren].([]packstream.Value); ok && len(children) > 0 {
		node.Children = make([]exec.PlanNode, 0, len(children))
		for _, child := range children {
			raw, ok := child.(map[string]packstream.Value)
			if !ok {
				continue
			}
			node.Children = append(node.Children, *planNodeOf(raw))
		}
	}
	return node
}

// estimateSourceOf inverts [exec.EstimateSource.String]. The engine publishes no
// parser of its own, so this is the inverse of the three names it writes; an
// unrecognised name yields [exec.EstimateAbsent], which omits the pair rather
// than publishing a provenance this build does not understand.
func estimateSourceOf(name string) exec.EstimateSource {
	switch name {
	case "exact":
		return exec.EstimateExact
	case "stats":
		return exec.EstimateStats
	case "heuristic":
		return exec.EstimateHeuristic
	default:
		return exec.EstimateAbsent
	}
}
