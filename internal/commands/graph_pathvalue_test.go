package commands

import (
	"reflect"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
)

// This file fences the ONE part of the published value mapping that stays in
// this package: the Path row of SPEC/DATA_FORMATS.md § Graph element mapping.
//
// Everything else serializeValue publishes now comes from internal/graphjson,
// which carries its own suite. The Path row does not go there because the CLI is
// the only surface that publishes a path — the graph data endpoint decomposes one
// into the elements it contains — so it already has exactly one realisation by
// having exactly one producer (SPEC/DATA_FORMATS.md § One Realisation of the
// Mapping).
//
// It is fenced HERE because that is where it lives, and because before the
// mapping was shared this function had no direct test of any kind, while the copy
// that was deleted had one. Collapsing the two must not leave the surviving code
// less covered than the code that went.

// TestSerializeValue_Path asserts the object a path publishes: the nodes it
// visits and the relationships it traverses, each rendered as the element object
// § Graph element mapping fixes, in path order.
func TestSerializeValue_Path(t *testing.T) {
	p := expr.PathValue{
		Nodes: []expr.NodeValue{
			{ID: 17, Labels: []string{"Component"}, Properties: expr.MapValue{
				"name": expr.StringValue("internal/graphjson"),
			}},
			{ID: 23, Labels: []string{"Spec"}},
		},
		Relationships: []expr.RelationshipValue{
			{ID: 9, StartID: 17, EndID: 23, Type: "SPECIFIED_BY", Properties: expr.MapValue{
				"since": expr.NewDate(2026, 6, 3),
			}},
		},
	}

	want := map[string]any{
		"nodes": []any{
			map[string]any{
				"id":         uint64(17),
				"labels":     []string{"Component"},
				"properties": map[string]any{"name": "internal/graphjson"},
			},
			map[string]any{
				"id":         uint64(23),
				"labels":     []string{"Spec"},
				"properties": map[string]any{},
			},
		},
		"relationships": []any{
			map[string]any{
				"id":         uint64(9),
				"type":       "SPECIFIED_BY",
				"startId":    uint64(17),
				"endId":      uint64(23),
				"properties": map[string]any{"since": "2026-06-03"},
			},
		},
	}

	if got := serializeValue(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("serializeValue(path) = %#v,\nwant %#v", got, want)
	}
}

// TestSerializeValue_PathAtEveryDepth asserts that where a path was found does
// not change how it is published.
//
// This is the property the graphjson.Unmapped seam exists to preserve. The
// shared mapping recurses through lists, maps and property bags, and it carries
// the caller's renderer with it; a recursion that dropped the renderer would
// publish the top-level path as the specified object and the nested ones as a
// Go-rendered string, which is exactly the class of divergence the single
// realisation was introduced to make impossible.
func TestSerializeValue_PathAtEveryDepth(t *testing.T) {
	p := expr.PathValue{
		Nodes:         []expr.NodeValue{{ID: 1, Labels: []string{"Component"}}},
		Relationships: []expr.RelationshipValue{},
	}
	want := serializeValue(p)
	if _, isObject := want.(map[string]any); !isObject {
		t.Fatalf("a path at the top level published %T, want the element object; the rest of this "+
			"test would then compare one wrong answer with another", want)
	}

	t.Run("inside a list", func(t *testing.T) {
		got := serializeValue(expr.ListValue{p})
		if !reflect.DeepEqual(got, []any{want}) {
			t.Errorf("path inside a list = %#v, want %#v", got, []any{want})
		}
	})

	t.Run("inside a map", func(t *testing.T) {
		got := serializeValue(expr.MapValue{"route": p})
		if !reflect.DeepEqual(got, map[string]any{"route": want}) {
			t.Errorf("path inside a map = %#v, want %#v", got, map[string]any{"route": want})
		}
	})

	t.Run("inside a property bag", func(t *testing.T) {
		n := expr.NodeValue{ID: 2, Properties: expr.MapValue{"route": p}}
		node, ok := serializeValue(n).(map[string]any)
		if !ok {
			t.Fatalf("a node published %T, want map[string]any", serializeValue(n))
		}
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Fatalf("properties published %T, want map[string]any", node["properties"])
		}
		if !reflect.DeepEqual(props["route"], want) {
			t.Errorf("path inside a property bag = %#v, want %#v", props["route"], want)
		}
	})
}

// TestSerializePath_NonPathKeepsTheHistoricFallback pins what happens to a value
// of a kind NEITHER the shared mapping nor this package carries a row for.
//
// There is no such kind in the pinned engine — internal/testenv's mapping gate
// fails the build if one appears — so this cannot be reached from a statement.
// It is pinned because serializeValue's behaviour for it changed hands: it used
// to be the default arm of this package's own switch, and it is now the
// graphjson.Unmapped this package supplies. The answer must not have changed
// with the ownership: the engine's own string form, exactly as before.
func TestSerializePath_NonPathKeepsTheHistoricFallback(t *testing.T) {
	v := expr.StringValue("not a path")
	if got, want := serializePath(v), v.String(); got != want {
		t.Errorf("serializePath(non-path) = %#v, want %#v", got, want)
	}
}
