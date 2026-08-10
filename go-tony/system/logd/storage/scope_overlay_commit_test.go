package storage

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"testing"
)

// scopeIndexAt lists "path@endCommit" for every segment this scope has, which is what an
// overlay keyed by index path with a commit stamp would be built from.
func scopeIndexAt(s *Storage, scope string) []string {
	var out []string
	for _, seg := range s.index.AllSegments() {
		if seg.ScopeID == nil || *seg.ScopeID != scope {
			continue
		}
		p := seg.KindedPath
		if p == "" {
			p = "<root>"
		}
		out = append(out, fmt.Sprintf("%s@%d", p, seg.EndCommit))
	}
	sort.Strings(out)
	return out
}

// TestScope_OverlayCommitStamps asks whether "latest write per index path, with the
// commit it was written at" is enough to decide which entries a later write kills.
//
// The pruning rule it suggests: an entry at a.x written at commit c is dead if an
// ANCESTOR of it was written at some commit > c. That is exactly right when the later
// ancestor write REPLACED what was there, and exactly wrong when it merged into it --
// and the index records both the same way, since indexPatchRec indexes an entry at
// every level of its patch regardless of what the patch does there.
func TestScope_OverlayCommitStamps(t *testing.T) {
	scope := "s1"

	t.Log("A. later ancestor write REPLACES: the deeper entries must die")
	{
		s, err := Open(t.TempDir(), nil)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer s.Close()
		scalingCommit(t, s, nil, `{keep: 0}`, nil)
		scalingCommit(t, s, &scope, `{a: {x: 1, y: 2}}`, nil)
		scalingCommit(t, s, &scope, `{a: "scalar"}`, nil)
		t.Logf("   read:  %s", showDocQuiet(t, s, &scope))
		t.Logf("   index: %v", scopeIndexAt(s, scope))
	}

	t.Log("B. later ancestor write MERGES: the deeper entries must survive")
	{
		s, err := Open(t.TempDir(), nil)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer s.Close()
		scalingCommit(t, s, nil, `{keep: 0}`, nil)
		scalingCommit(t, s, &scope, `{a: {x: 1}}`, nil)
		scalingCommit(t, s, &scope, `{a: {y: 2}}`, nil)
		t.Logf("   read:  %s", showDocQuiet(t, s, &scope))
		t.Logf("   index: %v", scopeIndexAt(s, scope))
	}

	t.Log("")
	t.Log("If A and B produce the same index shape -- a later segment at `a` above the")
	t.Log("earlier ones at a.x/a.y -- then the commit stamp cannot tell them apart, and")
	t.Log("the pruning rule needs to know what the ancestor write DID, not just when.")
}

func showDocQuiet(t *testing.T, s *Storage, scope *string) string {
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
		return "<empty>"
	}
	return encodeWire(t, doc)
}

func encodeWire(t *testing.T, n *ir.Node) string {
	t.Helper()
	var buf bytes.Buffer
	if err := encode.Encode(n, &buf, encode.EncodeWire(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.String()
}
