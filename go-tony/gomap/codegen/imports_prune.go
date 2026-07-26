package codegen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
)

// pruneUnusedImports drops import lines the generated body never qualifies.
//
// Imports are collected by walking the field types, which answers "does this
// package appear in the struct definition" — not "does the generated code spell
// it". Those differ: a struct-VALUE field of another package generates only
// method calls on the value (s.Val.ToTonyIR(...)), so the qualifier never
// appears and the import is unused, and the package does not compile
// (issue f69agjyeh12ks item 17). Predicting which emissions need a qualifier
// means mirroring every branch of the generator and keeping it mirrored;
// reading the finished body does not.
//
// A qualifier here is any identifier used on the left of a selector. That
// over-approximates — a local variable named like a package counts — which
// keeps an import that is not needed rather than dropping one that is, leaving
// the previous behaviour in place for that case.
//
// src that does not parse is returned unchanged: the caller's format.Source
// reports the real problem better than this can.
func pruneUnusedImports(src string) string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src
	}

	used := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok {
			used[id.Name] = true
		}
		return true
	})

	drop := map[string]bool{}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		local := path.Base(importPath)
		if spec.Name != nil {
			local = spec.Name.Name
			// A blank or dot import is there for its side effects, not a
			// qualifier; never drop those.
			if local == "_" || local == "." {
				continue
			}
		}
		if !used[local] {
			drop[spec.Path.Value] = true
		}
	}
	if len(drop) == 0 {
		return src
	}

	var kept []string
	for _, line := range strings.Split(src, "\n") {
		if drop[strings.TrimSpace(line)] {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
