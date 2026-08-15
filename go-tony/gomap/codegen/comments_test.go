package codegen

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/comments"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// comment= and lineComment= are read by both paths and mean the same thing on
// both: the named fields carry what was said about the value rather than data in
// it. Decoding filled them; encoding did neither half -- it wrote no comments,
// and it wrote the carrier fields out as ordinary keys, so a struct that had read
// "# about the spec" encoded to a document with a Comments: ["# about the spec"]
// member in it (3cdjz00jh12krns4g1n0).
//
// codegen also read the annotation as "LineComment" where the reflection mapper
// read "lineComment", so which spelling worked depended on whether the type had
// generated codecs.

// marker carries a struct-level tony tag. Reflection reads the tag from an
// EMBEDDED field; the blank-field form is codegen's, and gomap does not read it.
type marker struct{}

func specDoc(t *testing.T) *comments.Doc {
	t.Helper()
	return &comments.Doc{
		Name: "svc",
		Spec: &comments.Spec{
			Replicas:     3,
			Comments:     []string{"# about the spec"},
			LineComments: []string{" # after the spec"},
		},
	}
}

const wantEncoded = `name: svc
spec: # after the spec
  # about the spec
  replicas: 3
`

// TestGeneratedEncodesComments: the generated ToTonyIR puts the comments back
// where the IR keeps them, and does not write the carriers as fields.
func TestGeneratedEncodesComments(t *testing.T) {
	node, err := specDoc(t).ToTonyIR()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := encode.Encode(node, &b, encode.EncodeComments(true)); err != nil {
		t.Fatal(err)
	}
	if b.String() != wantEncoded {
		t.Errorf("generated encode gave\n%s\nwant\n%s", b.String(), wantEncoded)
	}
	if strings.Contains(b.String(), "Comments") {
		t.Errorf("the carrier fields were written as data:\n%s", b.String())
	}
}

// TestGeneratedRoundTripsComments: out through the generated encoder and back
// through the generated decoder.
func TestGeneratedRoundTripsComments(t *testing.T) {
	node, err := specDoc(t).ToTonyIR()
	if err != nil {
		t.Fatal(err)
	}
	var back comments.Doc
	if err := back.FromTonyIR(node); err != nil {
		t.Fatal(err)
	}
	if back.Spec == nil {
		t.Fatal("the spec did not survive")
	}
	if got := back.Spec.Comments; len(got) != 1 || got[0] != "# about the spec" {
		t.Errorf("head comments came back as %q", got)
	}
	if got := back.Spec.LineComments; len(got) != 1 || got[0] != " # after the spec" {
		t.Errorf("line comments came back as %q", got)
	}
	if back.Spec.Replicas != 3 {
		t.Errorf("replicas came back as %d", back.Spec.Replicas)
	}
}

// TestGeneratedAndReflectionAgreeOnComments is why the two read the same
// annotations: a type tagged once behaves the same whether its codecs are
// generated or reflected.
func TestGeneratedAndReflectionAgreeOnComments(t *testing.T) {
	genNode, err := specDoc(t).ToTonyIR()
	if err != nil {
		t.Fatal(err)
	}
	// gomap.ToTonyIR calls the generated method when a type has one, so the
	// reflection path is reached with a type that has none: the same shape,
	// declared inline.
	type spec struct {
		marker       `tony:"schema=comments-spec,notag,comment=Comments,lineComment=LineComments"`
		Replicas     int `tony:"field=replicas"`
		Comments     []string
		LineComments []string
	}
	type doc struct {
		marker `tony:"schema=comments-doc,notag"`
		Name   string `tony:"field=name"`
		Spec   *spec  `tony:"field=spec"`
	}
	reflNode, err := gomap.ToTonyIR(&doc{Name: "svc", Spec: &spec{
		Replicas:     3,
		Comments:     []string{"# about the spec"},
		LineComments: []string{" # after the spec"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !genNode.DeepEqualWithComments(reflNode) {
		t.Errorf("the two paths encoded differently:\n generated: %s\n reflected: %s",
			show(t, genNode), show(t, reflNode))
	}
}

// TestReflectionRoundTripsComments: the same document, through text, on the
// reflection path.
func TestReflectionRoundTripsComments(t *testing.T) {
	type spec struct {
		marker       `tony:"schema=rt-spec,notag,comment=Comments,lineComment=LineComments"`
		Replicas     int `tony:"field=replicas"`
		Comments     []string
		LineComments []string
	}
	type doc struct {
		marker `tony:"schema=rt-doc,notag"`
		Name   string `tony:"field=name"`
		Spec   *spec  `tony:"field=spec"`
	}
	out, err := gomap.ToTony(&doc{Name: "svc", Spec: &spec{
		Replicas:     3,
		Comments:     []string{"# about the spec"},
		LineComments: []string{" # after the spec"},
	}}, gomap.EncodeComments(true))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != wantEncoded {
		t.Errorf("reflection encode gave\n%s\nwant\n%s", out, wantEncoded)
	}
	node, err := parse.Parse(out, parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	var back doc
	if err := gomap.FromTonyIR(node, &back); err != nil {
		t.Fatal(err)
	}
	if back.Spec == nil {
		t.Fatal("the spec did not survive")
	}
	if got := back.Spec.Comments; len(got) != 1 || got[0] != "# about the spec" {
		t.Errorf("head comments came back as %q", got)
	}
	if got := back.Spec.LineComments; len(got) != 1 || got[0] != " # after the spec" {
		t.Errorf("line comments came back as %q", got)
	}
}

func show(t *testing.T, n *ir.Node) string {
	t.Helper()
	var b strings.Builder
	if err := encode.Encode(n, &b, encode.EncodeComments(true)); err != nil {
		return "<encode error>"
	}
	return strings.ReplaceAll(strings.TrimRight(b.String(), "\n"), "\n", " | ")
}
