package gomap

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
)

// IsNullable checks if a definition allows null (via !or with null).
// It returns true if the definition is a !or pattern that includes null as one of its options.
//
// Examples:
//   - !or [null, string] → true
//   - !or [string] → false
//   - string → false
//   - .[nullable-string] → recursively checks the referenced definition
//
// The function checks for null in three forms:
//   - ir.NullType node type
//   - Tag == "null"
//   - StringType with String == "null"
func IsNullable(def *ir.Node, s *schema.Schema, registry *schema.SchemaRegistry) bool {
	if def == nil {
		return false
	}

	// Check if this is a !or pattern
	if def.Tag != "" {
		head, _, _ := ir.TagArgs(def.Tag)
		if head == "!or" {
			return isNullableFromOr(def)
		}
	}

	// Check if this is a reference - recurse to check the referenced definition
	if def.Tag != "" && strings.HasPrefix(def.Tag, ".[") && strings.HasSuffix(def.Tag, "]") {
		name := eval.GetRaw(def.Tag)
		if name != "" {
			refDef, err := schema.ResolveDefinitionName(s, name)
			if err == nil {
				return IsNullable(refDef, s, registry)
			}
		}
	}

	// Check if this is a string value that's a reference
	if def.Type == ir.StringType && strings.HasPrefix(def.String, ".[") && strings.HasSuffix(def.String, "]") {
		name := eval.GetRaw(def.String)
		if name != "" {
			refDef, err := schema.ResolveDefinitionName(s, name)
			if err == nil {
				return IsNullable(refDef, s, registry)
			}
		}
	}

	return false
}

// isNullableFromOr checks if a !or pattern includes null.
func isNullableFromOr(def *ir.Node) bool {
	if def.Type != ir.ArrayType {
		return false
	}

	for _, elem := range def.Values {
		// Check for null in various forms
		if elem.Type == ir.NullType {
			return true
		}
		if elem.Tag == "null" {
			return true
		}
		if elem.Type == ir.StringType && elem.String == "null" {
			return true
		}
	}

	return false
}

