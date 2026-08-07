package parse

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/token"
)

// chunkReader hands out a bounded number of bytes per Read, the way a socket
// does. The bug this file guards needs the source to refill mid-node, which a
// bytes.Reader handed a small document never makes it do.
type chunkReader struct {
	data []byte
	n    int
	pos  int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if c.pos >= len(c.data) {
		return 0, io.EOF
	}
	end := min(c.pos+c.n, len(c.data))
	k := copy(p, c.data[c.pos:end])
	c.pos += k
	return k, nil
}

// A document large enough that reading it compacts the token source's buffer
// must stream to exactly what parsing it in memory produces.
//
// TokenSource hands out Tokens whose Bytes alias its buffer, and a caller holds
// them across many Reads -- ParseNodeFromSource accumulates a whole node's worth
// before parsing any of it. Compaction used to shift the remaining bytes to the
// front of that same array, rewriting the bytes under every token already handed
// out with data from later in the document. Documents under two buffers never
// compact and were always fine, which is why this needs a big one.
//
// The failure was mostly silent: where the shifted bytes happened to lex, the
// document simply parsed to something other than what was sent. The "bad
// literal" errors logd sees under write-heavy load are the same corruption in
// the cases where they happened not to.
func TestLargeDocumentSurvivesBufferCompaction(t *testing.T) {
	for _, keys := range []int{50, 200, 1000, 5000, 20000} {
		t.Run(fmt.Sprintf("keys=%d", keys), func(t *testing.T) {
			doc := oneLineDoc(keys)

			want, err := Parse([]byte(doc))
			if err != nil {
				t.Fatalf("in-memory parse of %d bytes failed: %v", len(doc), err)
			}

			src := token.NewTokenSource(&chunkReader{data: []byte(doc), n: 4096})
			got, err := ParseNodeFromSource(src)
			if err != nil {
				t.Fatalf("streaming parse of %d bytes failed: %v", len(doc), err)
			}

			if !got.DeepEqual(want) {
				t.Errorf("streaming parse of %d bytes disagrees with the in-memory parse of the same bytes",
					len(doc))
			}
		})
	}
}

// oneLineDoc builds a bracketed single-line document, the shape docd serves and
// the shape whose tokens outlive a refill.
func oneLineDoc(keys int) string {
	var b strings.Builder
	b.WriteString("{")
	for i := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "k%d: v%dxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", i, i)
	}
	b.WriteString("}")
	return b.String()
}
