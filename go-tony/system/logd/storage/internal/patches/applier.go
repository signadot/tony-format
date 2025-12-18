package patches

import (
	"io"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
)

// PatchApplier applies patches to streaming events.
// Implementations may materialize subtrees or stream fully depending on design.
type PatchApplier interface {
	// ApplyPatches applies patches to base events, writes result to sink.
	// Patches are applied in order.
	ApplyPatches(baseEvents stream.EventReader, patches []*ir.Node, sink stream.EventWriter) error
}

// InMemoryApplier is a temporary implementation that materializes the full document.
// This violates the streaming principle but provides a working implementation
// until the streaming processor (Piece 2 from patch_design_reference.md) is complete.
type InMemoryApplier struct{}

// NewInMemoryApplier creates an in-memory patch applier.
func NewInMemoryApplier() *InMemoryApplier {
	return &InMemoryApplier{}
}

// ApplyPatches materializes the full document, applies patches, converts back to events.
// TODO: Replace with streaming implementation that never materializes full document.
func (a *InMemoryApplier) ApplyPatches(baseEvents stream.EventReader, patches []*ir.Node, sink stream.EventWriter) error {
	// Read all events into memory
	var events []stream.Event
	for {
		ev, err := baseEvents.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		events = append(events, *ev)
	}

	// Convert events to ir.Node
	var state *ir.Node
	if len(events) == 0 {
		state = ir.Null()
	} else {
		var err error
		state, err = stream.EventsToNode(events)
		if err != nil {
			return err
		}
	}

	// Apply patches in order
	for _, patch := range patches {
		var err error
		state, err = tony.Patch(state, patch)
		if err != nil {
			return err
		}
	}

	// Convert result back to events
	resultEvents, err := stream.NodeToEvents(state)
	if err != nil {
		return err
	}

	// Write events to sink
	for i := range resultEvents {
		if err := sink.WriteEvent(&resultEvents[i]); err != nil {
			return err
		}
	}

	return nil
}
