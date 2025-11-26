package codegen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"

	"github.com/signadot/tony-format/go-tony/gomap"
)

// ParseFile parses a Go source file and returns its AST.
func ParseFile(filename string) (*ast.File, *token.FileSet, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse file %q: %w", filename, err)
	}
	return file, fset, nil
}

// ExtractTypes extracts all type declarations with schema tags from an AST file.
// Returns types with schema= or schemagen= tags.
func ExtractTypes(file *ast.File, filePath string) ([]*StructInfo, error) {
	var structs []*StructInfo

	// Extract imports
	imports := ExtractImports(file)

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			// Extract struct-level comments
			comments := ExtractComments(genDecl)

			var structSchema *gomap.StructSchema
			var err error

			// If it's a struct, check for schema tag on anonymous fields
			if structType, ok := typeSpec.Type.(*ast.StructType); ok {
				structSchema, err = extractStructSchemaTag(structType)
				if err != nil {
					return nil, fmt.Errorf("failed to extract schema tag from struct %q: %w", typeSpec.Name.Name, err)
				}
			}

			// If no struct tag (or not a struct), check for doc comment directives
			if structSchema == nil {
				structSchema, err = extractSchemaFromComments(comments)
				if err != nil {
					return nil, fmt.Errorf("failed to extract schema from comments for type %q: %w", typeSpec.Name.Name, err)
				}
			}

			// Only include types with schema= or schemagen= tags (or directives)
			if structSchema == nil {
				continue
			}

			// Extract fields (only for structs)
			var fields []*FieldInfo
			if structType, ok := typeSpec.Type.(*ast.StructType); ok {
				fields, err = extractFields(structType)
				if err != nil {
					return nil, fmt.Errorf("failed to extract fields from struct %q: %w", typeSpec.Name.Name, err)
				}
			}

			structs = append(structs, &StructInfo{
				Name:         typeSpec.Name.Name,
				Package:      file.Name.Name,
				FilePath:     filePath,
				Fields:       fields,
				StructSchema: structSchema,
				Comments:     comments,
				ASTNode:      typeSpec.Type,
				Imports:      imports,
			})
		}
	}

	return structs, nil
}

// ExtractImports extracts imports from an AST file.
// Returns a map of package name -> import path.
func ExtractImports(file *ast.File) map[string]string {
	imports := make(map[string]string)
	for _, imp := range file.Imports {
		var name string
		path := strings.Trim(imp.Path.Value, "\"")

		if imp.Name != nil {
			name = imp.Name.Name
		} else {
			// Default to the last component of the path
			parts := strings.Split(path, "/")
			name = parts[len(parts)-1]
		}
		imports[name] = path
	}
	return imports
}

// ExtractComments extracts comments from a declaration.
// Returns a slice of comment strings with "# " prefix (Tony format).
func ExtractComments(decl ast.Decl) []string {
	var comments []string

	genDecl, ok := decl.(*ast.GenDecl)
	if !ok {
		return comments
	}

	if genDecl.Doc != nil {
		for _, comment := range genDecl.Doc.List {
			text := comment.Text
			// Remove comment markers (//, /* */) and convert to Tony format (#)
			text = strings.TrimPrefix(text, "//")
			text = strings.TrimPrefix(text, "/*")
			text = strings.TrimSuffix(text, "*/")
			text = strings.TrimSpace(text)

			// Split multi-line comments and add "# " prefix to each line
			lines := strings.Split(text, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					// Add "# " prefix if not already present
					if !strings.HasPrefix(line, "#") {
						comments = append(comments, "# "+line)
					} else {
						// Already has #, ensure space after it
						if strings.HasPrefix(line, "#") && len(line) > 1 && line[1] != ' ' {
							line = "# " + strings.TrimPrefix(line, "#")
						}
						comments = append(comments, line)
					}
				}
			}
		}
	}

	return comments
}

