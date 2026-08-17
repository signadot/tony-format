package stream

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

// chunkedReader hands out at most n bytes per Read, the way a connection does.
type chunkedReader struct {
	d []byte
	n int
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if len(c.d) == 0 {
		return 0, io.EOF
	}
	n := min(min(c.n, len(c.d)), len(p))
	copy(p, c.d[:n])
	c.d = c.d[n:]
	return n, nil
}

// A value holding a bracket, read over a connection, must survive the read
// boundary landing between the bracket and its closer: the scanner reported the
// unfinished scan as a bad literal, which killed the session
// (75g1kbpdh12krs09gdn0).  The reader's buffer is 4096, so the bracket is walked
// across that, and every position has to read back what was sent.
func TestBracketAcrossAReadBoundary(t *testing.T) {
	for at := 4070; at <= 4100; at++ {
		for _, chunk := range []int{1 << 20, 4096, 1000, 7} {
			val := strings.Repeat("a", at) + "{x}" + strings.Repeat("b", 200)
			want := ir.FromMap(map[string]*ir.Node{"k": ir.FromString(val)})

			var w bytes.Buffer
			if err := encode.Encode(want, &w, encode.EncodeWire(true)); err != nil {
				t.Fatalf("encode: %s", err)
			}
			dec, err := NewDecoder(&chunkedReader{d: w.Bytes(), n: chunk}, WithBrackets())
			if err != nil {
				t.Fatalf("decoder: %s", err)
			}
			got, err := ReadDocument(dec)
			if err != nil && err != io.EOF {
				t.Fatalf("bracket at %d, %d-byte reads: %s", at, chunk, err)
			}
			if got == nil || !got.DeepEqual(want) {
				t.Fatalf("bracket at %d, %d-byte reads: not what was sent", at, chunk)
			}
		}
	}
}
