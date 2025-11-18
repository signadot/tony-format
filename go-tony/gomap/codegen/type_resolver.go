package codegen

import (
	"fmt"
	"go/ast"
	"reflect"
)

// ResolveFieldTypes resolves reflect.Type for all fields in the given structs.
// Uses AST analysis to resolve types to reflect.Type.
func ResolveFieldTypes(structs []*StructInfo, pkgDir string, pkgName string) error {
	// Build a map of struct names to placeholder struct types (for self-references)
	structTypeMap := make(map[string]reflect.Type)
	
	// First pass: create placeholder types for all structs
	for _, s := range structs {
		// Create a placeholder struct type using StructOf
		// We'll use an empty struct as a placeholder
		structTypeMap[s.Name] = reflect.StructOf([]reflect.StructField{})
	}
	
	// Second pass: resolve field types
	for _, s := range structs {
		for _, field := range s.Fields {
			if field.Type != nil {
				continue // Already resolved
			}
			
			typ, structName, err := resolveASTType(field.ASTType, structTypeMap, pkgName, s.Name)
			if err != nil {
				return fmt.Errorf("failed to resolve type for field %q.%q: %w", s.Name, field.Name, err)
			}
			field.Type = typ
			if structName != "" {
				field.StructTypeName = structName
			}
		}
	}
	
	return nil
}

// resolveASTType resolves an AST type expression to a reflect.Type.
// Returns the type and the struct name if it's a struct type.
// Handles basic types, pointers, slices, arrays, maps, and struct references.
func resolveASTType(expr ast.Expr, structTypeMap map[string]reflect.Type, pkgName string, currentStructName string) (reflect.Type, string, error) {
	if expr == nil {
		return nil, "", fmt.Errorf("nil type expression")
	}

	switch t := expr.(type) {
	case *ast.Ident:
		// Basic type or named type
		return resolveIdentType(t.Name, structTypeMap, pkgName, currentStructName)
		
	case *ast.StarExpr:
		// Pointer type: *T
		elemType, structName, err := resolveASTType(t.X, structTypeMap, pkgName, currentStructName)
		if err != nil {
			return nil, "", err
		}
		return reflect.PtrTo(elemType), structName, nil
		
	case *ast.ArrayType:
		// Array or slice: []T or [N]T
		elemType, structName, err := resolveASTType(t.Elt, structTypeMap, pkgName, currentStructName)
		if err != nil {
			return nil, "", err
		}
		if t.Len == nil {
			// Slice
			return reflect.SliceOf(elemType), structName, nil
		}
		// Array - for now, treat as slice (we can't know the length at compile time for codegen)
		return reflect.SliceOf(elemType), structName, nil
		
	case *ast.MapType:
		// Map: map[K]V
		keyType, _, err := resolveASTType(t.Key, structTypeMap, pkgName, currentStructName)
		if err != nil {
			return nil, "", err
		}
		valueType, structName, err := resolveASTType(t.Value, structTypeMap, pkgName, currentStructName)
		if err != nil {
			return nil, "", err
		}
		return reflect.MapOf(keyType, valueType), structName, nil
		
	case *ast.SelectorExpr:
		// Qualified identifier: pkg.Type (cross-package)
		// For now, we can't resolve cross-package types without loading those packages
		// Return an error or use a placeholder
		return nil, "", fmt.Errorf("cross-package type resolution not yet implemented: %v", expr)
		
	case *ast.ChanType:
		// Channel: chan T
		elemType, structName, err := resolveASTType(t.Value, structTypeMap, pkgName, currentStructName)
		if err != nil {
			return nil, "", err
		}
		return reflect.ChanOf(reflect.BothDir, elemType), structName, nil
		
	case *ast.InterfaceType:
		// Interface type (interface{})
		return reflect.TypeOf((*interface{})(nil)).Elem(), "", nil
		
	case *ast.StructType:
		// Anonymous struct type - create a placeholder
		// For codegen, we might not need the actual struct type
		return nil, "", fmt.Errorf("anonymous struct types not supported")
		
	default:
		return nil, "", fmt.Errorf("unsupported type expression: %T", expr)
	}
}

// resolveIdentType resolves a type identifier to a reflect.Type.
// Returns the type and the struct name if it's a struct type.
func resolveIdentType(name string, structTypeMap map[string]reflect.Type, pkgName string, currentStructName string) (reflect.Type, string, error) {
	// Check if it's a basic type
	if typ := resolveBasicType(name); typ != nil {
		return typ, "", nil
	}
	
	// Check if it's a struct type we know about (including self-reference)
	if typ, ok := structTypeMap[name]; ok {
		return typ, name, nil
	}
	
	// Unknown type - could be a struct from another package or a type we haven't seen
	// For now, return an error - we'll need to handle cross-package types later
	return nil, "", fmt.Errorf("unknown type %q (not a basic type and not found in struct map)", name)
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