// extractStructSchemaTag extracts the schema tag from a struct type.
// Looks for an anonymous field with tony:"schema=..." or tony:"schemagen=..." tag.
func extractStructSchemaTag(structType *ast.StructType) (*gomap.StructSchema, error) {
	if structType.Fields == nil {
		return nil, nil
	}

	for _, field := range structType.Fields.List {
		// Check if this is an anonymous field
		if len(field.Names) == 0 {
			tag := getFieldTag(field, "tony")
			if tag == "" {
				continue
			}

			// Parse the tag using our robust parser
			parsed, err := ParseStructTag(tag)
			if err != nil {
				return nil, fmt.Errorf("failed to parse struct tag: %w", err)
			}

			// Check for schema= or schemagen=
			var mode string
			var schemaName string

			if name, ok := parsed["schema"]; ok {
				mode = "schema"
				schemaName = name
			} else if name, ok := parsed["schemagen"]; ok {
				mode = "schemagen"
				schemaName = name
			} else {
				continue
			}

			if schemaName == "" {
				return nil, fmt.Errorf("schema tag requires a schema name")
			}

			allowExtra := false
			if _, ok := parsed["allowExtra"]; ok {
				allowExtra = true
			}

			commentFieldName := parsed["comment"]
			lineCommentFieldName := parsed["LineComment"]
			tagFieldName := parsed["tag"]
			context := parsed["context"]

			return &gomap.StructSchema{
				Mode:                 mode,
				SchemaName:           schemaName,
				Context:              context,
				AllowExtra:           allowExtra,
				CommentFieldName:     commentFieldName,
				LineCommentFieldName: lineCommentFieldName,
				TagFieldName:         tagFieldName,
			}, nil
		}
	}

	return nil, nil
}

// extractFields extracts field information from a struct type.
func extractFields(structType *ast.StructType) ([]*FieldInfo, error) {
	if structType.Fields == nil {
		return nil, nil
	}

	var fields []*FieldInfo

	for _, field := range structType.Fields.List {
		// Extract field comments
		comments := ExtractFieldComments(field)

		// Check if this is an embedded field
		isEmbedded := len(field.Names) == 0

		if isEmbedded {
			// Handle embedded field
			// For embedded fields, the field name is the type name
			fieldName, err := getEmbeddedFieldName(field.Type)
			if err != nil {
				return nil, fmt.Errorf("failed to get embedded field name: %w", err)
			}

			// Extract field comments
			comments := ExtractFieldComments(field)

			// Parse field tags (if any)
			tag := getFieldTag(field, "tony")
			parsed, err := gomap.ParseStructTag(tag)
			if err != nil {
				return nil, fmt.Errorf("failed to parse field tag for embedded field %q: %w", fieldName, err)
			}

			fieldInfo := &FieldInfo{
				Name:            fieldName,
				SchemaFieldName: fieldName, // Default to field name
				ASTType:         field.Type,
				Comments:        comments,
				ASTField:        field,
				IsEmbedded:      true,
			}

			// Extract field name override (unlikely for embedded fields, but possible)
			if name, ok := parsed["field"]; ok {
				fieldInfo.SchemaFieldName = name
			}

			// Extract omit flag
			if _, ok := parsed["omit"]; ok || parsed["field"] == "-" {
				fieldInfo.Omit = true
			}

			// Skip blank identifier fields (used for schema tags)
			if fieldInfo.Name == "_" {
				continue
			}

			// Skip embedded fields that are schema markers (have schema= or schemagen= tags)
			// These are used only for struct-level schema configuration, not as actual fields
			if _, hasSchema := parsed["schema"]; hasSchema {
				continue
			}
			if _, hasSchemagen := parsed["schemagen"]; hasSchemagen {
				continue
			}

			fields = append(fields, fieldInfo)
		} else {
			for _, name := range field.Names {
				// Skip unexported fields
				if !name.IsExported() {
					continue
				}

				// Skip blank identifier fields
				if name.Name == "_" {
					continue
				}

				// Parse field tags
				tag := getFieldTag(field, "tony")
				parsed, err := gomap.ParseStructTag(tag)
				if err != nil {
					return nil, fmt.Errorf("failed to parse field tag for %q: %w", name.Name, err)
				}

				// Extract field information
				fieldInfo := &FieldInfo{
					Name:            name.Name,
					SchemaFieldName: name.Name, // Default to field name
					ASTType:         field.Type,
					Comments:        comments,
					ASTField:        field,
					IsEmbedded:      false,
				}

				// Extract field name override
				if fieldName, ok := parsed["field"]; ok {
					fieldInfo.SchemaFieldName = fieldName
				}

				// Extract omit flag
				if _, ok := parsed["omit"]; ok || parsed["field"] == "-" {
					fieldInfo.Omit = true
				}

				// Extract required/optional flags
				if _, ok := parsed["required"]; ok {
					fieldInfo.Required = true
				}
				if _, ok := parsed["optional"]; ok {
					fieldInfo.Optional = true
				}

				// Validate: cannot have both required and optional
				if fieldInfo.Required && fieldInfo.Optional {
					return nil, fmt.Errorf("field %q cannot have both required and optional tags", name.Name)
				}

				fields = append(fields, fieldInfo)
			}
		}
	}

	return fields, nil
}

