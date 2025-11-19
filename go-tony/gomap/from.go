package gomap

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/signadot/tony-format/go-tony/ir"
)

// FromIR converts a Tony IR node to a Go value.
// v must be a pointer to the target type.
// It automatically uses a FromTony() method if available (user-implemented or generated),
// otherwise falls back to reflection-based conversion.
func FromIR(node *ir.Node, v interface{}) error {
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
	if method := elemVal.MethodByName("FromTony"); method.IsValid() {
		return callFromTony(method, node)
	}

	// Check for FromTony() method on pointer type
	ptrType := reflect.PointerTo(elemType)
	if _, ok := ptrType.MethodByName("FromTony"); ok {
		// Call on the pointer value itself
		return callFromTony(val.MethodByName("FromTony"), node)
	}

	// Fall back to reflection-based conversion
	return fromIRReflect(node, elemVal, "")
}

// callFromTony calls the FromTony() method with the given node.
func callFromTony(method reflect.Value, node *ir.Node) error {
	// Verify method signature: FromTony(*ir.Node) error
	mt := method.Type()
	if mt.NumIn() != 1 || mt.NumOut() != 1 {
		return &UnmarshalError{
			Message: "FromTony() method must have signature: FromTony(*ir.Node) error",
		}
	}

	// Check parameter and return types
	if mt.In(0) != reflect.TypeOf((*ir.Node)(nil)) || mt.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
		return &UnmarshalError{
			Message: "FromTony() method must accept (*ir.Node) and return error",
		}
	}

	// Call the method
	results := method.Call([]reflect.Value{reflect.ValueOf(node)})
	err := results[0].Interface()
	if err != nil {
		return err.(error)
	}
	return nil
}

// fromIRReflect implements reflection-based conversion from IR.
// This is the fallback when generated code is not available.
func fromIRReflect(node *ir.Node, val reflect.Value, fieldPath string) error {
	visited := make(map[uintptr]string) // Track visited pointers for cycle detection
	return fromIRReflectWithVisited(node, val, fieldPath, visited)
}

// fromIRReflectWithVisited implements reflection-based conversion from IR with cycle detection.
func fromIRReflectWithVisited(node *ir.Node, val reflect.Value, fieldPath string, visited map[uintptr]string) error {
	if node == nil {
		return &UnmarshalError{
			FieldPath: fieldPath,
			Message:   "IR node is nil",
		}
	}

	typ := val.Type()
	kind := typ.Kind()

	// Handle pointers - check for cycles
	if kind == reflect.Ptr {
		// Allocate pointer if needed
		if val.IsNil() {
			val.Set(reflect.New(typ.Elem()))
		}
		// Check for FromTony() method on pointer type
		if m := val.MethodByName("FromTony"); m.IsValid() {
			// Call on the pointer value itself
			return callFromTony(m, node)
		}
		// Handle null values
		if node.Type == ir.NullType {
			// Set zero value for the target
			if val.CanSet() {
				val.Set(reflect.Zero(val.Type()))
			}
			return nil
		}

		// Check if we've seen this pointer before (we're currently building it)
		ptrAddr := val.Pointer()
		if prevPath, seen := visited[ptrAddr]; seen {
			return &UnmarshalError{
				FieldPath: fieldPath,
				Message:   fmt.Sprintf("circular reference detected: %s -> %s (previously seen at %s)", prevPath, fieldPath, prevPath),
			}
		}
		// Mark this pointer as visited
		visited[ptrAddr] = fieldPath
		// Recurse on the element
		err := fromIRReflectWithVisited(node, val.Elem(), fieldPath, visited)
		// Remove from visited after processing (allows same pointer to appear in different branches)
		delete(visited, ptrAddr)
		return err
	}
	// Handle null values
	if node.Type == ir.NullType {
		// Set zero value for the target
		if val.CanSet() {
			val.Set(reflect.Zero(val.Type()))
		}
		return nil
	}

	// Handle basic types
	switch kind {
	case reflect.String:
		return fromIRToString(node, val, fieldPath)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fromIRToInt(node, val, fieldPath)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fromIRToUint(node, val, fieldPath)

	case reflect.Float32, reflect.Float64:
		return fromIRToFloat(node, val, fieldPath)

	case reflect.Bool:
		return fromIRToBool(node, val, fieldPath)

	case reflect.Slice, reflect.Array:
		return fromIRToSlice(node, val, fieldPath, visited)

	case reflect.Map:
		return fromIRToMap(node, val, fieldPath, visited)

	case reflect.Struct:
		return fromIRToStruct(node, val, fieldPath, visited)

	case reflect.Interface:
		// For interface{}, determine the concrete type from the IR node
		return fromIRToInterface(node, val, fieldPath, visited)

	default:
		return &UnmarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("unsupported type: %s", typ),
		}
	}
}

