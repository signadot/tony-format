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
