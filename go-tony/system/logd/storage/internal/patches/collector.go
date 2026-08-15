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
	index       *PatchIndex
	state       *stream.State
	events      []stream.Event
	depth       int    // depth within collected subtree
	collecting  bool   // true when actively collecting events
	pendingPath string // path where key was matched, waiting for value
	startPath   string // path where collection started

	// preValue holds the comments that precede a value about to be collected. A
	// head comment describes the value AFTER it, and the value at a patched path
	// is collected, materialized, patched and re-emitted -- so its comment has to
	// travel with it. Taken for the value instead, it WAS the collected subtree:
	// the patch applied to a comment, the base's real value stayed in the stream,
	// and the object came back with the same key twice
	// (3cdjz00jh12krns4g1n0).
	preValue []stream.Event
	took     bool           // the last event went into preValue
	released []stream.Event // held comments that belong to nothing being collected
}

// Took reports whether the collector kept the last event for itself, in which
// case the caller must not also emit it.
func (sc *SubtreeCollector) Took() bool { return sc.took }

// Release hands back comments the collector held for a value it turned out not to
// collect. They belong in the stream, in the place they were held from.
func (sc *SubtreeCollector) Release() []stream.Event {
	r := sc.released
	sc.released = nil
	return r
}

// takePreValue returns the held comments as the start of a collected window.
func (sc *SubtreeCollector) takePreValue() []stream.Event {
	evs := sc.preValue
	sc.preValue = nil
	return evs
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
	sc.took = false

	// If already collecting
	if sc.collecting {
		return sc.continueCollecting(event)
	}

	// A comment before a value that is about to be collected belongs to that
	// value: hold it rather than take it FOR the value.
	isComment := event.Type == stream.EventHeadComment || event.Type == stream.EventLineComment
	if isComment {
		if sc.pendingPath != "" || sc.index.HasPatches(currentPath) {
			sc.preValue = append(sc.preValue, *event)
			sc.took = true
		}
		return nil, nil
	}
	if len(sc.preValue) > 0 && !event.IsValueStart() {
		// The value never came -- a container closed, or another key began. What
		// was held describes nothing this collects, so it goes back to the stream.
		sc.released = append(sc.released, sc.takePreValue()...)
	}

	// If we have a pending path from a previous key match, start collecting now
	if sc.pendingPath != "" {
		sc.startPath = sc.pendingPath
		sc.pendingPath = ""
		sc.collecting = true

		switch event.Type {
		case stream.EventBeginObject, stream.EventBeginArray:
			sc.events = append(sc.takePreValue(), *event)
			sc.depth = 1
			return nil, nil
		default:
			// Scalar value after key
			sc.events = append(sc.takePreValue(), *event)
			return sc.finishCollecting()
		}
	}

	// Check if this path needs patching
	if !sc.index.HasPatches(currentPath) {
		return nil, nil
	}

	// Start collecting based on event type
	switch event.Type {
	case stream.EventKey, stream.EventIntKey:
		// Key event - remember the path, collect value on next event
		// Don't set collecting=true so IsCollecting() returns false
		// and the processor can emit the key
		sc.pendingPath = currentPath
		return nil, nil

	case stream.EventBeginObject, stream.EventBeginArray:
		// Container start - collect from here
		sc.startPath = currentPath
		sc.collecting = true
		sc.events = append(sc.takePreValue(), *event)
		sc.depth = 1
		return nil, nil

	case stream.EventString, stream.EventInt, stream.EventFloat, stream.EventBool, stream.EventNull:
		// Scalar value - collect immediately and complete
		node, err := stream.EventsToNode(append(sc.takePreValue(), *event))
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
	sc.events = append(sc.events, *event)

	switch event.Type {
	case stream.EventBeginObject, stream.EventBeginArray:
		sc.depth++
	case stream.EventEndObject, stream.EventEndArray:
		sc.depth--
		if sc.depth <= 0 {
			return sc.finishCollecting()
		}
	}

	return nil, nil
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

// PendingPath returns the path whose value is about to be collected, or "" if none.
//
// A key event at a patched path deliberately does not start collection (see
// ProcessEvent), so that the caller decides what to do with the key. It has to: the
// value may patch away to nothing, and then the key must not be written either.
func (sc *SubtreeCollector) PendingPath() string {
	return sc.pendingPath
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
	sc.pendingPath = ""
	sc.startPath = ""
}
