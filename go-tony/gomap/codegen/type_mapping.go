package codegen

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"reflect"

	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
)

// typeToSchemaRef converts a Go type to a schema reference string.
// This is used for parameterized types like .[array(t)], .[object(t)], etc.
//
// Returns strings like:
//   - "string" for string types
//   - "int" for integer types
//   - "float" for float types
//   - "bool" for bool types
//   - "person" for a struct with schemadef=person
//   - "format:format" for cross-package types like format.Format
//
// Returns an error if:
//   - A struct type has no schema definition
//   - The type cannot be mapped to a schema reference
func typeToSchemaRef(typ reflect.Type, fieldInfo *FieldInfo, structMap map[string]*StructInfo, currentPkg string, currentStructName string, currentSchemaName string, loader *PackageLoader, imports map[string]string) (string, error) {
	if typ == nil {
		return "", fmt.Errorf("type is nil")
	}

	kind := typ.Kind()

	// Check for cross-package named types (including basic types like format.Format)
	pkgPath := typ.PkgPath()
	typeName := typ.Name()

	// Override with fieldInfo if available
	if fieldInfo != nil {
		if fieldInfo.TypePkgPath != "" {
			pkgPath = fieldInfo.TypePkgPath
		}
		if fieldInfo.TypeName != "" {
			typeName = fieldInfo.TypeName
		}
	}

	if pkgPath != "" && pkgPath != currentPkg && typeName != "" {
		// Cross-package type
		pkgName := filepath.Base(pkgPath)
		lowerTypeName := strings.ToLower(typeName)
		imports[pkgPath] = pkgName
		return fmt.Sprintf("%s:%s", pkgName, lowerTypeName), nil
	}

	// Special handling for *ir.Node
	if kind == reflect.Ptr && typ.Elem().PkgPath() == "github.com/signadot/tony-format/go-tony/ir" && typ.Elem().Name() == "Node" {
		imports["github.com/signadot/tony-format/go-tony/schema"] = "tony-base"
		return "tony-base:ir", nil
	}

	// Handle basic types
	switch kind {
	case reflect.String:
		return "string", nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int", nil
	case reflect.Float32, reflect.Float64:
		return "float", nil
	case reflect.Bool:
		return "bool", nil
	}

	// Handle struct types - check for schema references
	if kind == reflect.Struct {
		// First, check if this is a struct with a schema using fieldInfo
		if fieldInfo != nil && fieldInfo.StructTypeName != "" {
			if structInfo, ok := structMap[fieldInfo.StructTypeName]; ok {
				if structInfo.StructSchema != nil && structInfo.StructSchema.SchemaName != "" {
					return structInfo.StructSchema.SchemaName, nil
				}
			}
		}

		// Struct without schema - this is an error for parameterized types
		// We need an explicit schema reference
		structName := typ.Name()
		if fieldInfo != nil && fieldInfo.StructTypeName != "" {
			structName = fieldInfo.StructTypeName
		}
		if structName == "" {
			structName = "anonymous struct"
		}
		return "", fmt.Errorf("struct type %q has no schema definition (add schemadef= tag)", structName)
	}

	// For other types, try to use the type name
	if typ.Name() != "" {
		return strings.ToLower(typ.Name()), nil
	}

	// Fallback: return "object" for unknown types
	// This might not be ideal, but it's better than failing
	return "object", nil
}

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
	// Special handling for *ir.Node - represents any Tony value
	if kind == reflect.Ptr && typ.Elem().PkgPath() == "github.com/signadot/tony-format/go-tony/ir" && typ.Elem().Name() == "Node" {
		// *ir.Node maps to tony-base:ir
		// Add import for tony-base schema
		imports["github.com/signadot/tony-format/go-tony/schema"] = "tony-base"

		// Return reference to ir definition in tony-base
		return ir.FromString(".[tony-base:ir]"), nil
	}

	// Handle pointers (nullable types)
	if kind == reflect.Ptr {
		elemType := typ.Elem()
		// Get the schema reference for the element type
		elemTypeRef, err := typeToSchemaRef(elemType, fieldInfo, structMap, currentPkg, currentStructName, currentSchemaName, loader, imports)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema reference for nullable element type: %w", err)
		}
		// Create .[nullable(elemTypeRef)] format
		// This uses the parameterized nullable type from base.tony
		return ir.FromString(fmt.Sprintf(".[nullable(%s)]", elemTypeRef)), nil
	}

	// Handle slices (arrays)
	if kind == reflect.Slice || kind == reflect.Array {
		elemType := typ.Elem()
		// Get the schema reference for the element type
		elemTypeRef, err := typeToSchemaRef(elemType, fieldInfo, structMap, currentPkg, currentStructName, currentSchemaName, loader, imports)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema reference for slice element type: %w", err)
		}
		// Create .[array(elemTypeRef)] format
		// This uses the parameterized array type from base.tony
		return ir.FromString(fmt.Sprintf(".[array(%s)]", elemTypeRef)), nil
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
					return ir.FromString(".[sparsearray]"), nil
				}
			}
			// Get the schema reference for the value type
			valTypeRef, err := typeToSchemaRef(valType, fieldInfo, structMap, currentPkg, currentStructName, currentSchemaName, loader, imports)
			if err != nil {
				return nil, fmt.Errorf("failed to get schema reference for sparse array value type: %w", err)
			}
			// Create .[sparsearray(valTypeRef)] format
			return ir.FromString(fmt.Sprintf(".[sparsearray(%s)]", valTypeRef)), nil
		}

		// Regular map[string]T → .[object(t)]
		if keyType.Kind() == reflect.String {
			// Get the schema reference for the value type
			valTypeRef, err := typeToSchemaRef(valType, fieldInfo, structMap, currentPkg, currentStructName, currentSchemaName, loader, imports)
			if err != nil {
				return nil, fmt.Errorf("failed to get schema reference for map value type: %w", err)
			}
			// Create .[object(valTypeRef)] format
			// This uses the parameterized object type from base.tony
			return ir.FromString(fmt.Sprintf(".[object(%s)]", valTypeRef)), nil
		}

		// Other map types (not string or uint32 keys) - fallback to generic object
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
			// Parse tag using our robust parser
			if val := reflect.StructTag(tag).Get("tony"); val != "" {
				parsed, err := ParseStructTag(val)
				if err == nil {
					if name, ok := parsed["schemadef"]; ok {
						schemaName = name
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

// astTypeToSchemaRef converts an AST type expression to a schema reference string.
func astTypeToSchemaRef(expr ast.Expr, structMap map[string]*StructInfo, currentPkg string, loader *PackageLoader, imports map[string]string) (string, error) {
	switch x := expr.(type) {
	case *ast.Ident:
		name := x.Name
		switch name {
		case "string":
			return "string", nil
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64":
			return "int", nil
		case "float32", "float64":
			return "float", nil
		case "bool":
			return "bool", nil
		default:
			// Check if it's a struct with schema
			if structInfo, ok := structMap[name]; ok && structInfo.Package == currentPkg {
				if structInfo.StructSchema != nil && structInfo.StructSchema.Mode == "schemadef" {
					return structInfo.StructSchema.SchemaName, nil
				}
			}
			// Unknown type - return object
			return "object", nil
		}
	case *ast.SelectorExpr:
		// Handle package.Type
		if pkgIdent, ok := x.X.(*ast.Ident); ok {
			pkgName := pkgIdent.Name
			typeName := strings.ToLower(x.Sel.Name)
			// Note: We don't have full package path here, so imports map might not be updated correctly
			// This is a limitation of AST-only mode
			return fmt.Sprintf("%s:%s", pkgName, typeName), nil
		}
		return "object", nil
	default:
		return "object", nil
	}
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
		// Pointer type *T -> .[nullable(t)]
		elemRef, err := astTypeToSchemaRef(x.X, structMap, currentPkg, loader, imports)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema reference for pointer element: %w", err)
		}
		return ir.FromString(fmt.Sprintf(".[nullable(%s)]", elemRef)), nil

	case *ast.ArrayType:
		// Slice []T or array [N]T -> .[array(t)]
		elemRef, err := astTypeToSchemaRef(x.Elt, structMap, currentPkg, loader, imports)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema reference for array element: %w", err)
		}
		return ir.FromString(fmt.Sprintf(".[array(%s)]", elemRef)), nil

	case *ast.MapType:
		// Map type map[K]V
		keyType := x.Key
		valRef, err := astTypeToSchemaRef(x.Value, structMap, currentPkg, loader, imports)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema reference for map value: %w", err)
		}

		// Check if key is uint32 (sparse array) -> .[sparsearray(t)]
		if keyIdent, ok := keyType.(*ast.Ident); ok && keyIdent.Name == "uint32" {
			return ir.FromString(fmt.Sprintf(".[sparsearray(%s)]", valRef)), nil
		}

		// Regular map[string]T -> .[object(t)]
		if keyIdent, ok := keyType.(*ast.Ident); ok && keyIdent.Name == "string" {
			return ir.FromString(fmt.Sprintf(".[object(%s)]", valRef)), nil
		}

		// Other map types - fallback to generic object
		node := ir.FromMap(map[string]*ir.Node{})
		node.Tag = "!irtype"
		return node, nil

	case *ast.SelectorExpr:
		// Qualified type (package.Type)
		if pkgIdent, ok := x.X.(*ast.Ident); ok {
			pkgName := pkgIdent.Name
			typeName := strings.ToLower(x.Sel.Name)

			node := ir.Null()
			node.Tag = fmt.Sprintf("!%s:%s", pkgName, typeName)
			return node, nil
		}

		node := ir.FromMap(map[string]*ir.Node{})
		node.Tag = "!irtype"
		return node, nil

	default:
		return nil, fmt.Errorf("unsupported AST type expression: %T", expr)
	}
}
