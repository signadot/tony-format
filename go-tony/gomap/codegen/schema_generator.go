package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

// GenerateSchema generates a schema IR node for a specific struct.
// allStructs: all structs (used to build struct map for resolving references)
// targetStruct: the struct to generate the schema for (must have schemadef= tag)
//
// The schema has:
//   - signature.name: schema name from schemadef= tag
//   - define: map of struct definitions
func GenerateSchema(allStructs []*StructInfo, targetStruct *StructInfo, loader *PackageLoader) (*ir.Node, error) {
	if targetStruct == nil {
		return nil, fmt.Errorf("target struct is nil")
	}
	if targetStruct.StructSchema == nil || targetStruct.StructSchema.Mode != "schemadef" {
		return nil, fmt.Errorf("target struct %q does not have schemadef= tag", targetStruct.Name)
	}

	// Build a map of struct names to StructInfo for lookups (includes all structs)
	structMap := make(map[string]*StructInfo)
	for _, s := range allStructs {
		structMap[s.Name] = s
	}

	schemaName := targetStruct.StructSchema.SchemaName
	currentPkg := targetStruct.Package

	// Get context from struct tag, default to "tony-format/context"
	context := targetStruct.StructSchema.Context
	if context == "" {
		context = "tony-format/context"
	}

	// Imports map to collect cross-package references (pkgPath -> localName)
	imports := make(map[string]string)

	// Generate definition for the target struct
	defNode, err := generateStructDefinition(targetStruct, structMap, currentPkg, loader, imports)
	if err != nil {
		return nil, fmt.Errorf("failed to generate definition for %q: %w", schemaName, err)
	}

	// Build the define map - for single schema, put fields directly in define:
	defineMap := make(map[string]*ir.Node)
	var defineNode *ir.Node
	if defNode.Type == ir.ObjectType {
		// Convert object node to map and merge fields directly
		fieldMap := ir.ToMap(defNode)
		for k, v := range fieldMap {
			defineMap[k] = v
		}
		// Preserve the comment from defNode on the define node
		defineNode = ir.FromMap(defineMap)
		if defNode.Comment != nil {
			defineNode.Comment = defNode.Comment
		}
	} else {
		// Fallback: use the schema name as key
		defineMap[schemaName] = defNode
		defineNode = ir.FromMap(defineMap)
	}

	// Build context node
	// Start with the base context
	contextItems := []*ir.Node{ir.FromString(context)}

	// Add collected imports
	// We want deterministic order, but map iteration is random.
	// However, for now, let's just add them. Ideally we should sort by key.
	// TODO: Sort imports for stability
	importPaths := make([]string, 0, len(imports))
	for path := range imports {
		importPaths = append(importPaths, path)
	}
	// Sort paths
	sort.Strings(importPaths)

	for _, path := range importPaths {
		name := imports[path]
		// Create mapping: name: path
		item := ir.FromMap(map[string]*ir.Node{
			name: ir.FromString(path),
		})
		contextItems = append(contextItems, item)
	}

	contextNode := ir.FromSlice(contextItems)

	// Debug: Print available structs
	// for name := range structMap {
	// 	fmt.Printf("DEBUG: Available struct in map: %s\n", name)
	// }

	// Create the schema IR node
	schemaNode := ir.FromMap(map[string]*ir.Node{
		"context": contextNode,
		"signature": ir.FromMap(map[string]*ir.Node{
			"name": ir.FromString(schemaName),
		}),
		"define": defineNode,
	})

	return schemaNode, nil
}

