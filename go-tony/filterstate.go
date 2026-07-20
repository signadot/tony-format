package tony

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
)

// FilterState applies a match/trim pattern to a read result and returns the
// trimmed projection, mirroring how logd answers a MATCH that carries a pattern:
//
//   - a non-array state that matches the pattern is trimmed to the pattern's
//     shape; one that does not match yields null;
//   - an array state is filtered to the items that match, each trimmed, with the
//     original array tag (e.g. !key(id)) preserved.
//
// It is the shared implementation used by logd (server-side) and docd (over a
// composed read it assembles from several sources) so both project identically.
func FilterState(state, match *ir.Node) (*ir.Node, error) {
	if state == nil {
		state = ir.Null()
	}
	if state.Type != ir.ArrayType {
		matches, err := Match(state, match)
		if err != nil {
			return nil, err
		}
		if matches {
			return Trim(match, state), nil
		}
		return ir.Null(), nil
	}

	var filtered []*ir.Node
	for _, item := range state.Values {
		matches, err := Match(item, match)
		if err != nil {
			return nil, fmt.Errorf("match error on item: %w", err)
		}
		if matches {
			filtered = append(filtered, Trim(match, item))
		}
	}

	result := ir.FromSlice(filtered)
	if state.Tag != "" {
		result = result.WithTag(state.Tag)
	}
	return result, nil
}
