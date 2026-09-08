package patches

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

func TestSubtreeCollector_ScalarValue(t *testing.T) {
	// Build index with patch at "users.alice"
	patch := ir.FromMap(map[string]*ir.Node{
		"users": ir.FromMap(map[string]*ir.Node{
			"alice": ir.FromString("patched"),
		}),
	})
	entries := []*dlog.Entry{{Commit: 1, Patch: patch}}
	index := BuildPatchIndex(entries)

	collector := NewSubtreeCollector(index)

	// Simulate events for: { "users": { "alice": "data" } }
	events := []stream.Event{
		{Type: stream.EventBeginObject},            // path=""
		{Type: stream.EventKey, Key: "users"},      // path="users"
		{Type: stream.EventBeginObject},            // path="users"
		{Type: stream.EventKey, Key: "alice"},      // path="users.alice" - matches!
		{Type: stream.EventString, String: "data"}, // path="users.alice" - value
		{Type: stream.EventEndObject},              // path="users"
		{Type: stream.EventEndObject},              // path=""
	}

	var collected *CollectedSubtree
	for _, evt := range events {
		evt := evt
		result, err := collector.ProcessEvent(&evt)
		if err != nil {
			t.Fatalf("ProcessEvent error: %v", err)
		}
		if result != nil {
			collected = result
		}
	}

	if collected == nil {
		t.Fatal("expected to collect subtree at users.alice")
	}
	if collected.Path != "users.alice" {
		t.Errorf("expected path users.alice, got %s", collected.Path)
	}
	if collected.Node.Type != ir.StringType {
		t.Errorf("expected StringType, got %v", collected.Node.Type)
	}
	if collected.Node.String != "data" {
		t.Errorf("expected 'data', got %s", collected.Node.String)
	}
}

func TestSubtreeCollector_ContainerValue(t *testing.T) {
	// A patch that states something about "config" as a whole -- here that it is empty
	// -- is rooted there, so the base's container is what gets collected.
	patch := ir.FromMap(map[string]*ir.Node{
		"config": ir.FromMap(map[string]*ir.Node{}),
	})
	entries := []*dlog.Entry{{Commit: 1, Patch: patch}}
	index := BuildPatchIndex(entries)

	collector := NewSubtreeCollector(index)

	// Simulate events for: { "config": { "a": 1, "b": 2 } }
	events := []stream.Event{
		{Type: stream.EventBeginObject},
		{Type: stream.EventKey, Key: "config"}, // path="config" - matches!
		{Type: stream.EventBeginObject},        // Container start at "config"
		{Type: stream.EventKey, Key: "a"},
		{Type: stream.EventInt, Int: 1},
		{Type: stream.EventKey, Key: "b"},
		{Type: stream.EventInt, Int: 2},
		{Type: stream.EventEndObject}, // Container end
		{Type: stream.EventEndObject},
	}

	var collected *CollectedSubtree
	for _, evt := range events {
		evt := evt
		result, err := collector.ProcessEvent(&evt)
		if err != nil {
			t.Fatalf("ProcessEvent error: %v", err)
		}
		if result != nil {
			collected = result
		}
	}

	if collected == nil {
		t.Fatal("expected to collect subtree at config")
	}
	if collected.Path != "config" {
		t.Errorf("expected path config, got %s", collected.Path)
	}
	if collected.Node.Type != ir.ObjectType {
		t.Errorf("expected ObjectType, got %v", collected.Node.Type)
	}
	if len(collected.Node.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(collected.Node.Fields))
	}
}

func TestSubtreeCollector_ArrayIsCollectedWhole(t *testing.T) {
	// An array in a patch is a value, so a patch that IS an array is rooted at the
	// document and the whole base is collected for it.
	patch := ir.FromSlice([]*ir.Node{
		ir.FromString("first"),
		ir.FromString("patched"),
	})
	entries := []*dlog.Entry{{Commit: 1, Patch: patch}}
	index := BuildPatchIndex(entries)

	collector := NewSubtreeCollector(index)

	// Simulate events for: ["a", "b", "c"]
	events := []stream.Event{
		{Type: stream.EventBeginArray},
		{Type: stream.EventString, String: "a"},
		{Type: stream.EventString, String: "b"},
		{Type: stream.EventString, String: "c"},
		{Type: stream.EventEndArray},
	}

	var collected *CollectedSubtree
	for _, evt := range events {
		evt := evt
		result, err := collector.ProcessEvent(&evt)
		if err != nil {
			t.Fatalf("ProcessEvent error: %v", err)
		}
		if result != nil {
			collected = result
		}
	}

	if collected == nil {
		t.Fatal("expected to collect the document")
	}
	if collected.Path != "" {
		t.Errorf("expected path %q, got %q", "", collected.Path)
	}
	if collected.Node.Type != ir.ArrayType || len(collected.Node.Values) != 3 {
		t.Errorf("expected the 3-element base array, got %v", collected.Node)
	}
}

