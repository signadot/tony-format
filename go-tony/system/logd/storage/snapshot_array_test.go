package storage

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A field written under a path that holds an unkeyed array replaces the array, whether or
// not a snapshot sits between the two writes. It did not: with the array in the snapshot,
// the write was accepted -- its site, k2.n, read absent -- and every read at k2 and at the
// root failed from then on, "cannot graft [n] into the array", while k2.n and the
// siblings still read (0v2ws9w4h12kr7stm5n0; internal/patches replaceArray).
func TestAFieldWrittenUnderAnArrayAfterASnapshotReplacesIt(t *testing.T) {
	for _, tc := range []struct{ name, then, wantK2 string }{
		{"a value", `5`, `{n: 5}`},
		{"a delete", `!delete`, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plain := openTestStorage(t)
			snapped := openTestStorage(t)
			for _, s := range []*Storage{plain, snapped} {
				mustCommit(t, s, nil, `{k2: [{n: 1}], other: 1}`)
			}
			if err := snapped.SwitchDLog(); err != nil {
				t.Fatal(err)
			}
			for _, s := range []*Storage{plain, snapped} {
				commitAt(t, s, nil, "k2.n", tc.then)
			}
			for _, kp := range []string{"", "k2", "k2.n", "other"} {
				want, _, err := readSubtreeAt(plain, kp, 2, nil)
				if err != nil {
					t.Fatalf("without a snapshot, read %q: %v", kp, err)
				}
				got, _, err := readSubtreeAt(snapped, kp, 2, nil)
				if err != nil {
					t.Fatalf("with the array in the snapshot, read %q: %v", kp, err)
				}
				if withComments(got) != withComments(want) {
					t.Errorf("read %q: with the snapshot %s, without %s", kp, withComments(got), withComments(want))
				}
			}
			expectAt(t, snapped, nil, "k2", tc.wantK2)
		})
	}
}

// expectAt reads kp at the head and compares it to want as data.
func expectAt(t *testing.T, s *Storage, scope *string, kp, want string) {
	t.Helper()
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	got, _, err := readSubtreeAt(s, kp, commit, scope)
	if err != nil {
		t.Fatalf("read %s: %v", kp, err)
	}
	w, err := parse.Parse([]byte(want))
	if err != nil {
		t.Fatalf("parse %q: %v", want, err)
	}
	if !mergeop.RawEqual(got, w) {
		t.Errorf("at %q:\n got  %s\n want %s", kp, encode.MustString(got), encode.MustString(w))
	}
}
