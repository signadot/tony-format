package patches

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

func TestSubtreeCollector_ScalarValue(t *testing.T) {
	// Build index with patch at "users.alice"
	patch := ir.FromMap(map[string]*ir.Node{
		"users": ir.FromMap(map[string]*ir.Node{
			"alice": ir.FromString("patched").WithTag(tx.PatchRootTag),
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
	// Build index with patch at "config"
	patch := ir.FromMap(map[string]*ir.Node{
		"config": ir.FromMap(map[string]*ir.Node{
			"nested": ir.FromString("value"),
		}).WithTag(tx.PatchRootTag),
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

func TestSubtreeCollector_ArrayElement(t *testing.T) {
	// Build index with patch at "[1]"
	patch := ir.FromSlice([]*ir.Node{
		ir.FromString("first"),
		ir.FromString("patched").WithTag(tx.PatchRootTag),
	})
	entries := []*dlog.Entry{{Commit: 1, Patch: patch}}
	index := BuildPatchIndex(entries)

	collector := NewSubtreeCollector(index)

	// Simulate events for: ["a", "b", "c"]
	events := []stream.Event{
		{Type: stream.EventBeginArray},
		{Type: stream.EventString, String: "a"}, // path="[0]"
		{Type: stream.EventString, String: "b"}, // path="[1]" - matches!
		{Type: stream.EventString, String: "c"}, // path="[2]"
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
		t.Fatal("expected to collect subtree at [1]")
	}
	if collected.Path != "[1]" {
		t.Errorf("expected path [1], got %s", collected.Path)
	}
	if collected.Node.String != "b" {
		t.Errorf("expected 'b', got %s", collected.Node.String)
	}
}

func TestSubtreeCollector_NestedContainerInArray(t *testing.T) {
	// Build index with patch at "items[0]"
	patch := ir.FromMap(map[string]*ir.Node{
		"items": ir.FromSlice([]*ir.Node{
			ir.FromMap(map[string]*ir.Node{
				"nested": ir.FromString("v"),
			}).WithTag(tx.PatchRootTag),
		}),
	})
	entries := []*dlog.Entry{{Commit: 1, Patch: patch}}
	index := BuildPatchIndex(entries)

	collector := NewSubtreeCollector(index)

	// Simulate events for: { "items": [ { "x": 1 } ] }
	events := []stream.Event{
		{Type: stream.EventBeginObject},
		{Type: stream.EventKey, Key: "items"},
		{Type: stream.EventBeginArray},
		{Type: stream.EventBeginObject}, // path="items[0]" - matches!
		{Type: stream.EventKey, Key: "x"},
		{Type: stream.EventInt, Int: 1},
		{Type: stream.EventEndObject}, // End container at items[0]
		{Type: stream.EventEndArray},
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
		t.Fatal("expected to collect subtree at items[0]")
	}
	if collected.Path != "items[0]" {
		t.Errorf("expected path items[0], got %s", collected.Path)
	}
	if collected.Node.Type != ir.ObjectType {
		t.Errorf("expected ObjectType, got %v", collected.Node.Type)
	}
}

func TestSubtreeCollector_NoMatch(t *testing.T) {
	// Build index with patch at "other"
	patch := ir.FromMap(map[string]*ir.Node{
		"other": ir.FromString("data").WithTag(tx.PatchRootTag),
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
		"a": ir.FromString("val-a").WithTag(tx.PatchRootTag),
		"b": ir.FromString("val-b").WithTag(tx.PatchRootTag),
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
