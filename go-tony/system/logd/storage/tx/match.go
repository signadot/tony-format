package tx

import (
	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// evaluateMatches checks all match conditions in the transaction state.
// readState is called with (kpath, commit, scopeID) to get current state at that path.
func evaluateMatches(state *State, readState func(kpath string, commit int64, scopeID *string) (*ir.Node, error), commit int64) (bool, error) {
	scopeID := state.Scope

	for _, patcher := range state.PatcherData {
		m := patcher.API.Match
		if m == nil || m.Data == nil {
			continue
		}

		kpath := m.Path
		doc, err := readState(kpath, commit, scopeID)
		if err != nil {
			return false, err
		}

		// readState returns the state rooted at the document root (the path
		// structure is preserved), so navigate down to the value at kpath before
		// comparing — mirroring how the match/read path extracts it. Comparing the
		// rooted doc directly would never match a pattern written against the value.
		current := valueAtPath(doc, kpath)

		matched, err := tony.Match(current, m.Data)
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
	}
	return true, nil
}

// valueAtPath navigates a root-rooted document down to the value at kpath,
// returning null when the path is absent (so a concrete precondition simply
// fails to match rather than erroring).
func valueAtPath(doc *ir.Node, kp string) *ir.Node {
	if doc == nil {
		return ir.Null()
	}
	if kp == "" {
		return doc
	}
	current := doc
	for _, part := range kpath.SplitAll(kp) {
		if current == nil || current.Type != ir.ObjectType {
			return ir.Null()
		}
		var next *ir.Node
		for i, field := range current.Fields {
			if field.String == part {
				next = current.Values[i]
				break
			}
		}
		if next == nil {
			return ir.Null()
		}
		current = next
	}
	return current
}
