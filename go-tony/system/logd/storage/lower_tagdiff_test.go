package storage

import "testing"

// A relative write at the DOCUMENT ROOT destroyed the document.
//
// A no-op !rename changes nothing but presentation, so lowering it produces a
// presentation-only delta whose ROOT is a tag op over a null:
//
//	!addtag(bracket) null
//
// markDeltaRoots cannot descend past that null -- the descent wants a single-field
// object -- so it marks that node, and the marker composes into the operation's own tag
// chain. mergeop hands everything after the first registered op to the child, so the null
// that meant "the value did not change" arrived as a TAGGED null and the merge answered
// with it. The store read back null (1hf5pzj6h12ksd40jdn0).
//
// Fixed in mergeop -- a null child says the value did not change unless an OPERATION
// trails the op -- and pinned here because this is the path that reaches it. Lowering is
// on by default, so this was the shipped answer.
//
// The same write one level down was always safe: its delta is `{a: !addtag(bracket) null}`
// and the marker lands on the plain object above the op. Both are kept, because the
// difference between them is what made this hard to see.
func TestARelativeRootWriteDoesNotDestroyTheDocument(t *testing.T) {
	for _, tc := range []struct{ name, path, write string }{
		{"at the document root", "", `!rename [{from: "k1", to: "k1"}]`},
		{"one level down", "a", `!rename [{from: "z", to: "z"}]`},
	} {
		for _, lower := range []bool{true, false} {
			name := tc.name
			if !lower {
				name += " (lowering off)"
			}
			t.Run(name, func(t *testing.T) {
				s := openTestStorage(t)
				s.EnableLowering(lower)
				mustCommit(t, s, nil, `{k1: 5, k2: 16, a: {z: 1}}`)
				c := commitAt(t, s, nil, tc.path, tc.write)

				doc, err := s.ReadStateAt("", c, nil)
				if err != nil {
					t.Fatalf("read: %v", err)
				}
				if doc == nil {
					t.Fatalf("the write destroyed the document")
				}
				// A no-op rename says nothing about any value, so every one stands.
				if got := getInt(doc, "k1"); got != 5 {
					t.Errorf("k1 = %d, want 5\n%s", got, mustEncode(t, doc))
				}
				if got := getInt(doc, "k2"); got != 16 {
					t.Errorf("k2 = %d, want 16\n%s", got, mustEncode(t, doc))
				}
				if got := getInt(doc, "a", "z"); got != 1 {
					t.Errorf("a.z = %d, want 1\n%s", got, mustEncode(t, doc))
				}
			})
		}
	}
}
