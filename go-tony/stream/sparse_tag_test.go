package stream_test

import (
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/stream"
)

// An object built from int-key events is a sparse array, and carries the tag that
// says so, as one the parser builds does. Without it a client's {1: b} arriving
// over the wire merged onto a stored !sparsearray {0: a} as an object with a field
// "1", and 0: a was gone (4ynqp7wqh12krg32msn0 item 6).
func TestIntKeyedObjectFromEventsIsASparseArray(t *testing.T) {
	dec, err := stream.NewDecoder(strings.NewReader("{1: b}"), stream.WithBrackets())
	if err != nil {
		t.Fatal(err)
	}
	patch, err := stream.ReadDocument(dec)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !ir.TagHas(patch.Tag, ir.IntKeysTag) {
		t.Fatalf("tag %q, want %s", patch.Tag, ir.IntKeysTag)
	}

	stored, err := parse.Parse([]byte("!sparsearray {0: a}"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := tony.Patch(stored, patch)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	var b strings.Builder
	if err := encode.Encode(out, &b); err != nil {
		t.Fatal(err)
	}
	want, _ := parse.Parse([]byte("!sparsearray {0: a, 1: b}"))
	var w strings.Builder
	_ = encode.Encode(want, &w)
	if b.String() != w.String() {
		t.Errorf("patched:\n%s\nwant:\n%s", b.String(), w.String())
	}

	// A tag the object already carries is kept beside it.
	dec, _ = stream.NewDecoder(strings.NewReader("!mine {1: b}"), stream.WithBrackets())
	tagged, err := stream.ReadDocument(dec)
	if err != nil {
		t.Fatalf("read tagged: %v", err)
	}
	if !ir.TagHas(tagged.Tag, ir.IntKeysTag) || !ir.TagHas(tagged.Tag, "!mine") {
		t.Errorf("tag %q, want both !mine and %s", tagged.Tag, ir.IntKeysTag)
	}
}
