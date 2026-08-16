package gomap

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/schema"
)

// (*Mapper) is a second public entry point, and for a type whose schema resolves
// from a registry it takes its own path -- not the one gomap.ToTonyIR/FromTonyIR
// take. Everything about comments was different there, in three ways:
//
//   - the carrier fields were matched against the schema and reported as extra,
//     so a type using the annotations could not be encoded through a
//     registry-backed mapper at all
//   - encoding them was two empty blocks, "a placeholder for when marshaling
//     from IR -> IR"
//   - decoding read the fields off the node BEFORE unwrapping the head comment,
//     so a commented document decoded to an empty struct, silently
//
// (3cdjz00jh12krns4g1n0)

type mapperCommentTag struct{}

type mapperSpec struct {
	mapperCommentTag `tony:"schema=mapper-comment-spec,comment=Comments,lineComment=LineComments"`
	Replicas         int `tony:"field=replicas"`
	Comments         []string
	LineComments     []string
}

func commentMapper(t *testing.T) *Mapper {
	t.Helper()
	ctxReg := schema.NewContextRegistry()
	schemaReg := schema.NewSchemaRegistry(ctxReg)
	s := &schema.Schema{
		Context:   schema.DefaultContext(),
		Signature: &schema.Signature{Name: "mapper-comment-spec"},
		Accept:    ir.FromMap(map[string]*ir.Node{"replicas": ir.FromInt(0)}),
	}
	if err := schemaReg.RegisterSchema(s); err != nil {
		t.Fatal(err)
	}
	return NewMapper(schemaReg, ctxReg)
}

// TestMapperEncodesComments: the carriers are not members, so a schema that does
// not declare them is not violated by them, and what they hold reaches the node.
func TestMapperEncodesComments(t *testing.T) {
	m := commentMapper(t)
	node, err := m.ToTonyIR(mapperSpec{
		Replicas:     3,
		Comments:     []string{"# head"},
		LineComments: []string{" # line"},
	})
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var b strings.Builder
	if err := encode.Encode(node, &b, encode.EncodeComments(true)); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "# head") {
		t.Errorf("the head comment did not reach the node:\n%s", out)
	}
	if strings.Contains(out, "Comments") {
		t.Errorf("a carrier field was written as data:\n%s", out)
	}
	if !strings.Contains(out, "replicas: 3") {
		t.Errorf("the data did not survive:\n%s", out)
	}
}

// TestMapperDecodesLineComment: an object with a line comment is what the
// dispatch actually routes here (it sends ObjectType nodes only), and the
// carrier gets it. The extractors this replaced looked for comment text in
// .Values and .String of a comment node and in .Lines of a value node -- none of
// which is where the IR keeps it -- so the carriers were never filled on this
// path at all.
func TestMapperDecodesLineComment(t *testing.T) {
	m := commentMapper(t)
	node, err := parse.Parse([]byte("replicas: 3\n"), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	node.Comment = &ir.Node{Type: ir.CommentType, Lines: []string{" # line"}, Parent: node}
	var s mapperSpec
	if err := m.FromTonyIR(node, &s); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if s.Replicas != 3 {
		t.Errorf("replicas came back as %d", s.Replicas)
	}
	if len(s.LineComments) != 1 || s.LineComments[0] != " # line" {
		t.Errorf("line comments came back as %q", s.LineComments)
	}
	if len(s.Comments) != 0 {
		t.Errorf("a line comment filled the HEAD comment carrier: %q", s.Comments)
	}
}

// TestMapperDecodesCommentedDocument: a wrapped document reaches this mapper
// through the reflection fallback -- the schema-aware path is entered for
// ObjectType nodes only -- so this pins the behaviour rather than the route.
func TestMapperDecodesCommentedDocument(t *testing.T) {
	m := commentMapper(t)
	node, err := parse.Parse([]byte("# head\nreplicas: 3\n"), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	var s mapperSpec
	if err := m.FromTonyIR(node, &s); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if s.Replicas != 3 {
		t.Errorf("a commented document decoded to replicas=%d: the fields were read from the wrapper", s.Replicas)
	}
	if len(s.Comments) != 1 || s.Comments[0] != "# head" {
		t.Errorf("head comments came back as %q", s.Comments)
	}
}

// TestMapperRoundTripsComments: through its own output, both kinds.
func TestMapperRoundTripsComments(t *testing.T) {
	m := commentMapper(t)
	in := mapperSpec{Replicas: 3, Comments: []string{"# head"}, LineComments: []string{" # line"}}
	node, err := m.ToTonyIR(in)
	if err != nil {
		t.Fatal(err)
	}
	var back mapperSpec
	if err := m.FromTonyIR(node, &back); err != nil {
		t.Fatal(err)
	}
	if back.Replicas != in.Replicas {
		t.Errorf("replicas came back as %d", back.Replicas)
	}
	if len(back.Comments) != 1 || back.Comments[0] != "# head" {
		t.Errorf("head comments came back as %q", back.Comments)
	}
	if len(back.LineComments) != 1 || back.LineComments[0] != " # line" {
		t.Errorf("line comments came back as %q", back.LineComments)
	}
}
