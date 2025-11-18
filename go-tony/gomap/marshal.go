package gomap

import (
	"fmt"
	"reflect"

	"github.com/signadot/tony-format/go-tony/ir"
)

// ToIR converts a Go value to a Tony IR node.
// It automatically uses a ToTony() method if available (user-implemented or generated),
// otherwise falls back to reflection-based conversion.
func ToIR(v interface{}) (*ir.Node, error) {
	if v == nil {
		return ir.Null(), nil
	}

	val := reflect.ValueOf(v)
	typ := val.Type()

	// Check for ToTony() method on the value type (works for both value and pointer types)
	if method := val.MethodByName("ToTony"); method.IsValid() {
		return callToTony(method)
	}

	// If v is a value type, check if pointer type has the method
	if typ.Kind() != reflect.Ptr {
		ptrType := reflect.PtrTo(typ)
		if _, ok := ptrType.MethodByName("ToTony"); ok {
			// Create a pointer to the value and call the method
			ptrVal := reflect.New(typ)
			ptrVal.Elem().Set(val)
			return callToTony(ptrVal.MethodByName("ToTony"))
		}
	}

	// Fall back to reflection-based conversion
	return toIRReflect(v)
}

// callToTony calls the ToTony() method and returns the result.
func callToTony(method reflect.Value) (*ir.Node, error) {
	// Verify method signature: ToTony() (*ir.Node, error)
	mt := method.Type()
	if mt.NumIn() != 0 || mt.NumOut() != 2 {
		return nil, &MarshalError{
			Message: "ToTony() method must have signature: ToTony() (*ir.Node, error)",
		}
	}

	// Check return types
	if mt.Out(0) != reflect.TypeOf((*ir.Node)(nil)) || mt.Out(1) != reflect.TypeOf((*error)(nil)).Elem() {
		return nil, &MarshalError{
			Message: "ToTony() method must return (*ir.Node, error)",
		}
	}

	// Call the method
	results := method.Call(nil)
	node := results[0].Interface().(*ir.Node)
	err := results[1].Interface()
	if err != nil {
		return nil, err.(error)
	}
	return node, nil
}

// toIRReflect implements reflection-based conversion to IR.
// This is the fallback when generated code is not available.
func toIRReflect(v interface{}) (*ir.Node, error) {
	if v == nil {
		return ir.Null(), nil
	}

	val := reflect.ValueOf(v)
	return toIRReflectValue(val, "")
}

// toIRReflectValue converts a reflect.Value to an IR node.
// fieldPath is used for error reporting (e.g., "person.address.street").
func toIRReflectValue(val reflect.Value, fieldPath string) (*ir.Node, error) {
	// Handle invalid/zero values
	if !val.IsValid() {
		return ir.Null(), nil
	}

	typ := val.Type()
	kind := typ.Kind()

	// Handle pointers
	if kind == reflect.Ptr {
		if val.IsNil() {
			return ir.Null(), nil
		}
		// Dereference and recurse
		return toIRReflectValue(val.Elem(), fieldPath)
	}

	// Handle basic types
	switch kind {
	case reflect.String:
		return ir.FromString(val.String()), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return ir.FromInt(val.Int()), nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		// Convert uint to int64 (may overflow for very large uint64, but IR uses int64)
		return ir.FromInt(int64(val.Uint())), nil

	case reflect.Float32, reflect.Float64:
		return ir.FromFloat(val.Float()), nil

	case reflect.Bool:
		return ir.FromBool(val.Bool()), nil

	case reflect.Slice, reflect.Array:
		return toIRReflectSlice(val, fieldPath)

	case reflect.Map:
		return toIRReflectMap(val, fieldPath)

	case reflect.Struct:
		return toIRReflectStruct(val, fieldPath)

	case reflect.Interface:
		// If interface is nil, return null
		if val.IsNil() {
			return ir.Null(), nil
		}
		// Recurse on the underlying value
		return toIRReflectValue(val.Elem(), fieldPath)

	default:
		return nil, &MarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("unsupported type: %s", typ),
		}
	}
}

