package patches

import (
	"bytes"
	"io"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

func TestStreamingProcessor_SimpleScalar(t *testing.T) {
	// Base: { "users": { "alice": "old" } }
	// Patch: { "users": { "alice": "new" !logd-patch-root } }
	// Expected: { "users": { "alice": "new" } }

	base := ir.FromMap(map[string]*ir.Node{
		"users": ir.FromMap(map[string]*ir.Node{
			"alice": ir.FromString("old"),
		}),
	})

	patch := ir.FromMap(map[string]*ir.Node{
		"users": ir.FromMap(map[string]*ir.Node{
			"alice": ir.FromString("new").WithTag(tx.PatchRootTag),
		}),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	if result.Type != ir.ObjectType {
		t.Fatalf("expected ObjectType, got %v", result.Type)
	}
	users := findField(result, "users")
	if users == nil {
		t.Fatal("expected users field")
	}
	alice := findField(users, "alice")
	if alice == nil {
		t.Fatal("expected alice field")
	}
	if alice.String != "new" {
		t.Errorf("expected 'new', got %q", alice.String)
	}
}

func TestStreamingProcessor_ContainerPatch(t *testing.T) {
	// Base: { "config": { "a": 1, "b": 2 } }
	// Patch: { "config": { "a": 10, "c": 3 } !logd-patch-root }
	// Expected: { "config": { "a": 10, "b": 2, "c": 3 } } (merged)

	base := ir.FromMap(map[string]*ir.Node{
		"config": ir.FromMap(map[string]*ir.Node{
			"a": ir.FromInt(1),
			"b": ir.FromInt(2),
		}),
	})

	patch := ir.FromMap(map[string]*ir.Node{
		"config": ir.FromMap(map[string]*ir.Node{
			"a": ir.FromInt(10),
			"c": ir.FromInt(3),
		}).WithTag(tx.PatchRootTag),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	config := findField(result, "config")
	if config == nil {
		t.Fatal("expected config field")
	}

	a := findField(config, "a")
	if a == nil || *a.Int64 != 10 {
		t.Errorf("expected a=10, got %v", a)
	}
	b := findField(config, "b")
	if b == nil || *b.Int64 != 2 {
		t.Errorf("expected b=2, got %v", b)
	}
	c := findField(config, "c")
	if c == nil || *c.Int64 != 3 {
		t.Errorf("expected c=3, got %v", c)
	}
}

func TestStreamingProcessor_MultiplePatches(t *testing.T) {
	// Base: { "x": 1 }
	// Patch1: { "x": 2 !logd-patch-root }
	// Patch2: { "x": 3 !logd-patch-root }
	// Expected: { "x": 3 } (patches applied in order)

	base := ir.FromMap(map[string]*ir.Node{
		"x": ir.FromInt(1),
	})

	patch1 := ir.FromMap(map[string]*ir.Node{
		"x": ir.FromInt(2).WithTag(tx.PatchRootTag),
	})
	patch2 := ir.FromMap(map[string]*ir.Node{
		"x": ir.FromInt(3).WithTag(tx.PatchRootTag),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch1, patch2})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	x := findField(result, "x")
	if x == nil || *x.Int64 != 3 {
		t.Errorf("expected x=3, got %v", x)
	}
}

func TestStreamingProcessor_ArrayElement(t *testing.T) {
	// Base: [ "a", "b", "c" ]
	// Patch: [ null, "B" !logd-patch-root, null ]
	// Expected: [ "a", "B", "c" ]

	base := ir.FromSlice([]*ir.Node{
		ir.FromString("a"),
		ir.FromString("b"),
		ir.FromString("c"),
	})

	patch := ir.FromSlice([]*ir.Node{
		ir.Null(),
		ir.FromString("B").WithTag(tx.PatchRootTag),
		ir.Null(),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	if result.Type != ir.ArrayType {
		t.Fatalf("expected ArrayType, got %v", result.Type)
	}
	if len(result.Values) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result.Values))
	}
	if result.Values[1].String != "B" {
		t.Errorf("expected 'B', got %q", result.Values[1].String)
	}
}

func TestStreamingProcessor_NoPatches(t *testing.T) {
	// Base: { "x": 1 }
	// No patches
	// Expected: { "x": 1 } (unchanged)

	base := ir.FromMap(map[string]*ir.Node{
		"x": ir.FromInt(1),
	})

	result, err := applyStreamingProcessor(base, nil)
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	x := findField(result, "x")
	if x == nil || *x.Int64 != 1 {
		t.Errorf("expected x=1, got %v", x)
	}
}

func TestStreamingProcessor_PassthroughUnpatched(t *testing.T) {
	// Base: { "a": 1, "b": 2, "c": 3 }
	// Patch: { "b": 20 !logd-patch-root }
	// Expected: { "a": 1, "b": 20, "c": 3 }

	base := ir.FromMap(map[string]*ir.Node{
		"a": ir.FromInt(1),
		"b": ir.FromInt(2),
		"c": ir.FromInt(3),
	})

	patch := ir.FromMap(map[string]*ir.Node{
		"b": ir.FromInt(20).WithTag(tx.PatchRootTag),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	a := findField(result, "a")
	if a == nil || *a.Int64 != 1 {
		t.Errorf("expected a=1, got %v", a)
	}
	b := findField(result, "b")
	if b == nil || *b.Int64 != 20 {
		t.Errorf("expected b=20, got %v", b)
	}
	c := findField(result, "c")
	if c == nil || *c.Int64 != 3 {
		t.Errorf("expected c=3, got %v", c)
	}
}

func TestStreamingProcessor_DeeplyNestedObject(t *testing.T) {
	// Base: { "a": { "b": { "c": { "d": "old" } } } }
	// Patch: { "a": { "b": { "c": { "d": "new" !logd-patch-root } } } }
	// Expected: { "a": { "b": { "c": { "d": "new" } } } }

	base := ir.FromMap(map[string]*ir.Node{
		"a": ir.FromMap(map[string]*ir.Node{
			"b": ir.FromMap(map[string]*ir.Node{
				"c": ir.FromMap(map[string]*ir.Node{
					"d": ir.FromString("old"),
				}),
			}),
		}),
	})

	patch := ir.FromMap(map[string]*ir.Node{
		"a": ir.FromMap(map[string]*ir.Node{
			"b": ir.FromMap(map[string]*ir.Node{
				"c": ir.FromMap(map[string]*ir.Node{
					"d": ir.FromString("new").WithTag(tx.PatchRootTag),
				}),
			}),
		}),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	d := findField(findField(findField(findField(result, "a"), "b"), "c"), "d")
	if d == nil || d.String != "new" {
		t.Errorf("expected d='new', got %v", d)
	}
}

func TestStreamingProcessor_NestedArrays(t *testing.T) {
	// Base: [ [ "a", "b" ], [ "c", "d" ] ]
	// Patch: [ null, [ null, "D" !logd-patch-root ] ]
	// Expected: [ [ "a", "b" ], [ "c", "D" ] ]

	base := ir.FromSlice([]*ir.Node{
		ir.FromSlice([]*ir.Node{ir.FromString("a"), ir.FromString("b")}),
		ir.FromSlice([]*ir.Node{ir.FromString("c"), ir.FromString("d")}),
	})

	patch := ir.FromSlice([]*ir.Node{
		ir.Null(),
		ir.FromSlice([]*ir.Node{
			ir.Null(),
			ir.FromString("D").WithTag(tx.PatchRootTag),
		}),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	if result.Type != ir.ArrayType || len(result.Values) != 2 {
		t.Fatalf("expected 2-element array, got %v", result)
	}
	inner := result.Values[1]
	if inner.Type != ir.ArrayType || len(inner.Values) != 2 {
		t.Fatalf("expected 2-element inner array, got %v", inner)
	}
	if inner.Values[1].String != "D" {
		t.Errorf("expected 'D', got %q", inner.Values[1].String)
	}
}

func TestStreamingProcessor_MixedObjectArray(t *testing.T) {
	// Base: { "items": [ { "name": "old" } ] }
	// Patch: { "items": [ { "name": "new" !logd-patch-root } ] }
	// Expected: { "items": [ { "name": "new" } ] }

	base := ir.FromMap(map[string]*ir.Node{
		"items": ir.FromSlice([]*ir.Node{
			ir.FromMap(map[string]*ir.Node{"name": ir.FromString("old")}),
		}),
	})

	patch := ir.FromMap(map[string]*ir.Node{
		"items": ir.FromSlice([]*ir.Node{
			ir.FromMap(map[string]*ir.Node{
				"name": ir.FromString("new").WithTag(tx.PatchRootTag),
			}),
		}),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	items := findField(result, "items")
	if items == nil || items.Type != ir.ArrayType || len(items.Values) != 1 {
		t.Fatalf("expected items array with 1 element, got %v", items)
	}
	name := findField(items.Values[0], "name")
	if name == nil || name.String != "new" {
		t.Errorf("expected name='new', got %v", name)
	}
}

func TestStreamingProcessor_MultipleDifferentPaths(t *testing.T) {
	// Base: { "a": 1, "b": 2, "c": 3 }
	// Patch: { "a": 10 !logd-patch-root, "c": 30 !logd-patch-root }
	// Expected: { "a": 10, "b": 2, "c": 30 }

	base := ir.FromMap(map[string]*ir.Node{
		"a": ir.FromInt(1),
		"b": ir.FromInt(2),
		"c": ir.FromInt(3),
	})

	patch := ir.FromMap(map[string]*ir.Node{
		"a": ir.FromInt(10).WithTag(tx.PatchRootTag),
		"c": ir.FromInt(30).WithTag(tx.PatchRootTag),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	a := findField(result, "a")
	if a == nil || *a.Int64 != 10 {
		t.Errorf("expected a=10, got %v", a)
	}
	b := findField(result, "b")
	if b == nil || *b.Int64 != 2 {
		t.Errorf("expected b=2, got %v", b)
	}
	c := findField(result, "c")
	if c == nil || *c.Int64 != 30 {
		t.Errorf("expected c=30, got %v", c)
	}
}

func TestStreamingProcessor_EmptyObject(t *testing.T) {
	// Base: { "config": {} }
	// Patch: { "config": { "new": "value" } !logd-patch-root }
	// Expected: { "config": { "new": "value" } }

	base := ir.FromMap(map[string]*ir.Node{
		"config": ir.FromMap(map[string]*ir.Node{}),
	})

	patch := ir.FromMap(map[string]*ir.Node{
		"config": ir.FromMap(map[string]*ir.Node{
			"new": ir.FromString("value"),
		}).WithTag(tx.PatchRootTag),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	config := findField(result, "config")
	if config == nil || config.Type != ir.ObjectType {
		t.Fatalf("expected config object, got %v", config)
	}
	newField := findField(config, "new")
	if newField == nil || newField.String != "value" {
		t.Errorf("expected new='value', got %v", newField)
	}
}

func TestStreamingProcessor_EmptyArray(t *testing.T) {
	// Base: { "items": [] }
	// Patch: { "items": [ "added" ] !logd-patch-root }
	// Expected: { "items": [ "added" ] }

	base := ir.FromMap(map[string]*ir.Node{
		"items": ir.FromSlice([]*ir.Node{}),
	})

	patch := ir.FromMap(map[string]*ir.Node{
		"items": ir.FromSlice([]*ir.Node{
			ir.FromString("added"),
		}).WithTag(tx.PatchRootTag),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	items := findField(result, "items")
	if items == nil || items.Type != ir.ArrayType {
		t.Fatalf("expected items array, got %v", items)
	}
	if len(items.Values) != 1 || items.Values[0].String != "added" {
		t.Errorf("expected ['added'], got %v", items.Values)
	}
}

func TestStreamingProcessor_ArrayInObject(t *testing.T) {
	// Base: { "data": { "list": [ 1, 2, 3 ] } }
	// Patch: { "data": { "list": [ null, 20 !logd-patch-root, null ] } }
	// Expected: { "data": { "list": [ 1, 20, 3 ] } }

	base := ir.FromMap(map[string]*ir.Node{
		"data": ir.FromMap(map[string]*ir.Node{
			"list": ir.FromSlice([]*ir.Node{
				ir.FromInt(1),
				ir.FromInt(2),
				ir.FromInt(3),
			}),
		}),
	})

	patch := ir.FromMap(map[string]*ir.Node{
		"data": ir.FromMap(map[string]*ir.Node{
			"list": ir.FromSlice([]*ir.Node{
				ir.Null(),
				ir.FromInt(20).WithTag(tx.PatchRootTag),
				ir.Null(),
			}),
		}),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	data := findField(result, "data")
	list := findField(data, "list")
	if list == nil || list.Type != ir.ArrayType || len(list.Values) != 3 {
		t.Fatalf("expected 3-element list, got %v", list)
	}
	if *list.Values[0].Int64 != 1 {
		t.Errorf("expected list[0]=1, got %v", list.Values[0])
	}
	if *list.Values[1].Int64 != 20 {
		t.Errorf("expected list[1]=20, got %v", list.Values[1])
	}
	if *list.Values[2].Int64 != 3 {
		t.Errorf("expected list[2]=3, got %v", list.Values[2])
	}
}

func TestStreamingProcessor_ObjectInArray(t *testing.T) {
	// Base: [ { "id": 1, "v": "a" }, { "id": 2, "v": "b" } ]
	// Patch: [ { "v": "A" !logd-patch-root }, null ]
	// Expected: [ { "id": 1, "v": "A" }, { "id": 2, "v": "b" } ]

	base := ir.FromSlice([]*ir.Node{
		ir.FromMap(map[string]*ir.Node{"id": ir.FromInt(1), "v": ir.FromString("a")}),
		ir.FromMap(map[string]*ir.Node{"id": ir.FromInt(2), "v": ir.FromString("b")}),
	})

	patch := ir.FromSlice([]*ir.Node{
		ir.FromMap(map[string]*ir.Node{
			"v": ir.FromString("A").WithTag(tx.PatchRootTag),
		}),
		ir.Null(),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	if result.Type != ir.ArrayType || len(result.Values) != 2 {
		t.Fatalf("expected 2-element array, got %v", result)
	}
	first := result.Values[0]
	v := findField(first, "v")
	if v == nil || v.String != "A" {
		t.Errorf("expected v='A', got %v", v)
	}
	id := findField(first, "id")
	if id == nil || *id.Int64 != 1 {
		t.Errorf("expected id=1, got %v", id)
	}
}

// applyStreamingProcessor is a test helper that runs the processor.
func applyStreamingProcessor(base *ir.Node, patches []*ir.Node) (*ir.Node, error) {
	// Convert base to events
	baseEvents, err := stream.NodeToEvents(base)
	if err != nil {
		return nil, err
	}

	// Create event reader from base events
	baseReader := &eventSliceReader{events: baseEvents}

	// Create event buffer for output
	var outputEvents []stream.Event
	sink := &eventSliceWriter{events: &outputEvents}

	// Apply patches
	processor := NewStreamingProcessor()
	if err := processor.ApplyPatches(baseReader, patches, sink); err != nil {
		return nil, err
	}

	// Convert output events back to node
	return stream.EventsToNode(outputEvents)
}

// eventSliceReader reads events from a slice.
type eventSliceReader struct {
	events []stream.Event
	index  int
}

func (r *eventSliceReader) ReadEvent() (*stream.Event, error) {
	if r.index >= len(r.events) {
		return nil, io.EOF
	}
	ev := &r.events[r.index]
	r.index++
	return ev, nil
}

// eventSliceWriter writes events to a slice.
type eventSliceWriter struct {
	events *[]stream.Event
}

func (w *eventSliceWriter) WriteEvent(ev *stream.Event) error {
	*w.events = append(*w.events, *ev)
	return nil
}

// findField finds a field in an object by key.
func findField(node *ir.Node, key string) *ir.Node {
	if node.Type != ir.ObjectType {
		return nil
	}
	for i, field := range node.Fields {
		if field.String == key {
			return node.Values[i]
		}
	}
	return nil
}

// bufferWriter collects events for testing.
type bufferWriter struct {
	buf bytes.Buffer
}

func (w *bufferWriter) WriteEvent(ev *stream.Event) error {
	return ev.WriteBinary(&w.buf)
}

func TestStreamingProcessor_SparseArray(t *testing.T) {
	// Base: { 0: "a", 1: "b", 2: "c" } (sparse array with {index} paths)
	// Patch: { 1: "B" !logd-patch-root }
	// Expected: { 0: "a", 1: "B", 2: "c" }

	base := ir.FromIntKeysMap(map[uint32]*ir.Node{
		0: ir.FromString("a"),
		1: ir.FromString("b"),
		2: ir.FromString("c"),
	})

	patch := ir.FromIntKeysMap(map[uint32]*ir.Node{
		1: ir.FromString("B").WithTag(tx.PatchRootTag),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	if result.Type != ir.ObjectType {
		t.Fatalf("expected ObjectType, got %v", result.Type)
	}
	if len(result.Values) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result.Values))
	}
	// Find element at key 1
	var found *ir.Node
	for i, field := range result.Fields {
		if field.Int64 != nil && *field.Int64 == 1 {
			found = result.Values[i]
			break
		}
	}
	if found == nil || found.String != "B" {
		t.Errorf("expected element at {1}='B', got %v", found)
	}
}

func TestStreamingProcessor_InternalTagsStripped(t *testing.T) {
	// Verify that !logd-patch-root tags are stripped from output
	// Base: { "val": "old" }
	// Patch: { "val": "new" !logd-patch-root }
	// Expected: { "val": "new" } with NO tag on the result

	base := ir.FromMap(map[string]*ir.Node{
		"val": ir.FromString("old"),
	})

	patch := ir.FromMap(map[string]*ir.Node{
		"val": ir.FromString("new").WithTag(tx.PatchRootTag),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	val := findField(result, "val")
	if val == nil {
		t.Fatal("expected val field")
	}
	if val.String != "new" {
		t.Errorf("expected val='new', got %q", val.String)
	}
	// The key check: tag should be stripped
	if val.Tag != "" {
		t.Errorf("expected no tag on result, got %q", val.Tag)
	}
}

func TestStreamingProcessor_RootDominatesAll(t *testing.T) {
	// Base: { "a": 1, "b": 2 }
	// Patch 1: { "a": 10, "b": 20 } !logd-patch-root (root level)
	// Patch 2: { "a": 999 !logd-patch-root } (child - dominated by root)
	// Expected: { "a": 10, "b": 20 } (child patch filtered out)

	base := ir.FromMap(map[string]*ir.Node{
		"a": ir.FromInt(1),
		"b": ir.FromInt(2),
	})

	// Root-level patch
	rootPatch := ir.FromMap(map[string]*ir.Node{
		"a": ir.FromInt(10),
		"b": ir.FromInt(20),
	}).WithTag(tx.PatchRootTag)

	// Child patch at "a" - should be filtered out
	childPatch := ir.FromMap(map[string]*ir.Node{
		"a": ir.FromInt(999).WithTag(tx.PatchRootTag),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{rootPatch, childPatch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	a := findField(result, "a")
	if a == nil || *a.Int64 != 10 {
		t.Errorf("expected a=10 (from root patch, not 999), got %v", a)
	}

	b := findField(result, "b")
	if b == nil || *b.Int64 != 20 {
		t.Errorf("expected b=20, got %v", b)
	}
}

func TestStreamingProcessor_DominatedPathFiltered(t *testing.T) {
	// Base: { "users": { "alice": "old", "bob": "old" } }
	// Patch 1: { "users": { "alice": "new", "bob": "new" } !logd-patch-root } (parent)
	// Patch 2: { "users": { "alice": "ignored" !logd-patch-root } } (child - dominated)
	// Expected: { "users": { "alice": "new", "bob": "new" } } (child patch filtered out)

	base := ir.FromMap(map[string]*ir.Node{
		"users": ir.FromMap(map[string]*ir.Node{
			"alice": ir.FromString("old"),
			"bob":   ir.FromString("old"),
		}),
	})

	// Parent patch at "users" level
	parentPatch := ir.FromMap(map[string]*ir.Node{
		"users": ir.FromMap(map[string]*ir.Node{
			"alice": ir.FromString("new"),
			"bob":   ir.FromString("new"),
		}).WithTag(tx.PatchRootTag),
	})

	// Child patch at "users.alice" level - should be filtered out
	childPatch := ir.FromMap(map[string]*ir.Node{
		"users": ir.FromMap(map[string]*ir.Node{
			"alice": ir.FromString("ignored").WithTag(tx.PatchRootTag),
		}),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{parentPatch, childPatch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	users := findField(result, "users")
	if users == nil {
		t.Fatal("expected users field")
	}

	alice := findField(users, "alice")
	if alice == nil || alice.String != "new" {
		t.Errorf("expected alice='new' (from parent patch, not 'ignored'), got %v", alice)
	}

	bob := findField(users, "bob")
	if bob == nil || bob.String != "new" {
		t.Errorf("expected bob='new', got %v", bob)
	}
}

func TestStreamingProcessor_SparseArrayNested(t *testing.T) {
	// Base: { "data": { 100: { "name": "old" }, 200: { "name": "other" } } }
	// Patch: { "data": { 100: { "name": "new" !logd-patch-root } } }
	// Expected: { "data": { 100: { "name": "new" }, 200: { "name": "other" } } }
	// Path for patch: data{100}.name

	base := ir.FromMap(map[string]*ir.Node{
		"data": ir.FromIntKeysMap(map[uint32]*ir.Node{
			100: ir.FromMap(map[string]*ir.Node{"name": ir.FromString("old")}),
			200: ir.FromMap(map[string]*ir.Node{"name": ir.FromString("other")}),
		}),
	})

	patch := ir.FromMap(map[string]*ir.Node{
		"data": ir.FromIntKeysMap(map[uint32]*ir.Node{
			100: ir.FromMap(map[string]*ir.Node{
				"name": ir.FromString("new").WithTag(tx.PatchRootTag),
			}),
		}),
	})

	result, err := applyStreamingProcessor(base, []*ir.Node{patch})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}

	data := findField(result, "data")
	if data == nil || data.Type != ir.ObjectType {
		t.Fatalf("expected data object, got %v", data)
	}

	// Find element at key 100
	var elem100 *ir.Node
	for i, field := range data.Fields {
		if field.Int64 != nil && *field.Int64 == 100 {
			elem100 = data.Values[i]
			break
		}
	}
	if elem100 == nil {
		t.Fatal("expected element at {100}")
	}
	name := findField(elem100, "name")
	if name == nil || name.String != "new" {
		t.Errorf("expected name='new', got %v", name)
	}

	// Verify element at key 200 is unchanged
	var elem200 *ir.Node
	for i, field := range data.Fields {
		if field.Int64 != nil && *field.Int64 == 200 {
			elem200 = data.Values[i]
			break
		}
	}
	if elem200 == nil {
		t.Fatal("expected element at {200}")
	}
	name200 := findField(elem200, "name")
	if name200 == nil || name200.String != "other" {
		t.Errorf("expected name='other', got %v", name200)
	}
}
