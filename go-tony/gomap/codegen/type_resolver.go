package codegen

import (
	"fmt"
	"go/ast"
	"go/types"
	"reflect"

	"github.com/signadot/tony-format/go-tony/ir"
)

// TypeResolver handles type resolution for a package.
type TypeResolver struct {
	loader        *PackageLoader
	structTypeMap map[string]reflect.Type
	pkgName       string
}

// ResolveFieldTypes resolves reflect.Type for all fields in the given structs.
// Uses AST analysis to resolve types to reflect.Type.
func ResolveFieldTypes(structs []*StructInfo, pkgDir string, pkgName string) error {
	resolver := &TypeResolver{
		loader:        NewPackageLoader(),
		structTypeMap: make(map[string]reflect.Type),
		pkgName:       pkgName,
	}

	// First pass: create placeholder types for all structs
	for _, s := range structs {
		// Create a placeholder struct type using StructOf
		// We'll use an empty struct as a placeholder
		resolver.structTypeMap[s.Name] = reflect.StructOf([]reflect.StructField{})
	}

	// Second pass: resolve field types
	for _, s := range structs {
		for _, field := range s.Fields {
			if field.Type != nil {
				continue // Already resolved
			}

			typ, structName, pkgPath, typeName, err := resolver.resolveASTType(field.ASTType, s.Name, s.Imports)
			if err != nil {
				return fmt.Errorf("failed to resolve type for field %q.%q: %w", s.Name, field.Name, err)
			}
			field.Type = typ
			if structName != "" {
				field.StructTypeName = structName
			}
			if pkgPath != "" {
				field.TypePkgPath = pkgPath
			}
			if typeName != "" {
				field.TypeName = typeName
			}
		}
	}

	return nil
}

// resolveASTType resolves an AST type expression to a reflect.Type.
// Returns: (type, structName, pkgPath, typeName, error)
// - type: the reflect.Type
// - structName: qualified name for structs (e.g., "format.Format")
// - pkgPath: package path for cross-package named types (e.g., "github.com/.../format")
// - typeName: type name for cross-package named types (e.g., "Format")
// Handles basic types, pointers, slices, arrays, maps, and struct references.
func (r *TypeResolver) resolveASTType(expr ast.Expr, currentStructName string, imports map[string]string) (reflect.Type, string, string, string, error) {
	if expr == nil {
		return nil, "", "", "", fmt.Errorf("nil type expression")
	}

	switch t := expr.(type) {
	case *ast.Ident:
		// Basic type or named type
		return r.resolveIdentType(t.Name, currentStructName)

	case *ast.StarExpr:
		// Pointer type: *T
		elemType, structName, pkgPath, typeName, err := r.resolveASTType(t.X, currentStructName, imports)
		if err != nil {
			return nil, "", "", "", err
		}
		return reflect.PtrTo(elemType), structName, pkgPath, typeName, nil

	case *ast.ArrayType:
		// Array or slice: []T or [N]T
		elemType, structName, pkgPath, typeName, err := r.resolveASTType(t.Elt, currentStructName, imports)
		if err != nil {
			return nil, "", "", "", err
		}
		if t.Len == nil {
			// Slice
			return reflect.SliceOf(elemType), structName, pkgPath, typeName, nil
		}
		// Array - for now, treat as slice (we can't know the length at compile time for codegen)
		return reflect.SliceOf(elemType), structName, pkgPath, typeName, nil

	case *ast.MapType:
		// Map: map[K]V
		keyType, _, _, _, err := r.resolveASTType(t.Key, currentStructName, imports)
		if err != nil {
			return nil, "", "", "", err
		}
		valueType, structName, pkgPath, typeName, err := r.resolveASTType(t.Value, currentStructName, imports)
		if err != nil {
			return nil, "", "", "", err
		}
		return reflect.MapOf(keyType, valueType), structName, pkgPath, typeName, nil

	case *ast.SelectorExpr:
		// Qualified identifier: pkg.Type (cross-package)
		if ident, ok := t.X.(*ast.Ident); ok {
			// Special case for ir.Node to return the actual type
			if ident.Name == "ir" && t.Sel.Name == "Node" {
				return reflect.TypeOf(ir.Node{}), "ir.Node", "", "", nil
			}

			// Resolve cross-package type
			pkgName := ident.Name
			typeName := t.Sel.Name

			// Find import path
			importPath, ok := imports[pkgName]
			if !ok {
				// Maybe it's in the same package but referenced with package name?
				// Or maybe it's a standard library package?
				// For now, assume it's a missing import or error
				return nil, "", "", "", fmt.Errorf("unknown package %q (missing import?)", pkgName)
			}

			// Load package
			pkg, err := r.loader.LoadPackage(importPath)
			if err != nil {
				return nil, "", "", "", fmt.Errorf("failed to load package %q: %w", importPath, err)
			}

			// Find type in package
			_, underlying, err := r.loader.FindNamedType(pkg, typeName)
			if err != nil {
				// Try finding it as a general type if named type lookup fails (e.g. alias)
				// But for now, let's assume it's a named type or struct
				return nil, "", "", "", fmt.Errorf("failed to find type %q in package %q: %w", typeName, importPath, err)
			}

			// Handle the underlying type
			if structType, ok := underlying.(*types.Struct); ok {
				// It's a struct
				qualifiedName := pkgName + "." + typeName
				// Verify it is actually a struct (we did that with the type assertion)
				_ = structType
				return reflect.StructOf([]reflect.StructField{}), qualifiedName, importPath, typeName, nil
			}

			if basic, ok := underlying.(*types.Basic); ok {
				// It's a basic type (e.g. type Format int)
				// Return the corresponding reflect.Type AND the qualified name
				qualifiedName := pkgName + "." + typeName
				var reflectType reflect.Type
				switch basic.Kind() {
				case types.Bool:
					reflectType = reflect.TypeOf(bool(false))
				case types.Int:
					reflectType = reflect.TypeOf(int(0))
				case types.Int8:
					reflectType = reflect.TypeOf(int8(0))
				case types.Int16:
					reflectType = reflect.TypeOf(int16(0))
				case types.Int32:
					reflectType = reflect.TypeOf(int32(0))
				case types.Int64:
					reflectType = reflect.TypeOf(int64(0))
				case types.Uint:
					reflectType = reflect.TypeOf(uint(0))
				case types.Uint8:
					reflectType = reflect.TypeOf(uint8(0))
				case types.Uint16:
					reflectType = reflect.TypeOf(uint16(0))
				case types.Uint32:
					reflectType = reflect.TypeOf(uint32(0))
				case types.Uint64:
					reflectType = reflect.TypeOf(uint64(0))
				case types.Uintptr:
					reflectType = reflect.TypeOf(uintptr(0))
				case types.Float32:
					reflectType = reflect.TypeOf(float32(0))
				case types.Float64:
					reflectType = reflect.TypeOf(float64(0))
				case types.Complex64:
					reflectType = reflect.TypeOf(complex64(0))
				case types.Complex128:
					reflectType = reflect.TypeOf(complex128(0))
				case types.String:
					reflectType = reflect.TypeOf(string(""))
				case types.UntypedBool:
					reflectType = reflect.TypeOf(bool(false))
				case types.UntypedInt:
					reflectType = reflect.TypeOf(int(0))
				case types.UntypedRune:
					reflectType = reflect.TypeOf(rune(0))
				case types.UntypedFloat:
					reflectType = reflect.TypeOf(float64(0))
				case types.UntypedComplex:
					reflectType = reflect.TypeOf(complex128(0))
				case types.UntypedString:
					reflectType = reflect.TypeOf(string(""))
				case types.UntypedNil:
					return nil, "", "", "", fmt.Errorf("cannot resolve untyped nil")
				default:
					return nil, "", "", "", fmt.Errorf("unsupported basic type: %v", basic.Kind())
				}
				return reflectType, qualifiedName, importPath, typeName, nil
			}

			return nil, "", "", "", fmt.Errorf("unsupported underlying type for %q: %T", typeName, underlying)
		}

		return nil, "", "", "", fmt.Errorf("unsupported selector expression: %v", expr)

	case *ast.ChanType:
		// Channel: chan T
		elemType, structName, _, _, err := r.resolveASTType(t.Value, currentStructName, imports)
		if err != nil {
			return nil, "", "", "", err
		}
		return reflect.ChanOf(reflect.BothDir, elemType), structName, "", "", nil

	case *ast.InterfaceType:
		// Interface type (interface{})
		return reflect.TypeOf((*interface{})(nil)).Elem(), "", "", "", nil

	case *ast.StructType:
		// Anonymous struct type - create a placeholder
		// For codegen, we might not need the actual struct type
		return nil, "", "", "", fmt.Errorf("anonymous struct types not supported")

	default:
		return nil, "", "", "", fmt.Errorf("unsupported type expression: %T", expr)
	}
}

