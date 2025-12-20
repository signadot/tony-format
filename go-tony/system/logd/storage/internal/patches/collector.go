package patches

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
)

// SubtreeCollector collects events for subtrees that need patching.
// Uses path tracking to identify patched subtrees and collect their events.
//
// Path timing behavior (from stream.State):
// - EventKey("foo"): currentPath becomes "foo" immediately
// - Value event after key: currentPath is STILL "foo" (unchanged until next key)
// - EventBeginArray, then EventString: currentPath becomes "[0]" on the string
//
// Collection algorithm (following path_finder.go pattern):
// 1. When path matches a patch path:
//   - EventKey/EventIntKey: Set collecting=true (don't collect the key itself)
//   - EventBegin*: Set collecting=true, depth=1, append event
//   - Scalar: Append single event, done
//
// 2. While collecting (depth > 0 or waiting for value after key):
//   - Begin*: depth++, append
//   - End*: append, depth--, done if depth=0
//   - Others: append
type SubtreeCollector struct {
	index      *PatchIndex
	state      *stream.State
	events     []stream.Event
	depth      int    // depth within collected subtree
	collecting bool   // true when we've matched a path and are collecting
	startPath  string // path where collection started
}

// NewSubtreeCollector creates a new SubtreeCollector with the given patch index.
func NewSubtreeCollector(index *PatchIndex) *SubtreeCollector {
	return &SubtreeCollector{
		index: index,
		state: stream.NewState(),
	}
}

// CollectedSubtree represents a collected subtree with its path and IR node.
type CollectedSubtree struct {
	Path string
	Node *ir.Node
}

// ProcessEvent processes an event and returns a collected subtree if one is complete.
// Returns nil if no subtree is complete yet.
func (sc *SubtreeCollector) ProcessEvent(event *stream.Event) (*CollectedSubtree, error) {
	// Process event to update path state
	if err := sc.state.ProcessEvent(event); err != nil {
		return nil, err
	}

	currentPath := sc.state.CurrentPath()

	// If already collecting
	if sc.collecting {
		return sc.continueCollecting(event)
	}

	// Check if this path needs patching
	if !sc.index.HasPatches(currentPath) {
		return nil, nil
	}

	// Start collecting based on event type
	sc.startPath = currentPath

	switch event.Type {
	case stream.EventKey, stream.EventIntKey:
		// Key event - the VALUE will follow
		// Set collecting but don't append the key (it's part of parent structure)
		sc.collecting = true
		sc.depth = 0
		return nil, nil

	case stream.EventBeginObject, stream.EventBeginArray:
		// Container start - collect from here
		sc.collecting = true
		sc.events = []stream.Event{*event}
		sc.depth = 1
		return nil, nil

	case stream.EventString, stream.EventInt, stream.EventFloat, stream.EventBool, stream.EventNull:
		// Scalar value - collect immediately and complete
		node, err := stream.EventsToNode([]stream.Event{*event})
		if err != nil {
			return nil, err
		}
		return &CollectedSubtree{
			Path: currentPath,
			Node: node,
		}, nil

	default:
		// End events or comments at a patched path
		return nil, nil
	}
}

// continueCollecting handles events while collecting a subtree.
func (sc *SubtreeCollector) continueCollecting(event *stream.Event) (*CollectedSubtree, error) {
	switch event.Type {
	case stream.EventBeginObject, stream.EventBeginArray:
		sc.depth++
		sc.events = append(sc.events, *event)
		return nil, nil

	case stream.EventEndObject, stream.EventEndArray:
		sc.events = append(sc.events, *event)
		sc.depth--

		if sc.depth <= 0 {
			return sc.finishCollecting()
		}
		return nil, nil

	default:
		// All other events (keys, scalars, comments)
		sc.events = append(sc.events, *event)

		// If depth is 0, this is the scalar value after a key match
		if sc.depth == 0 && isValueEvent(event.Type) {
			return sc.finishCollecting()
		}
		return nil, nil
	}
}

// finishCollecting completes collection and returns the subtree.
func (sc *SubtreeCollector) finishCollecting() (*CollectedSubtree, error) {
	node, err := stream.EventsToNode(sc.events)
	if err != nil {
		return nil, err
	}

	result := &CollectedSubtree{
		Path: sc.startPath,
		Node: node,
	}

	// Reset collection state
	sc.collecting = false
	sc.events = nil
	sc.depth = 0
	sc.startPath = ""

	return result, nil
}

// isValueEvent returns true for events that represent values.
func isValueEvent(t stream.EventType) bool {
	switch t {
	case stream.EventString, stream.EventInt, stream.EventFloat, stream.EventBool, stream.EventNull,
		stream.EventBeginObject, stream.EventBeginArray:
		return true
	default:
		return false
	}
}

// IsCollecting returns true if currently collecting a subtree.
func (sc *SubtreeCollector) IsCollecting() bool {
	return sc.collecting
}

// Reset clears the collector state.
func (sc *SubtreeCollector) Reset() {
	sc.state = stream.NewState()
	sc.events = nil
	sc.depth = 0
	sc.collecting = false
	sc.startPath = ""
}
