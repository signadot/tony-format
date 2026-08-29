package storage

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A claim states what the scope holds at each path it claims, and one of the things it
// has to be able to state is that a path holds NOTHING. Only a patch can say that -- a
// document cannot, since absence in one is indistinguishable from a path nobody
// mentioned -- so the claim is built as a patch and never applied while being built.
//
// It used to be accumulated with api.NextState, which put each claim in the instruction
// role against the claim-so-far: a patch standing in the document position. A value
// survives that, because a value means the same thing as data and as an instruction. A
// tombstone does not -- it ran against the half-built claim and left only its effect --
// so the field ORDER of the client's patch decided whether their delete was kept.
func TestAClaimKeepsItsDeletesWhateverOrderTheyCome(t *testing.T) {
	tests := []struct {
		name, src string
	}{{
		name: "the delete last",
		src:  `{k1: !replace {from: 1, to: 5}, k0: !delete}`,
	}, {
		name: "the delete first",
		src:  `{k0: !delete, k1: !replace {from: 1, to: 5}}`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := openTestStorage(t)
			s.EnableLowering(true)
			const scope = "s1"
			sc := scope
			mustCommit(t, s, nil, `{d: {k0: 9, k1: 1}}`)

			if _, err := applyScopeOp(t, s, scopeOp{
				scoped: true, path: "d", src: test.src,
			}, scope); err != nil {
				t.Fatalf("scoped write: %v", err)
			}
			read := func(when string) *ir.Node {
				t.Helper()
				c, err := s.GetCurrentCommit()
				if err != nil {
					t.Fatalf("GetCurrentCommit: %v", err)
				}
				doc, err := s.ReadStateAt("", c, &sc)
				if err != nil {
					t.Fatalf("scoped read %s: %v", when, err)
				}
				return doc
			}
			if got, want := withComments(read("after the write")), "d: k1: 5"; got != want {
				t.Errorf("the scope reads %s, want %s -- the client's delete of k0 was not kept",
					got, want)
			}

			// And it is a CLAIM, so baseline writing k0 afterwards does not show through.
			if _, err := applyScopeOp(t, s, scopeOp{path: "d", src: `{k0: 26}`}, scope); err != nil {
				t.Fatalf("baseline: %v", err)
			}
			if got, want := withComments(read("after baseline wrote k0")), "d: k1: 5"; got != want {
				t.Errorf("after baseline wrote k0 the scope reads %s, want %s", got, want)
			}
		})
	}
}
