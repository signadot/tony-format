package stream

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestNodeToEvents_String(t *testing.T) {
	node := ir.FromString("test")
	events, err := NodeToEvents(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []Event{{Type: EventString, String: "test"}}
	if !reflect.DeepEqual(events, expected) {
		t.Errorf("got %+v, want %+v", events, expected)
	}
}

func TestNodeToEvents_Int(t *testing.T) {
	node := ir.FromInt(42)
	events, err := NodeToEvents(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []Event{{Type: EventInt, Int: 42}}
	if !reflect.DeepEqual(events, expected) {
		t.Errorf("got %+v, want %+v", events, expected)
	}
}

func TestNodeToEvents_Float(t *testing.T) {
	node := ir.FromFloat(3.14)
	events, err := NodeToEvents(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []Event{{Type: EventFloat, Float: 3.14}}
	if !reflect.DeepEqual(events, expected) {
		t.Errorf("got %+v, want %+v", events, expected)
	}
}

func TestNodeToEvents_Bool(t *testing.T) {
	node := ir.FromBool(true)
	events, err := NodeToEvents(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []Event{{Type: EventBool, Bool: true}}
	if !reflect.DeepEqual(events, expected) {
		t.Errorf("got %+v, want %+v", events, expected)
	}
}

func TestNodeToEvents_Null(t *testing.T) {
	node := ir.Null()
	events, err := NodeToEvents(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []Event{{Type: EventNull}}
	if !reflect.DeepEqual(events, expected) {
		t.Errorf("got %+v, want %+v", events, expected)
	}
}

func TestNodeToEvents_Object(t *testing.T) {
	node := ir.FromMap(map[string]*ir.Node{
		"key": ir.FromString("value"),
	})
	events, err := NodeToEvents(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []Event{
		{Type: EventBeginObject},
		{Type: EventKey, Key: "key"},
		{Type: EventString, String: "value"},
		{Type: EventEndObject},
	}
	if !reflect.DeepEqual(events, expected) {
		t.Errorf("got %+v, want %+v", events, expected)
	}
}

func TestNodeToEvents_Array(t *testing.T) {
	node := ir.FromSlice([]*ir.Node{
		ir.FromString("a"),
		ir.FromInt(42),
	})
	events, err := NodeToEvents(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []Event{
		{Type: EventBeginArray},
		{Type: EventString, String: "a"},
		{Type: EventInt, Int: 42},
		{Type: EventEndArray},
	}
	if !reflect.DeepEqual(events, expected) {
		t.Errorf("got %+v, want %+v", events, expected)
	}
}

func TestNodeToEvents_Nested(t *testing.T) {
	node := ir.FromMap(map[string]*ir.Node{
		"obj": ir.FromMap(map[string]*ir.Node{
			"nested": ir.FromString("value"),
		}),
		"arr": ir.FromSlice([]*ir.Node{
			ir.FromInt(1),
			ir.FromInt(2),
		}),
	})
	events, err := NodeToEvents(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []Event{
		{Type: EventBeginObject},
		{Type: EventKey, Key: "arr"},
		{Type: EventBeginArray},
		{Type: EventInt, Int: 1},
		{Type: EventInt, Int: 2},
		{Type: EventEndArray},
		{Type: EventKey, Key: "obj"},
		{Type: EventBeginObject},
		{Type: EventKey, Key: "nested"},
		{Type: EventString, String: "value"},
		{Type: EventEndObject},
		{Type: EventEndObject},
	}
	if !reflect.DeepEqual(events, expected) {
		t.Errorf("got %+v, want %+v", events, expected)
	}
}

func TestNodeToEvents_Tags(t *testing.T) {
	node := ir.FromString("test").WithTag("!mytag")
	events, err := NodeToEvents(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []Event{{Type: EventString, String: "test", Tag: "!mytag"}}
	if !reflect.DeepEqual(events, expected) {
		t.Errorf("got %+v, want %+v", events, expected)
	}
}

func TestNodeToEvents_HeadComment(t *testing.T) {
	commentNode := &ir.Node{
		Type:  ir.CommentType,
		Lines: []string{"head comment"},
	}
	valueNode := ir.FromString("value")
	commentNode.Values = []*ir.Node{valueNode}
	valueNode.Parent = commentNode

	events, err := NodeToEvents(commentNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []Event{
		{Type: EventHeadComment, CommentLines: []string{"head comment"}},
		{Type: EventString, String: "value"},
	}
	if !reflect.DeepEqual(events, expected) {
		t.Errorf("got %+v, want %+v", events, expected)
	}
}

func TestNodeToEvents_LineComment(t *testing.T) {
	node := ir.FromString("value")
	node.Comment = &ir.Node{
		Type:  ir.CommentType,
		Lines: []string{"line comment"},
	}

	events, err := NodeToEvents(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []Event{
		{Type: EventString, String: "value"},
		{Type: EventLineComment, CommentLines: []string{"line comment"}},
	}
	if !reflect.DeepEqual(events, expected) {
		t.Errorf("got %+v, want %+v", events, expected)
	}
}

func TestNodeToEvents_SparseArray(t *testing.T) {
	key0 := ir.FromInt(0)
	key1 := ir.FromInt(1)
	node := &ir.Node{
		Type:   ir.ObjectType,
		Fields: []*ir.Node{key0, key1},
		Values: []*ir.Node{
			ir.FromString("a"),
			ir.FromString("b"),
		},
	}

	events, err := NodeToEvents(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []Event{
		{Type: EventBeginObject},
		{Type: EventIntKey, IntKey: 0},
		{Type: EventString, String: "a"},
		{Type: EventIntKey, IntKey: 1},
		{Type: EventString, String: "b"},
		{Type: EventEndObject},
	}
	if !reflect.DeepEqual(events, expected) {
		t.Errorf("got %+v, want %+v", events, expected)
	}
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		node *ir.Node
	}{
		{"string", ir.FromString("test")},
		{"int", ir.FromInt(42)},
		{"float", ir.FromFloat(3.14)},
		{"bool", ir.FromBool(true)},
		{"null", ir.Null()},
		{"object", ir.FromMap(map[string]*ir.Node{"key": ir.FromString("value")})},
		{"array", ir.FromSlice([]*ir.Node{ir.FromString("a"), ir.FromInt(1)})},
		{"nested", ir.FromMap(map[string]*ir.Node{
			"obj": ir.FromMap(map[string]*ir.Node{"nested": ir.FromString("value")}),
			"arr": ir.FromSlice([]*ir.Node{ir.FromInt(1), ir.FromInt(2)}),
		})},
		{"with tag", ir.FromString("test").WithTag("!mytag")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := NodeToEvents(tt.node)
			if err != nil {
				t.Fatalf("NodeToEvents error: %v", err)
			}

			result, err := EventsToNode(events)
			if err != nil {
				t.Fatalf("EventsToNode error: %v", err)
			}

			if !nodesEqual(tt.node, result) {
				t.Errorf("round-trip failed:\noriginal: %+v\nresult: %+v", tt.node, result)
			}
		})
	}
}

func nodesEqual(a, b *ir.Node) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Type != b.Type {
		return false
	}
	if a.String != b.String {
		return false
	}
	if a.Bool != b.Bool {
		return false
	}
	if (a.Int64 == nil) != (b.Int64 == nil) {
		return false
	}
	if a.Int64 != nil && *a.Int64 != *b.Int64 {
		return false
	}
	if (a.Float64 == nil) != (b.Float64 == nil) {
		return false
	}
	if a.Float64 != nil && *a.Float64 != *b.Float64 {
		return false
	}
	if a.Tag != b.Tag {
		return false
	}
	if len(a.Fields) != len(b.Fields) {
		return false
	}
	if len(a.Values) != len(b.Values) {
		return false
	}
	for i := range a.Fields {
		if !nodesEqual(a.Fields[i], b.Fields[i]) {
			return false
		}
	}
	for i := range a.Values {
		if !nodesEqual(a.Values[i], b.Values[i]) {
			return false
		}
	}
	return true
}
