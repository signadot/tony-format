package storage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// The rule that holds the read interface to its bound, made a test rather than a review
// note (read_write_interface.md):
//
//	No exported function in storage or storage/index returns a slice, map, or node whose
//	length grows with the number of commits, segments, or elements in range.
//
// It is checked at the level of what a signature SPELLS, which is what the rule is about:
// both large terms of the read the forensics caught in flight were signatures -- a slice
// of every segment in range, every patch in range decoded and held -- and nothing subtler.
// An exported function that needs to answer with such a slice says why here, by name.
func TestNoExportedSignatureGrowsWithHistory(t *testing.T) {
	// Whole-index walks. Compaction selects survivors over the whole index and the
	// persister writes the whole index; both are the index's own size by definition,
	// and phase 7 of rebuild_plan.md is where their working set is decided.
	allowed := map[string]string{
		"index.LookupRangeAll": "compaction and persistence walk the whole index by design (rebuild_plan.md, phase 7)",
		"index.AllSegments":    "the whole index, for the persister and for tests that inspect it (phase 7)",
	}
	// Result types whose length grows with what is in range: segments, nodes and
	// notifications by the slice, and anything keyed by commit or by path. A map with a
	// fixed set of keys -- a counter report -- is not one of them.
	growing := []string{
		"[]LogSegment", "[]index.LogSegment", "[]*LogSegment",
		"[]*ir.Node", "[]ir.Node",
		"[]*CommitNotification", "[]CommitNotification",
		"map[int64]", "map[string][]", "map[string]*ir.Node", "map[string]LogSegment", "map[string]*Index",
	}

	fset := token.NewFileSet()
	for _, pkg := range []struct{ dir, name string }{{".", "storage"}, {"index", "index"}} {
		pkgs, err := parser.ParseDir(fset, pkg.dir, func(fi fs.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", pkg.dir, err)
		}
		for _, p := range pkgs {
			for fname, file := range p.Files {
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || !fn.Name.IsExported() || fn.Type.Results == nil {
						continue
					}
					if fn.Recv != nil && !receiverExported(fn.Recv) {
						continue
					}
					for _, res := range fn.Type.Results.List {
						spelled := typeString(res.Type)
						for _, g := range growing {
							if !strings.HasPrefix(spelled, g) {
								continue
							}
							key := pkg.name + "." + fn.Name.Name
							if why, ok := allowed[key]; ok {
								t.Logf("allowed: %s returns %s -- %s", key, spelled, why)
								break
							}
							t.Errorf("%s: %s returns %s, whose length grows with what is in range (%s)",
								filepath.Base(fname), key, spelled, "read_write_interface.md")
						}
					}
				}
			}
		}
	}
}

func receiverExported(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) == 0 {
		return true
	}
	typ := recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if idx, ok := typ.(*ast.IndexExpr); ok {
		typ = idx.X
	}
	ident, ok := typ.(*ast.Ident)
	return !ok || ident.IsExported()
}

// typeString spells a type expression the way the source does, well enough to match the
// shapes above.
func typeString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.ArrayType:
		return "[]" + typeString(t.Elt)
	case *ast.MapType:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Value)
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.FuncType:
		return "func"
	case *ast.InterfaceType:
		return "interface"
	case *ast.ChanType:
		return "chan " + typeString(t.Value)
	case *ast.IndexExpr:
		return typeString(t.X) + "[" + typeString(t.Index) + "]"
	}
	return "?"
}
