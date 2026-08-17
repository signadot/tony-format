package storage

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A field name holding a dot is quoted in a path, and the quotes are what say the
// dot belongs to the NAME.  logd rendered child paths by concatenation, so a write
// at `demo.probe."post.dotted"` became a path of five segments: it was grafted under
// a field `post` holding a field `dotted`, the write reported a commit, and the read
// at the path the client asked for found nothing.
//
// It showed only once a store had a snapshot, because the rendering happens where
// patches are folded onto a BASE.  So this test crosses one, and uses ids not
// written before it: a value already standing at the unsplit path masks the whole
// thing, the read answering with that older value (r05ms7nch12ksxttgdn0).
func TestDottedFieldNameAcrossASnapshot(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer s.Close()

	// ids which stress the segment renderer: a dot is what quoting is for, a slash
	// needs none, a space and a brace are the neighbours of the same rule
	ids := []string{"plain", "has.dot", "has/slash", "has space", "has{brace}", "a.b.c", "has(paren)", "*"}

	check := func(when string, ids []string) {
		t.Helper()
		commit, err := s.GetCurrentCommit()
		if err != nil {
			t.Fatalf("%s: current commit: %s", when, err)
		}
		root, err := s.ReadStateAt("", commit, nil)
		if err != nil {
			t.Fatalf("%s: read: %s", when, err)
		}
		for _, id := range ids {
			path := kpath.ChildField("demo.probe", id)
			node, err := root.GetKPath(path)
			if err != nil {
				t.Errorf("%s: read %s: %s", when, path, err)
				continue
			}
			if node == nil {
				t.Errorf("%s: %s was written and committed, and is not there", when, path)
				continue
			}
			got, _ := node.GetKPath("k")
			if got == nil || got.String != id {
				t.Errorf("%s: %s holds %v, want k=%q", when, path, got, id)
			}
		}
	}

	writeAll := func(prefix string) []string {
		t.Helper()
		var written []string
		for _, id := range ids {
			id = prefix + id
			path := kpath.ChildField("demo.probe", id)
			body, err := parse.Parse([]byte("{k: " + kpath.Field(id).String() + "}"))
			if err != nil {
				t.Fatalf("parse body for %q: %s", id, err)
			}
			tx, err := s.NewTx(1, nil)
			if err != nil {
				t.Fatalf("newtx: %s", err)
			}
			p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: path, Data: body}})
			if err != nil {
				t.Fatalf("patcher %s: %s", path, err)
			}
			if r := p.Commit(); !r.Committed {
				t.Fatalf("commit %s: %v", path, r.Error)
			}
			written = append(written, id)
		}
		return written
	}

	before := writeAll("before-")
	check("before the snapshot", before)

	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("snapshot: %s", err)
	}
	check("across the snapshot", before)

	// the write which the base has to be grafted onto -- the one that was lost
	after := writeAll("after-")
	check("after the snapshot", append(before, after...))

	// and again, with the grafted writes now inside the base
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("second snapshot: %s", err)
	}
	check("across the second", append(before, after...))

	// the ids are the fields of one object, not a tree the dots dug
	commit, _ := s.GetCurrentCommit()
	root, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Fatalf("read: %s", err)
	}
	at, err := root.GetKPath("demo.probe")
	if err != nil || at == nil {
		t.Fatalf("read demo.probe: %v %s", at, err)
	}
	if len(at.Fields) != len(before)+len(after) {
		var names []string
		for i := range at.Fields {
			names = append(names, at.Fields[i].String)
		}
		t.Errorf("demo.probe has %d fields, want %d: %v", len(at.Fields), len(before)+len(after), names)
	}
}
