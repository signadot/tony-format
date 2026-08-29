package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A write at an array index carries its meaning in the op at the leaf, and logd
// used to take that meaning away: TagPatchRoots composes !logd-patch-root onto the
// client's data root, MergePatches then wraps a votes[i] path in an !arraydiff, so
// the marker landed on the very node the arraydiff dispatches on. !insert became a
// positional patch and OVERWROTE the element it was meant to insert before,
// !delete became a patch of a null and panicked every reader, and an append became
// a patch of an element past the end, which no read could get past afterwards
// (jjbapb1ah12kranxg5n0).
//
// mergeop now finds the op the way SplitChild does -- the first label the registry
// knows, not simply the first label -- and these are the writes that were wrong.
func TestArrayElementWriteKeepsItsOp(t *testing.T) {
	const seed = `{votes: [{by: scott}, {by: dee}]}`
	for _, tc := range []struct {
		name, path, body, want string
	}{
		{
			name: "insert before the first element",
			path: `votes[0]`, body: `!insert {by: ana}`,
			want: `votes: [ { by: ana } { by: scott } { by: dee } ]`,
		},
		{
			name: "insert in the middle",
			path: `votes[1]`, body: `!insert {by: ana}`,
			want: `votes: [ { by: scott } { by: ana } { by: dee } ]`,
		},
		{
			name: "insert at the end, which is an append",
			path: `votes[2]`, body: `!insert {by: ana}`,
			want: `votes: [ { by: scott } { by: dee } { by: ana } ]`,
		},
		{
			name: "delete an element",
			path: `votes[0]`, body: `!delete {by: scott}`,
			want: `votes: [ { by: dee } ]`,
		},
		{
			name: "a plain body still patches the element in place",
			path: `votes[1]`, body: `{choice: approve}`,
			want: `votes: [ { by: scott } { by: dee choice: approve } ]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(t.TempDir(), nil)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer s.Close()

			if _, err := arrayWriteCommit(t, s, "", seed); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if _, err := arrayWriteCommit(t, s, tc.path, tc.body); err != nil {
				t.Fatalf("write %s %s: %v", tc.path, tc.body, err)
			}

			// The read is the point: a write that mangles the op does not fail at
			// the write, it fails -- or silently loses data -- at every read after.
			commit, err := s.GetCurrentCommit()
			if err != nil {
				t.Fatalf("GetCurrentCommit: %v", err)
			}
			doc, err := s.ReadStateAt("", commit, nil)
			if err != nil {
				t.Fatalf("read after %s %s: %v", tc.path, tc.body, err)
			}
			if got := flatten(t, doc); got != tc.want {
				t.Errorf("%s %s\n got %s\nwant %s", tc.path, tc.body, got, tc.want)
			}
		})
	}
}

// The marker is logd's own and must not be stored: an inserted element carries
// what the client wrote and nothing of how logd routed it.
func TestArrayElementInsertStoresNoMarker(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if _, err := arrayWriteCommit(t, s, "", `{votes: [{by: scott}]}`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := arrayWriteCommit(t, s, `votes[1]`, `!insert {by: ana}`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	doc, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := flatten(t, doc); strings.Contains(got, "logd-patch-root") {
		t.Errorf("the stored document carries logd's marker: %s", got)
	}
}

func arrayWriteCommit(t *testing.T, s *Storage, path, body string) (int64, error) {
	t.Helper()
	tx, err := s.NewTx(1, nil)
	if err != nil {
		return 0, err
	}
	data, err := parse.Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse %q: %v", body, err)
	}
	p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: path, Data: data}})
	if err != nil {
		return 0, err
	}
	r := p.Commit()
	if !r.Committed {
		return 0, r.Error
	}
	return r.Commit, nil
}

// flatten renders a document as one line, so a case reads as the array it is about.
func flatten(t *testing.T, doc *ir.Node) string {
	t.Helper()
	var b strings.Builder
	if err := encode.Encode(doc, &b); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// An index which names no element is refused AT THE WRITE, and the log stays
// readable. Before this, each of these committed and then no read of the store
// succeeded again -- not of the entity, not of entities the write never touched,
// and not after a client deleted the entity, because the read dies on the way past
// the bad patch (7cdvym1fh12ksmd5g5n0).
func TestArrayElementWriteMustNameAnElement(t *testing.T) {
	for _, tc := range []struct {
		name, seed, path, body string
	}{
		{
			name: "a patch one past the end",
			seed: `{votes: [{by: scott}, {by: dee}]}`,
			path: `votes[2]`, body: `{by: ana}`,
		},
		{
			name: "a patch well past the end",
			seed: `{votes: [{by: scott}, {by: dee}]}`,
			path: `votes[7]`, body: `{by: ana}`,
		},
		{
			name: "a patch at 0 of an empty array",
			seed: `{votes: []}`,
			path: `votes[0]`, body: `{by: ana}`,
		},
		{
			name: "an insert past the end, which no position can be",
			seed: `{votes: [{by: scott}, {by: dee}]}`,
			path: `votes[5]`, body: `!insert {by: ana}`,
		},
		{
			name: "a write through an index which is not there",
			seed: `{votes: [{by: scott}]}`,
			path: `votes[3].choice`, body: `approve`,
		},
		{
			name: "an index into an array which does not exist",
			seed: `{other: 1}`,
			path: `votes[0]`, body: `{by: ana}`,
		},
		{
			name: "an index into something which is not an array",
			seed: `{votes: {a: 1}}`,
			path: `votes[0]`, body: `{by: ana}`,
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
			want := readWholeStore(t, s, before)

			if commit, err := arrayWriteCommit(t, s, tc.path, tc.body); err == nil {
				t.Fatalf("%s %s committed at %d; it names no element", tc.path, tc.body, commit)
			} else {
				t.Logf("refused: %v", err)
			}

			// Nothing was written, and everything still reads.
			after, err := s.GetCurrentCommit()
			if err != nil {
				t.Fatalf("GetCurrentCommit: %v", err)
			}
			if after != before {
				t.Errorf("a refused write took commit %d", after)
			}
			if got := readWholeStore(t, s, after); got != want {
				t.Errorf("the store changed under a refused write:\n got %s\nwant %s", got, want)
			}
		})
	}
}

// The array can lose the element between the write's submission and its commit, so
// the commit asks again. This is the case a submit-time check alone cannot answer.
func TestArrayElementWriteRecheckedAtCommit(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if _, err := arrayWriteCommit(t, s, "", `{votes: [{by: scott}, {by: dee}]}`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Submitted while votes[1] is there.
	tx, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	data, err := parse.Parse([]byte(`{choice: approve}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: `votes[1]`, Data: data}})
	if err != nil {
		t.Fatalf("the write was submitted while votes[1] existed: %v", err)
	}

	// Another writer shortens the array before this one commits.
	if _, err := arrayWriteCommit(t, s, "votes", `[{by: scott}]`); err != nil {
		t.Fatalf("shorten: %v", err)
	}

	if r := p.Commit(); r.Committed {
		t.Fatalf("committed at %d; votes[1] was gone by then", r.Commit)
	} else {
		t.Logf("refused at commit: %v", r.Error)
	}

	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	if got, want := readWholeStore(t, s, commit), "votes: [ { by: scott } ]"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func readWholeStore(t *testing.T, s *Storage, commit int64) string {
	t.Helper()
	doc, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Fatalf("read at %d: %v", commit, err)
	}
	return flatten(t, doc)
}

