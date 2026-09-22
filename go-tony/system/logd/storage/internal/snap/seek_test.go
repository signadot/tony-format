package snap

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/stream"
)

// pathsOf is every path in n, the root included.
func pathsOf(n *ir.Node, at string, out *[]string) {
	*out = append(*out, at)
	n = ir.Uncomment(n)
	switch n.Type {
	case ir.ObjectType:
		for i, f := range n.Fields {
			pathsOf(n.Values[i], kpath.ChildField(at, f.String), out)
		}
	case ir.ArrayType:
		for i, v := range n.Values {
			pathsOf(v, at+kpath.Index(i).String(), out)
		}
	}
}

func eventsOf(t *testing.T, r EventReader) []stream.Event {
	t.Helper()
	var evs []stream.Event
	for {
		ev, err := r.ReadEvent()
		if errors.Is(err, io.EOF) {
			return evs
		}
		if err != nil {
			t.Fatal(err)
		}
		evs = append(evs, *ev)
	}
}

// TestTheDirectoryReadsWhatTheChunkIndexDoes: a path read through the directory
// answers the events the chunk index's seek and scan answered -- the value, its own
// head comments and its own line comment -- for every path the document has, and
// nothing for one it does not have.
func TestTheDirectoryReadsWhatTheChunkIndexDoes(t *testing.T) {
	for _, size := range []string{"1", "64", "4096"} {
		t.Run("chunk size "+size, func(t *testing.T) {
			t.Setenv("SNAP_MAX_CHUNK_SIZE", size)
			s, _ := snapOf(t, commentDoc)
			if s.dir == nil {
				t.Fatal("the builder wrote no directory")
			}
			scan := *s
			scan.dir, scan.Index = nil, nil
			if _, err := scan.ChunkIndex(); err != nil {
				t.Fatal(err)
			}

			doc, err := parse.Parse([]byte(commentDoc), parse.ParseComments(true))
			if err != nil {
				t.Fatal(err)
			}
			var paths []string
			pathsOf(doc, "", &paths)
			paths = append(paths, "zz", "aa", "spec.nope", "name.under", "ports[7]", "nope.deeper")
			for _, p := range paths {
				dr, err := s.ReadPathEventReader(p)
				if err != nil {
					t.Fatalf("directory read %q: %v", p, err)
				}
				got := eventsOf(t, dr)
				sr, err := scan.ReadPathEventReader(p)
				if err != nil {
					t.Fatalf("chunk index read %q: %v", p, err)
				}
				want := eventsOf(t, sr)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("read %q:\n directory   %v\n chunk index %v", p, got, want)
				}

				gotNode, err := s.ReadPath(p)
				if err != nil {
					t.Fatal(err)
				}
				wantNode, err := scan.ReadPath(p)
				if err != nil {
					t.Fatal(err)
				}
				if (gotNode == nil) != (wantNode == nil) || gotNode != nil && !gotNode.DeepEqual(wantNode) {
					t.Errorf("ReadPath %q: directory %v, chunk index %v", p, gotNode, wantNode)
				}
			}
		})
	}
}

// countingReader counts the bytes read through it.
type countingReader struct {
	R
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.R.Read(p)
	c.n += int64(n)
	return n, err
}

// TestAnAbsentPathReadsOnlyTheTablesOnTheWay is the defect: a path the snapshot does
// not hold was a read of every event from the chunk before it to the end of the
// snapshot, 94 ms on a 5 MB one, and a write creating an entity paid it at every
// leaf (9a4d9jm2h12krj9hp9n0). Through the directory it is a binary search per
// segment, and ends at the segment that is missing.
func TestAnAbsentPathReadsOnlyTheTablesOnTheWay(t *testing.T) {
	var b strings.Builder
	for i := range 500 {
		fmt.Fprintf(&b, "e%04d:\n  body: %s\n  name: entity-%d\n", i, strings.Repeat("x", 200), i)
	}
	s, _ := snapOf(t, b.String())
	c := &countingReader{R: s.R.(R)}
	s.R, s.dir.r = c, c

	for _, p := range []string{"e0000a", "e0250.missing", "e9999", "nope.deeper.still", "e0100.name.under"} {
		c.n = 0
		r, err := s.ReadPathEventReader(p)
		if err != nil {
			t.Fatal(err)
		}
		if evs := eventsOf(t, r); len(evs) != 0 {
			t.Errorf("read %q as %v; it is not there", p, evs)
		}
		if c.n > int64(s.EventSize)/20 {
			t.Errorf("reading the absent %q read %d bytes of a snapshot of %d", p, c.n, s.EventSize)
		}
	}
}

// TestTheBuilderRefusesAChildOutOfNameOrder: a table is binary-searched, so a
// container whose children arrive out of name order would answer a path it holds as
// absent. logd writes object keys in name order; anything else is refused where it
// is written, not misread later.
func TestTheBuilderRefusesAChildOutOfNameOrder(t *testing.T) {
	n, err := parse.Parse([]byte("b: 1\na: 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	evs, err := stream.NodeToEvents(n)
	if err != nil {
		t.Fatal(err)
	}
	_, w := newBytesWriteSeeker()
	bld, err := NewBuilder(w, &Index{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range evs {
		if err = bld.WriteEvent(&evs[i]); err != nil {
			break
		}
	}
	if err == nil || !strings.Contains(err.Error(), "out of name order") {
		t.Fatalf("a child out of name order was written: %v", err)
	}
}