// ExtractGoType extracts Go type information from a schema definition node.
// It resolves .[name] references, handles !or [null, T] patterns for nullable types,
// parameterized types like .array(string), and constructs a reflect.Type that represents the Go type.
//
// Examples:
//   - "string" → reflect.TypeOf("")
//   - ".[int]" → resolves to int definition, returns reflect.TypeOf(0)
//   - "!or [null, string]" → returns reflect.PtrTo(reflect.TypeOf(""))
//   - ".array(string)" → returns reflect.SliceOf(reflect.TypeOf("")) ([]string)
//   - ".array(.array(string))" → returns [][]string
//   - ".sparsearray(string)" → returns map[int]string
//
// The function recursively resolves references and builds up complex types, including
// nested parameterized types.
func ExtractGoType(def *ir.Node, s *schema.Schema, registry *schema.SchemaRegistry) (reflect.Type, error) {
	if def == nil {
		return nil, fmt.Errorf("definition node cannot be nil")
	}

	// Check if this is a parameterized reference like .array(string) or .array(.[string])
	if def.Tag != "" && strings.HasPrefix(def.Tag, ".") {
		if typ, err := extractGoTypeFromParameterizedRef(def.Tag, s, registry); err == nil {
			return typ, nil
		}
		// Fall through to check for simple .[name] syntax
	}

	// Check if this is a reference (.[name] syntax) - check Tag first
	if def.Tag != "" && strings.HasPrefix(def.Tag, ".[") && strings.HasSuffix(def.Tag, "]") {
		name := eval.GetRaw(def.Tag)
		if name == "" {
			return nil, fmt.Errorf("invalid reference syntax: %q", def.Tag)
		}
		// Resolve the reference and recurse
		refDef, err := schema.ResolveDefinitionName(s, name)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve reference %q: %w", name, err)
		}
		return ExtractGoType(refDef, s, registry)
	}

	// Check if this is a string value that's a parameterized reference
	if def.Type == ir.StringType && strings.HasPrefix(def.String, ".") {
		if typ, err := extractGoTypeFromParameterizedRef(def.String, s, registry); err == nil {
			return typ, nil
		}
		// Fall through to check for simple .[name] syntax
	}

	// Check if this is a string value that's a reference
	if def.Type == ir.StringType && strings.HasPrefix(def.String, ".[") && strings.HasSuffix(def.String, "]") {
		name := eval.GetRaw(def.String)
		if name == "" {
			return nil, fmt.Errorf("invalid reference syntax: %q", def.String)
		}
		refDef, err := schema.ResolveDefinitionName(s, name)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve reference %q: %w", name, err)
		}
		return ExtractGoType(refDef, s, registry)
	}

	// Handle cross-schema references: !from(schema-name, def-name)
	if def.Tag != "" {
		head, _, _ := ir.TagArgs(def.Tag)
		if head == "!from" {
			return extractGoTypeFromCrossSchema(def, registry)
		}
	}

	// Handle schema references: !schema(schema-name)
	if def.Tag != "" {
		head, _, _ := ir.TagArgs(def.Tag)
		if head == "!schema" {
			return extractGoTypeFromSchemaRef(def, registry)
		}
	}

	// Handle !or [null, T] pattern for nullable types
	if def.Tag != "" {
		head, _, _ := ir.TagArgs(def.Tag)
		if head == "!or" {
			return extractGoTypeFromOr(def, s, registry)
		}
	}

	// Handle !and - extract the base type from constraints
	if def.Tag != "" {
		head, _, _ := ir.TagArgs(def.Tag)
		if head == "!and" {
			return extractGoTypeFromAnd(def, s, registry)
		}
	}

	// Handle basic types by their IR node type first (before checking tags)
	switch def.Type {
	case ir.StringType:
		// Check for !type tag after handling the type
		if def.Tag != "" {
			head, _, _ := ir.TagArgs(def.Tag)
			if head == "!type" {
				return reflect.TypeOf(""), nil
			}
		}
		return reflect.TypeOf(""), nil
	case ir.NumberType:
		// Check for !type tag
		if def.Tag != "" {
			head, _, _ := ir.TagArgs(def.Tag)
			if head == "!type" {
				return reflect.TypeOf(float64(0)), nil
			}
		}
		// Default to float64 for numbers (could be int or float)
		return reflect.TypeOf(float64(0)), nil
	case ir.BoolType:
		// Check for !type tag
		if def.Tag != "" {
			head, _, _ := ir.TagArgs(def.Tag)
			if head == "!type" {
				return reflect.TypeOf(false), nil
			}
		}
		return reflect.TypeOf(false), nil
	case ir.NullType:
		return nil, fmt.Errorf("cannot extract Go type from null type")
	case ir.ArrayType:
		// Array type - need to determine element type
		// Check for !type tag - but still try to extract element type from values
		if def.Tag != "" {
			head, _, _ := ir.TagArgs(def.Tag)
			if head == "!type" {
				// If it's !type [] with no values, return []interface{}
				if len(def.Values) == 0 {
					return reflect.TypeOf([]interface{}(nil)), nil
				}
				// Otherwise, try to extract element type from values
			}
		}
		return extractGoTypeFromArray(def, s, registry)
	case ir.ObjectType:
		// Check for !type tag
		if def.Tag != "" {
			head, _, _ := ir.TagArgs(def.Tag)
			if head == "!type" {
				return reflect.TypeOf(map[string]interface{}(nil)), nil
			}
		}
		// Object type - could be a map or struct
		// For now, treat as map[string]interface{}
		return reflect.TypeOf(map[string]interface{}(nil)), nil
	}

	// Handle !type tags for other cases
	if def.Tag != "" {
		head, _, _ := ir.TagArgs(def.Tag)
		if head == "!type" {
			return extractGoTypeFromTypeTag(def)
		}
	}

	// If we have a string value, try to infer type
	if def.Type == ir.StringType {
		// Could be a type name or reference
		// Try common type names
		switch def.String {
		case "string":
			return reflect.TypeOf(""), nil
		case "int", "int64":
			return reflect.TypeOf(int64(0)), nil
		case "float", "float64":
			return reflect.TypeOf(float64(0)), nil
		case "bool":
			return reflect.TypeOf(false), nil
		}
	}

	return nil, fmt.Errorf("cannot extract Go type from definition node: tag=%q, type=%v", def.Tag, def.Type)
}

// extractGoTypeFromCrossSchema handles !from(schema-name, def-name) cross-schema references.
// It resolves the definition from another schema using the registry.
func extractGoTypeFromCrossSchema(def *ir.Node, registry *schema.SchemaRegistry) (reflect.Type, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry required for cross-schema reference")
	}

	// Parse the !from reference
	fromRef, err := schema.ParseFromReference(def)
	if err != nil {
		return nil, fmt.Errorf("failed to parse !from reference: %w", err)
	}

	// Resolve the definition from the referenced schema
	refDef, err := registry.ResolveDefinition(fromRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve definition %q from schema %q: %w", fromRef.DefName, fromRef.SchemaName, err)
	}

	// Get the schema that contains the definition
	schemaRef := &schema.SchemaReference{
		Name: fromRef.SchemaName,
		Args: fromRef.SchemaArgs,
	}
	refSchema, err := registry.ResolveSchema(schemaRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve schema %q: %w", fromRef.SchemaName, err)
	}

	// Recursively extract the type from the referenced definition
	return ExtractGoType(refDef, refSchema, registry)
}

