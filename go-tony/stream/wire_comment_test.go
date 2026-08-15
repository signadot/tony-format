package stream

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// TestWireCarriesComments is the property the session depends on: what a client
// encodes is what the server decodes. Requests and responses go out through
// encode and come back through the stream decoder, so a comment survives a hop
// only if both halves agree about it.
//
// The decoder used to drop comment tokens outright, which is why nothing a
// client wrote ever reached a store -- before any question of what the store
// would do with it (3cdjz00jh12krns4g1n0).
func TestWireCarriesComments(t *testing.T) {
	for _, src := range []string{
		"# above the document\nname: svc\n",
		"name: svc # after the name\n",
		"# above\nname: svc # after\n",
		"# one\n# two\nname: svc\n",
		"spec:\n  # above replicas\n  replicas: 3 # after replicas\n",
		"items:\n# above the first\n- a # after a\n- b\n",
		"a:\n  b:\n    # deep\n    c: 1 # deeper\n",
	} {
		t.Run(strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			doc, err := parse.Parse([]byte(src), parse.ParseComments(true))
			if err != nil {
				t.Fatal(err)
			}
			// out through encode in the wire form the session speaks
			var wire bytes.Buffer
			if err := encode.Encode(doc, &wire,
				encode.EncodeWire(true), encode.EncodeComments(true)); err != nil {
				t.Fatal(err)
			}
			dec, err := NewDecoder(bytes.NewReader(wire.Bytes()), WithBrackets())
			if err != nil {
				t.Fatal(err)
			}
			var evs []Event
			for {
				ev, err := dec.ReadEvent()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("decoding %q: %v", wire.String(), err)
				}
				evs = append(evs, *ev)
			}
			back, err := EventsToNode(evs)
			if err != nil {
				t.Fatalf("rebuilding %q: %v", wire.String(), err)
			}
			if !back.DeepEqualWithComments(doc) {
				var got, want strings.Builder
				encode.Encode(back, &got, encode.EncodeComments(true))
				encode.Encode(doc, &want, encode.EncodeComments(true))
				t.Errorf("the wire form %q came back as\n%s\nand went in as\n%s",
					wire.String(), got.String(), want.String())
			}
		})
	}
}
