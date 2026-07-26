package codegen

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

const modulePrefix = "github.com/signadot/tony-format/go-tony/"

// TestCrossPkgSchemaResolves guards issue f69agjyeh12ks: a cross-package
// reference used to be built as strings.ToLower(typeName) and returned without
// anyone checking that such a schema exists. It was right only by coincidence —
// api.Patch declares schemagen=patch — and dangling everywhere else.
func TestCrossPkgSchemaResolves(t *testing.T) {
	loader := NewPackageLoader()
	for _, c := range []struct {
		name    string
		pkgPath string
		typ     string
		wantRef string
		wantOK  bool
	}{
		{
			name:    "schemagen directive, name differs from the Go identifier",
			pkgPath: modulePrefix + "system/logd/storage/tx",
			typ:     "State", // //tony:schemagen=tx-state
			wantRef: "tx:tx-state",
			wantOK:  true,
		},
		{
			name:    "alias to a type that declares one",
			pkgPath: modulePrefix + "gomap/codegen/testdata/aliastarget",
			typ:     "Format", // type Format = Real, schemagen=aliastarget-real
			wantRef: "aliastarget:aliastarget-real",
			wantOK:  true,
		},
		{
			name:    "hand-written schema beside the package sources",
			pkgPath: modulePrefix + "format",
			typ:     "Format", // format/format.tony, signature.name: format
			wantRef: "format:format",
			wantOK:  true,
		},
		{
			name:    "no schema anywhere",
			pkgPath: "time",
			typ:     "Duration",
			wantOK:  false,
		},
		{
			name:    "no schema anywhere, TextMarshaler",
			pkgPath: "time",
			typ:     "Time",
			wantOK:  false,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			imports := map[string]string{}
			ref, ok := crossPkgSchema(c.pkgPath, c.typ, loader, imports)
			if ok != c.wantOK {
				t.Fatalf("crossPkgSchema(%s.%s) ok = %t, want %t (ref %q)",
					c.pkgPath, c.typ, ok, c.wantOK, ref)
			}
			if ok && ref != c.wantRef {
				t.Errorf("crossPkgSchema(%s.%s) = %q, want %q", c.pkgPath, c.typ, ref, c.wantRef)
			}
			if !ok && len(imports) != 0 {
				t.Errorf("unresolved reference left a dangling import: %v", imports)
			}
		})
	}
}

// schemaRef matches a "pkg:name" cross-package reference wherever it appears —
// as a tag (!tx:tx-state) or inside a parameterized type (.[nullable(tx:tx-state)]).
var schemaRef = regexp.MustCompile(`([a-zA-Z0-9_-]+):([a-zA-Z0-9_-]+)`)

// TestGeneratedSchemasHaveNoDanglingReferences walks every committed
// schema_gen.tony and checks that each cross-package reference in it names a
// schema the target package actually publishes.
//
// This is the invariant that was broken: three of the five references in the
// repo pointed at nothing, and nothing caught it, because a wrong schema breaks
// no build and no round-trip. Only a reader of the published schema would find
// out.
func TestGeneratedSchemasHaveNoDanglingReferences(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("locate module root: %v", err)
	}

	var checked int
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "schema_gen.tony" {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Errorf("%s: %v", path, readErr)
			return nil
		}
		docs, parseErr := parse.ParseMulti(data)
		if parseErr != nil {
			t.Errorf("%s: parse: %v", path, parseErr)
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		for _, doc := range docs {
			ctx := contextImports(doc)
			for _, ref := range collectRefs(doc) {
				checked++
				alias, name, _ := strings.Cut(ref, ":")
				importPath, ok := ctx[alias]
				if !ok {
					t.Errorf("%s: reference %q has no matching context import", rel, ref)
					continue
				}
				dir, ok := packageDir(root, importPath)
				if !ok {
					t.Errorf("%s: reference %q points outside the module (%s), so nothing defines it",
						rel, ref, importPath)
					continue
				}
				if !packageDefinesSchema(dir, name) {
					t.Errorf("%s: reference %q is dangling — %s publishes no schema named %q",
						rel, ref, importPath, name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if checked == 0 {
		t.Fatal("no cross-package references found; the walk is not reaching the schemas")
	}
	t.Logf("checked %d cross-package references", checked)
}

// contextImports maps each context alias in a schema document to its import
// path: "- tx: github.com/..." becomes tx → github.com/...
func contextImports(doc *ir.Node) map[string]string {
	out := map[string]string{}
	ctx := ir.ToMap(doc)["context"]
	if ctx == nil {
		return out
	}
	for _, entry := range ctx.Values {
		for alias, v := range ir.ToMap(entry) {
			if v != nil && v.Type == ir.StringType {
				out[alias] = v.String
			}
		}
	}
	return out
}

// collectRefs returns every "pkg:name" reference in a schema document's
// definitions, from both tags and parameterized type strings.
func collectRefs(doc *ir.Node) []string {
	define := ir.ToMap(doc)["define"]
	if define == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(n *ir.Node)
	walk = func(n *ir.Node) {
		if n == nil {
			return
		}
		for _, text := range []string{strings.TrimPrefix(n.Tag, "!"), n.String} {
			for _, m := range schemaRef.FindAllString(text, -1) {
				if !seen[m] {
					seen[m] = true
					out = append(out, m)
				}
			}
		}
		// Objects keep keys in Fields and values in Values, in step; only the
		// values can carry a reference.
		for _, v := range n.Values {
			walk(v)
		}
	}
	walk(define)
	return out
}

// packageDir maps an in-module import path to its directory.
func packageDir(root, importPath string) (string, bool) {
	if !strings.HasPrefix(importPath, modulePrefix) {
		return "", false
	}
	return filepath.Join(root, strings.TrimPrefix(importPath, modulePrefix)), true
}
