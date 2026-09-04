package graphjson

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
)

// This suite is the ONLY direct test of the project's value mapping, and it now
// sits beside the only implementation of it.
//
// It was written against internal/web's private copy of the mapping (rmp task
// #364) and moved here unchanged in substance when that copy was deleted and the
// three publishing surfaces were collapsed onto this package (rmp task #394). It
// travelled because it was the only fence the mapping had anywhere:
// internal/commands.serializeValue, which publishes every top-level result cell
// `rmp graph execute` and `rmp graph client` write, had no direct test at all.
// Deleting the copy without carrying its tests across would have left the
// surviving realisation less tested than the one that was removed.

// TestValue_AllKinds drives Value across every expr.Value kind the mapping
// carries a row for, asserting the exact JSON-compatible Go value produced
// (SPEC/DATA_FORMATS.md § Property-Type Mapping).
//
// Each case constructs the expr.Value directly, because that is the only way to
// reach a branch deterministically. That is a matter of test technique and NOT a
// statement about reachability: every kind below is reachable through the CLI,
// and the temporal ones persist. Measured against ./bin/rmp:
//
//	rmp graph execute -q "RETURN datetime('2026-09-04T10:11:12.123456789Z') AS dt,
//	  date('2026-09-04') AS d, localdatetime('2026-09-04T10:11:12.5') AS ldt,
//	  localtime('10:11:12.25') AS lt, time('10:11:12.75Z') AS t,
//	  duration({months:1, days:2, seconds:3, nanoseconds:400000000}) AS dur"
//
// publishes all six temporal renderings in one row; and a temporal written with
// CREATE (:Probe {at: datetime('2026-09-04T10:11:12.123456789Z')}) reads back in
// a LATER process as a temporal, with its nanoseconds intact and n.at.year
// resolving to the integer 2026. An earlier version of this comment claimed the
// opposite on both counts — that these kinds were "unreachable through the CLI"
// and that the graph "only persists string properties" — and both halves were
// measurably false.
func TestValue_AllKinds(t *testing.T) {
	cases := []struct {
		name string
		in   expr.Value
		want any
	}{
		{
			name: "nil interface maps to nil",
			in:   nil,
			want: nil,
		},
		{
			name: "KindNull maps to nil",
			in:   expr.Null,
			want: nil,
		},
		{
			name: "integer maps to int64",
			in:   expr.IntegerValue(42),
			want: int64(42),
		},
		{
			name: "negative integer maps to int64",
			in:   expr.IntegerValue(-7),
			want: int64(-7),
		},
		{
			name: "finite float maps to float64",
			in:   expr.FloatValue(3.5),
			want: float64(3.5),
		},
		{
			name: "NaN float maps to nil (JSON has no NaN)",
			in:   expr.FloatValue(math.NaN()),
			want: nil,
		},
		{
			name: "+Inf float maps to nil",
			in:   expr.FloatValue(math.Inf(1)),
			want: nil,
		},
		{
			name: "-Inf float maps to nil",
			in:   expr.FloatValue(math.Inf(-1)),
			want: nil,
		},
		{
			name: "string maps to string",
			in:   expr.StringValue("traceability"),
			want: "traceability",
		},
		{
			name: "bool true maps to bool",
			in:   expr.BoolValue(true),
			want: true,
		},
		{
			name: "bool false maps to bool",
			in:   expr.BoolValue(false),
			want: false,
		},
		{
			name: "date maps to YYYY-MM-DD",
			in:   expr.NewDate(2026, 6, 3),
			want: "2026-06-03",
		},
		{
			name: "datetime maps to RFC3339Nano UTC",
			in:   expr.NewDateTime(2026, 6, 3, 14, 30, 15, 123456789, time.UTC),
			want: "2026-06-03T14:30:15.123456789Z",
		},
		{
			name: "local datetime maps to zoneless timestamp",
			in:   expr.NewLocalDateTime(2026, 6, 3, 14, 30, 15, 123456789),
			want: "2026-06-03T14:30:15.123456789",
		},
		{
			name: "local time maps to its String form",
			in:   expr.NewLocalTime(14, 30, 15, 123456789),
			want: "14:30:15.123456789",
		},
		{
			name: "time with offset maps to its String form",
			in:   expr.NewTime(14, 30, 15, 123456789, 3600),
			want: "14:30:15.123456789+01:00",
		},
		{
			name: "duration maps to ISO-8601 String form",
			in:   expr.NewDuration(1, 2, 3, 400000000),
			want: "P1M2DT3.4S",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Value(tc.in, nil)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Value(%v, nil) = %#v (%T), want %#v (%T)",
					tc.in, got, got, tc.want, tc.want)
			}
		})
	}
}

