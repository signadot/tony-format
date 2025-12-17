package stream

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// KPathState creates a State positioned to process events at the given path.
//
// For leaf array elements, positions one element BEFORE the target so that
// processing the event at that offset advances to the correct position.
// For non-leaf array elements, uses the actual index for path matching.
//
// Examples:
//   KPathState("users[3]")      → positioned at "users[2]" (leaf)
//   KPathState("users[0]")      → positioned at "users" (leaf at index 0)
//   KPathState("users[0].name") → positioned at "users[0].name" (non-leaf)
//
// Returns an error if the kpath string is invalid.
func KPathState(kp string) (*State, error) {
	if kp == "" {
		return NewState(), nil
	}

	// Parse the kpath into structured form
	p, err := kpath.Parse(kp)
	if err != nil {
		return nil, err
	}

	// Build State by opening bracket contexts for each segment
	state := NewState()
	expectedPath := ""

	for current := p; current != nil; current = current.Next {
		switch current.EntryKind() {
		case kpath.FieldEntry:
			state.stack = append(state.stack, item{segment: kpath.Field(*current.Field), kind: &obj})
			if expectedPath != "" {
				expectedPath += "."
			}
			expectedPath += *current.Field

		case kpath.ArrayEntry:
			n := *current.Index
			isLeaf := current.Next == nil

			if isLeaf {
				// Leaf: position one before target. Processing the event at the indexed
				// offset will call inc() and advance to the target position.
				if n == 0 {
					// Arrays start at n=-1, segment=nil. inc() will create segment.
					state.stack = append(state.stack, item{segment: nil, n: -1, kind: &arr})
				} else {
					state.stack = append(state.stack, item{segment: kpath.Index(n - 1), n: n - 1, kind: &arr})
					expectedPath += fmt.Sprintf("[%d]", n-1)
				}
			} else {
				// Non-leaf: use actual index for path matching in nested structures.
				state.stack = append(state.stack, item{segment: kpath.Index(n), n: n, kind: &arr})
				expectedPath += fmt.Sprintf("[%d]", n)
			}

		case kpath.SparseArrayEntry:
			state.stack = append(state.stack, item{segment: kpath.SparseIndex(*current.SparseIndex), kind: &spr})
			expectedPath += fmt.Sprintf("{%d}", *current.SparseIndex)
		}
	}

	if state.CurrentPath() != expectedPath {
		panic(kp + " -> " + expectedPath + " != " + state.CurrentPath())
	}
	return state, nil
}
