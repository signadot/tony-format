package gomap

import (
	"fmt"
	"reflect"

	"github.com/signadot/tony-format/go-tony/ir"
)

// FromTonyIR converts a Tony IR node to a Go value using schema-aware unmarshaling.
// v must be a pointer to the target type.
// It automatically uses a FromTony() method if available (user-implemented or generated),
// otherwise falls back to schema-aware or reflection-based conversion.
func (m *Mapper) FromTonyIR(node *ir.Node, v interface{}, opts ...UnmapOption) error {
	if v == nil {
		return &UnmarshalError{Message: "destination value cannot be nil"}
	}

	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr {
		return &UnmarshalError{Message: "destination value must be a pointer"}
	}

	if val.IsNil() {
		return &UnmarshalError{Message: "destination pointer cannot be nil"}
	}

	elemVal := val.Elem()
	elemType := elemVal.Type()

	// Check for FromTony() method on the element type
	if method := elemVal.MethodByName("FromTonyIR"); method.IsValid() {
		return callFromTonyIR(method, node, opts...)
	}

	// Check for FromTony() method on pointer type
	ptrType := reflect.PtrTo(elemType)
	if _, ok := ptrType.MethodByName("FromTonyIR"); ok {
		// Call on the pointer value itself
		return callFromTonyIR(val.MethodByName("FromTonyIR"), node, opts...)
	}

	// Check for explicit schema tags
	structSchema, err := GetStructSchema(elemType)
	if err != nil {
		return err
	}

	if structSchema != nil && node.Type == ir.ObjectType && elemType.Kind() == reflect.Struct {
		// Schema-aware unmarshaling
		return m.fromIRWithSchema(node, elemVal, elemType, structSchema, opts...)
	}

	// Fall back to reflection-based conversion
	return fromIRReflect(node, elemVal, "")
}

// fromIRWithSchema performs schema-aware unmarshaling of a struct.
func (m *Mapper) fromIRWithSchema(node *ir.Node, val reflect.Value, typ reflect.Type, structSchema *StructSchema, opts ...UnmapOption) error {
	// Resolve schema via registry
	schema, err := m.resolveSchema(structSchema.SchemaName)
	if err != nil {
		return &UnmarshalError{
			Message: fmt.Sprintf("failed to resolve schema %q: %v", structSchema.SchemaName, err),
		}
	}

	if schema == nil {
		// Schema not found - fall back to reflection
		return fromIRReflect(node, val, "", opts...)
	}

	// Use GetStructFields to get field metadata
	fields, err := GetStructFields(typ, schema, structSchema.Mode,
		structSchema.AllowExtra, m.schemaRegistry)
	if err != nil {
		return &UnmarshalError{
			Message: fmt.Sprintf("failed to get struct fields: %v", err),
		}
	}

	// Build map of schema field names -> FieldInfo
	schemaFieldMap := make(map[string]*FieldInfo)
	for _, fieldInfo := range fields {
		schemaFieldMap[fieldInfo.SchemaFieldName] = fieldInfo
	}

	// Unmarshal using schema field names
	visited := make(map[uintptr]string) // Track visited pointers for cycle detection
	seenFields := make(map[string]bool)
	for i, fieldNameNode := range node.Fields {
		if i >= len(node.Values) {
			break
		}

		if fieldNameNode.Type != ir.StringType {
			continue
		}

		schemaFieldName := fieldNameNode.String
		fieldInfo, exists := schemaFieldMap[schemaFieldName]
		if !exists {
			if !structSchema.AllowExtra {
				return &UnmarshalError{
					FieldPath: schemaFieldName,
					Message:   fmt.Sprintf("extra field %q not in schema (use allowExtra flag to allow)", schemaFieldName),
				}
			}
			continue // Skip extra fields if allowExtra is true
		}

		seenFields[fieldInfo.Name] = true

		fieldVal := val.FieldByName(fieldInfo.Name)
		if !fieldVal.IsValid() || !fieldVal.CanSet() {
			continue
		}

		fieldNode := node.Values[i]
		if err := fromIRReflectWithVisited(fieldNode, fieldVal, schemaFieldName, visited, opts...); err != nil {
			return err
		}
	}

	// Validate required fields are present
	for _, fieldInfo := range fields {
		if !fieldInfo.Optional && !fieldInfo.Omit {
			if !seenFields[fieldInfo.Name] {
				return &UnmarshalError{
					FieldPath: fieldInfo.SchemaFieldName,
					Message:   fmt.Sprintf("required field %q is missing", fieldInfo.SchemaFieldName),
				}
			}
		}
	}

	// Handle comment/linecomment/tag fields
	if structSchema.CommentFieldName != "" {
		commentField := val.FieldByName(structSchema.CommentFieldName)
		if commentField.IsValid() && commentField.CanSet() {
			// Extract comments from IR node
			// Comments are stored in node.Comment (a CommentType node) or in node.Values
			// For now, extract comments from the comment node if present
			if commentField.Type() == reflect.TypeOf([]string(nil)) {
				comments := extractComments(node)
				commentField.Set(reflect.ValueOf(comments))
			}
		}
	}
	if structSchema.LineCommentFieldName != "" {
		lineCommentField := val.FieldByName(structSchema.LineCommentFieldName)
		if lineCommentField.IsValid() && lineCommentField.CanSet() {
			// Extract line comments from IR node
			// Line comments might be in node.Lines or node.Comment
			if lineCommentField.Type() == reflect.TypeOf([]string(nil)) {
				lineComments := extractLineComments(node)
				lineCommentField.Set(reflect.ValueOf(lineComments))
			}
		}
	}
	if structSchema.TagFieldName != "" {
		tagField := val.FieldByName(structSchema.TagFieldName)
		if tagField.IsValid() && tagField.CanSet() {
			// Populate from IR node tag
			if tagField.Type() == reflect.TypeOf("") {
				tagField.SetString(node.Tag)
			}
		}
	}

	return nil
}

// extractComments extracts all comments from an IR node as a []string.
// Comments can be in node.Comment (CommentType node) or in node.Values.
func extractComments(node *ir.Node) []string {
	if node == nil {
		return nil
	}

	var comments []string

	// Check if node has a Comment field (CommentType node)
	if node.Comment != nil {
		comments = append(comments, extractCommentsFromNode(node.Comment)...)
	}

	// Check Values for comment nodes
	for _, val := range node.Values {
		if val != nil && val.Type == ir.CommentType {
			comments = append(comments, extractCommentsFromNode(val)...)
		}
	}

	return comments
}

// extractCommentsFromNode extracts string values from a comment node.
func extractCommentsFromNode(node *ir.Node) []string {
	if node == nil {
		return nil
	}

	var comments []string

	// Comment nodes typically have string values in their Values array
	if node.Type == ir.CommentType {
		for _, val := range node.Values {
			if val != nil && val.Type == ir.StringType {
				comments = append(comments, val.String)
			}
		}
	}

	// Also check if the comment node itself has a String field
	if node.String != "" {
		comments = append(comments, node.String)
	}

	return comments
}

// extractLineComments extracts line comments from an IR node.
// Line comments are typically stored in node.Lines.
func extractLineComments(node *ir.Node) []string {
	if node == nil {
		return nil
	}

	// Line comments are stored in the Lines field
	if len(node.Lines) > 0 {
		return node.Lines
	}

	return nil
}
