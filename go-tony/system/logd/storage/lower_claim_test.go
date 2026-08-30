package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// What a SCOPE stores for a write, and the three rules claimDelta is built from.
//
// Baseline stores a difference; a scope stores a claim. The difference between the two is
// not a matter of degree: a scope's patches replay over a baseline that advances
// underneath them, so what it stored has to keep meaning what it meant at the write,
// whatever baseline does next.
//
//  1. A claim is not a DIFFERENCE. Where the effect against one baseline is smaller than
//     what the scope said, a delta built from the effect loses the rest.
//  2. A claim is BUILT as a patch and never applied while it is being built, because one
//     of the things it must be able to state is that a path holds nothing -- and only a
//     patch can say that.
//  3. A claim reports what it cannot READ. Absence is an answer and a failed read is not,
//     and conflating them stores a delete of the client's data.
//
// The property they protect -- that a scope reads its own write back unchanged until it
// writes there again -- is checked over generated streams in lower_claim_diff_test.go.

// Rule 1, in the case that shows it: a delete of a path baseline has not created yet. The
// write changes only the spine, so a diff says `a: {}`, which merges to nothing --
// and the scope stops shadowing that path forever after, so baseline's later write
// shows straight through into a scope that had said no to it.
//
// See claimDelta. Each row is: what the scope says, then what baseline does after,
// then what the scope must still read.
func TestAScopeStoresClaimsNotDifferences(t *testing.T) {
	tests := []struct {
		name             string
		seed             string
		scopePath, scope string
		basePath, base   string
		want             string
	}{{
		name:      "a delete of what baseline has not written yet",
		seed:      `{z: 0}`,
		scopePath: "a.b", scope: `!delete`,
		basePath: "a.b", base: `{k: 1}`,
		want: "a: {} z: 0",
	}, {
		// The same shape one level up: the scope deletes a whole subtree that is
		// not there, and baseline fills it in.
		name:      "a delete of a subtree baseline then creates",
		seed:      `{z: 0}`,
		scopePath: "a", scope: `!delete`,
		basePath: "a", base: `{b: {k: 1}}`,
		want: "z: 0",
	}, {
		// A relative op that DOES change something still has to hold its ground:
		// the scope appends, baseline replaces the array underneath.
		name:      "an append baseline overwrites",
		seed:      `{xs: [1]}`,
		scopePath: "xs", scope: `!arraydiff {1: !insert 2}`,
		basePath: "xs", base: `[9]`,
		want: "xs: [ 1 2 ]",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := openTestStorage(t)
			s.LowerEverything(true)
			mustCommit(t, s, nil, test.seed)

			const scope = "s1"
			if _, err := applyScopeOp(t, s, scopeOp{
				scoped: true, path: test.scopePath, src: test.scope,
			}, scope); err != nil {
				t.Fatalf("the scope's write: %v", err)
			}
			if _, err := applyScopeOp(t, s, scopeOp{
				path: test.basePath, src: test.base,
			}, scope); err != nil {
				t.Fatalf("baseline's write: %v", err)
			}

			commit, err := s.GetCurrentCommit()
			if err != nil {
				t.Fatalf("GetCurrentCommit: %v", err)
			}
			sc := scope
			doc, err := s.ReadStateAt("", commit, &sc)
			if err != nil {
				t.Fatalf("scoped read: %v", err)
			}
			if got := flatten(t, doc); got != test.want {
				t.Errorf("the scope reads\n  %s\nwant\n  %s", got, test.want)
			}
		})
	}
}

// Rule 3. Absence is an answer; a failure to read is not.
//
// ir.Node.GetKPathWith answers (nil, nil) for a path the document does not have -- the
// idiom claimDelta reads -- and an error for something else entirely: a type mismatch on
// the way down, an index past the end, a path that will not parse. Those two were one
// branch, so a failure to READ a path became a claim that the path holds NOTHING, and
// the scope stored a delete of the client's data.
//
// A refused write is recoverable and a stored one is not, which is why this is an error
// and not a log line -- the same reason lowerWrite refuses a delta it cannot promise to
// re-apply.
func TestAClaimRefusesWhatItCannotRead(t *testing.T) {
	next, err := parse.Parse([]byte(`{a: 1, o: {b: 2}, xs: [1, 2]}`))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path string
		wantErr    bool
		want       string
	}{
		{"a path the document does not have", "missing", false, "missing: !delete null"},
		{"a path under one it does not have", "o.missing", false, "o: missing: !delete null"},
		{"through a scalar", "a.b", true, ""},
		{"an index past the end", "xs[9]", true, ""},
		{"a path that will not parse", "((", true, ""},
		{"a path it does have", "o.b", false, "o: b: !raw 2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := claimDelta(next, []string{test.path})
			if test.wantErr {
				if err == nil {
					t.Fatalf("claiming %q was accepted, and answered %s",
						test.path, withComments(got))
				}
				if !strings.Contains(err.Error(), "cannot read it back") {
					t.Errorf("the refusal does not say why: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("claiming %q: %v", test.path, err)
			}
			if s := withComments(got); s != test.want {
				t.Errorf("claimed %s, want %s", s, test.want)
			}
		})
	}
}

// Rule 2, and what breaks without it.
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