// fromIRToString unmarshals an IR node to a string value.
func fromIRToString(node *ir.Node, val reflect.Value, fieldPath string) error {
	if node.Type != ir.StringType {
		return &UnmarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("expected string, got %s", node.Type),
		}
	}
	if val.CanSet() {
		val.SetString(node.String)
	}
	return nil
}

// fromIRToInt unmarshals an IR node to an integer value.
func fromIRToInt(node *ir.Node, val reflect.Value, fieldPath string) error {
	var intVal int64

	switch node.Type {
	case ir.NumberType:
		if node.Int64 != nil {
			intVal = *node.Int64
		} else if node.Number != "" {
			parsed, err := strconv.ParseInt(node.Number, 10, 64)
			if err != nil {
				return &UnmarshalError{
					FieldPath: fieldPath,
					Message:   fmt.Sprintf("invalid number: %q", node.Number),
					Err:       err,
				}
			}
			intVal = parsed
		} else {
			return &UnmarshalError{
				FieldPath: fieldPath,
				Message:   "number node has no value",
			}
		}
	case ir.StringType:
		// Try to parse string as int
		parsed, err := strconv.ParseInt(node.String, 10, 64)
		if err != nil {
			return &UnmarshalError{
				FieldPath: fieldPath,
				Message:   fmt.Sprintf("cannot convert string %q to int", node.String),
				Err:       err,
			}
		}
		intVal = parsed
	default:
		return &UnmarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("expected number, got %s", node.Type),
		}
	}

	if val.CanSet() {
		// Handle overflow for smaller int types
		switch val.Kind() {
		case reflect.Int8:
			if intVal < -128 || intVal > 127 {
				return &UnmarshalError{
					FieldPath: fieldPath,
					Message:   fmt.Sprintf("value %d overflows int8", intVal),
				}
			}
		case reflect.Int16:
			if intVal < -32768 || intVal > 32767 {
				return &UnmarshalError{
					FieldPath: fieldPath,
					Message:   fmt.Sprintf("value %d overflows int16", intVal),
				}
			}
		case reflect.Int32:
			if intVal < -2147483648 || intVal > 2147483647 {
				return &UnmarshalError{
					FieldPath: fieldPath,
					Message:   fmt.Sprintf("value %d overflows int32", intVal),
				}
			}
		}
		val.SetInt(intVal)
	}
	return nil
}

// fromIRToUint unmarshals an IR node to an unsigned integer value.
func fromIRToUint(node *ir.Node, val reflect.Value, fieldPath string) error {
	var uintVal uint64

	switch node.Type {
	case ir.NumberType:
		if node.Int64 != nil {
			if *node.Int64 < 0 {
				return &UnmarshalError{
					FieldPath: fieldPath,
					Message:   fmt.Sprintf("negative value %d cannot be converted to unsigned integer", *node.Int64),
				}
			}
			uintVal = uint64(*node.Int64)
		} else if node.Number != "" {
			parsed, err := strconv.ParseUint(node.Number, 10, 64)
			if err != nil {
				return &UnmarshalError{
					FieldPath: fieldPath,
					Message:   fmt.Sprintf("invalid unsigned number: %q", node.Number),
					Err:       err,
				}
			}
			uintVal = parsed
		} else {
			return &UnmarshalError{
				FieldPath: fieldPath,
				Message:   "number node has no value",
			}
		}
	case ir.StringType:
		parsed, err := strconv.ParseUint(node.String, 10, 64)
		if err != nil {
			return &UnmarshalError{
				FieldPath: fieldPath,
				Message:   fmt.Sprintf("cannot convert string %q to uint", node.String),
				Err:       err,
			}
		}
		uintVal = parsed
	default:
		return &UnmarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("expected number, got %s", node.Type),
		}
	}

	if val.CanSet() {
		// Handle overflow for smaller uint types
		switch val.Kind() {
		case reflect.Uint8:
			if uintVal > 255 {
				return &UnmarshalError{
					FieldPath: fieldPath,
					Message:   fmt.Sprintf("value %d overflows uint8", uintVal),
				}
			}
		case reflect.Uint16:
			if uintVal > 65535 {
				return &UnmarshalError{
					FieldPath: fieldPath,
					Message:   fmt.Sprintf("value %d overflows uint16", uintVal),
				}
			}
		case reflect.Uint32:
			if uintVal > 4294967295 {
				return &UnmarshalError{
					FieldPath: fieldPath,
					Message:   fmt.Sprintf("value %d overflows uint32", uintVal),
				}
			}
		}
		val.SetUint(uintVal)
	}
	return nil
}

