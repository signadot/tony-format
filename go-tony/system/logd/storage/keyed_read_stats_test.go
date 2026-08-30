package storage

import (
	"testing"
)

// Stage 0 of thqtmm2th12kr051jhn0: a read at a keyed path cannot be narrowed today, and
// the report has to SAY that, because every later stage is judged by these counters.
//
// Before this, a keyed read was counted two different ways depending on what happened to
// be in the log -- "operator" when a patch sat above the path, "absent" when the snapshot
// held it -- and neither names the path. reads.wide.keyed-or-idx stayed 0 in a store
// doing nothing but keyed reads.
func TestKeyedReadIsCountedAsKeyed(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{items: !key(sku) [{sku: "A", q: 1}, {sku: "B", q: 2}, {sku: "G", q: 7}]}`)
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	snapCommit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	patched := mustCommit(t, s, nil, `{items: !key(sku) [{sku: "G", q: 8}]}`)

	for _, tc := range []struct {
		name   string
		kp     string
		commit int64
	}{
		// the snapshot holds the element and nothing is written above it
		{"from the snapshot", `items("G")`, snapCommit},
		// a patch sits above the path, so the projection meets !key first
		{"under a patch", `items("G")`, patched},
		{"below the element", `items("G").q`, patched},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := s.ReadStats().WideNonField
			node, narrowed, err := s.ReadSubtreeAt(tc.kp, tc.commit, nil)
			if err != nil {
				t.Fatalf("ReadSubtreeAt(%q): %v", tc.kp, err)
			}
			if narrowed || node != nil {
				t.Errorf("ReadSubtreeAt(%q) answered narrowed=%v node=%v; a keyed path "+
					"cannot be addressed, and saying it was narrowed invites a caller to "+
					"trust the nil", tc.kp, narrowed, node)
			}
			if got := s.ReadStats().WideNonField; got != before+1 {
				t.Errorf("keyed read counted %d keyed-or-idx, want %d: the report names "+
					"the wrong reason", got-before, 1)
			}
		})
	}
}

// The element IS there: what a keyed read cannot do is find it. The wide read must still
// answer, which is what makes declining the right answer rather than a lost one.
func TestAKeyedElementTheNarrowReadDeclinesIsStillThere(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{items: !key(sku) [{sku: "A", q: 1}, {sku: "G", q: 7}]}`)
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	c, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	if _, narrowed, err := s.ReadSubtreeAt(`items("G")`, c, nil); err != nil || narrowed {
		t.Fatalf("the narrow read claimed a keyed path: narrowed=%v err=%v", narrowed, err)
	}
	doc, err := s.ReadStateAt("", c, nil)
	if err != nil {
		t.Fatalf("ReadStateAt: %v", err)
	}
	if got := intOf(elemField(t, doc, "items", "sku", "G", "q")); got != 7 {
		t.Fatalf("the wide read answers q=%d for the element the narrow read declined, want 7", got)
	}
}

// A read that cannot be re-rooted must not first do the read it will discard. An indexed
// path narrows in ReadSubtreeAt and is then thrown away by ReadSubtreeRootedAt, so asking
// afterwards paid for the narrow read AND the wide one.
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
	if node, narrowed, err := s.ReadSubtreeAt("items[2]", c, nil); err != nil || !narrowed || node == nil {
		t.Fatalf("a positional subtree read stopped working: narrowed=%v node=%v err=%v", narrowed, node, err)
	}

	before := s.ReadStats()
	if _, narrowed, err := s.ReadSubtreeRootedAt("items[2]", c, nil); err != nil || narrowed {
		t.Fatalf("ReadSubtreeRootedAt(items[2]) narrowed=%v err=%v, want declined", narrowed, err)
	}
	after := s.ReadStats()
	if after.Narrow != before.Narrow {
		t.Errorf("the rooted read performed %d narrow read(s) it then discarded", after.Narrow-before.Narrow)
	}
	if after.WideNonField != before.WideNonField+1 {
		t.Errorf("declined read counted %d keyed-or-idx, want 1", after.WideNonField-before.WideNonField)
	}
}
