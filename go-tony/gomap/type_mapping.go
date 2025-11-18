package gomap

import (
	"reflect"

	"github.com/signadot/tony-format/go-tony/ir"
)

// goTypeToIRType maps a Go type to an IR type.
// This is used for type inference and validation.
func goTypeToIRType(goType reflect.Type) ir.Type {
	kind := goType.Kind()

	switch kind {
	case reflect.String:
		return ir.StringType

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return ir.NumberType

	case reflect.Bool:
		return ir.BoolType

	case reflect.Slice, reflect.Array:
		return ir.ArrayType

	case reflect.Map:
		return ir.ObjectType

	case reflect.Struct:
		return ir.ObjectType

	case reflect.Ptr:
		// Pointer types are nullable, but the underlying type determines the IR type
		return goTypeToIRType(goType.Elem())

	case reflect.Interface:
		// Interface{} can be any type, so we can't determine a specific IR type
		// This will be handled dynamically at runtime
		return ir.ObjectType // Default to object, but may vary at runtime

	default:
		// Unknown type - default to object
		return ir.ObjectType
	}
}

// isNullableType determines if a Go type represents a nullable value.
// Types that are nullable:
//   - Pointer types (*T)
//   - Interface types (interface{})
//   - Slice types ([]T) - can be nil
//   - Map types (map[K]V) - can be nil
func isNullableType(goType reflect.Type) bool {
	kind := goType.Kind()
	return kind == reflect.Ptr ||
		kind == reflect.Interface ||
		kind == reflect.Slice ||
		kind == reflect.Map
}

// isArrayType determines if a Go type represents an array or slice.
func isArrayType(goType reflect.Type) bool {
	kind := goType.Kind()
	return kind == reflect.Slice || kind == reflect.Array
}

// isMapType determines if a Go type represents a map.
func isMapType(goType reflect.Type) bool {
	return goType.Kind() == reflect.Map
}

// isStructType determines if a Go type represents a struct.
func isStructType(goType reflect.Type) bool {
	return goType.Kind() == reflect.Struct
}
