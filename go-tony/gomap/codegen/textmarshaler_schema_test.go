package codegen

import (
	"os"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/textscalar"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// TestTextMarshalerSchemaMatchesCodegen guards issue f69agjyeh12ks: a field whose
// type implements encoding.TextMarshaler is written as a string by both the
// generated code and the reflection path, but the schema type mapper never
// consulted TextMarshaler and reported the underlying kind — ref: .[int] for a
// value that is always a string.
//
// Nothing fails to compile and nothing misbehaves at runtime when the schema is
// wrong, so nothing else catches it: only a published schema carries the lie.
// This asserts the two agree, field by field, rather than pinning the schema
// text alone.
func TestTextMarshalerSchemaMatchesCodegen(t *testing.T) {
	e := &textscalar.Entity{
		Ref:   5,
		Refs:  []textscalar.Ref{1, 2},
		Name:  "n",
		Count: 7,
	}

	gen, err := e.ToTonyIR()
	if err != nil {
		t.Fatalf("generated ToTonyIR: %v", err)
	}
	// The reflection path must produce the same shape, or "agrees with the
	// generated code" would only be half the story.
	refl, err := gomap.ToTonyIR(e)
	if err != nil {
		t.Fatalf("reflection ToTonyIR: %v", err)
	}

	defs := schemaDefinitions(t, "testdata/textscalar/schema_gen.tony")

	// want maps each field to the schema definition it must carry and the IR
	// type both encoders must actually produce for it.
	want := []struct {
		field  string
		schema string
		irType ir.Type
	}{
		{"ref", ".[string]", ir.StringType},     // uint64 underneath, MarshalText on top
		{"refs", ".[array(int)]", ir.ArrayType}, // []Ref: neither encoder marshals elements as text
		{"name", ".[string]", ir.StringType},    // plain string
		{"count", ".[int]", ir.NumberType},      // plain uint64, no MarshalText
	}

	for _, w := range want {
		def, ok := defs[w.field]
		if !ok {
			t.Errorf("%s: no definition in generated schema", w.field)
			continue
		}
		if def != w.schema {
			t.Errorf("%s: schema says %s, want %s", w.field, def, w.schema)
		}
		for path, doc := range map[string]*ir.Node{"generated": gen, "reflection": refl} {
			val := ir.ToMap(doc)[w.field]
			if val == nil {
				t.Errorf("%s: %s encoder emitted no value", w.field, path)
				continue
			}
			if val.Type != w.irType {
				t.Errorf("%s: %s encoder emitted %v, but schema says %s",
					w.field, path, val.Type, def)
			}
		}
	}

	// Element types too: .[array(int)] has to mean the elements really are ints.
	for _, path := range []*ir.Node{gen, refl} {
		refs := ir.ToMap(path)["refs"]
		for i, v := range refs.Values {
			if v.Type != ir.NumberType {
				t.Errorf("refs[%d]: emitted %v, but schema says .[array(int)]", i, v.Type)
			}
		}
	}
}

// schemaDefinitions reads a generated schema file and returns its define: map as
// definition-name → schema text.
func schemaDefinitions(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc, err := parse.Parse(data)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	define := ir.ToMap(doc)["define"]
	if define == nil {
		t.Fatalf("%s: no define section", path)
	}
	defs := make(map[string]string)
	for name, node := range ir.ToMap(define) {
		defs[name] = node.String
	}
	return defs
}
