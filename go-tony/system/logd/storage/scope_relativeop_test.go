package storage

import (
	"bytes"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
)

func showDoc(t *testing.T, s *Storage, scope *string, label string) string {
	t.Helper()
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	doc, err := s.ReadStateAt("", commit, scope)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if doc == nil {
		t.Logf("%s: <empty>", label)
		return ""
	}
	var buf bytes.Buffer
	if err := encode.Encode(doc, &buf, encode.EncodeWire(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	t.Logf("%s: %s", label, buf.String())
	return buf.String()
}

// TestScope_RelativeOpReevaluates asks whether a scope patch holding a RELATIVE op —
// one whose result depends on the value it is applied to — is re-evaluated against the
// baseline as the baseline moves.
//
// This is the premise the planned scope-overlay compaction rests on (issue
// 5hmq80f3h12krh1mbsn0: "leaf writes are absolute (latest-per-path is sound)... a
// relative leaf op would need more, but those are not believed to exist here"). If a
// relative op does re-evaluate, then no absolute per-leaf materialization can stand in
// for it: the overlay would freeze a result computed against one baseline.
func TestScope_RelativeOpReevaluates(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	scope := "sandbox"

	scalingCommit(t, s, nil, `{a: {x: 1}}`, nil)
	showDoc(t, s, nil, "baseline after {a:{x:1}}")

	scalingCommit(t, s, &scope, `{a: !rename [{from: "x", to: "y"}]}`, nil)
	afterRename := showDoc(t, s, &scope, "scoped after !rename x->y")

	// Baseline now writes x again, AFTER the scope's rename.
	scalingCommit(t, s, nil, `{a: {x: 2}}`, nil)
	showDoc(t, s, nil, "baseline after {a:{x:2}}")
	afterBaseline := showDoc(t, s, &scope, "scoped after baseline x=2")

	t.Logf("")
	t.Logf("If the op re-evaluates, the scoped view renames the NEW x (y:2) and the")
	t.Logf("overlay cannot be collapsed to the absolute value it produced earlier (y:1).")
	t.Logf("first=%q later=%q", afterRename, afterBaseline)
}