// toIRReflectSlice converts a slice or array to an IR array node.
func toIRReflectSlice(val reflect.Value, fieldPath string) (*ir.Node, error) {
	length := val.Len()
	elements := make([]*ir.Node, 0, length)

	for i := 0; i < length; i++ {
		elemPath := fieldPath
		if fieldPath != "" {
			elemPath = fmt.Sprintf("%s[%d]", fieldPath, i)
		} else {
			elemPath = fmt.Sprintf("[%d]", i)
		}

		elemVal := val.Index(i)
		elemNode, err := toIRReflectValue(elemVal, elemPath)
		if err != nil {
			return nil, err
		}
		elements = append(elements, elemNode)
	}

	return ir.FromSlice(elements), nil
}

// toIRReflectMap converts a map to an IR object node.
// Maps with uint32 keys are converted to sparse arrays (!sparsearray tag).
// Maps with string keys are converted to regular objects.
func toIRReflectMap(val reflect.Value, fieldPath string) (*ir.Node, error) {
	if val.IsNil() {
		return ir.Null(), nil
	}

	keyType := val.Type().Key().Kind()

	// Handle uint32 keys as sparse arrays
	if keyType == reflect.Uint32 {
		intKeysMap := make(map[uint32]*ir.Node)
		iter := val.MapRange()
		for iter.Next() {
			key := uint32(iter.Key().Uint())
			valueVal := iter.Value()

			valuePath := fieldPath
			if fieldPath != "" {
				valuePath = fmt.Sprintf("%s[%d]", fieldPath, key)
			} else {
				valuePath = fmt.Sprintf("[%d]", key)
			}

			valueNode, err := toIRReflectValue(valueVal, valuePath)
			if err != nil {
				return nil, err
			}
			intKeysMap[key] = valueNode
		}
		return ir.FromIntKeysMap(intKeysMap), nil
	}

	// Map keys must be strings for regular IR objects
	if keyType != reflect.String {
		return nil, &MarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("map keys must be strings or uint32, got %s", val.Type().Key()),
		}
	}

	irMap := make(map[string]*ir.Node)
	iter := val.MapRange()
	for iter.Next() {
		key := iter.Key().String()
		valueVal := iter.Value()

		valuePath := fieldPath
		if fieldPath != "" {
			valuePath = fmt.Sprintf("%s.%s", fieldPath, key)
		} else {
			valuePath = key
		}

		valueNode, err := toIRReflectValue(valueVal, valuePath)
		if err != nil {
			return nil, err
		}
		irMap[key] = valueNode
	}

	return ir.FromMap(irMap), nil
}

// toIRReflectStruct converts a struct to an IR object node.
// Embedded structs are flattened (fields are promoted to the parent object).
func toIRReflectStruct(val reflect.Value, fieldPath string) (*ir.Node, error) {
	typ := val.Type()
	irMap := make(map[string]*ir.Node)

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		fieldVal := val.Field(i)

		if field.Anonymous {
			// Handle embedded structs - flatten their fields
			if fieldVal.Kind() == reflect.Struct {
				// Recursively marshal embedded struct and merge its fields
				embeddedNode, err := toIRReflectValue(fieldVal, fieldPath)
				if err != nil {
					return nil, err
				}
				// Merge embedded struct's fields into parent
				if embeddedNode.Type == ir.ObjectType {
					for j, fieldNameNode := range embeddedNode.Fields {
						if j < len(embeddedNode.Values) {
							fieldName := fieldNameNode.String
							// Check for field name conflicts
							if _, exists := irMap[fieldName]; exists {
								return nil, &MarshalError{
									FieldPath: fieldPath,
									Message:   fmt.Sprintf("field name conflict: embedded struct field %q conflicts with existing field", fieldName),
								}
							}
							irMap[fieldName] = embeddedNode.Values[j]
						}
					}
				}
			}
			// Skip anonymous non-struct fields (they're used for schema tags)
			continue
		}

		fieldName := field.Name

		// Build field path for error reporting
		nextPath := fieldPath
		if fieldPath != "" {
			nextPath = fmt.Sprintf("%s.%s", fieldPath, fieldName)
		} else {
			nextPath = fieldName
		}

		// Marshal field value
		fieldNode, err := toIRReflectValue(fieldVal, nextPath)
		if err != nil {
			return nil, err
		}

		irMap[fieldName] = fieldNode
	}

	return ir.FromMap(irMap), nil
}
