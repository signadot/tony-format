package snap

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/stream"
)

// commentDoc is a document whose every position carries a comment: above the
// document, above a field, after a value, inside a nested object and around an
// array element.
const commentDoc = `# above the document
name: svc # after the name
# above the spec
spec:
  # above replicas
  replicas: 3 # after replicas
  items:
  # above the first item
  - id: a # after a
  - id: b
# above ports
ports:
- 80
- 443
`

// snapOf writes doc through the builder and opens the result.
func snapOf(t *testing.T, src string) (*Snapshot, *Index) {
	t.Helper()
	n, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	evs, err := stream.NodeToEvents(n)
	if err != nil {
		t.Fatal(err)
	}
	p, w := newBytesWriteSeeker()
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(p)) })
	index := &Index{Entries: []IndexEntry{}}
	b, err := NewBuilder(w, index, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range evs {
		if err := b.WriteEvent(&evs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	s, err := Open(r)
	if err != nil {
		t.Fatal(err)
	}
	return s, index
}

// eventOffsets decodes the whole event stream, answering each event with the
// offset it begins at.
func eventOffsets(t *testing.T, s *Snapshot) ([]stream.Event, []int64) {
	t.Helper()
	raw := make([]byte, s.EventSize)
	if _, err := s.R.Seek(HeaderSize, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(s.R, raw); err != nil {
		t.Fatal(err)
	}
	rd := bytes.NewReader(raw)
	var evs []stream.Event
	var offs []int64
	for {
		off := int64(len(raw)) - int64(rd.Len())
		ev := &stream.Event{}
		if err := ev.ReadBinary(rd); err != nil {
			break
		}
		evs = append(evs, *ev)
		offs = append(offs, off)
	}
	return evs, offs
}

func show(t *testing.T, n *ir.Node) string {
	t.Helper()
	if n == nil {
		return "<nil>\n"
	}
	var b strings.Builder
	if err := encode.Encode(n, &b, encode.EncodeComments(true)); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
