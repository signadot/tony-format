package codegen

import (
	"fmt"
	"go/ast"
	"reflect"

	"github.com/signadot/tony-format/go-tony/ir"
)

// GoTypeToSchemaNode converts a Go type to an IR schema node.
// This is used for generating schema definitions from Go structs.
//
// Type mappings:
//   - string → !irtype ""
//   - int, int64, float64, etc. → !irtype 1 (number)
//   - bool → !irtype true
//   - *T → !or [null, T] (nullable)
//   - []T → .array(T) (array)
//   - struct → object with fields or .schemaName reference
func GoTypeToSchemaNode(typ reflect.Type, fieldInfo *FieldInfo, structMap map[string]*StructInfo, currentPkg string) (*ir.Node, error) {
	if typ == nil {
		return nil, fmt.Errorf("type is nil")
	}

	kind := typ.Kind()

	// Handle pointers (nullable types)
	if kind == reflect.Ptr {
		elemType := typ.Elem()
		elemNode, err := GoTypeToSchemaNode(elemType, nil, structMap, currentPkg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert pointer element type: %w", err)
		}
		// Create !or [null, T] for nullable types
		return ir.FromSlice([]*ir.Node{
			ir.FromString("!or"),
			ir.FromSlice([]*ir.Node{
				ir.FromString("!irtype"),
				ir.Null(),
			}),
			elemNode,
		}), nil
	}

	// Handle slices (arrays)
	if kind == reflect.Slice || kind == reflect.Array {
		elemType := typ.Elem()
		elemNode, err := GoTypeToSchemaNode(elemType, nil, structMap, currentPkg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert slice element type: %w", err)
		}
		// Create .array(T) reference
		return ir.FromSlice([]*ir.Node{
			ir.FromString(".array"),
			elemNode,
		}), nil
	}

	// Handle maps
	if kind == reflect.Map {
		keyType := typ.Key()
		valType := typ.Elem()

		// Check if this is a sparse array (map[uint32]T)
		if keyType.Kind() == reflect.Uint32 {
			valNode, err := GoTypeToSchemaNode(valType, nil, structMap, currentPkg)
			if err != nil {
				return nil, fmt.Errorf("failed to convert map value type: %w", err)
			}
			// Create .sparsearray(T) reference
			return ir.FromSlice([]*ir.Node{
				ir.FromString(".sparsearray"),
				valNode,
			}), nil
		}

		// Regular map[string]T → object with dynamic keys
		// For now, we'll represent this as an object type
		// TODO: Consider if we need a more specific schema representation
		_, err := GoTypeToSchemaNode(valType, nil, structMap, currentPkg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert map value type: %w", err)
		}
		// Represent as object type (keys are strings)
		return ir.FromSlice([]*ir.Node{
			ir.FromString("!irtype"),
			ir.FromMap(map[string]*ir.Node{}),
		}), nil
	}

	// Handle basic types
	switch kind {
	case reflect.String:
		return ir.FromSlice([]*ir.Node{
			ir.FromString("!irtype"),
			ir.FromString(""),
		}), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return ir.FromSlice([]*ir.Node{
			ir.FromString("!irtype"),
			ir.FromInt(1), // Number type
		}), nil

	case reflect.Bool:
		return ir.FromSlice([]*ir.Node{
			ir.FromString("!irtype"),
			ir.FromBool(true),
		}), nil

	case reflect.Interface:
		// interface{} → any type (no constraint)
		// For schema generation, we might want to use a more permissive type
		// For now, represent as object (most common use case)
		return ir.FromSlice([]*ir.Node{
			ir.FromString("!irtype"),
			ir.FromMap(map[string]*ir.Node{}),
		}), nil

	case reflect.Struct:
		return goStructToSchemaNode(typ, structMap, currentPkg)

	default:
		return nil, fmt.Errorf("unsupported type for schema generation: %s", typ)
	}
}

