package stream

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestNodeToEvents(t *testing.T) {
	// TODO: Test conversion from ir.Node to events
	node := ir.FromString("test")
	events, err := NodeToEvents(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = events
}

func TestEventsToNode(t *testing.T) {
	// TODO: Test conversion from events to ir.Node
	events := []Event{
		{Type: EventString, String: "test"},
	}
	node, err := EventsToNode(events)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = node
}

func TestRoundTrip(t *testing.T) {
	// TODO: Test round-trip conversion
	// node -> events -> node
	original := ir.FromMap(map[string]*ir.Node{
		"key": ir.FromString("value"),
	})

	events, err := NodeToEvents(original)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := EventsToNode(events)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = result
	// TODO: Compare original and result
}