// TestValue_KindWithNoRowFallsBackToStringWithoutAHook is the surviving half of
// the test that used to be called TestSerializeGraphValue_PathHitsDefault.
//
// What it fenced there is gone, and it is worth saying exactly what: it asserted
// that internal/web's copy of the mapping rendered a path through its default
// arm, and it described KindPath as "a genuine expr.Value kind the serialiser
// does not special-case". That branch was UNREACHABLE in production — every call
// into that copy arrived through a property bag, and a property value is never a
// graph element — so the test fenced a branch no request could enter. Deleting
// the copy deleted the branch with it.
//
// The behaviour it described did not go away, though; it became a documented
// contract instead of an accident. A kind this package carries no row for is
// handed to the caller's graphjson.Unmapped, and a caller that passes nil — which
// is what the graph data endpoint passes, because it publishes no path object —
// gets the engine's own String form. So the assertion is kept and re-pointed at
// the realisation that survived, where it now pins a live configuration rather
// than a dead arm.
func TestValue_KindWithNoRowFallsBackToStringWithoutAHook(t *testing.T) {
	p := expr.PathValue{
		Nodes: []expr.NodeValue{
			{ID: 1, Labels: []string{"Requirement"}},
			{ID: 2, Labels: []string{"Test"}},
		},
		Relationships: []expr.RelationshipValue{
			{ID: 9, StartID: 1, EndID: 2, Type: "VERIFIES"},
		},
	}
	got := Value(p, nil)
	want := p.String()
	if got != want {
		t.Errorf("Value(path, nil) = %#v, want %#v", got, want)
	}
	// Sanity: the fallback must yield a non-empty string, not nil.
	if s, ok := got.(string); !ok || s == "" {
		t.Errorf("Value(path, nil) = %#v (%T), want non-empty string", got, got)
	}
}

// TestValue_KindWithNoRowReachesTheHook is the other half: with a
// graphjson.Unmapped in hand, the caller decides, and it decides at EVERY depth.
//
// The nesting cases are the ones that matter. internal/commands renders a path
// through this seam, and its published shape must not depend on where in a
// result the path was found — a path returned in its own column, one inside a
// returned list, and one inside a returned map are one rendering
// (SPEC/DATA_FORMATS.md § Graph element mapping). A recursion that forgot to
// carry the hook would still pass the top-level case.
func TestValue_KindWithNoRowReachesTheHook(t *testing.T) {
	const sentinel = "rendered by the caller"
	calls := 0
	hook := func(v expr.Value) any {
		if v.Kind() != expr.KindPath {
			t.Errorf("the hook was handed a %v, want a path: only a kind with no row here reaches it", v.Kind())
		}
		calls++
		return sentinel
	}

	p := expr.PathValue{
		Nodes:         []expr.NodeValue{{ID: 1, Labels: []string{"Component"}}},
		Relationships: []expr.RelationshipValue{},
	}

	t.Run("top level", func(t *testing.T) {
		if got := Value(p, hook); got != sentinel {
			t.Errorf("Value(path, hook) = %#v, want %#v", got, sentinel)
		}
	})

	t.Run("inside a list", func(t *testing.T) {
		got := Value(expr.ListValue{expr.IntegerValue(1), p}, hook)
		want := []any{int64(1), sentinel}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Value(list, hook) = %#v, want %#v", got, want)
		}
	})

	t.Run("inside a map", func(t *testing.T) {
		got := Value(expr.MapValue{"route": p}, hook)
		want := map[string]any{"route": sentinel}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Value(map, hook) = %#v, want %#v", got, want)
		}
	})

	t.Run("inside a property bag", func(t *testing.T) {
		n := expr.NodeValue{ID: 3, Labels: []string{"Component"}, Properties: expr.MapValue{"route": p}}
		got, ok := Node(n, hook)["properties"].(map[string]any)
		if !ok {
			t.Fatalf("properties did not map to map[string]any")
		}
		if got["route"] != sentinel {
			t.Errorf("node property = %#v, want %#v", got["route"], sentinel)
		}
	})

	if calls != 4 {
		t.Errorf("the hook was called %d times, want 4: a case above did not reach it at all, which "+
			"would make its assertion pass for the wrong reason", calls)
	}
}

