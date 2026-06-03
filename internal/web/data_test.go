package web

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
)

// TestSerializeGraphValue_AllKinds drives serializeGraphValue across every
// expr.Value kind the switch handles, asserting the exact JSON-compatible Go
// value produced. Most of these kinds are unreachable through the CLI (the
// knowledge graph only persists string properties via `rmp graph create`),
// but the serialiser must map them correctly should a future writer or a
// computed Cypher expression yield them, so each branch is exercised by
// constructing the expr.Value directly (SPEC/DATA_FORMATS.md § Graph Query
// Result property-type mapping).
func TestSerializeGraphValue_AllKinds(t *testing.T) {
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
			got := serializeGraphValue(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("serializeGraphValue(%v) = %#v (%T), want %#v (%T)",
					tc.in, got, got, tc.want, tc.want)
			}
		})
	}
}

// TestSerializeGraphValue_PathHitsDefault exercises the switch's default
// branch. A PathValue (KindPath) is a genuine expr.Value kind the serialiser
// does not special-case — `MATCH p = (...)-[...]->(...) RETURN p` would yield
// one — so it falls through to the default arm and is rendered via its
// String() form rather than dropped.
func TestSerializeGraphValue_PathHitsDefault(t *testing.T) {
	p := expr.PathValue{
		Nodes: []expr.NodeValue{
			{ID: 1, Labels: []string{"Requirement"}},
			{ID: 2, Labels: []string{"Test"}},
		},
		Relationships: []expr.RelationshipValue{
			{ID: 9, StartID: 1, EndID: 2, Type: "VERIFIES"},
		},
	}
	got := serializeGraphValue(p)
	want := p.String() // default branch returns v.String()
	if got != want {
		t.Errorf("path via default branch = %#v, want %#v", got, want)
	}
	// Sanity: the default arm must yield a non-empty string, not nil.
	if s, ok := got.(string); !ok || s == "" {
		t.Errorf("default branch = %#v (%T), want non-empty string", got, got)
	}
}

// TestSerializeGraphValue_NestedList exercises the KindList branch, including
// recursion into mixed-kind elements and the float NaN guard inside a list.
func TestSerializeGraphValue_NestedList(t *testing.T) {
	in := expr.ListValue{
		expr.IntegerValue(1),
		expr.StringValue("two"),
		expr.FloatValue(math.NaN()), // becomes nil inside the list
		expr.ListValue{expr.BoolValue(true)},
	}
	got := serializeGraphValue(in)

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

// TestSerializeGraphValue_NestedMap exercises the KindMap branch and its
// recursion into nested maps and lists.
func TestSerializeGraphValue_NestedMap(t *testing.T) {
	in := expr.MapValue{
		"label":  expr.StringValue("Component"),
		"weight": expr.FloatValue(0.75),
		"tags":   expr.ListValue{expr.StringValue("core"), expr.StringValue("db")},
		"meta":   expr.MapValue{"active": expr.BoolValue(true)},
	}
	got := serializeGraphValue(in)

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

// TestSerializeGraphValue_Node exercises the KindNode branch: a node carries
// its storage id, labels, and a recursively serialised property bag. This is
// the element shape the graph data endpoint returns for `MATCH (n) RETURN n`
// (SPEC/DATA_FORMATS.md § Graph View Data).
func TestSerializeGraphValue_Node(t *testing.T) {
	in := expr.NodeValue{
		ID:     17,
		Labels: []string{"Component", "CodeFile"},
		Properties: expr.MapValue{
			"name":    expr.StringValue("internal/web/data.go"),
			"lines":   expr.IntegerValue(305),
			"covered": expr.BoolValue(true),
		},
	}
	got := serializeGraphValue(in)

	want := map[string]any{
		"id":     uint64(17),
		"labels": []string{"Component", "CodeFile"},
		"properties": map[string]any{
			"name":    "internal/web/data.go",
			"lines":   int64(305),
			"covered": true,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("node = %#v, want %#v", got, want)
	}
}

// TestSerializeGraphValue_NodeNoProps confirms a node with no properties
// serialises to a non-nil empty properties map (so the JSON renders {} not
// null), via serializeProps.
func TestSerializeGraphValue_NodeNoProps(t *testing.T) {
	in := expr.NodeValue{ID: 1, Labels: []string{"Requirement"}}
	got, ok := serializeGraphValue(in).(map[string]any)
	if !ok {
		t.Fatalf("node did not serialise to map[string]any, got %T", serializeGraphValue(in))
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

// TestSerializeGraphValue_Relationship exercises the KindRelationship branch:
// an edge carries its id, type, endpoint ids, and recursively serialised
// properties. This is the element shape returned for `MATCH ()-[r]->() RETURN
// r` (SPEC/DATA_FORMATS.md § Graph View Data).
func TestSerializeGraphValue_Relationship(t *testing.T) {
	in := expr.RelationshipValue{
		ID:      9,
		StartID: 17,
		EndID:   23,
		Type:    "VERIFIES",
		Properties: expr.MapValue{
			"since": expr.StringValue("2026-06-03"),
		},
	}
	got := serializeGraphValue(in)

	want := map[string]any{
		"id":      uint64(9),
		"type":    "VERIFIES",
		"startId": uint64(17),
		"endId":   uint64(23),
		"properties": map[string]any{
			"since": "2026-06-03",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relationship = %#v, want %#v", got, want)
	}
}

// TestSerializeProps_Direct exercises serializeProps independently of a node
// or relationship, including the empty-bag (non-nil) and recursive cases.
func TestSerializeProps_Direct(t *testing.T) {
	t.Run("empty bag yields non-nil empty map", func(t *testing.T) {
		out := serializeProps(map[string]expr.Value{})
		if out == nil {
			t.Fatalf("serializeProps(empty) = nil, want non-nil empty map")
		}
		if len(out) != 0 {
			t.Errorf("serializeProps(empty) = %#v, want empty", out)
		}
	})

	t.Run("nil bag yields non-nil empty map", func(t *testing.T) {
		out := serializeProps(nil)
		if out == nil {
			t.Fatalf("serializeProps(nil) = nil, want non-nil empty map")
		}
		if len(out) != 0 {
			t.Errorf("serializeProps(nil) = %#v, want empty", out)
		}
	})

	t.Run("recursive values are serialised", func(t *testing.T) {
		out := serializeProps(map[string]expr.Value{
			"id":    expr.IntegerValue(3),
			"label": expr.StringValue("Spec"),
			"nan":   expr.FloatValue(math.NaN()),
		})
		want := map[string]any{
			"id":    int64(3),
			"label": "Spec",
			"nan":   nil,
		}
		if !reflect.DeepEqual(out, want) {
			t.Fatalf("serializeProps = %#v, want %#v", out, want)
		}
	})
}
