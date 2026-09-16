package snap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/stream"
)

// A snapshot is a directory: every container it holds has a table of its children, in
// the order the events have them, and each entry says where the child's events are and
// what kind of value it is (3kgxprskh12krjrmndn0).
// Keys are in the order the store keeps them, sorted, which the tables' search rests on.
const directoryDoc = `# above the document
empty: {}
name: svc # after the name
none: []
ports:
- 80
- 443
runs:
  "(id=r1)": {id: r1, n: 1}
  "(id=r2)": {id: r2, n: 2}
sparse: !sparsearray {3: three, 7: {deep: 1}}
spec:
  flags: {nil: null, off: false, on: true}
  items:
  # above the first item
  - id: a # after a
  - id: b
    tags: [x, y]
  # above replicas
  replicas: 3 # after replicas
`

// children answers a node's direct children as the table should list them: the
// segment as the store spells it, and the kind.
func children(t *testing.T, n *ir.Node) (segs []string, kinds []Kind) {
	t.Helper()
	n = ir.Uncomment(n)
	switch n.Type {
	case ir.ObjectType:
		sparse := len(n.Fields) > 0 && n.Fields[0].Type == ir.NumberType
		for i, f := range n.Fields {
			if sparse {
				segs = append(segs, kpath.SparseIndex(int(*f.Int64)).String())
			} else {
				segs = append(segs, kpath.Field(f.String).String())
			}
			kinds = append(kinds, kindOf(t, n.Values[i]))
		}
	case ir.ArrayType:
		for i, v := range n.Values {
			segs = append(segs, kpath.Index(i).String())
			kinds = append(kinds, kindOf(t, v))
		}
	}
	return segs, kinds
}

func kindOf(t *testing.T, n *ir.Node) Kind {
	t.Helper()
	n = ir.Uncomment(n)
	switch n.Type {
	case ir.ObjectType:
		if len(n.Fields) > 0 && n.Fields[0].Type == ir.NumberType {
			return KindSparseArray
		}
		return KindObject
	case ir.ArrayType:
		return KindArray
	case ir.StringType:
		return KindString
	case ir.NumberType:
		return KindNumber
	case ir.BoolType:
		return KindBool
	}
	return KindNull
}

// readAll drains a table from its current position.
func readAll(t *testing.T, tb *Table) []Entry {
	t.Helper()
	var out []Entry
	for {
		e, ok, err := tb.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			return out
		}
		out = append(out, e)
	}
}

// walkTables checks the table at every container against the document, and that each
// entry's events, read on their own, are the child's value with its own comments.
func walkTables(t *testing.T, s *Snapshot, doc *ir.Node, at string, tb *Table) {
	t.Helper()
	node, err := doc.GetKPathWith(at, ir.WithComments(true))
	if err != nil {
		t.Fatalf("%q: %v", at, err)
	}
	wantSegs, wantKinds := children(t, node)
	got := readAll(t, tb)
	if len(got) != len(wantSegs) {
		t.Fatalf("%q: table has %d entries, want %d: %+v", at, len(got), len(wantSegs), got)
	}
	if tb.Len() != len(wantSegs) {
		t.Errorf("%q: Len() = %d, want %d", at, tb.Len(), len(wantSegs))
	}
	for i, e := range got {
		if e.Segment != wantSegs[i] || e.Kind != wantKinds[i] {
			t.Errorf("%q entry %d: %s %v, want %s %v", at, i, e.Segment, e.Kind, wantSegs[i], wantKinds[i])
		}
		// The child's events, read by the entry alone.
		childPath := kpath.Join(at, e.Segment)
		want, err := doc.GetKPathWith(childPath, ir.WithComments(true))
		if err != nil {
			t.Fatalf("%q: %v", childPath, err)
		}
		gotNode, err := s.ReadEntry(at, e)
		if err != nil {
			t.Fatalf("%q: ReadEntry: %v", childPath, err)
		}
		if show(t, gotNode) != show(t, want) {
			t.Errorf("%q by its entry:\n%s\nwant:\n%s", childPath, show(t, gotNode), show(t, want))
		}
		if e.Kind.IsContainer() {
			child, err := tb.Child(e)
			if err != nil {
				t.Fatalf("%q: Child: %v", childPath, err)
			}
			walkTables(t, s, doc, childPath, child)
		}
	}
}

func TestDirectory_ListsEveryLevel(t *testing.T) {
	s, _ := snapOf(t, directoryDoc)
	doc, err := parse.Parse([]byte(directoryDoc), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.Table("")
	if err != nil {
		t.Fatalf("root table: %v", err)
	}
	walkTables(t, s, doc, "", root)

	// A table found by path is the same table.
	tb, err := s.Table("spec.items")
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, tb); len(got) != 2 || got[0].Segment != "[0]" || got[1].Segment != "[1]" {
		t.Errorf("spec.items: %+v", got)
	}
	// A leaf has no table, and a path that is not there has none.
	if _, err := s.Table("name"); !errors.Is(err, ErrNotAContainer) {
		t.Errorf("Table(name) = %v, want ErrNotAContainer", err)
	}
	if _, err := s.Table("spec.nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Table(spec.nope) = %v, want ErrNotFound", err)
	}
}