// extractGoTypeFromSchemaRef handles !schema(schema-name) references.
// It resolves the entire schema and extracts the type from its Accept field.
func extractGoTypeFromSchemaRef(def *ir.Node, registry *schema.SchemaRegistry) (reflect.Type, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry required for schema reference")
	}

	// Parse the schema reference
	schemaRef, err := schema.ParseSchemaReference(def)
	if err != nil {
		return nil, fmt.Errorf("failed to parse !schema reference: %w", err)
	}

	// Resolve the schema
	refSchema, err := registry.ResolveSchema(schemaRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve schema %q: %w", schemaRef.Name, err)
	}

	// Extract type from the schema's Accept field
	if refSchema.Accept == nil {
		return nil, fmt.Errorf("schema %q has no Accept field", schemaRef.Name)
	}

	// Recursively extract the type from the Accept definition
	return ExtractGoType(refSchema.Accept, refSchema, registry)
}

// extractGoTypeFromOr handles !or patterns.
//   - !or [null, T] → *T (nullable single type)
//   - !or [T] → T (single type)
//   - !or [T1, T2, ...] → struct{ T1 *T1; T2 *T2; ... } (union type with optional fields)
//   - !or [null, T1, T2, ...] → struct{ T1 *T1; T2 *T2; ... } (union type, all fields optional)
func extractGoTypeFromOr(def *ir.Node, s *schema.Schema, registry *schema.SchemaRegistry) (reflect.Type, error) {
	if def.Type != ir.ArrayType {
		return nil, fmt.Errorf("!or must be an array")
	}

	hasNull := false
	var nonNullNodes []*ir.Node

	for _, elem := range def.Values {
		// Check if this is a reference first (references are never null)
		if elem.Tag != "" && strings.HasPrefix(elem.Tag, ".[") && strings.HasSuffix(elem.Tag, "]") {
			// Found a reference - this is a non-null element
			nonNullNodes = append(nonNullNodes, elem)
			continue
		}
		
		// Check for null (only if not a reference)
		if elem.Type == ir.NullType {
			hasNull = true
			continue
		}
		if elem.Tag == "null" {
			hasNull = true
			continue
		}
		if elem.Type == ir.StringType && elem.String == "null" {
			hasNull = true
			continue
		}
		// Found a non-null element
		nonNullNodes = append(nonNullNodes, elem)
	}

	if len(nonNullNodes) == 0 {
		return nil, fmt.Errorf("!or must have at least one non-null type")
	}

	// Single non-null type: return pointer if nullable, direct type otherwise
	if len(nonNullNodes) == 1 {
		typ, err := ExtractGoType(nonNullNodes[0], s, registry)
		if err != nil {
			return nil, fmt.Errorf("failed to extract type from !or element: %w", err)
		}
		if hasNull {
			return reflect.PtrTo(typ), nil
		}
		return typ, nil
	}

	// Multiple non-null types: create a struct with optional fields for each type
	return extractGoTypeFromOrUnion(nonNullNodes, s, registry)
}

// extractGoTypeFromOrUnion creates a struct type for !or with multiple non-null types.
// Each type becomes an optional field (pointer) in the struct.
func extractGoTypeFromOrUnion(nonNullNodes []*ir.Node, s *schema.Schema, registry *schema.SchemaRegistry) (reflect.Type, error) {
	fields := make([]reflect.StructField, 0, len(nonNullNodes))
	fieldNames := make(map[string]bool) // Track field names to avoid duplicates

	for i, node := range nonNullNodes {
		typ, err := ExtractGoType(node, s, registry)
		if err != nil {
			return nil, fmt.Errorf("failed to extract type from !or element %d: %w", i, err)
		}

		// Generate a field name from the type
		fieldName := typeToFieldName(typ)
		
		// Ensure unique field names
		baseName := fieldName
		counter := 1
		for fieldNames[fieldName] {
			fieldName = fmt.Sprintf("%s%d", baseName, counter)
			counter++
		}
		fieldNames[fieldName] = true

		// Create struct field with pointer type (all fields optional)
		fields = append(fields, reflect.StructField{
			Name: fieldName,
			Type: reflect.PtrTo(typ),
		})
	}

	// Create the struct type
	return reflect.StructOf(fields), nil
}