// fromIRToFloat unmarshals an IR node to a float value.
func fromIRToFloat(node *ir.Node, val reflect.Value, fieldPath string) error {
	var floatVal float64

	switch node.Type {
	case ir.NumberType:
		if node.Float64 != nil {
			floatVal = *node.Float64
		} else if node.Int64 != nil {
			floatVal = float64(*node.Int64)
		} else if node.Number != "" {
			parsed, err := strconv.ParseFloat(node.Number, 64)
			if err != nil {
				return &UnmarshalError{
					FieldPath: fieldPath,
					Message:   fmt.Sprintf("invalid float: %q", node.Number),
					Err:       err,
				}
			}
			floatVal = parsed
		} else {
			return &UnmarshalError{
				FieldPath: fieldPath,
				Message:   "number node has no value",
			}
		}
	case ir.StringType:
		parsed, err := strconv.ParseFloat(node.String, 64)
		if err != nil {
			return &UnmarshalError{
				FieldPath: fieldPath,
				Message:   fmt.Sprintf("cannot convert string %q to float", node.String),
				Err:       err,
			}
		}
		floatVal = parsed
	default:
		return &UnmarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("expected number, got %s", node.Type),
		}
	}

	if val.CanSet() {
		if val.Kind() == reflect.Float32 {
			val.SetFloat(float64(float32(floatVal))) // May lose precision
		} else {
			val.SetFloat(floatVal)
		}
	}
	return nil
}

// fromIRToBool unmarshals an IR node to a bool value.
func fromIRToBool(node *ir.Node, val reflect.Value, fieldPath string) error {
	if node.Type != ir.BoolType {
		return &UnmarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("expected bool, got %s", node.Type),
		}
	}
	if val.CanSet() {
		val.SetBool(node.Bool)
	}
	return nil
}

// fromIRToInterface unmarshals an IR node to an interface{} value.
// It infers the concrete Go type from the IR node type.
func fromIRToInterface(node *ir.Node, val reflect.Value, fieldPath string, visited map[uintptr]string) error {
	if node == nil {
		if val.CanSet() {
			val.Set(reflect.Zero(val.Type()))
		}
		return nil
	}

	// Handle null values
	if node.Type == ir.NullType {
		if val.CanSet() {
			val.Set(reflect.Zero(val.Type()))
		}
		return nil
	}

	// Infer concrete type from IR node type
	var concreteVal reflect.Value

	switch node.Type {
	case ir.StringType:
		concreteVal = reflect.ValueOf(node.String)

	case ir.BoolType:
		concreteVal = reflect.ValueOf(node.Bool)

	case ir.NumberType:
		// Prefer int64 if available, otherwise float64
		if node.Int64 != nil {
			concreteVal = reflect.ValueOf(*node.Int64)
		} else if node.Float64 != nil {
			concreteVal = reflect.ValueOf(*node.Float64)
		} else if node.Number != "" {
			// Try to parse as int first, then float
			if intVal, err := strconv.ParseInt(node.Number, 10, 64); err == nil {
				concreteVal = reflect.ValueOf(intVal)
			} else if floatVal, err := strconv.ParseFloat(node.Number, 64); err == nil {
				concreteVal = reflect.ValueOf(floatVal)
			} else {
				return &UnmarshalError{
					FieldPath: fieldPath,
					Message:   fmt.Sprintf("invalid number: %q", node.Number),
				}
			}
		} else {
			return &UnmarshalError{
				FieldPath: fieldPath,
				Message:   "number node has no value",
			}
		}

	case ir.ArrayType:
		// Create []interface{}
		slice := make([]interface{}, len(node.Values))
		for i, elemNode := range node.Values {
			// Recursively unmarshal each element to interface{}
			var elemResult interface{}
			elemVal := reflect.ValueOf(&elemResult).Elem()
			elemPath := fieldPath
			if fieldPath != "" {
				elemPath = fmt.Sprintf("%s[%d]", fieldPath, i)
			} else {
				elemPath = fmt.Sprintf("[%d]", i)
			}
			if err := fromIRToInterface(elemNode, elemVal, elemPath, visited); err != nil {
				return err
			}
			slice[i] = elemResult
		}
		concreteVal = reflect.ValueOf(slice)

	case ir.ObjectType:
		// Create map[string]interface{}
		irMap := ir.ToMap(node)
		m := make(map[string]interface{}, len(irMap))
		for key, valueNode := range irMap {
			// Recursively unmarshal each value to interface{}
			var valueResult interface{}
			valueVal := reflect.ValueOf(&valueResult).Elem()
			valuePath := fieldPath
			if fieldPath != "" {
				valuePath = fmt.Sprintf("%s.%s", fieldPath, key)
			} else {
				valuePath = key
			}
			if err := fromIRToInterface(valueNode, valueVal, valuePath, visited); err != nil {
				return err
			}
			m[key] = valueResult
		}
		concreteVal = reflect.ValueOf(m)

	default:
		return &UnmarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("unsupported IR type for interface{}: %s", node.Type),
		}
	}

	// Set the interface{} value
	if val.CanSet() {
		val.Set(concreteVal)
	}

	return nil
}

