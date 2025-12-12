package stream

import (
	"io"

	"github.com/signadot/tony-format/go-tony/ir"
)

// NodeToEvents converts an ir.Node to a sequence of events.
// Returns events that can be written via Encoder.
//
// Phase 1: Comments are skipped (not included in events).
// Phase 2: Comments are converted to EventHeadComment or EventLineComment.
func NodeToEvents(node *ir.Node) ([]Event, error) {
	// TODO: Implement conversion
	// 1. Handle different node types
	// 2. For objects: EventBeginObject, EventKey, value events, EventEndObject
	// 3. For arrays: EventBeginArray, value events, EventEndArray
	// 4. For primitives: EventString, EventInt, etc.
	// 5. Phase 1: Skip comments
	// 6. Phase 2: Handle head comments (CommentType with 1 value) and line comments (CommentType in Comment field)

	return nil, nil
}

// EventsToNode converts a sequence of events to an ir.Node.
// Takes events read from Decoder.
//
// Phase 1: Comment events are not present (comments skipped).
// Phase 2: Comment events are converted to IR comment nodes.
func EventsToNode(events []Event) (*ir.Node, error) {
	// TODO: Implement conversion
	// 1. Parse event sequence
	// 2. Build ir.Node structure
	// 3. Handle nested structures
	// 4. Phase 1: No comment handling
	// 5. Phase 2: Handle EventHeadComment (create CommentType node with 1 value) and EventLineComment (set Comment field)

	return nil, nil
}

// EncodeNode encodes an ir.Node to bytes using Encoder.
// Convenience function: NodeToEvents + Encoder.
func EncodeNode(node *ir.Node, w io.Writer, opts ...StreamOption) error {
	// TODO: Implement
	// 1. Convert node to events
	// 2. Create encoder
	// 3. Write events via encoder

	return nil
}

// DecodeNode decodes bytes to ir.Node using Decoder.
// Convenience function: Decoder + EventsToNode.
func DecodeNode(r io.Reader, opts ...StreamOption) (*ir.Node, error) {
	// TODO: Implement
	// 1. Create decoder
	// 2. Read all events
	// 3. Convert events to node

	return nil, nil
}
