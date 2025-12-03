package tx

import (
	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
)

// evaluateMatches checks all match conditions in the transaction state.
// readState is called with (kpath, commit) to get current state at that path.
func evaluateMatches(state *State, readState func(kpath string, commit int64) (*ir.Node, error), commit int64) (bool, error) {
	for _, patcher := range state.PatcherData {
		m := patcher.API.Match
		if m == nil || m.Data == nil {
			continue
		}

		kpath := m.Path
		current, err := readState(kpath, commit)
		if err != nil {
			return false, err
		}

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