// ExtractFieldComments extracts comments from a field declaration.
// Returns a slice of comment strings with "# " prefix (Tony format).
func ExtractFieldComments(field *ast.Field) []string {
	var comments []string

	if field.Doc != nil {
		for _, comment := range field.Doc.List {
			text := comment.Text
			// Remove comment markers (//, /* */) and convert to Tony format (#)
			text = strings.TrimPrefix(text, "//")
			text = strings.TrimPrefix(text, "/*")
			text = strings.TrimSuffix(text, "*/")
			text = strings.TrimSpace(text)

			// Split multi-line comments and add "# " prefix to each line
			lines := strings.Split(text, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					// Add "# " prefix if not already present
					if !strings.HasPrefix(line, "#") {
						comments = append(comments, "# "+line)
					} else {
						// Already has #, ensure space after it
						if strings.HasPrefix(line, "#") && len(line) > 1 && line[1] != ' ' {
							line = "# " + strings.TrimPrefix(line, "#")
						}
						comments = append(comments, line)
					}
				}
			}
		}
	}

	return comments
}

// getFieldTag extracts a specific tag value from a field's tag string.
func getFieldTag(field *ast.Field, tagName string) string {
	if field.Tag == nil {
		return ""
	}

	tag := field.Tag.Value
	// Remove backticks
	tag = strings.Trim(tag, "`")

	// Parse the tag to find the specific tag name
	// Format: `key1:"value1" key2:"value2"`
	parts := strings.Fields(tag)
	for _, part := range parts {
		if strings.HasPrefix(part, tagName+":") {
			// Extract the value (remove quotes)
			value := strings.TrimPrefix(part, tagName+":")
			value = strings.Trim(value, `"`)
			return value
		}
	}

	return ""
}

// GetStructSchemaTag uses reflection to get the struct schema tag.
// This is a helper that wraps gomap.GetStructSchema for use with reflect.Type.
// Note: This requires the type to be fully resolved (not just AST).
// For AST-based parsing, use extractStructSchemaTag instead.
func GetStructSchemaTag(typ reflect.Type) (*gomap.StructSchema, error) {
	return gomap.GetStructSchema(typ)
}

// extractSchemaFromComments extracts schema information from doc comments.
// Looks for //tony: directives.
func extractSchemaFromComments(comments []string) (*gomap.StructSchema, error) {
	var combinedTagContent strings.Builder
	foundDirective := false

	for _, comment := range comments {
		// Check for # tony: prefix
		if strings.HasPrefix(comment, "# tony:") {
			tagContent := strings.TrimPrefix(comment, "# tony:")
			if foundDirective {
				combinedTagContent.WriteString(",")
			}
			combinedTagContent.WriteString(tagContent)
			foundDirective = true
		}
	}

	if foundDirective {
		return parseSchemaTagContent(combinedTagContent.String())
	}
	return nil, nil
}

// parseSchemaTagContent parses the content of a tony tag or directive.
func parseSchemaTagContent(content string) (*gomap.StructSchema, error) {
	parsed, err := ParseStructTag(content)
	if err != nil {
		return nil, err
	}

	var mode string
	var schemaName string

	if name, ok := parsed["schema"]; ok {
		mode = "schema"
		schemaName = name
	} else if name, ok := parsed["schemagen"]; ok {
		mode = "schemagen"
		schemaName = name
	} else {
		return nil, nil
	}

	if schemaName == "" {
		return nil, fmt.Errorf("schema tag requires a schema name")
	}

	allowExtra := false
	if _, ok := parsed["allowExtra"]; ok {
		allowExtra = true
	}

	return &gomap.StructSchema{
		Mode:                 mode,
		SchemaName:           schemaName,
		Context:              parsed["context"],
		AllowExtra:           allowExtra,
		CommentFieldName:     parsed["comment"],
		LineCommentFieldName: parsed["LineComment"],
		TagFieldName:         parsed["tag"],
	}, nil
}

// ResolveType attempts to resolve an AST type expression to a reflect.Type.
// This is a helper for when we need reflection-based type information.
// Returns nil if the type cannot be resolved (e.g., it's a local type that hasn't been loaded).
func ResolveType(expr ast.Expr, pkg *ast.Package) (reflect.Type, error) {
	// TODO: Implement type resolution from AST to reflect.Type
	// This will be needed for code generation but can be deferred to later phases
	return nil, fmt.Errorf("type resolution from AST not yet implemented")
}

// getEmbeddedFieldName extracts the field name from an embedded field type.
func getEmbeddedFieldName(expr ast.Expr) (string, error) {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name, nil
	case *ast.SelectorExpr:
		return x.Sel.Name, nil
	case *ast.StarExpr:
		return getEmbeddedFieldName(x.X)
	default:
		return "", fmt.Errorf("unsupported embedded field type: %T", expr)
	}
}