// TestValue_NestedList exercises the KindList branch, including recursion into
// mixed-kind elements and the float NaN guard inside a list.
func TestValue_NestedList(t *testing.T) {
	in := expr.ListValue{
		expr.IntegerValue(1),
		expr.StringValue("two"),
		expr.FloatValue(math.NaN()), // becomes nil inside the list
		expr.ListValue{expr.BoolValue(true)},
	}
	got := Value(in, nil)

	want := []any{
		int64(1),
		"two",
		nil,
		[]any{true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nested list = %#v, want %#v", got, want)
	}
}

// TestValue_NestedMap exercises the KindMap branch and its recursion into nested
// maps and lists.
func TestValue_NestedMap(t *testing.T) {
	in := expr.MapValue{
		"label":  expr.StringValue("Component"),
		"weight": expr.FloatValue(0.75),
		"tags":   expr.ListValue{expr.StringValue("core"), expr.StringValue("db")},
		"meta":   expr.MapValue{"active": expr.BoolValue(true)},
	}
	got := Value(in, nil)

	want := map[string]any{
		"label":  "Component",
		"weight": float64(0.75),
		"tags":   []any{"core", "db"},
		"meta":   map[string]any{"active": true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nested map = %#v, want %#v", got, want)
	}
}

// TestValue_Node exercises the KindNode branch: a node carries its storage id,
// its labels, and a recursively mapped property bag. This is the element shape
// `rmp graph execute` publishes for a node in a result cell AND the one the
// graph data endpoint publishes in its nodes array — one shape, because one
// piece of code produces both (SPEC/DATA_FORMATS.md § Graph element mapping;
// § Graph View Data).
func TestValue_Node(t *testing.T) {
	in := expr.NodeValue{
		ID:     17,
		Labels: []string{"Component", "CodeFile"},
		Properties: expr.MapValue{
			"name":    expr.StringValue("internal/web/data.go"),
			"lines":   expr.IntegerValue(305),
			"covered": expr.BoolValue(true),
		},
	}
	want := map[string]any{
		"id":     uint64(17),
		"labels": []string{"Component", "CodeFile"},
		"properties": map[string]any{
			"name":    "internal/web/data.go",
			"lines":   int64(305),
			"covered": true,
		},
	}

	if got := Value(in, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("Value(node, nil) = %#v, want %#v", got, want)
	}
	// Reaching the row through Value and reaching it through Node must produce
	// the same object: the two entry points exist because the callers need
	// different Go types, not because they publish different JSON.
	if got := Node(in, nil); !reflect.DeepEqual(any(got), want) {
		t.Fatalf("Node(node, nil) = %#v, want %#v", got, want)
	}
}

// TestValue_NodeNoProps confirms a node with no properties maps to a NON-NIL
// empty properties object, so the JSON renders {} rather than null.
func TestValue_NodeNoProps(t *testing.T) {
	in := expr.NodeValue{ID: 1, Labels: []string{"Requirement"}}
	got, ok := Value(in, nil).(map[string]any)
	if !ok {
		t.Fatalf("node did not map to map[string]any, got %T", Value(in, nil))
	}
	props, ok := got["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties not map[string]any, got %T", got["properties"])
	}
	if props == nil {
		t.Errorf("properties = nil, want non-nil empty map")
	}
	if len(props) != 0 {
		t.Errorf("properties = %#v, want empty", props)
	}
}

// TestValue_Relationship exercises the KindRelationship branch: an edge carries
// its id, its type, its endpoint ids, and a recursively mapped property bag.
// This is the element shape published for `MATCH ()-[r]->() RETURN r` on the CLI
// and in the endpoint's edges array.
func TestValue_Relationship(t *testing.T) {
	in := expr.RelationshipValue{
		ID:      9,
		StartID: 17,
		EndID:   23,
		Type:    "VERIFIES",
		Properties: expr.MapValue{
			"since": expr.StringValue("2026-06-03"),
		},
	}
	want := map[string]any{
		"id":      uint64(9),
		"type":    "VERIFIES",
		"startId": uint64(17),
		"endId":   uint64(23),
		"properties": map[string]any{
			"since": "2026-06-03",
		},
	}

	if got := Value(in, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("Value(relationship, nil) = %#v, want %#v", got, want)
	}
	if got := Relationship(in, nil); !reflect.DeepEqual(any(got), want) {
		t.Fatalf("Relationship(relationship, nil) = %#v, want %#v", got, want)
	}
}

// TestProperties_Direct exercises the property-bag mapping independently of a
// node or a relationship, including the empty-bag and nil-bag cases that must
// still yield a non-nil map, and the recursion into the values.
func TestProperties_Direct(t *testing.T) {
	t.Run("empty bag yields non-nil empty map", func(t *testing.T) {
		out := properties(map[string]expr.Value{}, nil)
		if out == nil {
			t.Fatalf("properties(empty) = nil, want non-nil empty map")
		}
		if len(out) != 0 {
			t.Errorf("properties(empty) = %#v, want empty", out)
		}
	})

	t.Run("nil bag yields non-nil empty map", func(t *testing.T) {
		out := properties(nil, nil)
		if out == nil {
			t.Fatalf("properties(nil) = nil, want non-nil empty map")
		}
		if len(out) != 0 {
			t.Errorf("properties(nil) = %#v, want empty", out)
		}
	})

	t.Run("recursive values are mapped", func(t *testing.T) {
		out := properties(map[string]expr.Value{
			"id":    expr.IntegerValue(3),
			"label": expr.StringValue("Spec"),
			"nan":   expr.FloatValue(math.NaN()),
		}, nil)
		want := map[string]any{
			"id":    int64(3),
			"label": "Spec",
			"nan":   nil,
		}
		if !reflect.DeepEqual(out, want) {
			t.Fatalf("properties = %#v, want %#v", out, want)
		}
	})
}
