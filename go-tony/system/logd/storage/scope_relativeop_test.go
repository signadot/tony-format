package storage

import (
	"bytes"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
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

// TestScope_CannotStoreARelativeOp: a scope cannot store an operation whose result
// depends on the value it is applied to.
//
// This test used to ask whether such an op RE-EVALUATES as baseline moves, and it
// does. That was the premise the planned scope-overlay compaction rests on (issue
// 5hmq80f3h12krh1mbsn0: "leaf writes are absolute (latest-per-path is sound)... a
// relative leaf op would need more, but those are not believed to exist here") --
// and demonstrating it meant storing one, which left the scope holding a delta whose
// meaning changed underneath it. A !replace was worse than a !rename: once baseline
// moved, the scope could not be read at all, and nothing a client did afterwards
// repaired it (3xn08cb6h12kr4psg5n0).
//
// The store refuses them now, so the premise is enforced rather than believed: an
// absolute per-leaf materialization cannot be standing in for a relative op, because
// there is no relative op in a scope to stand in for.
//
// Baseline is deliberately not held to this, and the second half checks that: its
// replay is deterministic, so a relative op there means the same thing forever.
func TestScope_CannotStoreARelativeOp(t *testing.T) {
	for _, body := range []string{
		`{a: !rename [{from: "x", to: "y"}]}`,
		`{a: {x: !replace {from: 1, to: 2}}}`,
		`{a: {x: !strdiff(false) {0: !insert "s"}}}`,
	} {
		s, err := Open(t.TempDir(), nil)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		scalingCommit(t, s, nil, `{a: {x: 1}}`, nil)

		scope := "sandbox"
		txn, err := s.NewTx(1, &scope)
		if err != nil {
			t.Fatalf("NewTx: %v", err)
		}
		data, err := parse.Parse([]byte(body))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		_, err = txn.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: data}})
		if err == nil {
			t.Errorf("a scope stored %s; when baseline moves it means something else", body)
		} else {
			t.Logf("refused: %v", err)
		}
		s.Close()
	}
}

// The same operation in BASELINE is sound and stays sound: the base a baseline delta
// replays against is the same base forever. Refusing it there would refuse a correct
// write to catch an incorrect one.
func TestBaseline_MayStoreARelativeOp(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	scalingCommit(t, s, nil, `{a: {x: 1}}`, nil)
	scalingCommit(t, s, nil, `{a: {x: !replace {from: 1, to: 2}}}`, nil)

	if got, want := showDoc(t, s, nil, "baseline after !replace"), "{a: {x: 2}}"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	// And it still reads the same on a later replay, which is the property that makes
	// it storable here at all.
	if got, want := showDoc(t, s, nil, "baseline, read again"), "{a: {x: 2}}"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
