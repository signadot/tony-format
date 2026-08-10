package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// TestDiffKeyedArrayEqual: diffArray takes the keyed branch only when BOTH sides carry
// !key(f) with the same arg. DiffArrayByKey then diffs the two as objects and indexes
// the result — but an equal pair diffs to nil, so the result is dereferenced nil.
//
// Equal is the ordinary case for a watch delta: most commits leave most arrays alone.
func TestDiffKeyedArrayEqual(t *testing.T) {
	mk := func(s string) *ir.Node {
		n, err := parse.Parse([]byte(s))
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return n
	}
	a := mk(`!key(name) [{name: "a", v: 1}]`)
	b := mk(`!key(name) [{name: "a", v: 1}]`)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Diff panicked on two equal keyed arrays: %v", r)
		}
	}()
	d := Diff(a, b)
	t.Logf("diff of two equal keyed arrays: %v", d)
}
