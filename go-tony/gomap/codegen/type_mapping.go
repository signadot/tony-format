package codegen

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"reflect"

	"strings"

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
func GoTypeToSchemaNode(typ reflect.Type, fieldInfo *FieldInfo, structMap map[string]*StructInfo, currentPkg string, currentStructName string, currentSchemaName string, loader *PackageLoader, imports map[string]string) (*ir.Node, error) {
	if typ == nil {
		return nil, fmt.Errorf("type is nil")
	}

	kind := typ.Kind()

	// Check for cross-package named types (including non-struct types like format.Format)
	// This must come before kind-based handling to catch named types from other packages

	// Determine package path and type name
	// Use reflect.Type info if available (for structs and some named types)
	// Fall back to fieldInfo if available (for named basic types where reflect.Type loses the name)
	pkgPath := typ.PkgPath()
	typeName := typ.Name()

	// Only use FieldInfo to override if we are NOT a container type (Ptr, Slice, Array, Map)
	// because FieldInfo typically refers to the element type for these containers
	isContainer := kind == reflect.Ptr || kind == reflect.Slice || kind == reflect.Array || kind == reflect.Map

	if pkgPath == "" && fieldInfo != nil && !isContainer {
		if fieldInfo.TypePkgPath != "" {
			pkgPath = fieldInfo.TypePkgPath
		}
		if fieldInfo.TypeName != "" {
			typeName = fieldInfo.TypeName
		}
	}

	if pkgPath != "" && pkgPath != currentPkg && typeName != "" && loader != nil {
		// This is a named type from another package
		pkgName := filepath.Base(pkgPath)          // Use last component as package name
		lowerTypeName := strings.ToLower(typeName) // Lowercase type name for schema reference

		// Add to imports
		imports[pkgPath] = pkgName

		// Create !pkgName:typeName reference
		// Format: !pkg:typename (e.g., !format:format)
		node := ir.Null()
		node.Tag = fmt.Sprintf("!%s:%s", pkgName, lowerTypeName)
		return node, nil
	}

	// Special handling for *ir.Node - represents any Tony value
	if kind == reflect.Ptr && typ.Elem().PkgPath() == "github.com/signadot/tony-format/go-tony/ir" && typ.Elem().Name() == "Node" {
		// *ir.Node is represented as: !or [!irtype null, !irtype {}]
		// This means "either null or any object"
		nullNode := ir.Null()
		nullNode.Tag = "!irtype"

		objNode := ir.FromMap(map[string]*ir.Node{})
		objNode.Tag = "!irtype"

		orNode := ir.FromSlice([]*ir.Node{
			nullNode,
			objNode,
		})
		orNode.Tag = "!or"
		return orNode, nil
	}

	// Handle pointers (nullable types)
	if kind == reflect.Ptr {
		elemType := typ.Elem()
		// Pass fieldInfo to recursive call because for pointers, FieldInfo usually describes the element type
		elemNode, err := GoTypeToSchemaNode(elemType, fieldInfo, structMap, currentPkg, currentStructName, currentSchemaName, loader, imports)
		if err != nil {
			return nil, fmt.Errorf("failed to convert pointer element type: %w", err)
		}
		// Create !or [null, T] for nullable types
		// The null should be tagged as !irtype
		nullNode := ir.Null()
		nullNode.Tag = "!irtype"

		// Create array with !or tag
		orNode := ir.FromSlice([]*ir.Node{
			nullNode,
			elemNode,
		})
		orNode.Tag = "!or"
		return orNode, nil
	}

	// Handle slices (arrays)
	if kind == reflect.Slice || kind == reflect.Array {
		elemType := typ.Elem()
		// Pass fieldInfo to recursive call because for slices, FieldInfo usually describes the element type
		elemNode, err := GoTypeToSchemaNode(elemType, fieldInfo, structMap, currentPkg, currentStructName, currentSchemaName, loader, imports)
		if err != nil {
			return nil, fmt.Errorf("failed to convert slice element type: %w", err)
		}
		// Create !and [.[array], elemType] format
		// This represents "it's an array AND each element is of type elemType"
		arrayNode := ir.FromSlice([]*ir.Node{
			ir.FromString(".[array]"),
			elemNode,
		})
		arrayNode.Tag = "!and"
		return arrayNode, nil
	}

	// Handle maps
	if kind == reflect.Map {
		keyType := typ.Key()
		valType := typ.Elem()

		// Check if this is a sparse array (map[uint32]T)
		if keyType.Kind() == reflect.Uint32 {
			// Check if this is a self-reference (map[uint32]CurrentStruct)
			if valType.Kind() == reflect.Struct && fieldInfo != nil && fieldInfo.StructTypeName != "" {
				// If the struct type name matches the current struct being defined, use compact form
				if fieldInfo.StructTypeName == currentStructName {
					// Create .[sparsearray] reference (compact form for self-reference)
					// Return as a string node directly, not wrapped in a slice
					return ir.FromString(".[sparsearray]"), nil
				}
				// Check if this struct has a schema definition
				if structInfo, ok := structMap[fieldInfo.StructTypeName]; ok {
					if structInfo.StructSchema != nil && structInfo.StructSchema.Mode == "schemadef" {
						schemaName := structInfo.StructSchema.SchemaName
						// Create .sparsearray(!all.schema(schemaName) null) reference
						// Note: sparsearray expects a schema reference or type definition
						valNode := ir.Null()
						valNode.Tag = fmt.Sprintf("!all.schema(%s)", schemaName)
						return ir.FromSlice([]*ir.Node{
							ir.FromString(".sparsearray"),
							valNode,
						}), nil
					}
				}
			}
			// Pass fieldInfo to recursive call for value type
			valNode, err := GoTypeToSchemaNode(valType, fieldInfo, structMap, currentPkg, currentStructName, currentSchemaName, loader, imports)
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
		_, err := GoTypeToSchemaNode(valType, nil, structMap, currentPkg, currentStructName, currentSchemaName, loader, imports)
		if err != nil {
			return nil, fmt.Errorf("failed to convert map value type: %w", err)
		}
		// Represent as object type (keys are strings)
		node := ir.FromMap(map[string]*ir.Node{})
		node.Tag = "!irtype"
		return node, nil
	}

	// Handle basic types
	switch kind {
	case reflect.String:
		// Create !irtype "" format (tagged string node)
		node := ir.FromString("")
		node.Tag = "!irtype"
		return node, nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		// Create a compact !irtype 1 format (tagged number node)
		node := ir.FromInt(1)
		node.Tag = "!irtype"
		return node, nil

	case reflect.Bool:
		// Create !irtype true format (tagged bool node)
		node := ir.FromBool(true)
		node.Tag = "!irtype"
		return node, nil

	case reflect.Interface:
		// interface{} → any type (no constraint)
		// For schema generation, we might want to use a more permissive type
		// For now, represent as object (most common use case)
		node := ir.FromMap(map[string]*ir.Node{})
		node.Tag = "!irtype"
		return node, nil

	case reflect.Struct:
		structTypeName := ""
		if fieldInfo != nil {
			structTypeName = fieldInfo.StructTypeName
		}
		return goStructToSchemaNode(typ, structMap, currentPkg, structTypeName, currentStructName, currentSchemaName, loader, imports)

	default:
		return nil, fmt.Errorf("unsupported type for schema generation: %s", typ)
	}
}

// goStructToSchemaNode converts a Go struct type to a schema node.
// If the struct has a schemadef= tag, it creates a schema reference.
// Otherwise, it creates an inline object definition.
func goStructToSchemaNode(typ reflect.Type, structMap map[string]*StructInfo, currentPkg string, structTypeName string, currentStructName string, currentSchemaName string, loader *PackageLoader, imports map[string]string) (*ir.Node, error) {
	// Determine the actual struct name to use for lookup
	// Prefer structTypeName (from AST) over typ.Name() (which may be empty for placeholder structs)
	lookupName := structTypeName
	if lookupName == "" {
		lookupName = typ.Name()
	}

	// If we have a struct name, look it up
	if lookupName != "" {
		// If this is a self-reference, use compact form
		if lookupName == currentStructName {
			// Self-reference - use !all.schema(currentSchemaName) null
			node := ir.Null()
			node.Tag = fmt.Sprintf("!all.schema(%s)", currentSchemaName)
			return node, nil
		}
		// Look up struct in our map
		if structInfo, ok := structMap[lookupName]; ok {
			if structInfo.StructSchema != nil && structInfo.StructSchema.Mode == "schemadef" {
				schemaName := structInfo.StructSchema.SchemaName
				// Create !all.schema(schemaName) null reference
				node := ir.Null()
				node.Tag = fmt.Sprintf("!all.schema(%s)", schemaName)
				return node, nil
			}
		}
		// Debug: if lookupName is set but not found, it means the struct isn't in the map
		// This can happen if the struct doesn't have a schemadef= tag or isn't being processed
	}

	// Fallback: try to look up by type name and package path (for non-placeholder structs)
	structName := typ.Name()
	pkgPath := typ.PkgPath()
	if structName != "" {
		var structInfo *StructInfo
		for name, info := range structMap {
			if name == structName && info.Package == pkgPath {
				structInfo = info
				break
			}
		}
		if structInfo != nil && structInfo.StructSchema != nil && structInfo.StructSchema.Mode == "schemadef" {
			schemaName := structInfo.StructSchema.SchemaName
			// Create !all.schema(schemaName) null reference
			node := ir.Null()
			node.Tag = fmt.Sprintf("!all.schema(%s)", schemaName)
			return node, nil
		}
	}

	// Check for cross-package reference
	if typ.PkgPath() != "" && typ.PkgPath() != currentPkg && loader != nil {
		// Load the package
		pkg, err := loader.LoadPackage(typ.PkgPath())
		if err != nil {
			return nil, fmt.Errorf("failed to load package %q for type %q: %w", typ.PkgPath(), typ.Name(), err)
		}

		// Find the struct type
		_, structType, err := loader.FindStructType(pkg, typ.Name())
		if err != nil {
			// It might not be a struct (e.g. named basic type), but we only handle structs with schemas here
			// For named basic types, we might want to resolve to their underlying type, but that's handled elsewhere?
			// Actually, GoTypeToSchemaNode calls this for reflect.Struct.
			return nil, fmt.Errorf("failed to find struct type %q in package %q: %w", typ.Name(), typ.PkgPath(), err)
		}

		// Look for schemadef tag in fields
		var schemaName string
		for i := 0; i < structType.NumFields(); i++ {
			tag := structType.Tag(i)
			if tag == "" {
				continue
			}
			// Parse tag manually or use a helper
			// We need to parse `tony:"schemadef=name"`
			// Simple parsing for now
			if val := reflect.StructTag(tag).Get("tony"); val != "" {
				// Parse tony tag value (comma separated key=value)
				// We can use ParseStructTags helper if we move it to a shared place or duplicate logic
				// For now, simple string search
				// TODO: Use proper tag parsing
				parts := strings.Split(val, ",")
				for _, part := range parts {
					kv := strings.SplitN(part, "=", 2)
					if len(kv) == 2 && strings.TrimSpace(kv[0]) == "schemadef" {
						schemaName = strings.TrimSpace(kv[1])
						break
					}
				}
			}
			if schemaName != "" {
				break
			}
		}

		if schemaName != "" {
			// Found a schema definition!
			// Add to imports
			pkgName := pkg.Name
			imports[typ.PkgPath()] = pkgName

			// Create !localPkg:schemaName reference
			// Format: !pkg:name (just the tag, no !irtype wrapper)
			node := ir.Null()
			node.Tag = fmt.Sprintf("!%s:%s", pkgName, schemaName)
			return node, nil
		}
	}

	// If we get here, we couldn't find a schema reference
	// This means the struct type doesn't have a schemadef= tag or isn't in the struct map
	// Return a placeholder object type (fallback)
	node := ir.FromMap(map[string]*ir.Node{})
	node.Tag = "!irtype"
	return node, nil
}

// ASTTypeToSchemaNode converts an AST type expression to an IR schema node.
// This is used when we only have AST information (before type resolution).
func ASTTypeToSchemaNode(expr ast.Expr, structMap map[string]*StructInfo, currentPkg string, loader *PackageLoader, imports map[string]string) (*ir.Node, error) {
	switch x := expr.(type) {
	case *ast.Ident:
		// Simple type name (string, int, bool, or custom type)
		name := x.Name
		switch name {
		case "string":
			node := ir.FromString("")
			node.Tag = "!irtype"
			return node, nil
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64",
			"float32", "float64":
			node := ir.FromInt(1)
			node.Tag = "!irtype"
			return node, nil
		case "bool":
			node := ir.FromBool(true)
			node.Tag = "!irtype"
			return node, nil
		default:
			// Custom type - check if it's a struct with schema
			if structInfo, ok := structMap[name]; ok && structInfo.Package == currentPkg {
				if structInfo.StructSchema != nil && structInfo.StructSchema.Mode == "schemadef" {
					schemaName := structInfo.StructSchema.SchemaName
					// Create !all.schema(schemaName) null reference
					node := ir.Null()
					node.Tag = fmt.Sprintf("!all.schema(%s)", schemaName)
					return node, nil
				}
			}
			// Unknown type - return placeholder
			node := ir.FromMap(map[string]*ir.Node{})
			node.Tag = "!irtype"
			return node, nil
		}

	case *ast.StarExpr:
		// Pointer type *T
		elemNode, err := ASTTypeToSchemaNode(x.X, structMap, currentPkg, loader, imports)
		if err != nil {
			return nil, fmt.Errorf("failed to convert pointer element: %w", err)
		}
		nullNode := ir.Null()
		nullNode.Tag = "!irtype"

		orNode := ir.FromSlice([]*ir.Node{
			nullNode,
			elemNode,
		})
		orNode.Tag = "!or"
		return orNode, nil

	case *ast.ArrayType:
		// Slice []T or array [N]T
		elemNode, err := ASTTypeToSchemaNode(x.Elt, structMap, currentPkg, loader, imports)
		if err != nil {
			return nil, fmt.Errorf("failed to convert array element: %w", err)
		}
		// Create !and [.[array], elemType] format
		arrayNode := ir.FromSlice([]*ir.Node{
			ir.FromString(".[array]"),
			elemNode,
		})
		arrayNode.Tag = "!and"
		return arrayNode, nil

	case *ast.MapType:
		// Map type map[K]V
		keyType := x.Key
		valNode, err := ASTTypeToSchemaNode(x.Value, structMap, currentPkg, loader, imports)
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
		node := ir.FromMap(map[string]*ir.Node{})
		node.Tag = "!irtype"
		return node, nil

	case *ast.SelectorExpr:
		// Qualified type (package.Type)
		// We can handle this if we have loader
		if loader != nil {
			// We need to resolve the package path for the selector
			// This is hard with just AST. We need type info.
			// But ASTTypeToSchemaNode is usually used when we DON'T have type info (or as fallback).
			// However, if we are in the generator, we might have enough info if we look at imports.
			// But for now, let's leave AST handling as is (fallback) or TODO.
			// Real cross-package resolution should happen via GoTypeToSchemaNode (reflection) which has PkgPath.
		}
		node := ir.FromMap(map[string]*ir.Node{})
		node.Tag = "!irtype"
		return node, nil

	default:
		return nil, fmt.Errorf("unsupported AST type expression: %T", expr)
	}
}
