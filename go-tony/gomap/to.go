package gomap

import (
	"bytes"
	"encoding"
	"fmt"
	"reflect"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

// ToTony converts a Go value to Tony-formatted bytes.
// It first converts the value to an IR node (using ToTonyIR with mapOpts),
// then marshals the IR to bytes (using encOpts from mapOpts).
func ToTony(v interface{}, opts ...MapOption) ([]byte, error) {
	node, err := ToTonyIR(v, opts...)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	encOpts := ToEncodeOptions(opts...)
	if err := encode.Encode(node, &buf, encOpts...); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ToTonyIR converts a Go value to a Tony IR node.
// It automatically uses a ToTonyIR() method if available (user-implemented or generated),
// otherwise falls back to reflection-based conversion.
func ToTonyIR(v interface{}, opts ...MapOption) (*ir.Node, error) {
	if v == nil {
		return ir.Null(), nil
	}

	val := reflect.ValueOf(v)
	typ := val.Type()

	// Check for ToTonyIR() method on the value type (works for both value and pointer types)
	if method := val.MethodByName("ToTonyIR"); method.IsValid() {
		return callToTonyIR(method, opts...)
	}

	// If v is a value type, check if pointer type has the method
	if typ.Kind() != reflect.Ptr {
		if _, ok := reflect.PtrTo(typ).MethodByName("ToTonyIR"); ok {
			// Create a pointer to the value and call the method
			ptrVal := reflect.New(typ)
			ptrVal.Elem().Set(val)
			return callToTonyIR(ptrVal.MethodByName("ToTonyIR"), opts...)
		}
	}

	// Fall back to reflection-based conversion
	return toIRReflect(v, opts...)
}

// callToTonyIR calls the ToTonyIR() method and returns the result.
func callToTonyIR(method reflect.Value, opts ...MapOption) (*ir.Node, error) {
	// Verify method signature: ToTonyIR(opts ...MapOption) (*ir.Node, error)
	// Note: We allow the old signature ToTonyIR() (*ir.Node, error) for backward compatibility if needed,
	// but the generated code now uses the new signature.
	mt := method.Type()

	// Check for new signature: ToTonyIR(opts ...MapOption) (*ir.Node, error)
	if mt.NumIn() == 1 && mt.IsVariadic() && mt.In(0) == reflect.TypeOf([]MapOption(nil)) &&
		mt.NumOut() == 2 && mt.Out(0) == reflect.TypeOf((*ir.Node)(nil)) && mt.Out(1) == reflect.TypeOf((*error)(nil)).Elem() {
		// Call with options - use CallSlice for variadic method
		results := method.CallSlice([]reflect.Value{reflect.ValueOf(opts)})
		node := results[0].Interface().(*ir.Node)
		err := results[1].Interface()
		if err != nil {
			return nil, err.(error)
		}
		return node, nil
	}

	// Check for old signature: ToTonyIR() (*ir.Node, error)
	if mt.NumIn() == 0 && mt.NumOut() == 2 &&
		mt.Out(0) == reflect.TypeOf((*ir.Node)(nil)) && mt.Out(1) == reflect.TypeOf((*error)(nil)).Elem() {
		// Call without options
		results := method.Call(nil)
		node := results[0].Interface().(*ir.Node)
		err := results[1].Interface()
		if err != nil {
			return nil, err.(error)
		}
		return node, nil
	}

	return nil, &MarshalError{
		Message: "ToTonyIR() method must have signature: ToTonyIR(opts ...MapOption) (*ir.Node, error) or ToTonyIR() (*ir.Node, error)",
	}
}

// toIRReflect implements reflection-based conversion to IR.
// This is the fallback when generated code is not available.
func toIRReflect(v interface{}, opts ...MapOption) (*ir.Node, error) {
	if v == nil {
		return ir.Null(), nil
	}

	val := reflect.ValueOf(v)
	visited := make(map[uintptr]string) // Track visited pointers by address and field path
	return toIRReflectValue(val, "", visited, opts...)
}

// toIRReflectValue converts a reflect.Value to an IR node.
// fieldPath is used for error reporting (e.g., "person.address.street").
// visited tracks pointer addresses to detect circular references.
func toIRReflectValue(val reflect.Value, fieldPath string, visited map[uintptr]string, opts ...MapOption) (*ir.Node, error) {
	// Handle invalid/zero values
	if !val.IsValid() {
		return ir.Null(), nil
	}
	typ := val.Type()
	kind := typ.Kind()

	// Handle pointers - check for cycles
	if kind == reflect.Ptr {
		if val.IsNil() {
			return ir.Null(), nil
		}
		// Check for ToTonyIR() method on the value type (works for both value and pointer types)
		if method := val.MethodByName("ToTonyIR"); method.IsValid() {
			return callToTonyIR(method, opts...)
		}

		// Check for encoding.TextMarshaler
		if tm, ok := val.Interface().(encoding.TextMarshaler); ok {
			text, err := tm.MarshalText()
			if err != nil {
				return nil, err
			}
			return ir.FromString(string(text)), nil
		}

		// Check if we've seen this pointer before
		ptrAddr := val.Pointer()
		if prevPath, seen := visited[ptrAddr]; seen {
			return nil, &MarshalError{
				FieldPath: fieldPath,
				Message:   fmt.Sprintf("circular reference detected: %s -> %s (previously seen at %s)", prevPath, fieldPath, prevPath),
			}
		}
		// Mark this pointer as visited
		visited[ptrAddr] = fieldPath
		// Dereference and recurse
		node, err := toIRReflectValue(val.Elem(), fieldPath, visited, opts...)
		// Remove from visited after processing (allows same pointer to appear in different branches)
		delete(visited, ptrAddr)
		return node, err
	}

	// Check for encoding.TextMarshaler for non-pointers
	if tm, ok := val.Interface().(encoding.TextMarshaler); ok {
		text, err := tm.MarshalText()
		if err != nil {
			return nil, err
		}
		return ir.FromString(string(text)), nil
	}
	// Also check pointer receiver if addressable
	if val.CanAddr() {
		if tm, ok := val.Addr().Interface().(encoding.TextMarshaler); ok {
			text, err := tm.MarshalText()
			if err != nil {
				return nil, err
			}
			return ir.FromString(string(text)), nil
		}
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
		return toIRReflectSlice(val, fieldPath, visited, opts...)

	case reflect.Map:
		return toIRReflectMap(val, fieldPath, visited, opts...)

	case reflect.Struct:
		return toIRReflectStruct(val, fieldPath, visited, opts...)

	case reflect.Interface:
		// If interface is nil, return null
		if val.IsNil() {
			return ir.Null(), nil
		}
		// Recurse on the underlying value
		return toIRReflectValue(val.Elem(), fieldPath, visited, opts...)

	default:
		return nil, &MarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("unsupported type: %s", typ),
		}
	}
}

// toIRReflectSlice converts a slice or array to an IR array node.
func toIRReflectSlice(val reflect.Value, fieldPath string, visited map[uintptr]string, opts ...MapOption) (*ir.Node, error) {
	length := val.Len()
	elements := make([]*ir.Node, 0, length)

	// For slices, check if we've seen this slice before (by its underlying array pointer)
	if val.Kind() == reflect.Slice && !val.IsNil() {
		slicePtr := val.Pointer()
		if prevPath, seen := visited[slicePtr]; seen {
			return nil, &MarshalError{
				FieldPath: fieldPath,
				Message:   fmt.Sprintf("circular reference detected: %s -> %s (previously seen at %s)", prevPath, fieldPath, prevPath),
			}
		}
		visited[slicePtr] = fieldPath
		defer delete(visited, slicePtr)
	}

	for i := 0; i < length; i++ {
		elemPath := fieldPath
		if fieldPath != "" {
			elemPath = fmt.Sprintf("%s[%d]", fieldPath, i)
		} else {
			elemPath = fmt.Sprintf("[%d]", i)
		}

		elemVal := val.Index(i)
		elemNode, err := toIRReflectValue(elemVal, elemPath, visited, opts...)
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
func toIRReflectMap(val reflect.Value, fieldPath string, visited map[uintptr]string, opts ...MapOption) (*ir.Node, error) {
	if val.IsNil() {
		return ir.Null(), nil
	}

	// Check if we've seen this map before (by its pointer)
	mapPtr := val.Pointer()
	if prevPath, seen := visited[mapPtr]; seen {
		return nil, &MarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("circular reference detected: %s -> %s (previously seen at %s)", prevPath, fieldPath, prevPath),
		}
	}
	visited[mapPtr] = fieldPath
	defer delete(visited, mapPtr)

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

			valueNode, err := toIRReflectValue(valueVal, valuePath, visited, opts...)
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

		valueNode, err := toIRReflectValue(valueVal, valuePath, visited, opts...)
		if err != nil {
			return nil, err
		}
		irMap[key] = valueNode
	}

	return ir.FromMap(irMap), nil
}

// toIRReflectStruct converts a struct to an IR object node.
// Embedded structs are flattened (fields are promoted to the parent object).
// Note: We don't track struct values themselves for cycle detection, only pointers/slices/maps.
// A struct value appearing multiple times is not a cycle - only reference types can create cycles.
func toIRReflectStruct(val reflect.Value, fieldPath string, visited map[uintptr]string, opts ...MapOption) (*ir.Node, error) {
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
				embeddedNode, err := toIRReflectValue(fieldVal, fieldPath, visited, opts...)
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
		// Parse field tag to check for field= renaming
		tag := field.Tag.Get("tony")
		if tag != "" {
			parsed, err := ParseStructTag(tag)
			if err == nil {
				// Check for field name override (field= tag)
				if renamed, ok := parsed["field"]; ok && renamed != "" && renamed != "-" {
					fieldName = renamed
				}
			}
		}

		// Build field path for error reporting
		nextPath := fieldPath
		if fieldPath != "" {
			nextPath = fmt.Sprintf("%s.%s", fieldPath, fieldName)
		} else {
			nextPath = fieldName
		}

		// Marshal field value
		fieldNode, err := toIRReflectValue(fieldVal, nextPath, visited, opts...)
		if err != nil {
			return nil, err
		}

		irMap[fieldName] = fieldNode
	}

	return ir.FromMap(irMap), nil
}