// fromIRToSlice unmarshals an IR array node to a slice or array value.
func fromIRToSlice(node *ir.Node, val reflect.Value, fieldPath string, visited map[uintptr]string) error {
	if node.Type != ir.ArrayType {
		return &UnmarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("expected array, got %s", node.Type),
		}
	}

	length := len(node.Values)
	typ := val.Type()
	kind := typ.Kind()

	if kind == reflect.Array {
		// For arrays, check if length matches
		if val.Len() != length {
			return &UnmarshalError{
				FieldPath: fieldPath,
				Message:   fmt.Sprintf("array length mismatch: expected %d, got %d", val.Len(), length),
			}
		}
	} else {
		// For slices, allocate if needed
		if val.IsNil() || val.Cap() < length {
			val.Set(reflect.MakeSlice(typ, length, length))
		} else {
			val.SetLen(length)
		}
		// Track slice pointer for cycle detection
		if !val.IsNil() {
			slicePtr := val.Pointer()
			if prevPath, seen := visited[slicePtr]; seen {
				return &UnmarshalError{
					FieldPath: fieldPath,
					Message:   fmt.Sprintf("circular reference detected: %s -> %s (previously seen at %s)", prevPath, fieldPath, prevPath),
				}
			}
			visited[slicePtr] = fieldPath
			defer delete(visited, slicePtr)
		}
	}

	for i := 0; i < length; i++ {
		elemPath := fieldPath
		if fieldPath != "" {
			elemPath = fmt.Sprintf("%s[%d]", fieldPath, i)
		} else {
			elemPath = fmt.Sprintf("[%d]", i)
		}

		elemVal := val.Index(i)
		if err := fromIRReflectWithVisited(node.Values[i], elemVal, elemPath, visited); err != nil {
			return err
		}
	}

	return nil
}

// fromIRToMap unmarshals an IR object node to a map value.
// If the IR node has the !sparsearray tag, it's treated as a sparse array (map[uint32]T).
// Otherwise, it's treated as a regular object (map[string]T).
func fromIRToMap(node *ir.Node, val reflect.Value, fieldPath string, visited map[uintptr]string) error {
	if node.Type != ir.ObjectType {
		return &UnmarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("expected object, got %s", node.Type),
		}
	}

	typ := val.Type()
	keyType := typ.Key()
	valType := typ.Elem()

	// Allocate map if needed
	if val.IsNil() {
		val.Set(reflect.MakeMap(typ))
	}

	// Track map pointer for cycle detection
	mapPtr := val.Pointer()
	if prevPath, seen := visited[mapPtr]; seen {
		return &UnmarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("circular reference detected: %s -> %s (previously seen at %s)", prevPath, fieldPath, prevPath),
		}
	}
	visited[mapPtr] = fieldPath
	defer delete(visited, mapPtr)

	// Clear existing entries (or we could merge - for now, clear)
	val.Set(reflect.MakeMap(typ))

	// Check if this is a sparse array (!sparsearray tag)
	isSparseArray := ir.TagHas(node.Tag, ir.IntKeysTag)

	if isSparseArray {
		// Handle sparse array: map[uint32]T
		if keyType.Kind() != reflect.Uint32 {
			return &UnmarshalError{
				FieldPath: fieldPath,
				Message:   fmt.Sprintf("sparse array requires map[uint32]T, got map[%s]T", keyType),
			}
		}

		// Convert IR sparse array to map[uint32]*Node
		intKeysMap, err := node.ToIntKeysMap()
		if err != nil {
			return &UnmarshalError{
				FieldPath: fieldPath,
				Message:   fmt.Sprintf("failed to convert sparse array: %v", err),
				Err:       err,
			}
		}

		// Unmarshal each value
		for key, valueNode := range intKeysMap {
			keyVal := reflect.ValueOf(key)
			valueVal := reflect.New(valType).Elem()

			valuePath := fieldPath
			if fieldPath != "" {
				valuePath = fmt.Sprintf("%s[%d]", fieldPath, key)
			} else {
				valuePath = fmt.Sprintf("[%d]", key)
			}

			if err := fromIRReflectWithVisited(valueNode, valueVal, valuePath, visited); err != nil {
				return err
			}

			val.SetMapIndex(keyVal, valueVal)
		}

		return nil
	}

	// Regular object: map[string]T
	if keyType.Kind() != reflect.String {
		return &UnmarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("map keys must be strings or uint32, got %s", keyType),
		}
	}

	// Convert IR object to map
	irMap := ir.ToMap(node)
	for key, valueNode := range irMap {
		keyVal := reflect.ValueOf(key)
		valueVal := reflect.New(valType).Elem()

		valuePath := fieldPath
		if fieldPath != "" {
			valuePath = fmt.Sprintf("%s.%s", fieldPath, key)
		} else {
			valuePath = key
		}

		if err := fromIRReflectWithVisited(valueNode, valueVal, valuePath, visited); err != nil {
			return err
		}

		val.SetMapIndex(keyVal, valueVal)
	}

	return nil
}

