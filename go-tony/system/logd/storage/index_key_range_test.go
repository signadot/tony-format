package storage

import "testing"

// What the INDEX can represent as a key is narrower than what a merge accepts, and the
// difference is silent: the merge is correct in every case below, the state is correct,
// and only the index is wrong. indexPatchRec discards ElemKey's second return, so "not a
// key" becomes "the empty key" -- which looks like a valid path segment.
//
// Latent today. Under the scope overlay the index IS the ownership set (plan R3), so a
// collapse means two elements share one ownership path. See P1.
func TestIndexKeyRange(t *testing.T) {
	for _, tc := range []struct {
		name, write string
		wantPaths   int // distinct items(...) element paths the index ends up with
		wantElems   int // elements the state actually holds
	}{
		{"string keys", `{items: !key(sku) [{sku: "A", q: 1}, {sku: "B", q: 2}]}`, 2, 2},
		{"number keys", `{items: !key(sku) [{sku: 1, q: 1}, {sku: 2, q: 2}]}`, 2, 2},
		{"number and string that render alike", `{items: !key(sku) [{sku: 1, q: 1}, {sku: "1", q: 2}]}`, 1, 2},
		{"object-valued key", `{items: !key(sku) [{sku: {a: 1}, q: 1}, {sku: {a: 2}, q: 2}]}`, 1, 2},
		{"bare !key", `{items: !key [{a: 1}, {a: 2}]}`, 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStorage(t)
			c := mustCommit(t, s, nil, tc.write)

			elems := map[string]bool{}
			for _, p := range indexPathSet(s) {
				if len(p) > 6 && p[:6] == "items(" {
					// keep only the element path itself, not its fields
					seg := p
					for i := 6; i < len(seg); i++ {
						if seg[i] == ')' {
							seg = seg[:i+1]
							break
						}
					}
					elems[seg] = true
				}
			}
			doc := mustReadScope(t, s, c, nil)
			got := len(elems)
			t.Logf("  index element paths: %d  state: %s", got, encodeWire(t, doc))
			if got != tc.wantPaths {
				t.Errorf("index has %d element paths, want %d", got, tc.wantPaths)
			}
			if tc.wantPaths < tc.wantElems {
				t.Logf("  => %d elements share %d index path(s): the collapse P1 has to reject",
					tc.wantElems, tc.wantPaths)
			}
		})
	}
}