func TestSubtreeCollector_ArrayUnderField(t *testing.T) {
	// The same one level down: the array under "items" is the root, not the element
	// inside it, and the base's array is collected at "items".
	patch := ir.FromMap(map[string]*ir.Node{
		"items": ir.FromSlice([]*ir.Node{
			ir.FromMap(map[string]*ir.Node{
				"nested": ir.FromString("v"),
			}),
		}),
	})
	entries := []*dlog.Entry{{Commit: 1, Patch: patch}}
	index := BuildPatchIndex(entries)

	collector := NewSubtreeCollector(index)

	// Simulate events for: { "items": [ { "x": 1 } ] }
	events := []stream.Event{
		{Type: stream.EventBeginObject},
		{Type: stream.EventKey, Key: "items"},
		{Type: stream.EventBeginArray}, // path="items" - matches!
		{Type: stream.EventBeginObject},
		{Type: stream.EventKey, Key: "x"},
		{Type: stream.EventInt, Int: 1},
		{Type: stream.EventEndObject},
		{Type: stream.EventEndArray}, // End container at items
		{Type: stream.EventEndObject},
	}

	var collected *CollectedSubtree
	for _, evt := range events {
		evt := evt
		result, err := collector.ProcessEvent(&evt)
		if err != nil {
			t.Fatalf("ProcessEvent error: %v", err)
		}
		if result != nil {
			collected = result
		}
	}

	if collected == nil {
		t.Fatal("expected to collect subtree at items")
	}
	if collected.Path != "items" {
		t.Errorf("expected path items, got %s", collected.Path)
	}
	if collected.Node.Type != ir.ArrayType {
		t.Errorf("expected ArrayType, got %v", collected.Node.Type)
	}
}

func TestSubtreeCollector_NoMatch(t *testing.T) {
	// Build index with patch at "other"
	patch := ir.FromMap(map[string]*ir.Node{
		"other": ir.FromString("data"),
	})
	entries := []*dlog.Entry{{Commit: 1, Patch: patch}}
	index := BuildPatchIndex(entries)

	collector := NewSubtreeCollector(index)

	// Events that don't match "other"
	events := []stream.Event{
		{Type: stream.EventBeginObject},
		{Type: stream.EventKey, Key: "users"},
		{Type: stream.EventString, String: "data"},
		{Type: stream.EventEndObject},
	}

	for _, evt := range events {
		evt := evt
		result, err := collector.ProcessEvent(&evt)
		if err != nil {
			t.Fatalf("ProcessEvent error: %v", err)
		}
		if result != nil {
			t.Errorf("unexpected collection at path %s", result.Path)
		}
	}
}

func TestSubtreeCollector_MultiplePatches(t *testing.T) {
	// Build index with patches at "a" and "b"
	patch := ir.FromMap(map[string]*ir.Node{
		"a": ir.FromString("val-a"),
		"b": ir.FromString("val-b"),
	})
	entries := []*dlog.Entry{{Commit: 1, Patch: patch}}
	index := BuildPatchIndex(entries)

	collector := NewSubtreeCollector(index)

	// Events for: { "a": "x", "b": "y", "c": "z" }
	events := []stream.Event{
		{Type: stream.EventBeginObject},
		{Type: stream.EventKey, Key: "a"},       // matches
		{Type: stream.EventString, String: "x"}, // collected
		{Type: stream.EventKey, Key: "b"},       // matches
		{Type: stream.EventString, String: "y"}, // collected
		{Type: stream.EventKey, Key: "c"},       // no match
		{Type: stream.EventString, String: "z"}, // not collected
		{Type: stream.EventEndObject},
	}

	var results []*CollectedSubtree
	for _, evt := range events {
		evt := evt
		result, err := collector.ProcessEvent(&evt)
		if err != nil {
			t.Fatalf("ProcessEvent error: %v", err)
		}
		if result != nil {
			results = append(results, result)
		}
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 collected subtrees, got %d", len(results))
	}
	if results[0].Path != "a" || results[0].Node.String != "x" {
		t.Errorf("first result: path=%s, value=%s", results[0].Path, results[0].Node.String)
	}
	if results[1].Path != "b" || results[1].Node.String != "y" {
		t.Errorf("second result: path=%s, value=%s", results[1].Path, results[1].Node.String)
	}
}