// typeToFieldName converts a reflect.Type to a Go-exported field name.
func typeToFieldName(typ reflect.Type) string {
	// Handle common types
	switch typ.Kind() {
	case reflect.String:
		return "String"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "Int"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "Uint"
	case reflect.Float32, reflect.Float64:
		return "Float"
	case reflect.Bool:
		return "Bool"
	case reflect.Slice:
		return "Slice"
	case reflect.Map:
		return "Map"
	case reflect.Ptr:
		// For pointer types, use the element type name
		return typeToFieldName(typ.Elem())
	case reflect.Struct:
		// Use the struct name if available, otherwise "Struct"
		if typ.Name() != "" {
			return typ.Name()
		}
		return "Struct"
	case reflect.Interface:
		return "Interface"
	default:
		// Fallback: use the type name or "Value"
		if typ.Name() != "" {
			return typ.Name()
		}
		return "Value"
	}
}

// extractGoTypeFromAnd handles !and constraints - extracts the base type.
//
// !and means ALL constraints must be satisfied. For type extraction, we need to:
// 1. Find the "base type" (typically a reference like .[number] or type tag like !type "")
// 2. Handle parameterized types: if we see !all.type t, extract the parameter type
// 3. Skip constraint nodes (like !not null, !not, etc.)
//
// Example:
//   int: !and
//     - .[number]        # Base type: number → float64
//     - int: !not null    # Constraint: skip this
//   Result: float64
//
// Example with parameterized type:
//   array(t): !and
//     - .[array]          # Base type: array
//     - !all.type t       # Parameter constraint: t is the element type
//   When used as .array(string), we extract []string
func extractGoTypeFromAnd(def *ir.Node, s *schema.Schema, registry *schema.SchemaRegistry) (reflect.Type, error) {
	if def.Type != ir.ObjectType && def.Type != ir.ArrayType {
		return nil, fmt.Errorf("!and must be an object or array")
	}

	// Note: Parameterized types like array(t) with !all.type t are handled when the
	// parameterized reference is used (e.g., .array(string)), not when the definition is extracted.
	// The parameterized reference handler (extractGoTypeFromParameterizedRef) handles the full extraction.

	// For !and, we need to find the "base type" - typically the first reference or type.
	// Skip constraint nodes like "!not null", "!not", "!all.type", etc.
	for _, elem := range def.Values {
		// Skip constraint nodes (these don't define types, they constrain them)
		if elem.Tag != "" {
			head, _, _ := ir.TagArgs(elem.Tag)
			if head == "!not" || head == "!not null" {
				continue
			}
			// Skip !all.type constraints (handled above for parameter detection)
			if head == "!all.type" {
				continue
			}
		}
		// Try to extract type from this element (could be a reference, type tag, etc.)
		typ, err := ExtractGoType(elem, s, registry)
		if err == nil {
			return typ, nil
		}
	}

	// If we have fields, try the first field value
	if def.Type == ir.ObjectType && len(def.Fields) > 0 && len(def.Values) > 0 {
		// Check if first field is a type reference
		firstValue := def.Values[0]
		typ, err := ExtractGoType(firstValue, s, registry)
		if err == nil {
			return typ, nil
		}
	}

	return nil, fmt.Errorf("could not extract base type from !and constraints")
}

// extractGoTypeFromTypeTag handles !type tags
func extractGoTypeFromTypeTag(def *ir.Node) (reflect.Type, error) {
	// !type tags typically have a value that indicates the type
	// Examples: !type "", !type 1, !type true, !type []
	
	if def.Type == ir.StringType {
		// !type "" → string
		return reflect.TypeOf(""), nil
	}
	if def.Type == ir.NumberType {
		// !type 1 → number (float64)
		return reflect.TypeOf(float64(0)), nil
	}
	if def.Type == ir.BoolType {
		// !type true → bool
		return reflect.TypeOf(false), nil
	}
	if def.Type == ir.ArrayType {
		// !type [] → slice
		// Default to []interface{} if we can't determine element type
		return reflect.TypeOf([]interface{}(nil)), nil
	}
	if def.Type == ir.ObjectType {
		// !type {} → map or object
		return reflect.TypeOf(map[string]interface{}(nil)), nil
	}

	return nil, fmt.Errorf("cannot determine type from !type tag: type=%v", def.Type)
}

