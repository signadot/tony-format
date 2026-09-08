package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A read at a keyed element narrows: the element is a field of the array
// (element_identity.md), so the snapshot's path index holds it and the projection descends
// to it. reads.wide.keyed-or-idx stays where it was, and the answer is the element.
func TestKeyedReadNarrows(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)
	mustCommit(t, s, nil, `{items: [{sku: "A", q: 1}, {sku: "B", q: 2}, {sku: "G", q: 7}]}`)
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	snapCommit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	patched := mustCommit(t, s, nil, `{items: [{sku: "G", q: 8}]}`)

	g := elementName("sku", "G")
	for _, tc := range []struct {
		name   string
		kp     string
		commit int64
		wantQ  int64
	}{
		{"from the snapshot", g, snapCommit, 7},
		{"under a patch", g, patched, 8},
		{"below the element", g + ".q", patched, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := s.ReadStats()
			node, narrowed, err := readSubtreeAt(s, tc.kp, tc.commit, nil)
			if err != nil {
				t.Fatalf("ReadSubtreeAt(%q): %v", tc.kp, err)
			}
			if !narrowed || node == nil {
				t.Fatalf("ReadSubtreeAt(%q) narrowed=%v node=%v; an element is a field", tc.kp, narrowed, node)
			}
			got := node
			if strings.HasSuffix(tc.kp, ".q") {
				if intOf(got) != tc.wantQ {
					t.Errorf("q = %d, want %d", intOf(got), tc.wantQ)
				}
			} else if intOf(ir.Get(got, "q")) != tc.wantQ {
				t.Errorf("G.q = %d, want %d", intOf(ir.Get(got, "q")), tc.wantQ)
			}
			after := s.ReadStats()
			if after.WideNonField != before.WideNonField {
				t.Errorf("a keyed read was counted keyed-or-idx")
			}
			if after.Narrow != before.Narrow+1 {
				t.Errorf("narrow reads %d -> %d, want one more", before.Narrow, after.Narrow)
			}
		})
	}
}

// A read that cannot be re-rooted must not first do the read it will discard: a position
// is not a field, so a rooted read at one is refused before any read is opened.
func TestARootedReadDecidesBeforeItReads(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{items: [{q: 1}, {q: 2}, {q: 7}]}`)
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	c, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}

	// the subtree read itself can address a position, and still does
	if node, narrowed, err := readSubtreeAt(s, "items[2]", c, nil); err != nil || !narrowed || node == nil {
		t.Fatalf("a positional subtree read stopped working: narrowed=%v node=%v err=%v", narrowed, node, err)
	}

	before := s.ReadStats()
	if _, _, err := readSubtreeRootedAt(s, "items[2]", c, nil); err == nil {
		t.Fatal("a rooted read at a position was answered; a position is not a field")
	}
	after := s.ReadStats()
	if after.Narrow != before.Narrow {
		t.Errorf("the rooted read performed %d narrow read(s) it then discarded", after.Narrow-before.Narrow)
	}
}
