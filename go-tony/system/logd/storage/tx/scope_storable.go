package tx

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Baseline and a scope are safe from different things, because their bases behave
// differently, and this is the half a verified write cannot cover.
//
// Verifying a patch applies before storing it settles baseline completely: the base
// a baseline delta replays against is the same base forever, so a delta which
// applied once applies always. A scope's base MOVES -- baseline advances underneath
// it -- so an operation a scope stores can stop applying long after it was
// verified, with nothing wrong at the time of the write:
//
//	baseline: {s: bob}
//	scope s1 writes  s: !replace {from: bob, to: rob}   verifies, commits, reads s: rob
//	baseline writes  s: someone-else
//	reading s1 -> replace patching "someone-else" gave # node at $.from differes ...
//
// The scope is unreadable from then on and stays that way, because the delta is
// stored and every read of that scope replays it. DeleteScope is the only repair,
// which is to say the sandbox is gone (3xn08cb6h12kr4psg5n0).
//
// api.StorageContext is exactly the rule for this, and it says so: an operation
// which re-evaluates against a base that has moved may not be stored. It was
// enforced on the overlay logd builds itself (scope_overlay.go) and never on the
// write a client sends, which is the only place it can be broken.
//
// Baseline writes are deliberately NOT held to it. `!arraydiff {0: 99}` on a
// two-element array is sound in baseline and stays sound, so refusing it would
// refuse a correct write to catch an incorrect one. The distinction is the whole
// point:
//
//	baseline   deterministic replay   verify it applies, once
//	scope      base moves             hold it to the vocabulary
//
// It is asked of the CLIENT's patch data, before MergePatches -- which manufactures
// an !arraydiff of its own for a write at an array index. Holding the merged patch
// to the vocabulary would refuse every positional write in a scope on account of
// logd's own routing.

// NotStorableInScopeError is what a scoped write gets when its data uses an
// operation whose meaning depends on a base that will move. Typed so the session
// can report it as the client's mistake -- invalid_diff -- rather than as a failure
// of the store.
type NotStorableInScopeError struct {
	Path  string // the write path, as the client gave it
	Scope string
	Err   error // what api.ValidateForStorage said, which names the op and why
}

func (e *NotStorableInScopeError) Error() string {
	return fmt.Sprintf("the patch at %q cannot be stored in scope %q, whose base moves as "+
		"baseline advances: %v", e.Path, e.Scope, e.Err)
}

func (e *NotStorableInScopeError) Unwrap() error { return e.Err }

// checkStorableInScope holds a scoped write to the storage vocabulary. A baseline
// write is not held to it and returns nil.
func checkStorableInScope(p *api.Patch, scope *string) error {
	if p == nil || p.Data == nil || scope == nil {
		return nil
	}
	if err := api.ValidateForStorage(p.Data); err != nil {
		return &NotStorableInScopeError{Path: p.Path, Scope: *scope, Err: err}
	}
	return nil
}