// A table is read from a name: Seek positions after it, by binary search over the
// slots, and Next carries on from there. Seeking past the end reads nothing.
func TestDirectory_SeeksByName(t *testing.T) {
	s, _ := snapOf(t, directoryDoc)
	tb, err := s.Table("")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		after string
		want  []string
	}{
		{"", []string{"empty", "name", "none", "ports", "runs", "sparse", "spec"}},
		{"name", []string{"none", "ports", "runs", "sparse", "spec"}},
		{"nam", []string{"name", "none", "ports", "runs", "sparse", "spec"}}, // between two names
		{"spec", nil},
		{"zzz", nil},
	} {
		if err := tb.Seek(tc.after); err != nil {
			t.Fatalf("Seek(%q): %v", tc.after, err)
		}
		var got []string
		for _, e := range readAll(t, tb) {
			got = append(got, e.Segment)
		}
		if len(got) != len(tc.want) {
			t.Errorf("after %q: %v, want %v", tc.after, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("after %q: %v, want %v", tc.after, got, tc.want)
				break
			}
		}
	}
	// Positions, sparse keys and names are sought the same way.
	sp, err := s.Table("sparse")
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Seek("{3}"); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, sp); len(got) != 1 || got[0].Segment != "{7}" {
		t.Errorf("sparse after {3}: %+v", got)
	}
	runs, err := s.Table("runs")
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.Seek(`"(id=r1)"`); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, runs); len(got) != 1 || got[0].Segment != `"(id=r2)"` {
		t.Errorf("runs after (id=r1): %+v", got)
	}
}

// A snapshot of a scalar has no directory to speak of; a document read through the
// path index answers exactly as before the directory existed.
func TestDirectory_ScalarRootAndReadsUnchanged(t *testing.T) {
	s, _ := snapOf(t, `42`)
	if _, err := s.Table(""); !errors.Is(err, ErrNotAContainer) {
		t.Errorf("Table of a scalar root = %v, want ErrNotAContainer", err)
	}
	n, err := s.ReadPath("")
	if err != nil || n == nil || n.Int64 == nil || *n.Int64 != 42 {
		t.Errorf("ReadPath of a scalar root: %v, %v", n, err)
	}

	s, _ = snapOf(t, directoryDoc)
	for _, p := range []string{"", "spec", "spec.items[1].tags", "sparse{7}", `runs."(id=r2)".n`, "ports[1]"} {
		doc, _ := parse.Parse([]byte(directoryDoc), parse.ParseComments(true))
		want, err := doc.GetKPathWith(p, ir.WithComments(true))
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.ReadPath(p)
		if err != nil {
			t.Fatalf("ReadPath(%q): %v", p, err)
		}
		if show(t, got) != show(t, want) {
			t.Errorf("ReadPath(%q):\n%s\nwant:\n%s", p, show(t, got), show(t, want))
		}
	}
}

// A snapshot written before there was a directory -- the 12-byte header, events, index
// -- opens and reads as it did, and says it has no directory.
func TestDirectory_LegacySnapshotOpens(t *testing.T) {
	n, err := parse.Parse([]byte(directoryDoc), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	evs, err := stream.NodeToEvents(n)
	if err != nil {
		t.Fatal(err)
	}
	var events bytes.Buffer
	for i := range evs {
		if err := evs[i].WriteBinary(&events); err != nil {
			t.Fatal(err)
		}
	}
	index := &Index{Entries: []IndexEntry{{Path: nil, Offset: 0}}}
	id, err := index.ToTony(gomap.EncodeWire(true))
	if err != nil {
		t.Fatal(err)
	}
	var file bytes.Buffer
	header := make([]byte, LegacyHeaderSize)
	binary.BigEndian.PutUint64(header[0:8], uint64(events.Len()))
	binary.BigEndian.PutUint32(header[8:12], uint32(len(id)))
	file.Write(header)
	file.Write(events.Bytes())
	file.Write(id)

	dir := t.TempDir()
	p := filepath.Join(dir, "legacy.snap")
	if err := os.WriteFile(p, file.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s, err := Open(r)
	if err != nil {
		t.Fatalf("Open a legacy snapshot: %v", err)
	}
	if _, err := s.Table(""); !errors.Is(err, ErrNoDirectory) {
		t.Errorf("Table on a legacy snapshot = %v, want ErrNoDirectory", err)
	}
	got, err := s.ReadPath("spec.replicas")
	if v := ir.Uncomment(got); err != nil || v == nil || v.Int64 == nil || *v.Int64 != 3 {
		t.Errorf("a legacy snapshot reads spec.replicas as %v (%v)", got, err)
	}
	if _, err := s.R.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
}
