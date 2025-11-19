package codegen

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sync"

	"golang.org/x/tools/go/packages"
)

// PackageLoader loads and caches Go packages.
type PackageLoader struct {
	cache map[string]*packages.Package
	mu    sync.RWMutex
}

// NewPackageLoader creates a new PackageLoader.
func NewPackageLoader() *PackageLoader {
	return &PackageLoader{
		cache: make(map[string]*packages.Package),
	}
}

// LoadPackage loads a package by its import path.
func (l *PackageLoader) LoadPackage(importPath string) (*packages.Package, error) {
	l.mu.RLock()
	if pkg, ok := l.cache[importPath]; ok {
		l.mu.RUnlock()
		return pkg, nil
	}
	l.mu.RUnlock()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Check again in case it was loaded while we were waiting for the lock
	if pkg, ok := l.cache[importPath]; ok {
		return pkg, nil
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedTypes | packages.NeedTypesSizes | packages.NeedSyntax | packages.NeedTypesInfo,
	}

	pkgs, err := packages.Load(cfg, importPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load package %q: %w", importPath, err)
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("package %q not found", importPath)
	}

	// Check for errors in the loaded package
	if len(pkgs[0].Errors) > 0 {
		// Just log the first error for now, but return the package anyway as it might be partially usable
		// or we might not care about some errors (e.g. missing dependencies we don't use)
		// fmt.Printf("warning: errors loading package %q: %v\n", importPath, pkgs[0].Errors[0])
	}

	pkg := pkgs[0]
	l.cache[importPath] = pkg
	return pkg, nil
}

// FindType looks up a type definition in a loaded package.
func (l *PackageLoader) FindType(pkg *packages.Package, typeName string) (types.Object, error) {
	if pkg.Types == nil {
		return nil, fmt.Errorf("package %q has no type information", pkg.PkgPath)
	}

	obj := pkg.Types.Scope().Lookup(typeName)
	if obj == nil {
		return nil, fmt.Errorf("type %q not found in package %q", typeName, pkg.PkgPath)
	}

	return obj, nil
}

// FindStructType finds a struct type definition in a loaded package.
// It returns the types.TypeName and the underlying *types.Struct.
func (l *PackageLoader) FindStructType(pkg *packages.Package, typeName string) (*types.TypeName, *types.Struct, error) {
	obj, err := l.FindType(pkg, typeName)
	if err != nil {
		return nil, nil, err
	}

	typeNameObj, ok := obj.(*types.TypeName)
	if !ok {
		return nil, nil, fmt.Errorf("%q is not a type name", typeName)
	}

	named, ok := typeNameObj.Type().(*types.Named)
	if !ok {
		return nil, nil, fmt.Errorf("%q is not a named type", typeName)
	}

	structType, ok := named.Underlying().(*types.Struct)
	if !ok {
		return nil, nil, fmt.Errorf("%q is not a struct", typeName)
	}

	return typeNameObj, structType, nil
}

// FindNamedType finds a named type definition in a loaded package.
// It returns the types.TypeName and the underlying types.Type.
func (l *PackageLoader) FindNamedType(pkg *packages.Package, typeName string) (*types.TypeName, types.Type, error) {
	obj, err := l.FindType(pkg, typeName)
	if err != nil {
		return nil, nil, err
	}

	typeNameObj, ok := obj.(*types.TypeName)
	if !ok {
		return nil, nil, fmt.Errorf("%q is not a type name", typeName)
	}

	named, ok := typeNameObj.Type().(*types.Named)
	if !ok {
		return nil, nil, fmt.Errorf("%q is not a named type", typeName)
	}

	return typeNameObj, named.Underlying(), nil
}

// ASTToStructType converts a types.Struct to an ast.StructType (simplified).
// This is useful if we need to inspect the AST, but for now we might just rely on types.Type.
func (l *PackageLoader) FindASTStructType(pkg *packages.Package, typeName string) (*ast.StructType, error) {
	// This is harder because we have to find the AST node corresponding to the type.
	// We can iterate through the syntax trees.
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if typeSpec.Name.Name == typeName {
					if structType, ok := typeSpec.Type.(*ast.StructType); ok {
						return structType, nil
					}
					return nil, fmt.Errorf("%q is not a struct in AST", typeName)
				}
			}
		}
	}
	return nil, fmt.Errorf("AST for type %q not found in package %q", typeName, pkg.PkgPath)
}
