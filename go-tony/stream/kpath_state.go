package stream

import (
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// KPathState creates a State that represents being at the given kinded path.
// It parses the kpath string and builds up the State's internal structure.
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

	for current := p; current != nil; current = current.Next {
		switch current.EntryKind() {
		case kpath.FieldEntry:
			state.stack = append(state.stack, item{segment: kpath.Field(*current.Field), kind: &obj})

		case kpath.ArrayEntry:
			n := *current.Index
			state.stack = append(state.stack, item{segment: kpath.Index(n), n: n, kind: &arr})

		case kpath.SparseArrayEntry:
			state.stack = append(state.stack, item{segment: kpath.SparseIndex(*current.SparseIndex), kind: &spr})
		}
	}

	return state, nil
}
