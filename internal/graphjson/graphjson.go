// Package graphjson holds the project's ONE realisation of the mapping from an
// engine value to the JSON Groadmap publishes.
//
// SPEC/DATA_FORMATS.md § Property-Type Mapping and § Graph element mapping are
// canonical for WHAT the mapping produces; this package neither adds to that nor
// departs from it. What it settles is the question those sections do not:
// § One Realisation of the Mapping fixes how many times the mapping may be
// written, and the answer is once. Three surfaces are bound by that rule —
// `rmp graph execute`, `rmp graph client` and the web interface's graph data
// endpoint — and all three arrive here.
//
// # Why a package rather than a function inside one of the callers
//
// internal/commands imports internal/web, so the dependency cannot run the other
// way, and a mapping from an engine value to published JSON is neither a CLI
// concern nor an HTTP one. That is the same reasoning that gave
// internal/graphstore, internal/graphlock and internal/backoff packages of their
// own (SPEC/ARCHITECTURE.md § Modules and Responsibilities, module 8).
//
// # What is here and what is not
//
// Here: the property-type mapping in full, and the Node and Relationship rows of
// the element mapping. Not here: the Path row. The graph data endpoint publishes
// no path object at all — a path in a result is decomposed into the elements it
// contains — so the Path rendering already has exactly one realisation by having
// exactly one producer, and it stays with the surface that produces it. The
// Unmapped seam below is how that surface reaches it without this package
// growing a second opinion about the rest.
//
// # Why the mapping is expressed as a value rather than as a document
//
// The two surfaces publish different documents around the same values: the CLI
// places them in the {columns, rows} object of § Graph Query Result, and the
// endpoint places its element objects in the node-and-edge object of
// § Graph View Data. This package therefore returns the mapped VALUE and never
// the document, so sharing it constrains neither surface's shape.
package graphjson

import (
	"math"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
)

// Unmapped renders a value whose kind this package carries no row for.
//
// Exactly one kind is in that position today: a path, whose rendering belongs to
// the only surface that publishes one. A caller that publishes such a value
// passes a function; a caller that cannot receive one passes nil, and the value
// then falls back to the engine's own string form — which is what both callers
// did for an unhandled kind before the mapping was shared, so passing nil
// changes no byte either of them could previously produce.
//
// The seam is deliberately not a "path hook". It is defined by what this package
// does NOT carry rather than by what the caller wants to add, so a kind the
// engine gains later reaches the caller's fallback rather than silently
// acquiring a rendering here that no specification fixed.
type Unmapped func(expr.Value) any

// Value maps a single engine value onto the JSON-compatible Go value the
// published formats require (SPEC/DATA_FORMATS.md § Property-Type Mapping, and
// the Node and Relationship rows of § Graph element mapping). It recurses
// through lists, maps and property bags, so a value nested at any depth is
// mapped exactly as the same value returned in its own right would be.
//
// unmapped renders a kind this package carries no row for, and may be nil; see
// Unmapped.
//
// A nil value and a NULL value both map to nil, which the JSON encoders render
// as null. A non-finite float has no JSON representation at all and maps to nil
// for the same reason.
func Value(v expr.Value, unmapped Unmapped) any {
	if v == nil {
		return nil
	}
	switch v.Kind() {
	case expr.KindNull:
		return nil

	case expr.KindInteger:
		iv, _ := v.(expr.IntegerValue)
		return int64(iv)

	case expr.KindFloat:
		fv, _ := v.(expr.FloatValue)
		f := float64(fv)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return f

	case expr.KindString:
		sv, _ := v.(expr.StringValue)
		return string(sv)

	case expr.KindBool:
		bv, _ := v.(expr.BoolValue)
		return bool(bv)

	case expr.KindDate:
		dv, _ := v.(expr.DateValue)
		return dv.ToTime().UTC().Format("2006-01-02")

	case expr.KindDateTime:
		dtv, _ := v.(expr.DateTimeValue)
		return dtv.T.UTC().Format(time.RFC3339Nano)

	case expr.KindLocalDateTime:
		ldtv, _ := v.(expr.LocalDateTimeValue)
		return ldtv.T.Format("2006-01-02T15:04:05.999999999")

	case expr.KindLocalTime:
		ltv, _ := v.(expr.LocalTimeValue)
		return ltv.String()

	case expr.KindTime:
		tv, _ := v.(expr.TimeValue)
		return tv.String()

	case expr.KindDuration:
		durv, _ := v.(expr.DurationValue)
		return durv.String()

	case expr.KindList:
		lv, _ := v.(expr.ListValue)
		out := make([]any, len(lv))
		for i, elem := range lv {
			out[i] = Value(elem, unmapped)
		}
		return out

	case expr.KindMap:
		mv, _ := v.(expr.MapValue)
		out := make(map[string]any, len(mv))
		for k, val := range mv {
			out[k] = Value(val, unmapped)
		}
		return out

	case expr.KindNode:
		nv, _ := v.(expr.NodeValue)
		return Node(nv, unmapped)

	case expr.KindRelationship:
		rv, _ := v.(expr.RelationshipValue)
		return Relationship(rv, unmapped)

	default:
		if unmapped != nil {
			return unmapped(v)
		}
		return v.String()
	}
}

// Node maps a node onto the element object the published formats require: its
// storage id, its labels, and its property bag mapped through Value.
//
// It returns the concrete map rather than any, because a caller assembling a
// document of element objects needs the map itself; Value reaches it for a node
// found in a result cell.
func Node(nv expr.NodeValue, unmapped Unmapped) map[string]any {
	return map[string]any{
		"id":         nv.ID,
		"labels":     nv.Labels,
		"properties": properties(nv.Properties, unmapped),
	}
}

// Relationship maps a relationship onto the element object the published formats
// require: its storage id, its type, its two endpoint ids, and its property bag
// mapped through Value. See Node for why it returns the concrete map.
func Relationship(rv expr.RelationshipValue, unmapped Unmapped) map[string]any {
	return map[string]any{
		"id":         rv.ID,
		"type":       rv.Type,
		"startId":    rv.StartID,
		"endId":      rv.EndID,
		"properties": properties(rv.Properties, unmapped),
	}
}

// properties maps a property bag's values through Value, producing a NON-NIL map
// so that a bag with no entries renders as {} rather than as null. The
// distinction is published: an element always carries a properties object.
func properties(props map[string]expr.Value, unmapped Unmapped) map[string]any {
	out := make(map[string]any, len(props))
	for k, val := range props {
		out[k] = Value(val, unmapped)
	}
	return out
}
