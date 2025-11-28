package codegen

import (
	"fmt"
	"go/ast"
	"go/types"
	"reflect"

	"github.com/signadot/tony-format/go-tony/ir"
	"golang.org/x/tools/go/packages"
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

	// Load current package to resolve local types
	currentPkg, err := resolver.loader.LoadPackage(pkgDir)
	if err != nil {
		return fmt.Errorf("failed to load current package %q: %w", pkgDir, err)
	}

	// Load encoding package to get TextMarshaler/TextUnmarshaler interfaces
	encodingPkg, err := resolver.loader.LoadPackage("encoding")
	if err != nil {
		return fmt.Errorf("failed to load encoding package: %w", err)
	}

	textMarshalerObj := encodingPkg.Types.Scope().Lookup("TextMarshaler")
	if textMarshalerObj == nil {
		return fmt.Errorf("encoding.TextMarshaler not found")
	}
	textMarshalerInterface := textMarshalerObj.Type().Underlying().(*types.Interface)

	textUnmarshalerObj := encodingPkg.Types.Scope().Lookup("TextUnmarshaler")
	if textUnmarshalerObj == nil {
		return fmt.Errorf("encoding.TextUnmarshaler not found")
	}
	textUnmarshalerInterface := textUnmarshalerObj.Type().Underlying().(*types.Interface)

	// First pass: create placeholder types for all structs/types
	for _, s := range structs {
		// If it's a struct, create a placeholder struct type
		if _, ok := s.ASTNode.(*ast.StructType); ok {
			resolver.structTypeMap[s.Name] = reflect.StructOf([]reflect.StructField{})
		} else {
			// For other types, try to resolve the underlying type immediately
			// This is needed for recursive types or just to have the type available
			typ, _, _, _, _, err := resolver.resolveASTType(s.ASTNode, s.Name, s.Imports, currentPkg)
			if err == nil {
				resolver.structTypeMap[s.Name] = typ
			} else {
				// Fallback to int placeholder if resolution fails (e.g. recursive or complex)
				resolver.structTypeMap[s.Name] = reflect.TypeOf(int(0))
			}
		}
	}

	// Second pass: resolve field types and struct/type info
	for _, s := range structs {
		// Resolve the type of the struct/type itself
		var typ reflect.Type
		var typesType types.Type

		// For struct types, use the placeholder we created in the first pass
		if _, ok := s.ASTNode.(*ast.StructType); ok {
			typ = resolver.structTypeMap[s.Name]
			// Look up types.Type from the current package
			if obj := currentPkg.Types.Scope().Lookup(s.Name); obj != nil {
				typesType = obj.Type()
			}
		} else {
			// For non-struct types (e.g., type A int), resolve the underlying type
			var err error
			typ, typesType, _, _, _, err = resolver.resolveASTType(s.ASTNode, s.Name, s.Imports, currentPkg)
			if err != nil {
				return fmt.Errorf("failed to resolve type for %q: %w", s.Name, err)
			}
		}
		s.Type = typ

		// Check for TextMarshaler/TextUnmarshaler implementation on the type itself
		if typesType != nil {
			// Check value type
			if types.Implements(typesType, textMarshalerInterface) {
				s.ImplementsTextMarshaler = true
			} else if types.Implements(types.NewPointer(typesType), textMarshalerInterface) {
				s.ImplementsTextMarshaler = true
			}

			if types.Implements(typesType, textUnmarshalerInterface) {
				s.ImplementsTextUnmarshaler = true
			} else if types.Implements(types.NewPointer(typesType), textUnmarshalerInterface) {
				s.ImplementsTextUnmarshaler = true
			}
		}

		// Resolve fields (only for structs)
		for _, field := range s.Fields {
			if field.Type != nil {
				continue // Already resolved
			}

			typ, typesType, structName, pkgPath, typeName, err := resolver.resolveASTType(field.ASTType, s.Name, s.Imports, currentPkg)
			if err != nil {
				return fmt.Errorf("failed to resolve type for field %q.%q: %w", s.Name, field.Name, err)
			}
			field.Type = typ
			// For slice/array types, structName contains the element type name
			// For pointer types, structName contains the pointed-to type name
			// We need to preserve this so we can generate correct code
			// IMPORTANT: structName is the named type (e.g., "PendingFileRef", "api.Patch")
			// not the underlying type. This is what we need for code generation.
			if structName != "" {
				field.StructTypeName = structName
			}
			if pkgPath != "" {
				field.TypePkgPath = pkgPath
			}
			if typeName != "" {
				field.TypeName = typeName
			}

			// Check for TextMarshaler/TextUnmarshaler implementation
			if typesType != nil {
				// Check value type
				if types.Implements(typesType, textMarshalerInterface) {
					field.ImplementsTextMarshaler = true
				} else if types.Implements(types.NewPointer(typesType), textMarshalerInterface) {
					// Check pointer receiver (if value type doesn't implement it)
					// Note: If the field is already a pointer, typesType is a Pointer, so NewPointer makes it **T
					// But standard library usually implements on *T.
					// If field is T, we check *T.
					// If field is *T, types.Implements(typesType) covers it.
					field.ImplementsTextMarshaler = true
				}

				if types.Implements(typesType, textUnmarshalerInterface) {
					field.ImplementsTextUnmarshaler = true
				} else if types.Implements(types.NewPointer(typesType), textUnmarshalerInterface) {
					field.ImplementsTextUnmarshaler = true
				}
			}
		}
	}

	return nil
}

