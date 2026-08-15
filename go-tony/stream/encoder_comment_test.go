package stream

import (
	"bytes"
	"io"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// TestEncoderWritesComments: the encoder's comment methods wrote nothing and
// answered nil, so a caller writing a document through this API got one back
// without its comments and no sign of where they went. The decoder reads
// comments, so the pair could not round trip its own output
// (3cdjz00jh12krns4g1n0).
func TestEncoderWritesComments(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, WithBrackets())
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteHeadComment([]string{"# about the document"}); err != nil {
		t.Fatal(err)
	}
	if err := enc.BeginObject(); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteKey("name"); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteString("svc"); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteLineComment([]string{" # after the name"}); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteHeadComment([]string{"# about count"}); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteKey("count"); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteInt(3); err != nil {
		t.Fatal(err)
	}
	if err := enc.EndObject(); err != nil {
		t.Fatal(err)
	}

	// what came out has to read back as what went in
	dec, err := NewDecoder(bytes.NewReader(buf.Bytes()), WithBrackets())
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
			t.Fatalf("decoding %q: %v", buf.String(), err)
		}
		evs = append(evs, *ev)
	}
	got, err := EventsToNode(evs)
	if err != nil {
		t.Fatalf("rebuilding %q: %v", buf.String(), err)
	}

	want := &ir.Node{Type: ir.CommentType, Lines: []string{"# about the document"}}
	inner := ir.FromKeyVals([]ir.KeyVal{
		{Key: ir.FromString("name"), Val: ir.FromString("svc")},
		{Key: ir.FromString("count"), Val: ir.Comment(ir.FromInt(3), "# about count")},
	})
	ir.Get(inner, "name").Comment = &ir.Node{Type: ir.CommentType, Lines: []string{" # after the name"}}
	want.Values = []*ir.Node{inner}

	if !got.DeepEqualWithComments(want) {
		t.Errorf("the encoder wrote %q, which reads back as a document differing from what was written", buf.String())
	}
}

// TestEncoderCommentSeparators: a comma belongs to the value it separates, not
// under a comment describing the next one.
func TestEncoderCommentSeparators(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, WithBrackets())
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.BeginArray(); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteInt(1); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteHeadComment([]string{"# about the second"}); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteInt(2); err != nil {
		t.Fatal(err)
	}
	if err := enc.EndArray(); err != nil {
		t.Fatal(err)
	}

	dec, err := NewDecoder(bytes.NewReader(buf.Bytes()), WithBrackets())
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
			t.Fatalf("decoding %q: %v", buf.String(), err)
		}
		evs = append(evs, *ev)
	}
	got, err := EventsToNode(evs)
	if err != nil {
		t.Fatalf("rebuilding %q: %v", buf.String(), err)
	}
	if len(got.Values) != 2 {
		t.Fatalf("the array came back with %d elements from %q", len(got.Values), buf.String())
	}
	second := got.Values[1]
	if second.Type != ir.CommentType || len(second.Lines) != 1 {
		t.Fatalf("the second element lost its comment: %q", buf.String())
	}
}
