package codegen

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

// GenerateSchema generates a schema IR node from a list of structs.
// The structs should already be in dependency order (from TopologicalSort).
//
// For each struct with schemadef= tag, it creates a schema definition in the define map.
// The schema has:
//   - signature.name: schema name from schemadef= tag
//   - define: map of struct definitions
func GenerateSchema(structs []*StructInfo) (*ir.Node, error) {
	if len(structs) == 0 {
		return nil, fmt.Errorf("no structs provided")
	}

	// Build a map of struct names to StructInfo for lookups
	structMap := make(map[string]*StructInfo)
	for _, s := range structs {
		structMap[s.Name] = s
	}

	// Find the primary schema (first struct with schemadef= tag)
	var primarySchema *StructInfo
	for _, s := range structs {
		if s.StructSchema != nil && s.StructSchema.Mode == "schemadef" {
			primarySchema = s
			break
		}
	}

	if primarySchema == nil {
		return nil, fmt.Errorf("no struct with schemadef= tag found")
	}

	schemaName := primarySchema.StructSchema.SchemaName
	currentPkg := primarySchema.Package

	// Build the define map
	defineMap := make(map[string]*ir.Node)

	// Process each struct with schemadef= tag
	for _, structInfo := range structs {
		if structInfo.StructSchema == nil || structInfo.StructSchema.Mode != "schemadef" {
			continue
		}

		defName := structInfo.StructSchema.SchemaName
		defNode, err := generateStructDefinition(structInfo, structMap, currentPkg)
		if err != nil {
			return nil, fmt.Errorf("failed to generate definition for %q: %w", defName, err)
		}

		defineMap[defName] = defNode
	}

	// Create the schema IR node
	schemaNode := ir.FromMap(map[string]*ir.Node{
		"signature": ir.FromMap(map[string]*ir.Node{
			"name": ir.FromString(schemaName),
		}),
		"define": ir.FromMap(defineMap),
	})

	return schemaNode, nil
}

// generateStructDefinition generates a schema definition node for a struct.
func generateStructDefinition(structInfo *StructInfo, structMap map[string]*StructInfo, currentPkg string) (*ir.Node, error) {
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

		if field.Type != nil {
			// Use reflection-based type if available
			fieldTypeNode, err = GoTypeToSchemaNode(field.Type, field, structMap, currentPkg)
		} else if field.ASTType != nil {
			// Fall back to AST-based type conversion
			fieldTypeNode, err = ASTTypeToSchemaNode(field.ASTType, structMap, currentPkg)
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

	// Encode schema to Tony format
	if err := encode.Encode(schema, file); err != nil {
		return fmt.Errorf("failed to encode schema: %w", err)
	}

	return nil
}