// resolveASTType resolves an AST type expression to a reflect.Type and types.Type.
// Returns: (reflectType, typesType, structName, pkgPath, typeName, error)
func (r *TypeResolver) resolveASTType(expr ast.Expr, currentStructName string, imports map[string]string, currentPkg *packages.Package) (reflect.Type, types.Type, string, string, string, error) {
	if expr == nil {
		return nil, nil, "", "", "", fmt.Errorf("nil type expression")
	}

	switch t := expr.(type) {
	case *ast.Ident:
		// Basic type or named type
		reflectType, structName, pkgPath, typeName, err := r.resolveIdentType(t.Name, currentStructName)
		if err != nil {
			return nil, nil, "", "", "", err
		}

		// Resolve types.Type
		var typesType types.Type
		if basic := types.Universe.Lookup(t.Name); basic != nil {
			typesType = basic.Type()
		} else {
			// Look up in current package
			if obj := currentPkg.Types.Scope().Lookup(t.Name); obj != nil {
				typesType = obj.Type()
				// If structName is empty but this is a named type, use the name
				// This handles cases where resolveIdentType didn't find it in structTypeMap
				// but go/types knows about it
				if structName == "" && obj.Name() != "" {
					// Check if it's a struct type
					if _, ok := typesType.Underlying().(*types.Struct); ok {
						structName = obj.Name()
						// Also set pkgPath if it's from another package
						if obj.Pkg() != nil && obj.Pkg().Path() != currentPkg.PkgPath {
							pkgPath = obj.Pkg().Path()
							typeName = obj.Name()
						}
					}
				}
			} else {
				// Might be a type that hasn't been type-checked yet or is missing
				// For now, we can't get the types.Type if it's not in the scope
				// This might happen if we're parsing a file that hasn't been fully loaded/checked
			}
		}

		return reflectType, typesType, structName, pkgPath, typeName, nil

	case *ast.StarExpr:
		// Pointer type: *T
		elemType, elemTypesType, structName, pkgPath, typeName, err := r.resolveASTType(t.X, currentStructName, imports, currentPkg)
		if err != nil {
			return nil, nil, "", "", "", err
		}
		var typesType types.Type
		if elemTypesType != nil {
			typesType = types.NewPointer(elemTypesType)
		}
		return reflect.PtrTo(elemType), typesType, structName, pkgPath, typeName, nil

	case *ast.ArrayType:
		// Array or slice: []T or [N]T
		elemType, elemTypesType, structName, pkgPath, typeName, err := r.resolveASTType(t.Elt, currentStructName, imports, currentPkg)
		if err != nil {
			return nil, nil, "", "", "", err
		}

		var typesType types.Type
		if elemTypesType != nil {
			if t.Len == nil {
				// Slice
				typesType = types.NewSlice(elemTypesType)
			} else {
				// Array - we don't support array length in types.Type construction easily here without evaluating t.Len
				// For now, treat as slice or ignore typesType for arrays
				typesType = types.NewSlice(elemTypesType) // Approximation
			}
		}

		if t.Len == nil {
			// Slice
			return reflect.SliceOf(elemType), typesType, structName, pkgPath, typeName, nil
		}
		// Array - for now, treat as slice (we can't know the length at compile time for codegen)
		return reflect.SliceOf(elemType), typesType, structName, pkgPath, typeName, nil

	case *ast.MapType:
		// Map: map[K]V
		keyType, keyTypesType, _, _, _, err := r.resolveASTType(t.Key, currentStructName, imports, currentPkg)
		if err != nil {
			return nil, nil, "", "", "", err
		}
		valueType, valueTypesType, structName, pkgPath, typeName, err := r.resolveASTType(t.Value, currentStructName, imports, currentPkg)
		if err != nil {
			return nil, nil, "", "", "", err
		}

		var typesType types.Type
		if keyTypesType != nil && valueTypesType != nil {
			typesType = types.NewMap(keyTypesType, valueTypesType)
		}

		return reflect.MapOf(keyType, valueType), typesType, structName, pkgPath, typeName, nil

	case *ast.SelectorExpr:
		// Qualified identifier: pkg.Type (cross-package)
		if ident, ok := t.X.(*ast.Ident); ok {
			// Special case for ir.Node to return the actual type
			if ident.Name == "ir" && t.Sel.Name == "Node" {
				return reflect.TypeOf(ir.Node{}), nil, "ir.Node", "", "", nil
			}

			// Resolve cross-package type
			pkgName := ident.Name
			typeName := t.Sel.Name

			// Find import path
			importPath, ok := imports[pkgName]
			if !ok {
				return nil, nil, "", "", "", fmt.Errorf("unknown package %q (missing import?)", pkgName)
			}

			// Load package
			pkg, err := r.loader.LoadPackage(importPath)
			if err != nil {
				return nil, nil, "", "", "", fmt.Errorf("failed to load package %q: %w", importPath, err)
			}

			// Find type in package
			_, typesType, err := r.loader.FindNamedType(pkg, typeName)
			if err != nil {
				return nil, nil, "", "", "", fmt.Errorf("failed to find type %q in package %q: %w", typeName, importPath, err)
			}

			// Handle the underlying type for reflection
			if structType, ok := typesType.Underlying().(*types.Struct); ok {
				// It's a struct
				qualifiedName := pkgName + "." + typeName
				_ = structType
				return reflect.StructOf([]reflect.StructField{}), typesType, qualifiedName, importPath, typeName, nil
			}

			if basic, ok := typesType.Underlying().(*types.Basic); ok {
				// It's a basic type
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
				default:
					// Fallback for other types
					reflectType = reflect.TypeOf(int(0)) // Placeholder
				}
				return reflectType, typesType, qualifiedName, importPath, typeName, nil
			}

			// For other types (interfaces, etc.), return placeholder
			// But still return the qualified name so it can be used for code generation
			// This handles cases where the type is not a struct but we still need the name
			qualifiedName := pkgName + "." + typeName
			return reflect.TypeOf((*interface{})(nil)).Elem(), typesType, qualifiedName, importPath, typeName, nil
		}

		return nil, nil, "", "", "", fmt.Errorf("unsupported selector expression: %v", expr)

	case *ast.ChanType:
		// Channel: chan T
		elemType, elemTypesType, structName, _, _, err := r.resolveASTType(t.Value, currentStructName, imports, currentPkg)
		if err != nil {
			return nil, nil, "", "", "", err
		}
		var typesType types.Type
		if elemTypesType != nil {
			typesType = types.NewChan(types.SendRecv, elemTypesType)
		}
		return reflect.ChanOf(reflect.BothDir, elemType), typesType, structName, "", "", nil

	case *ast.InterfaceType:
		// Interface type (interface{})
		return reflect.TypeOf((*interface{})(nil)).Elem(), types.NewInterfaceType(nil, nil), "", "", "", nil

	default:
		return nil, nil, "", "", "", fmt.Errorf("unsupported type expression: %T", expr)
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
	// Return the name as structName so it can be used for code generation
	// even though the reflect.Type itself is a placeholder with Name() == ""
	if typ, ok := r.structTypeMap[name]; ok {
		// Return the name as structName - this is the named type, not the underlying type
		return typ, name, "", "", nil
	}

	// Unknown type - could be a struct from another package or a type we haven't seen
	// For now, return an error - we'll need to handle cross-package types later
	// But since we are now handling local types via go/types in resolveASTType, we can be more lenient here
	// and return a placeholder if we can't resolve it via structTypeMap or basic types.
	// However, resolveIdentType is called for *ast.Ident, so it expects a reflect.Type.
	// If it's a named type in the current package that is NOT a struct (e.g. type A int),
	// we might not have it in structTypeMap.

	// We'll return a placeholder int for now if it's not found, assuming it's a named basic type.
	// The caller (resolveASTType) will get the real types.Type from go/types.
	return reflect.TypeOf(int(0)), "", "", "", nil
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