// fromIRToStruct unmarshals an IR object node to a struct value.
// Embedded structs are handled by flattening (fields are promoted from embedded structs).
func fromIRToStruct(node *ir.Node, val reflect.Value, fieldPath string, visited map[uintptr]string) error {
	if node.Type != ir.ObjectType {
		return &UnmarshalError{
			FieldPath: fieldPath,
			Message:   fmt.Sprintf("expected object, got %s", node.Type),
		}
	}

	typ := val.Type()
	// Note: We don't track struct values themselves for cycle detection, only pointers/slices/maps.
	// A struct value appearing multiple times is not a cycle - only reference types can create cycles.

	// Build a map of struct field names (case-sensitive) to their field indices
	// For embedded structs, we need to track both the embedded struct index and the field index
	type fieldInfo struct {
		index []int // Full index path (e.g., [0, 1] for embedded struct at index 0, field at index 1)
		field reflect.StructField
	}
	structFieldMap := make(map[string]fieldInfo)

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Anonymous {
			// Handle embedded structs - flatten their fields
			if field.Type.Kind() == reflect.Struct {
				embeddedType := field.Type
				for j := 0; j < embeddedType.NumField(); j++ {
					embeddedField := embeddedType.Field(j)
					if !embeddedField.IsExported() || embeddedField.Anonymous {
						continue
					}
					// Check for conflicts
					if _, exists := structFieldMap[embeddedField.Name]; exists {
						return &UnmarshalError{
							FieldPath: fieldPath,
							Message:   fmt.Sprintf("field name conflict: embedded struct field %q conflicts with existing field", embeddedField.Name),
						}
					}
					// Build full index path: [embeddedStructIndex, embeddedFieldIndex]
					fullIndex := append(field.Index, embeddedField.Index...)
					structFieldMap[embeddedField.Name] = fieldInfo{
						index: fullIndex,
						field: embeddedField,
					}
				}
			}
			continue
		}
		structFieldMap[field.Name] = fieldInfo{
			index: field.Index,
			field: field,
		}
	}

	// Unmarshal each field from IR object
	for i, fieldNameNode := range node.Fields {
		if i >= len(node.Values) {
			break
		}

		fieldName := fieldNameNode.String
		fieldNode := node.Values[i]

		// Find matching struct field (case-sensitive)
		fieldInfo, found := structFieldMap[fieldName]
		if !found {
			// Field not found - skip it (could be extra field)
			continue
		}

		fieldVal := val.FieldByIndex(fieldInfo.index)
		if !fieldVal.IsValid() {
			continue
		}

		nextPath := fieldPath
		if fieldPath != "" {
			nextPath = fmt.Sprintf("%s.%s", fieldPath, fieldName)
		} else {
			nextPath = fieldName
		}

		if err := fromIRReflectWithVisited(fieldNode, fieldVal, nextPath, visited); err != nil {
			return err
		}
	}

	return nil
}
