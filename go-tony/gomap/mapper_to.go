package gomap

import (
	"fmt"
	"reflect"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

// ToTonyIR converts a Go value to a Tony IR node using schema-aware marshaling.
// It automatically uses a ToTony() method if available (user-implemented or generated),
// otherwise falls back to schema-aware or reflection-based conversion.
func (m *Mapper) ToTonyIR(v interface{}, opts ...encode.EncodeOption) (*ir.Node, error) {
	if v == nil {
		return ir.Null(), nil
	}

	val := reflect.ValueOf(v)
	typ := val.Type()

	// Check for ToTony() method on the value type (works for both value and pointer types)
	if method := val.MethodByName("ToTonyIR"); method.IsValid() {
		return callToTonyIR(method, opts...)
	}

	// If v is a value type, check if pointer type has the method
	if typ.Kind() != reflect.Ptr {
		ptrType := reflect.PtrTo(typ)
		if _, ok := ptrType.MethodByName("ToTonyIR"); ok {
			// Create a pointer to the value and call the method
			ptrVal := reflect.New(typ)
			ptrVal.Elem().Set(val)
			return callToTonyIR(ptrVal.MethodByName("ToTonyIR"), opts...)
		}
	}

	// Check for explicit schema tags
	structSchema, err := GetStructSchema(typ)
	if err != nil {
		return nil, err
	}

	if structSchema != nil && typ.Kind() == reflect.Struct {
		// Schema-aware marshaling
		return m.toIRWithSchema(val, typ, structSchema, opts...)
	}

	// Fall back to reflection-based conversion
	return toIRReflect(v)
}

// toIRWithSchema performs schema-aware marshaling of a struct.
func (m *Mapper) toIRWithSchema(val reflect.Value, typ reflect.Type, structSchema *StructSchema, opts ...encode.EncodeOption) (*ir.Node, error) {
	// Resolve schema via registry
	schema, err := m.resolveSchema(structSchema.SchemaName)
	if err != nil {
		return nil, &MarshalError{
			Message: fmt.Sprintf("failed to resolve schema %q: %v", structSchema.SchemaName, err),
		}
	}

	if schema == nil {
		// Schema not found - fall back to reflection
		return toIRReflect(val.Interface(), opts...)
	}

	// Use GetStructFields to get field metadata
	fields, err := GetStructFields(typ, schema, structSchema.Mode,
		structSchema.AllowExtra, m.schemaRegistry)
	if err != nil {
		return nil, &MarshalError{
			Message: fmt.Sprintf("failed to get struct fields: %v", err),
		}
	}

	// Build IR object using schema field names
	visited := make(map[uintptr]string) // Track visited pointers for cycle detection
	irMap := make(map[string]*ir.Node)
	for _, fieldInfo := range fields {
		if fieldInfo.Omit {
			continue
		}

		fieldVal := val.FieldByName(fieldInfo.Name)
		if !fieldVal.IsValid() {
			continue
		}

		// Skip optional zero values
		if fieldInfo.Optional && isZeroValue(fieldVal) {
			continue
		}

		// Use SchemaFieldName instead of Go field name
		fieldNode, err := toIRReflectValue(fieldVal, fieldInfo.SchemaFieldName, visited, opts...)
		if err != nil {
			return nil, &MarshalError{
				FieldPath: fieldInfo.SchemaFieldName,
				Message:   fmt.Sprintf("failed to marshal field: %v", err),
			}
		}
		irMap[fieldInfo.SchemaFieldName] = fieldNode
	}

	node := ir.FromMap(irMap)

	// Apply schema tag to IR node
	if structSchema.Mode == "schema" {
		node.Tag = fmt.Sprintf("!%s", structSchema.SchemaName)
	}

	// Handle comment/linecomment/tag fields
	if structSchema.CommentFieldName != "" {
		commentField := val.FieldByName(structSchema.CommentFieldName)
		if commentField.IsValid() && commentField.CanSet() {
			// Comments are stored on the IR node itself
			// We'll populate this when we have access to the source IR node
			// For now, this is a placeholder for when marshaling from IR -> IR
		}
	}
	if structSchema.LineCommentFieldName != "" {
		lineCommentField := val.FieldByName(structSchema.LineCommentFieldName)
		if lineCommentField.IsValid() && lineCommentField.CanSet() {
			// Line comments are stored on the IR node itself
		}
	}
	if structSchema.TagFieldName != "" {
		tagField := val.FieldByName(structSchema.TagFieldName)
		if tagField.IsValid() && tagField.CanSet() {
			// Tag is stored on the IR node itself
			// This would be populated when unmarshaling, not marshaling
		}
	}

	return node, nil
}

// isZeroValue checks if a reflect.Value represents the zero value for its type.
func isZeroValue(val reflect.Value) bool {
	if !val.IsValid() {
		return true
	}

	switch val.Kind() {
	case reflect.Bool:
		return !val.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return val.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return val.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return val.Float() == 0
	case reflect.Complex64, reflect.Complex128:
		return val.Complex() == 0
	case reflect.String:
		return val.String() == ""
	case reflect.Array:
		// Check if all elements are zero
		for i := 0; i < val.Len(); i++ {
			if !isZeroValue(val.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Map, reflect.Chan, reflect.Func, reflect.Interface, reflect.Ptr:
		return val.IsNil()
	case reflect.Struct:
		// Check if all fields are zero
		for i := 0; i < val.NumField(); i++ {
			if !isZeroValue(val.Field(i)) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
