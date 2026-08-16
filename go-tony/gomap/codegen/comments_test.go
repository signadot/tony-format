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

// A charter is the shape the FIELD-level annotation exists for. Two comments an
// author writes land on the list rather than on any element -- the one above
// `rules:`, and the one above the FIRST element, since a block array begins
// there -- so no struct is present to carry them and only the field that holds
// the list can name somewhere to put them.
//
// The annotation was declared on FieldInfo and assigned by nothing, so a field
// tag asking for it was accepted and did nothing: no error, no warning, and the
// named field written into the document as data (xvexrbthh12ksrahg5n0).
const charterDoc = `rules:
# about rule a
- name: a
# about rule b
- name: b
`

func TestGeneratedFieldComments(t *testing.T) {
	node, err := parse.Parse([]byte(charterDoc), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	var c comments.Charter
	if err := c.FromTonyIR(node); err != nil {
		t.Fatal(err)
	}
	if got := c.RulesComment; len(got) != 1 || got[0] != "# about rule a" {
		t.Errorf("the comment on the list came back as %q", got)
	}
	if len(c.Rules) != 2 {
		t.Fatalf("%d rules", len(c.Rules))
	}
	if got := c.Rules[1].Comments; len(got) != 1 || got[0] != "# about rule b" {
		t.Errorf("rule b's own comment came back as %q", got)
	}
	if got := c.Rules[0].Comments; len(got) != 0 {
		t.Errorf("rule a's comment is the LIST's, not the element's, but the element got %q", got)
	}

	// and back out, unchanged
	out, err := c.ToTonyIR()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := encode.Encode(out, &b, encode.EncodeComments(true)); err != nil {
		t.Fatal(err)
	}
	if b.String() != charterDoc {
		t.Errorf("the charter came back as\n%s\nand went in as\n%s", b.String(), charterDoc)
	}
	if strings.Contains(b.String(), "RulesComment") {
		t.Errorf("a carrier field was written as data:\n%s", b.String())
	}
}

// TestFieldCommentsAgree: generated and reflected, same type, same answer.
func TestFieldCommentsAgree(t *testing.T) {
	node, err := parse.Parse([]byte(charterDoc), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	var gen comments.Charter
	if err := gen.FromTonyIR(node); err != nil {
		t.Fatal(err)
	}
	genOut, err := gen.ToTonyIR()
	if err != nil {
		t.Fatal(err)
	}

	type rule struct {
		marker   `tony:"schema=comments-rule,notag,comment=Comments"`
		Name     string `tony:"field=name"`
		Comments []string
	}
	type charter struct {
		marker       `tony:"schema=comments-charter,notag"`
		Rules        []rule `tony:"field=rules,comment=RulesComment,lineComment=RulesLine"`
		RulesComment []string
		RulesLine    []string
	}
	var refl charter
	if err := gomap.FromTonyIR(node, &refl); err != nil {
		t.Fatal(err)
	}
	if got := refl.RulesComment; len(got) != 1 || got[0] != "# about rule a" {
		t.Errorf("reflection read the list comment as %q", got)
	}
	reflOut, err := gomap.ToTonyIR(&refl)
	if err != nil {
		t.Fatal(err)
	}
	if !genOut.DeepEqualWithComments(reflOut) {
		t.Errorf("the two paths differ:\n generated: %s\n reflected: %s", show(t, genOut), show(t, reflOut))
	}
}

// A carrier on a field that may not be written at all. The guard that omits the
// field is written by whichever branch of the generator built the value, so the
// comment emitter cannot see it; assigning through it left a nil node in the map
// and ir.FromMap dereferenced it, so EVERY charter without gates panicked on
// encode, whether or not it had anything to say about them.
func TestGeneratedFieldCommentsOmitted(t *testing.T) {
	c := comments.Charter{
		Rules:        []comments.Rule{{Name: "a"}},
		GatesComment: []string{"# about the gates"},
	}
	node, err := c.ToTonyIR()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := encode.Encode(node, &b, encode.EncodeComments(true)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "gates") {
		t.Errorf("the field was omitted, so its comment goes with it:\n%s", b.String())
	}

	// and when it IS written, the comment comes along
	c.Gates = []string{"open"}
	node, err = c.ToTonyIR()
	if err != nil {
		t.Fatal(err)
	}
	b.Reset()
	if err := encode.Encode(node, &b, encode.EncodeComments(true)); err != nil {
		t.Fatal(err)
	}
	// above the first element, which is where the format attributes a block
	// array's head comment -- the same shape charterDoc round-trips
	if !strings.Contains(b.String(), "gates:\n# about the gates\n- open") {
		t.Errorf("the comment did not head the field it belongs to:\n%s", b.String())
	}
}
