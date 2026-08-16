package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A delta the store cannot apply is not a write, it is a fault every later read
// replays. It is refused before anything is stored.
//
// Which patches can fail this way is not a property of the operation: a field
// write states what results and cannot fail, while an operation which asserts
// something about the base can, and its assertion can be false. So the pairs
// below are the same operation, once with a true assertion and once with a false
// one -- the first must still commit (7cdvym1fh12ksmd5g5n0).
func TestAWriteThatCannotBeAppliedIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name, seed, path, body string
		refused                bool
	}{
		{
			name: "an arraydiff which the array has room for",
			seed: `{v: [1, 2]}`, path: "v", body: `!arraydiff {0: 99}`,
		},
		{
			name: "an arraydiff past the end",
			seed: `{v: [1, 2]}`, path: "v", body: `!arraydiff {5: 99}`, refused: true,
		},
		{
			name: "an insert the arraydiff has a position for",
			seed: `{v: [1, 2]}`, path: "v", body: `!arraydiff {1: !insert 99}`,
		},
		{
			name: "a replace whose from: is what is there",
			seed: `{s: bob}`, path: "s", body: `!replace {from: bob, to: rob}`,
		},
		{
			name: "a replace whose from: is not",
			seed: `{s: bob}`, path: "s", body: `!replace {from: nope, to: rob}`, refused: true,
		},
		{
			name: "a strdiff of something which is not a string",
			seed: `{v: [1, 2]}`, path: "v", body: `!strdiff(false) {0: !insert x}`, refused: true,
		},
		{
			name: "an ordinary write, which cannot fail to apply",
			seed: `{a: 1}`, path: "b", body: `2`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(t.TempDir(), nil)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer s.Close()
			if _, err := arrayWriteCommit(t, s, "", tc.seed); err != nil {
				t.Fatalf("seed: %v", err)
			}
			before, err := s.GetCurrentCommit()
			if err != nil {
				t.Fatalf("GetCurrentCommit: %v", err)
			}
			was := readWholeStore(t, s, before)

			_, err = arrayWriteCommit(t, s, tc.path, tc.body)
			switch {
			case tc.refused && err == nil:
				t.Fatalf("%s committed; no read of the store would have worked again", tc.body)
			case tc.refused:
				if !strings.Contains(err.Error(), "does not apply") {
					t.Errorf("the refusal does not say what was wrong: %v", err)
				}
				t.Logf("refused: %v", err)
			case err != nil:
				t.Fatalf("%s applies and was refused: %v", tc.body, err)
			}

			// The store reads either way, and a refused write left no commit behind.
			now, err := s.GetCurrentCommit()
			if err != nil {
				t.Fatalf("GetCurrentCommit: %v", err)
			}
			if tc.refused {
				if now != before {
					t.Errorf("a refused write took commit %d", now)
				}
				if got := readWholeStore(t, s, now); got != was {
					t.Errorf("the store changed under a refused write:\n got %s\nwant %s", got, was)
				}
				return
			}
			readWholeStore(t, s, now)
		})
	}
}

// The same, for a scope: a scoped write is checked against the scoped view, which
// is the state its patch will be applied to.
func TestAScopedWriteThatCannotBeAppliedIsRefused(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if _, err := arrayWriteCommit(t, s, "", `{v: [1, 2]}`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	scope := "s1"
	if err := scopedCommit(t, s, &scope, "v", `!arraydiff {5: 99}`); err == nil {
		t.Fatal("the scoped write committed; no read of the scope would have worked again")
	} else {
		t.Logf("refused: %v", err)
	}

	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	if _, err := s.ReadStateAt("", commit, &scope); err != nil {
		t.Errorf("the scope is unreadable after a refused write: %v", err)
	}
	if _, err := s.ReadStateAt("", commit, nil); err != nil {
		t.Errorf("baseline is unreadable after a refused scoped write: %v", err)
	}
}

func scopedCommit(t *testing.T, s *Storage, scope *string, path, body string) error {
	t.Helper()
	tx, err := s.NewTx(1, scope)
	if err != nil {
		return err
	}
	data, err := parse.Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse %q: %v", body, err)
	}
	p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: path, Data: data}})
	if err != nil {
		return err
	}
	if r := p.Commit(); !r.Committed {
		return r.Error
	}
	return nil
}