// goStructToSchemaNode converts a Go struct type to a schema node.
// If the struct has a schemadef= tag, it creates a schema reference.
// Otherwise, it creates an inline object definition.
func goStructToSchemaNode(typ reflect.Type, structMap map[string]*StructInfo, currentPkg string) (*ir.Node, error) {
	// Check if this struct has a schema definition
	structName := typ.Name()
	pkgPath := typ.PkgPath()

	// Look up struct in our map
	var structInfo *StructInfo
	for name, info := range structMap {
		if name == structName && info.Package == pkgPath {
			structInfo = info
			break
		}
	}

	// If struct has schemadef= tag, create a schema reference
	if structInfo != nil && structInfo.StructSchema != nil && structInfo.StructSchema.Mode == "schemadef" {
		schemaName := structInfo.StructSchema.SchemaName
		// Create .schemaName reference
		return ir.FromSlice([]*ir.Node{
			ir.FromString("." + schemaName),
		}), nil
	}

	// Otherwise, create inline object definition
	// This is a simplified version - full implementation would need to
	// recursively process all fields
	// For now, return a placeholder object type
	return ir.FromSlice([]*ir.Node{
		ir.FromString("!irtype"),
		ir.FromMap(map[string]*ir.Node{}),
	}), nil
}

// ASTTypeToSchemaNode converts an AST type expression to an IR schema node.
// This is used when we only have AST information (before type resolution).
func ASTTypeToSchemaNode(expr ast.Expr, structMap map[string]*StructInfo, currentPkg string) (*ir.Node, error) {
	switch x := expr.(type) {
	case *ast.Ident:
		// Simple type name (string, int, bool, or custom type)
		name := x.Name
		switch name {
		case "string":
			return ir.FromSlice([]*ir.Node{
				ir.FromString("!irtype"),
				ir.FromString(""),
			}), nil
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64",
			"float32", "float64":
			return ir.FromSlice([]*ir.Node{
				ir.FromString("!irtype"),
				ir.FromInt(1),
			}), nil
		case "bool":
			return ir.FromSlice([]*ir.Node{
				ir.FromString("!irtype"),
				ir.FromBool(true),
			}), nil
		default:
			// Custom type - check if it's a struct with schema
			if structInfo, ok := structMap[name]; ok && structInfo.Package == currentPkg {
				if structInfo.StructSchema != nil && structInfo.StructSchema.Mode == "schemadef" {
					schemaName := structInfo.StructSchema.SchemaName
					return ir.FromSlice([]*ir.Node{
						ir.FromString("." + schemaName),
					}), nil
				}
			}
			// Unknown type - return placeholder
			return ir.FromSlice([]*ir.Node{
				ir.FromString("!irtype"),
				ir.FromMap(map[string]*ir.Node{}),
			}), nil
		}

	case *ast.StarExpr:
		// Pointer type *T
		elemNode, err := ASTTypeToSchemaNode(x.X, structMap, currentPkg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert pointer element: %w", err)
		}
		return ir.FromSlice([]*ir.Node{
			ir.FromString("!or"),
			ir.FromSlice([]*ir.Node{
				ir.FromString("!irtype"),
				ir.Null(),
			}),
			elemNode,
		}), nil

	case *ast.ArrayType:
		// Slice []T or array [N]T
		elemNode, err := ASTTypeToSchemaNode(x.Elt, structMap, currentPkg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert array element: %w", err)
		}
		return ir.FromSlice([]*ir.Node{
			ir.FromString(".array"),
			elemNode,
		}), nil

	case *ast.MapType:
		// Map type map[K]V
		keyType := x.Key
		valNode, err := ASTTypeToSchemaNode(x.Value, structMap, currentPkg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert map value: %w", err)
		}

		// Check if key is uint32 (sparse array)
		if keyIdent, ok := keyType.(*ast.Ident); ok && keyIdent.Name == "uint32" {
			return ir.FromSlice([]*ir.Node{
				ir.FromString(".sparsearray"),
				valNode,
			}), nil
		}

		// Regular map[string]T
		return ir.FromSlice([]*ir.Node{
			ir.FromString("!irtype"),
			ir.FromMap(map[string]*ir.Node{}),
		}), nil

	case *ast.SelectorExpr:
		// Qualified type (package.Type)
		// For now, treat as unknown type
		return ir.FromSlice([]*ir.Node{
			ir.FromString("!irtype"),
			ir.FromMap(map[string]*ir.Node{}),
		}), nil

	default:
		return nil, fmt.Errorf("unsupported AST type expression: %T", expr)
	}
}
