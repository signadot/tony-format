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

	// The comments come off the node before its fields are read, and each carrier
	// gets what it names: a head comment is the wrapper's lines, a line comment is
	// the node's own. What this replaced looked for comment text in .Values and
	// .String of a comment node, and in .Lines of a value -- none of which is
	// where the IR keeps it -- so on this path the carriers were never filled at
	// all (3cdjz00jh12krns4g1n0).
	//
	// The unwrap is defensive rather than load-bearing: FromTonyIR routes only
	// ObjectType nodes here, so a wrapped document reaches the reflection path
	// instead. It costs nothing and stops this depending on that.
	if node != nil && node.Type == ir.CommentType {
		setCommentField(val, structSchema.CommentFieldName, node.Lines)
		node = ir.Uncomment(node)
	}
	if node != nil && node.Comment != nil {
		setCommentField(val, structSchema.LineCommentFieldName, node.Comment.Lines)
	}
	if node == nil {
		return nil
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

	// Handle tag field
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

// setCommentField puts comment lines on a named []string field, which is the
// only type these annotations accept -- comments are lines of text. An unnamed
// field (the annotation absent) is nothing to do.
func setCommentField(val reflect.Value, fieldName string, lines []string) {
	if fieldName == "" || len(lines) == 0 {
		return
	}
	f := val.FieldByName(fieldName)
	if !f.IsValid() || !f.CanSet() || f.Type() != reflect.TypeOf([]string(nil)) {
		return
	}
	f.Set(reflect.ValueOf(lines))
}