// resolveIdentType resolves a type identifier to a reflect.Type.
// Returns the type and the struct name if it's a struct type.
func (r *TypeResolver) resolveIdentType(name string, currentStructName string) (reflect.Type, string, string, string, error) {
	// Check if it's a basic type
	if typ := resolveBasicType(name); typ != nil {
		return typ, "", "", "", nil
	}

	// Check if it's a struct type we know about (including self-reference)
	if typ, ok := r.structTypeMap[name]; ok {
		return typ, name, "", "", nil
	}

	// Unknown type - could be a struct from another package or a type we haven't seen
	// For now, return an error - we'll need to handle cross-package types later
	return nil, "", "", "", fmt.Errorf("unknown type %q (not a basic type and not found in struct map)", name)
}

// resolveBasicType resolves a basic Go type name to a reflect.Type.
func resolveBasicType(name string) reflect.Type {
	switch name {
	case "bool":
		return reflect.TypeOf(bool(false))
	case "string":
		return reflect.TypeOf(string(""))
	case "int":
		return reflect.TypeOf(int(0))
	case "int8":
		return reflect.TypeOf(int8(0))
	case "int16":
		return reflect.TypeOf(int16(0))
	case "int32":
		return reflect.TypeOf(int32(0))
	case "int64":
		return reflect.TypeOf(int64(0))
	case "uint":
		return reflect.TypeOf(uint(0))
	case "uint8":
		return reflect.TypeOf(uint8(0))
	case "uint16":
		return reflect.TypeOf(uint16(0))
	case "uint32":
		return reflect.TypeOf(uint32(0))
	case "uint64":
		return reflect.TypeOf(uint64(0))
	case "uintptr":
		return reflect.TypeOf(uintptr(0))
	case "float32":
		return reflect.TypeOf(float32(0))
	case "float64":
		return reflect.TypeOf(float64(0))
	case "complex64":
		return reflect.TypeOf(complex64(0))
	case "complex128":
		return reflect.TypeOf(complex128(0))
	case "byte": // alias for uint8
		return reflect.TypeOf(byte(0))
	case "rune": // alias for int32
		return reflect.TypeOf(rune(0))
	case "any": // Go 1.18+ alias for interface{}
		return reflect.TypeOf((*interface{})(nil)).Elem()
	case "interface{}": // Explicit interface{} type
		return reflect.TypeOf((*interface{})(nil)).Elem()
	default:
		return nil
	}
}