// The array a scoped write names is the SCOPE's array. A scope that has appended
// to a baseline array can write the element it added, and a baseline writer cannot
// see it -- so the check has to read the same view the write will be applied to.
func TestArrayElementWriteSeesItsOwnScope(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if _, err := arrayWriteCommit(t, s, "", `{votes: [{by: scott}]}`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	scope := "s1"
	scopedWrite := func(path, body string) error {
		t.Helper()
		tx, err := s.NewTx(1, &scope)
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

	// The scope appends a second vote, which baseline does not have.
	if err := scopedWrite(`votes[1]`, `!insert {by: dee}`); err != nil {
		t.Fatalf("scoped append: %v", err)
	}
	// So votes[1] is a real element IN THE SCOPE ...
	if err := scopedWrite(`votes[1]`, `{choice: approve}`); err != nil {
		t.Errorf("the scope's own element was refused: %v", err)
	}
	// ... and is still not one in baseline.
	if _, err := arrayWriteCommit(t, s, `votes[1]`, `{choice: approve}`); err == nil {
		t.Error("a baseline write reached an element only the scope has")
	}

	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	doc, err := s.ReadStateAt("", commit, &scope)
	if err != nil {
		t.Fatalf("scoped read: %v", err)
	}
	if got, want := flatten(t, doc), "votes: [ { by: scott } { by: dee choice: approve } ]"; got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}
