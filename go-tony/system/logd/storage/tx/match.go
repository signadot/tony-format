package tx

import (
	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

// evaluateMatches checks every precondition in the transaction state against the state at
// commit. readState answers the value at a path -- one bounded read there -- and an
// absent path is matched as null, so a concrete precondition simply fails to hold rather
// than erroring.
func evaluateMatches(state *State, readState func(kpath string, commit int64, scopeID *string) (*ir.Node, error), commit int64) (bool, error) {
	scopeID := state.Scope

	for _, patcher := range state.PatcherData {
		m := patcher.API.Match
		if m == nil || m.Data == nil {
			continue
		}
		current, err := readState(m.Path, commit, scopeID)
		if err != nil {
			return false, err
		}
		if current == nil {
			current = ir.Null()
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

// unquoteFieldKey strips surrounding quotes from a stored field key so a key stored
// quoted still matches its canonical (unquoted) name. A bare key is returned
// unchanged, so it is safe to call on any key.
//
// token.Unquote validates before it decodes. The shape test this used to do instead —
// opens with a quote, ends with the same one — admits keys that are not well-formed
// quoted strings (`"a"b"`, `"a\qb"`), and the decoder used to panic on exactly those.
// Anything Unquote rejects is not a quoted key, so the raw key is the right answer.
func unquoteFieldKey(s string) string {
	if u, err := token.Unquote(s); err == nil {
		return u
	}
	return s
}
