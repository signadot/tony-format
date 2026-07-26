package codegen

import (
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// crossPkgSchema resolves the schema a named type from another package is
// described by, and returns the reference to use for it — "<pkgName>:<name>" —
// or ok=false when that package describes no such schema.
//
// The reference has to come from what the target actually declares. Building it
// as strings.ToLower(typeName) — which is what this used to do, unconditionally
// and for every cross-package named type — fabricates a name that is right only
// when the Go identifier and the schema name happen to coincide: tx.State
// declares schemagen=tx-state and got a dangling !tx:state, while time.Duration,
// which declares nothing at all, got a dangling !time:duration. A reference that
// cannot be looked up is not an imprecise description, it is not a description;
// the caller falls back to what the encoder actually emits instead.
//
// Two kinds of target resolve:
//
//   - a Go type carrying a //tony:schemagen= directive or a schemagen= marker
//     field, read from the target package's own source, so both spellings work;
//   - a hand-written .tony in the target package's directory whose
//     signature.name matches the lowercased type name, which is the only link
//     those have to the Go type (format.Format ↔ format/format.tony).
//
// ok=false for everything else, including a type whose package has no schema at
// all and one whose schema names something the type does not claim.
func crossPkgSchema(pkgPath, typeName string, loader *PackageLoader, imports map[string]string) (string, bool) {
	if pkgPath == "" || typeName == "" || loader == nil {
		return "", false
	}

	pkg, err := loader.LoadPackage(pkgPath)
	if err != nil {
		return "", false
	}

	// An alias declares nothing of its own; the schema belongs to what it names,
	// which may live in a third package (type Format = format.Format). Resolve
	// to that before asking who declares a schema — issue f69agjyeh12ks item 14
	// seen from the schema side.
	if pkgPath, typeName, pkg, err = unalias(pkgPath, typeName, pkg, loader); err != nil {
		return "", false
	}

	pkgName := pkg.Name
	if pkgName == "" {
		pkgName = filepath.Base(pkgPath)
	}

	if name, ok := declaredSchemaName(pkg.Syntax, typeName); ok {
		imports[pkgPath] = pkgName
		return pkgName + ":" + name, true
	}

	// Fall back to a hand-written schema sitting beside the package's sources.
	var dir string
	for _, f := range pkg.GoFiles {
		if f != "" {
			dir = filepath.Dir(f)
			break
		}
	}
	if dir == "" {
		return "", false
	}
	want := strings.ToLower(typeName)
	if !packageDefinesSchema(dir, want) {
		return "", false
	}
	imports[pkgPath] = pkgName
	return pkgName + ":" + want, true
}

// unalias follows a type alias to the named type it stands for, returning that
// type's package path, name and loaded package. A non-alias, or anything that
// cannot be followed, comes back unchanged.
func unalias(pkgPath, typeName string, pkg *packages.Package, loader *PackageLoader) (string, string, *packages.Package, error) {
	if pkg.Types == nil {
		return pkgPath, typeName, pkg, nil
	}
	obj := pkg.Types.Scope().Lookup(typeName)
	tn, ok := obj.(*types.TypeName)
	if !ok || !tn.IsAlias() {
		return pkgPath, typeName, pkg, nil
	}
	named, ok := types.Unalias(tn.Type()).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return pkgPath, typeName, pkg, nil
	}
	targetPkg := named.Obj().Pkg().Path()
	if targetPkg == pkgPath {
		return pkgPath, named.Obj().Name(), pkg, nil
	}
	loaded, err := loader.LoadPackage(targetPkg)
	if err != nil {
		return "", "", nil, err
	}
	return targetPkg, named.Obj().Name(), loaded, nil
}

// declaredSchemaName returns the schema name the named type declares, via
// either spelling of the marker, by running the same extraction used on the
// package being generated.
func declaredSchemaName(files []*ast.File, typeName string) (string, bool) {
	for _, file := range files {
		structs, err := ExtractTypes(file, "")
		if err != nil {
			continue
		}
		for _, s := range structs {
			if s.Name != typeName || !hasSchemaGen(s) {
				continue
			}
			if s.StructSchema.SchemaName != "" {
				return s.StructSchema.SchemaName, true
			}
		}
	}
	return "", false
}

var (
	schemaNamesMu    sync.Mutex
	schemaNamesCache = map[string]map[string]bool{}
)

// packageDefinesSchema reports whether any .tony file in dir carries a
// signature.name of name. Results are cached per directory: a schema generation
// run asks about the same handful of packages repeatedly.
func packageDefinesSchema(dir, name string) bool {
	schemaNamesMu.Lock()
	defer schemaNamesMu.Unlock()

	names, ok := schemaNamesCache[dir]
	if !ok {
		names = map[string]bool{}
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() || filepath.Ext(e.Name()) != ".tony" {
					continue
				}
				data, err := os.ReadFile(filepath.Join(dir, e.Name()))
				if err != nil {
					continue
				}
				// A generated schema publishes exactly one name per document,
				// its signature.name; its define: keys are one struct's field
				// names and reference nothing. Only a hand-written schema
				// publishes reusable definitions that way.
				for _, n := range schemaNames(data, e.Name() != generatedSchemaFile) {
					names[n] = true
				}
			}
		}
		schemaNamesCache[dir] = names
	}
	return names[name]
}

// generatedSchemaFile is the schema tony-codegen writes for a package.
const generatedSchemaFile = "schema_gen.tony"

// schemaNames returns every name a .tony file makes referenceable: the
// signature.name of each document in it and, when withDefines is set, the keys
// of each document's define: block. A "pkg:name" reference resolves against
// either — schema/base.tony publishes ir as a define key, format/format.tony
// publishes format as a signature name.
func schemaNames(data []byte, withDefines bool) []string {
	docs, err := parse.ParseMulti(data)
	if err != nil {
		return nil
	}
	var names []string
	for _, doc := range docs {
		if sig := ir.ToMap(doc)["signature"]; sig != nil {
			if n := ir.ToMap(sig)["name"]; n != nil && n.Type == ir.StringType {
				names = append(names, n.String)
			}
		}
		if define := ir.ToMap(doc)["define"]; withDefines && define != nil {
			for name := range ir.ToMap(define) {
				names = append(names, name)
			}
		}
	}
	return names
}