// extractGoTypeFromArray handles array types
func extractGoTypeFromArray(def *ir.Node, s *schema.Schema, registry *schema.SchemaRegistry) (reflect.Type, error) {
	// If array has elements, use first element's type
	if len(def.Values) > 0 {
		elemType, err := ExtractGoType(def.Values[0], s, registry)
		if err != nil {
			// If we can't determine element type, default to []interface{}
			return reflect.TypeOf([]interface{}(nil)), nil
		}
		return reflect.SliceOf(elemType), nil
	}

	// Default to []interface{}
	return reflect.TypeOf([]interface{}(nil)), nil
}

// extractGoTypeFromParameterizedRef handles parameterized references like .array(string) or .array(.[string])
func extractGoTypeFromParameterizedRef(tag string, s *schema.Schema, registry *schema.SchemaRegistry) (reflect.Type, error) {
	if !strings.HasPrefix(tag, ".") {
		return nil, fmt.Errorf("not a reference")
	}

	// Remove leading "." and prepend "!" so TagArgs can parse it
	// TagArgs expects tags to start with "!"
	tagWithPrefix := "!" + tag[1:]

	// Use ir.TagArgs to parse the parameterized reference
	head, args, _ := ir.TagArgs(tagWithPrefix)

	// Remove "!" prefix from head to get the constructor name
	constructor := head
	if strings.HasPrefix(head, "!") {
		constructor = head[1:]
	}

	// Check if this is a parameterized reference (has arguments)
	if len(args) == 0 {
		// Not a parameterized reference, let caller handle it
		return nil, fmt.Errorf("not a parameterized reference")
	}

	// Handle type constructors
	switch constructor {
	case "array":
		// .array(T) → []T
		// Extract the element type T from the first argument
		elemType, err := extractElementTypeFromArg(args[0], s, registry)
		if err != nil {
			return nil, fmt.Errorf("failed to extract element type from .array(%s): %w", args[0], err)
		}
		return reflect.SliceOf(elemType), nil

	case "sparsearray":
		// .sparsearray(T) → map[int]T (sparse arrays are maps with numeric keys)
		elemType, err := extractElementTypeFromArg(args[0], s, registry)
		if err != nil {
			return nil, fmt.Errorf("failed to extract element type from .sparsearray(%s): %w", args[0], err)
		}
		return reflect.MapOf(reflect.TypeOf(0), elemType), nil

	default:
		// Unknown constructor - try to resolve as a definition name
		// This handles cases where the constructor itself might be a parameterized definition
		fullName := constructor + "(" + strings.Join(args, ",") + ")"
		refDef, err := schema.ResolveDefinitionName(s, fullName)
		if err == nil {
			return ExtractGoType(refDef, s, registry)
		}
		return nil, fmt.Errorf("unknown type constructor: %s", constructor)
	}
}

// extractElementTypeFromArg extracts the Go type from a parameter argument.
// The argument can be:
//   - A simple name: "string" → resolves to definition
//   - A reference: ".[string]" → resolves to definition
//   - A nested parameterized reference: ".array(string)" → recursively extracts
func extractElementTypeFromArg(arg string, s *schema.Schema, registry *schema.SchemaRegistry) (reflect.Type, error) {
	// Check if it's a reference syntax .[name]
	if strings.HasPrefix(arg, ".[") && strings.HasSuffix(arg, "]") {
		name := eval.GetRaw(arg)
		if name == "" {
			return nil, fmt.Errorf("invalid reference syntax: %q", arg)
		}
		refDef, err := schema.ResolveDefinitionName(s, name)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve reference %q: %w", name, err)
		}
		return ExtractGoType(refDef, s, registry)
	}

	// Check if it's a parameterized reference like .array(string)
	// Use ir.TagArgs to check if it has parentheses (parameterized)
	if strings.HasPrefix(arg, ".") {
		// Try parsing as parameterized reference using TagArgs
		tagWithPrefix := "!" + arg[1:]
		_, args, _ := ir.TagArgs(tagWithPrefix)
		if len(args) > 0 {
			// It's a parameterized reference
			return extractGoTypeFromParameterizedRef(arg, s, registry)
		}
		// Simple reference without parameters - resolve as definition name
		name := arg[1:] // Remove leading "."
		refDef, err := schema.ResolveDefinitionName(s, name)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve definition %q: %w", name, err)
		}
		return ExtractGoType(refDef, s, registry)
	}

	// Otherwise, treat as a simple definition name
	refDef, err := schema.ResolveDefinitionName(s, arg)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve definition %q: %w", arg, err)
	}
	return ExtractGoType(refDef, s, registry)
}