// generateStructDefinition generates a schema definition node for a struct.
func generateStructDefinition(structInfo *StructInfo, structMap map[string]*StructInfo, currentPkg string, loader *PackageLoader, imports map[string]string) (*ir.Node, error) {
	// Create object type with fields
	fields := make(map[string]*ir.Node)

	for _, field := range structInfo.Fields {
		// Skip omitted fields
		if field.Omit {
			continue
		}

		// Skip unexported fields (handled by parser, but double-check)
		if len(field.Name) > 0 && field.Name[0] >= 'a' && field.Name[0] <= 'z' {
			continue
		}

		// Get schema field name (from field= tag or default to Name)
		fieldName := field.SchemaFieldName
		if fieldName == "" {
			fieldName = field.Name
		}

		// Convert field type to schema node
		var fieldTypeNode *ir.Node
		var err error

		if field.Name == "Format" {
			fmt.Printf("DEBUG: Format field - Type=%v, ASTType=%v\n", field.Type, field.ASTType != nil)
			if field.Type != nil {
				fmt.Printf("DEBUG: Format field Type details - Kind=%v, PkgPath=%q, Name=%q\n",
					field.Type.Kind(), field.Type.PkgPath(), field.Type.Name())
			}
		}

		if field.Type != nil {
			// Use reflection-based type if available
			// Pass the current struct name to detect self-references
			fieldTypeNode, err = GoTypeToSchemaNode(field.Type, field, structMap, currentPkg, structInfo.Name, structInfo.StructSchema.SchemaName, loader, imports)
		} else if field.ASTType != nil {
			// Fall back to AST-based type conversion
			fieldTypeNode, err = ASTTypeToSchemaNode(field.ASTType, structMap, currentPkg, loader, imports)
		} else {
			return nil, fmt.Errorf("field %q has no type information", field.Name)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to convert field %q type: %w", field.Name, err)
		}

		// Handle optional/required first (before adding comments)
		// If field is optional by tag (not pointer), wrap in !or [null, T]
		// Pointers are already handled by GoTypeToSchemaNode
		if field.Optional && field.Type != nil && field.Type.Kind() != reflect.Ptr {
			// Wrap non-pointer optional field
			fieldTypeNode = ir.FromSlice([]*ir.Node{
				ir.FromString("!or"),
				ir.FromSlice([]*ir.Node{
					ir.FromString("!irtype"),
					ir.Null(),
				}),
				fieldTypeNode,
			})
		}

		// Add field-level comments if present
		// Comments go on the outermost node (wrapped or not)
		if len(field.Comments) > 0 {
			if fieldTypeNode.Comment == nil {
				fieldTypeNode.Comment = &ir.Node{
					Type:  ir.CommentType,
					Lines: field.Comments,
				}
			} else {
				// Append to existing comments if any
				fieldTypeNode.Comment.Lines = append(fieldTypeNode.Comment.Lines, field.Comments...)
			}
		}

		fields[fieldName] = fieldTypeNode
	}

	// Create object definition node
	// In the define: section, we use plain objects (no !type tag needed)
	// The object structure with Fields/Values arrays is what GetStructFields expects
	objNode := ir.FromMap(fields)

	// Add struct-level comments if present
	if len(structInfo.Comments) > 0 {
		objNode.Comment = &ir.Node{
			Type:  ir.CommentType,
			Lines: structInfo.Comments,
		}
	}

	return objNode, nil
}

// WriteSchema writes a schema IR node to a .tony file.
func WriteSchema(schema *ir.Node, outputPath string) error {
	// Ensure directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dir, err)
	}

	// Open file for writing
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file %q: %w", outputPath, err)
	}
	defer file.Close()

	// Write generated comment header
	if _, err := fmt.Fprintf(file, "# Code generated by tony-codegen. DO NOT EDIT.\n"); err != nil {
		return fmt.Errorf("failed to write comment header: %w", err)
	}

	// Encode schema to Tony format
	if err := encode.Encode(schema, file); err != nil {
		return fmt.Errorf("failed to encode schema: %w", err)
	}

	return nil
}

// WriteSchemasToSingleFile writes multiple schema IR nodes to a single .tony file,
// separated by "---" document separators.
func WriteSchemasToSingleFile(schemas []*ir.Node, outputPath string) error {
	// Ensure directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dir, err)
	}

	// Open file for writing
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file %q: %w", outputPath, err)
	}
	defer file.Close()

	// Write generated comment header
	if _, err := fmt.Fprintf(file, "# Code generated by tony-codegen. DO NOT EDIT.\n"); err != nil {
		return fmt.Errorf("failed to write comment header: %w", err)
	}

	// Write each schema separated by ---
	for i, schema := range schemas {
		if i > 0 {
			// Write document separator
			if _, err := fmt.Fprintf(file, "---\n"); err != nil {
				return fmt.Errorf("failed to write document separator: %w", err)
			}
		}

		// Encode schema to Tony format
		if err := encode.Encode(schema, file); err != nil {
			return fmt.Errorf("failed to encode schema %d: %w", i, err)
		}
	}

	return nil
}
