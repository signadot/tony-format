package codegen

import (
	"os"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/aliasnode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// TestUpstreamItem18_AliasToIRNode guards issue f69agjyeh12ks item 18: an alias
// to *ir.Node — the type every open-valued field wants — did not resolve. In the
// same package it fell back to int, describing that field as an integer in the
// schema and generating Go that does not compile; across a package boundary it
// failed resolution outright and took the whole package with it.
//
// The three spellings are the same type and must produce the same schema and the
// same behaviour. These packages compiling is half the assertion.
func TestUpstreamItem18_AliasToIRNode(t *testing.T) {
	payload := ir.FromString("anything")

	direct := &aliasnode.Direct{P: payload}
	local := &aliasnode.LocalAlias{P: payload}
	cross := &aliasnode.CrossAlias{P: payload}

	dn, err := direct.ToTonyIR()
	if err != nil {
		t.Fatalf("Direct.ToTonyIR: %v", err)
	}
	ln, err := local.ToTonyIR()
	if err != nil {
		t.Fatalf("LocalAlias.ToTonyIR: %v", err)
	}
	cn, err := cross.ToTonyIR()
	if err != nil {
		t.Fatalf("CrossAlias.ToTonyIR: %v", err)
	}

	for name, node := range map[string]*ir.Node{"local": ln, "cross": cn} {
		got := ir.ToMap(node)["p"]
		want := ir.ToMap(dn)["p"]
		if got == nil || want == nil || got.String != want.String || got.Type != want.Type {
			t.Errorf("%s alias encoded p as %v, direct spelling encoded %v", name, got, want)
		}
	}

	var back aliasnode.LocalAlias
	if err := back.FromTonyIR(ln); err != nil {
		t.Fatalf("LocalAlias.FromTonyIR: %v", err)
	}
	if back.P == nil || back.P.String != "anything" {
		t.Errorf("alias field did not round-trip: %v", back.P)
	}

	// The schema must describe all three identically — the silent half of the
	// defect was a signature claiming int for a field that accepts any value.
	defs := aliasSchemaDefs(t, "testdata/aliasnode/schema_gen.tony")
	for _, want := range defs {
		if want != ".[tony-base:ir]" {
			t.Errorf("a spelling of *ir.Node is described as %q, want .[tony-base:ir]", want)
		}
	}
	if len(defs) != 3 {
		t.Errorf("got %d p definitions, want 3 (direct, local alias, cross-package alias)", len(defs))
	}
}

// aliasSchemaDefs returns the "p" definition from every document in a schema file.
func aliasSchemaDefs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	docs, err := parse.ParseMulti(data)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var out []string
	for _, doc := range docs {
		define := ir.ToMap(doc)["define"]
		if define == nil {
			continue
		}
		if p := ir.ToMap(define)["p"]; p != nil {
			out = append(out, p.String)
		}
	}
	return out
}
