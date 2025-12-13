package gomap

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/schema"
)

// StructSchema holds schema reference and mode information extracted from struct tags
type StructSchema struct {
	// Mode is either "schema" (usage mode) or "schemagen" (definition mode)
	Mode string

	// SchemaName is the schema name/URI from the tag
	SchemaName string

	// Context is the schema context URI (e.g., "tony-format/context")
	// If empty, defaults to "tony-format/context"
	Context string

	// AllowExtra allows struct to have extra fields not in schema (default: false, strict)
	AllowExtra bool

	// CommentFieldName is the name of a struct field (type []string) to populate with comment data
	CommentFieldName string

	// LineCommentFieldName is the name of a struct field (type []string) to populate with line comment data
	LineCommentFieldName string

	// TagFieldName is the name of a struct field (type string) to populate with the IR node's tag
	TagFieldName string
}

// FieldInfo holds field metadata extracted from struct tags
type FieldInfo struct {
	// Name is the struct field name
	Name string

	// SchemaFieldName is the field name in the schema (may differ from struct field name)
	SchemaFieldName string

	// Type is the Go type of the field
	Type reflect.Type

	// Optional indicates if the field is optional (nullable or can be empty)
	Optional bool

	// Omit indicates if the field should be omitted from marshaling/unmarshaling
	Omit bool

	// Required indicates if the field is required (overrides type-based inference)
	Required bool

	// CommentFieldName is the name of a struct field (type []string) to populate with comment data
	CommentFieldName string

	// LineCommentFieldName is the name of a struct field (type []string) to populate with line comment data
	LineCommentFieldName string

	// ImplementsTextMarshaler indicates if the field type implements encoding.TextMarshaler
	ImplementsTextMarshaler bool

	// ImplementsTextUnmarshaler indicates if the field type implements encoding.TextUnmarshaler
	ImplementsTextUnmarshaler bool
}

// ParseStructTag parses a struct tag string and returns a map of key-value pairs.
// Handles comma-separated values: `tony:"key1=value1,key2=value2,flag"`
// Supports quoted values with spaces: `tony:"key='value with spaces'"`
func ParseStructTag(tag string) (map[string]string, error) {
	result := make(map[string]string)

	if tag == "" {
		return result, nil
	}

	// Parse the tag, handling quoted values and space-separated flags
	var parts []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false

	for i := 0; i < len(tag); i++ {
		char := tag[i]

		switch {
		case char == '\'' && !inDoubleQuote:
			inSingleQuote = !inSingleQuote
			current.WriteByte(char)
		case char == '"' && !inSingleQuote:
			inDoubleQuote = !inDoubleQuote
			current.WriteByte(char)
		case char == ',' && !inSingleQuote && !inDoubleQuote:
			// End of current part
			part := strings.TrimSpace(current.String())
			if part != "" {
				parts = append(parts, part)
			}
			current.Reset()
		case char == ' ' && !inSingleQuote && !inDoubleQuote:
			// Space separator - check if it's separating a flag/key
			part := strings.TrimSpace(current.String())
			if part != "" {
				parts = append(parts, part)
				current.Reset()
			}
			// Skip the space
		default:
			current.WriteByte(char)
		}
	}

	// Add the last part
	part := strings.TrimSpace(current.String())
	if part != "" {
		parts = append(parts, part)
	}

	// Parse each part
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if it's a key=value pair or just a flag
		if idx := strings.Index(part, "="); idx >= 0 {
			key := strings.TrimSpace(part[:idx])
			value := strings.TrimSpace(part[idx+1:])
			if key == "" {
				return nil, fmt.Errorf("invalid tag: empty key in %q", part)
			}

			// Remove quotes from value if present
			value = unquoteValue(value)
			result[key] = value
		} else {
			// It's a boolean flag (no value)
			result[part] = ""
		}
	}

	return result, nil
}

// unquoteValue removes surrounding single or double quotes from a value.
// Handles: 'value' -> value, "value" -> value, 'value with spaces' -> value with spaces
func unquoteValue(value string) string {
	if len(value) == 0 {
		return value
	}

	// Single quotes
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}

	// Double quotes
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return value[1 : len(value)-1]
	}

	return value
}

// hasSchemaTag checks if an embedded struct field has a schema tag.
// Returns true if:
//  1. The field itself is anonymous and has a tony:"schema=..." or tony:"schemagen=..." tag, OR
//  2. The embedded struct type has a schema tag (checked recursively)
func hasSchemaTag(field reflect.StructField) bool {
	if !field.Anonymous {
		return false
	}

	// Check if the field itself has a schema tag
	tag := field.Tag.Get("tony")
	if tag != "" {
		parsed, err := ParseStructTag(tag)
		if err == nil {
			_, hasSchema := parsed["schema"]
			_, hasSchemaDef := parsed["schemagen"]
			if hasSchema || hasSchemaDef {
				return true
			}
		}
	}

	// Check if the embedded struct type has a schema tag
	if field.Type.Kind() == reflect.Struct {
		schema, err := GetStructSchema(field.Type)
		if err == nil && schema != nil {
			return true
		}
	}

	return false
}

// flattenEmbeddedFields adds exported fields from an embedded struct to the field map.
// This recursively flattens nested embedded structs as well.
// The schema tag on embedded structs is metadata, but fields are always flattened.
func flattenEmbeddedFields(embeddedField reflect.StructField, structFieldMap map[string]reflect.StructField) error {
	return flattenEmbeddedFieldsWithRenaming(embeddedField, structFieldMap)
}

// flattenEmbeddedFieldsWithRenaming adds exported fields from an embedded struct to the field map,
// respecting field= tags for renaming. This recursively flattens nested embedded structs as well.
func flattenEmbeddedFieldsWithRenaming(embeddedField reflect.StructField, structFieldMap map[string]reflect.StructField) error {
	embeddedType := embeddedField.Type
	if embeddedType.Kind() != reflect.Struct {
		return nil // Not a struct, nothing to flatten
	}

	for j := 0; j < embeddedType.NumField(); j++ {
		field := embeddedType.Field(j)

		if field.Anonymous {
			// Recursively flatten nested embedded structs
			if err := flattenEmbeddedFieldsWithRenaming(field, structFieldMap); err != nil {
				return err
			}
			continue
		}

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Parse field tag to check for field= renaming
		tag := field.Tag.Get("tony")
		schemaFieldName := field.Name // Default to struct field name
		if tag != "" {
			parsed, err := ParseStructTag(tag)
			if err == nil {
				// Check for field name override (field= tag)
				if renamed, ok := parsed["field"]; ok && renamed != "" && renamed != "-" {
					schemaFieldName = renamed
				}
			}
		}

		// Check for field name conflicts (by schema field name)
		if existing, exists := structFieldMap[schemaFieldName]; exists {
			return fmt.Errorf("field name conflict: embedded struct field %q (schema name %q) conflicts with existing field %q", field.Name, schemaFieldName, existing.Name)
		}

		// Add the field to the map using schema field name
		structFieldMap[schemaFieldName] = field
		// Also allow lookup by struct field name for backwards compatibility
		if schemaFieldName != field.Name {
			structFieldMap[field.Name] = field
		}
	}
	return nil
}

// GetStructSchema extracts schema information from a struct type.
// It looks for an anonymous field with a `tony:"schema=..."` or `tony:"schemagen=..."` tag.
//
// Schema tags are only checked on direct anonymous fields of the struct, not recursively
// into embedded structs. If a struct embeds another struct that has a schema tag, only
// the outer struct's schema tag is used.
//
// Example:
//
//	type A struct {
//	    schemaTag `tony:"schema=person"`
//	    Name string
//	}
//	type B struct {
//	    schemaTag `tony:"schema=company"`  // This tag is used for B
//	    A                                  // A's schema tag is ignored
//	    CompanyName string
//	}
func GetStructSchema(typ reflect.Type) (*StructSchema, error) {
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct type, got %s", typ.Kind())
	}

	var found *StructSchema
	var foundField string

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.Anonymous {
			continue
		}

		tag := field.Tag.Get("tony")
		if tag == "" {
			continue
		}

		parsed, err := ParseStructTag(tag)
		if err != nil {
			return nil, fmt.Errorf("failed to parse tag on field %s: %w", field.Name, err)
		}

		var mode string
		var schemaName string

		// Check for schema= or schemagen=
		if name, ok := parsed["schema"]; ok {
			mode = "schema"
			schemaName = name
		} else if name, ok := parsed["schemagen"]; ok {
			mode = "schemagen"
			schemaName = name
		} else {
			continue // This anonymous field doesn't have a schema tag
		}

		if schemaName == "" {
			return nil, fmt.Errorf("schema tag on field %s requires a schema name", field.Name)
		}

		// Check if we already found a schema tag
		if found != nil {
			return nil, fmt.Errorf("multiple schema tags found: field %s has %s=%s, field %s has %s=%s",
				foundField, found.Mode, found.SchemaName, field.Name, mode, schemaName)
		}

		// Extract allowExtra flag
		allowExtra := false
		if _, ok := parsed["allowExtra"]; ok {
			allowExtra = true
		}

		// Extract comment field name
		commentFieldName := ""
		if name, ok := parsed["comment"]; ok {
			commentFieldName = name
		}

		// Extract line comment field name
		lineCommentFieldName := ""
		if name, ok := parsed["lineComment"]; ok {
			lineCommentFieldName = name
		}

		// Extract tag field name
		tagFieldName := ""
		if name, ok := parsed["tag"]; ok {
			tagFieldName = name
		}

		// Extract context URI
		context := ""
		if ctx, ok := parsed["context"]; ok {
			context = ctx
		}

		found = &StructSchema{
			Mode:                 mode,
			SchemaName:           schemaName,
			Context:              context,
			AllowExtra:           allowExtra,
			CommentFieldName:     commentFieldName,
			LineCommentFieldName: lineCommentFieldName,
			TagFieldName:         tagFieldName,
		}
		foundField = field.Name
	}

	if found == nil {
		return nil, fmt.Errorf("no schema tag found on struct %s (need anonymous field with tony:\"schema=...\" or tony:\"schemagen=...\")", typ.Name())
	}

	return found, nil
}

// GetStructFields extracts field information from a struct type.
// The behavior depends on the mode:
//   - "schema" mode: Schema is source of truth, match struct fields to schema fields
//   - "schemagen" mode: Struct is source of truth, extract field info from struct
//
// registry can be nil for single-schema cases, but is required for cross-schema references.
func GetStructFields(typ reflect.Type, s *schema.Schema, mode string, allowExtra bool, registry *schema.SchemaRegistry) ([]*FieldInfo, error) {
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct type, got %s", typ.Kind())
	}

	if mode == "schema" {
		return getStructFieldsFromSchema(typ, s, allowExtra, registry)
	} else if mode == "schemagen" {
		return getStructFieldsFromStruct(typ)
	}

	return nil, fmt.Errorf("unknown mode: %s", mode)
}

// getStructFieldsFromSchema extracts field info when schema is source of truth (schema= mode)
func getStructFieldsFromSchema(typ reflect.Type, s *schema.Schema, allowExtra bool, registry *schema.SchemaRegistry) ([]*FieldInfo, error) {
	if s == nil {
		return nil, fmt.Errorf("schema is required for schema= mode")
	}

	if s.Accept == nil {
		return nil, fmt.Errorf("schema has no Accept field")
	}

	// Extract fields from schema.Accept
	// Accept should be an object type defining the fields
	if s.Accept.Type != ir.ObjectType {
		return nil, fmt.Errorf("schema Accept must be an object type, got %v", s.Accept.Type)
	}

	// Build a map of struct field names (case-sensitive matching, like jsonv2)
	// Only exported fields are accessible from a different package (like json package)
	// Embedded structs have their fields flattened into this map (schema tag is just metadata)
	// The map keys are schema field names (from field= tag if present, otherwise struct field name)
	structFieldMap := make(map[string]reflect.StructField)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		if field.Anonymous {
			// Flatten embedded structs regardless of schema tags
			// The schema tag on the embedded struct is metadata about what schema it conforms to,
			// but when embedded, its fields are flattened into the parent struct
			if err := flattenEmbeddedFieldsWithRenaming(field, structFieldMap); err != nil {
				return nil, fmt.Errorf("failed to flatten embedded struct %q: %w", field.Name, err)
			}
			continue
		}

		// Skip unexported fields (cannot access from different package)
		if !field.IsExported() {
			continue
		}

		// Parse field tag to check for field= renaming
		tag := field.Tag.Get("tony")
		schemaFieldName := field.Name // Default to struct field name
		if tag != "" {
			parsed, err := ParseStructTag(tag)
			if err == nil {
				// Check for field name override (field= tag)
				if renamed, ok := parsed["field"]; ok && renamed != "" && renamed != "-" {
					schemaFieldName = renamed
				}
			}
		}

		// Store mapping: schema field name -> struct field
		// Also store original struct field name for backwards compatibility
		structFieldMap[schemaFieldName] = field
		if schemaFieldName != field.Name {
			// Also allow lookup by struct field name for backwards compatibility
			structFieldMap[field.Name] = field
		}
	}

	var fields []*FieldInfo
	seenStructFields := make(map[string]bool)

	// Iterate through schema Accept fields
	for i := range s.Accept.Fields {
		if i >= len(s.Accept.Values) {
			break
		}

		fieldNameNode := s.Accept.Fields[i]
		fieldDefNode := s.Accept.Values[i]

		if fieldNameNode.Type != ir.StringType {
			continue // Skip non-string field names
		}

		schemaFieldName := fieldNameNode.String

		// Find matching struct field (case-sensitive, like jsonv2)
		// First check direct fields, then try FieldByName for embedded fields
		var structField reflect.StructField
		var found bool

		// Exact match in field map (includes flattened embedded fields)
		if sf, ok := structFieldMap[schemaFieldName]; ok {
			structField = sf
			found = true
		} else {
			// Try FieldByName for embedded fields (Go's field promotion)
			if f, ok := typ.FieldByName(schemaFieldName); ok && f.IsExported() {
				// Check if this field comes from an embedded struct without schema tag
				// (if it has a schema tag, it should have been in the map already)
				// FieldByName returns the promoted field, so we can use it
				structField = f
				found = true
			}
		}

		if !found {
			return nil, fmt.Errorf("struct field %q not found for schema field %q", schemaFieldName, schemaFieldName)
		}

		// Track by struct field name for extra field checking
		seenStructFields[structField.Name] = true
		// Also track by schema field name (in case of renaming)
		seenStructFields[schemaFieldName] = true

		// Extract Go type from schema definition
		// Registry is required for cross-schema references (!from, !schema)
		goType, err := ExtractGoType(fieldDefNode, s, registry)
		if err != nil {
			return nil, fmt.Errorf("failed to extract Go type for schema field %q: %w", schemaFieldName, err)
		}

		// Check if field is nullable (optional)
		// A field is optional if:
		// 1. The schema definition explicitly allows null (!or [null, T])
		// 2. The struct field is a pointer type (can be nil)
		// 3. The struct field is a slice type (can be nil/empty)
		isOptional := IsNullable(fieldDefNode, s, registry) ||
			structField.Type.Kind() == reflect.Ptr ||
			structField.Type.Kind() == reflect.Slice ||
			structField.Type.Kind() == reflect.Map

		// Validate that struct field type matches expected Go type
		if !structField.Type.AssignableTo(goType) && !isTypeCompatible(structField.Type, goType, isOptional) {
			return nil, fmt.Errorf("struct field %q has type %v, but schema field %q expects %v", structField.Name, structField.Type, schemaFieldName, goType)
		}

		fields = append(fields, &FieldInfo{
			Name:            structField.Name,
			SchemaFieldName: schemaFieldName,
			Type:            structField.Type,
			Optional:        isOptional,
		})
	}

	// Check for extra struct fields not in schema (if allowExtra is false)
	// Only check exported fields (unexported fields are not accessible)
	// This includes flattened embedded fields
	if !allowExtra {
		// Check all fields in the structFieldMap (includes flattened embedded fields)
		for fieldName := range structFieldMap {
			if !seenStructFields[fieldName] {
				return nil, fmt.Errorf("struct has extra field %q not in schema (use allowExtra flag to allow)", fieldName)
			}
		}
	}

	return fields, nil
}

// isTypeCompatible checks if a struct field type is compatible with the expected Go type from schema.
// Handles cases like pointer vs non-pointer, and basic type conversions.
func isTypeCompatible(structType, expectedType reflect.Type, isOptional bool) bool {
	// Exact match
	if structType.AssignableTo(expectedType) {
		return true
	}

	// If expected type is a pointer and struct type is the element type, that's OK (optional field)
	if expectedType.Kind() == reflect.Ptr && structType == expectedType.Elem() {
		return true
	}

	// If struct type is a pointer and expected type is the element type, check if optional
	if structType.Kind() == reflect.Ptr && structType.Elem() == expectedType {
		return isOptional
	}

	// Handle pointer types: check if the underlying types are compatible
	if structType.Kind() == reflect.Ptr && expectedType.Kind() == reflect.Ptr {
		return isTypeCompatible(structType.Elem(), expectedType.Elem(), isOptional)
	}
	if structType.Kind() == reflect.Ptr {
		return isTypeCompatible(structType.Elem(), expectedType, isOptional)
	}
	if expectedType.Kind() == reflect.Ptr {
		return isTypeCompatible(structType, expectedType.Elem(), isOptional)
	}

	// Allow compatible integer type conversions (int <-> int64, but not int <-> float64)
	// This handles common cases where schemas use int64 but structs use int (or vice versa)
	if isIntegerType(structType) && isIntegerType(expectedType) {
		return true
	}

	// Allow compatible float type conversions (float32 <-> float64)
	if structType.Kind() == reflect.Float32 && expectedType.Kind() == reflect.Float64 {
		return true
	}
	if structType.Kind() == reflect.Float64 && expectedType.Kind() == reflect.Float32 {
		return true
	}

	return false
}

// isIntegerType checks if a reflect.Type represents an integer type.
func isIntegerType(typ reflect.Type) bool {
	kind := typ.Kind()
	return kind == reflect.Int ||
		kind == reflect.Int8 ||
		kind == reflect.Int16 ||
		kind == reflect.Int32 ||
		kind == reflect.Int64 ||
		kind == reflect.Uint ||
		kind == reflect.Uint8 ||
		kind == reflect.Uint16 ||
		kind == reflect.Uint32 ||
		kind == reflect.Uint64 ||
		kind == reflect.Uintptr
}

// getStructFieldsFromStruct extracts field info when struct is source of truth (schemagen= mode)
// In schemagen mode, we're generating a schema definition from the struct, so embedded structs
// should be flattened regardless of whether they have schema tags (we're defining the schema structure).
func getStructFieldsFromStruct(typ reflect.Type) ([]*FieldInfo, error) {
	var fields []*FieldInfo

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		if field.Anonymous {
			// In schemagen mode, flatten embedded structs regardless of schema tags
			// (we're generating a schema definition, so include all fields)
			// Embedded struct without schema tag = flatten its fields
			embeddedType := field.Type
			if embeddedType.Kind() == reflect.Struct {
				for j := 0; j < embeddedType.NumField(); j++ {
					embeddedField := embeddedType.Field(j)
					// Skip anonymous fields (they might have their own schema tags)
					if embeddedField.Anonymous {
						continue
					}
					// Skip unexported fields
					if !embeddedField.IsExported() {
						continue
					}
					// Add flattened field
					info := &FieldInfo{
						Name:            embeddedField.Name,
						SchemaFieldName: embeddedField.Name, // Default to struct field name
						Type:            embeddedField.Type,
					}
					// Parse field tags from embedded field
					tag := embeddedField.Tag.Get("tony")
					if tag != "" {
						parsed, err := ParseStructTag(tag)
						if err != nil {
							return nil, fmt.Errorf("failed to parse tag on embedded field %s.%s: %w", field.Name, embeddedField.Name, err)
						}
						// Check for omit flag
						if _, ok := parsed["omit"]; ok {
							info.Omit = true
						} else if val, ok := parsed["-"]; ok && val == "" {
							info.Omit = true
						}
						// Check for field name override
						if name, ok := parsed["field"]; ok {
							info.SchemaFieldName = name
						}
						// Check for required/optional flags
						if _, ok := parsed["required"]; ok {
							info.Optional = false
						} else if _, ok := parsed["optional"]; ok {
							info.Optional = true
						}
					}
					// Skip omitted fields
					if info.Omit {
						continue
					}
					fields = append(fields, info)
				}
			}
			continue
		}

		// Skip unexported fields (cannot access from different package, like json package)
		if !field.IsExported() {
			continue
		}

		info := &FieldInfo{
			Name:            field.Name,
			SchemaFieldName: field.Name, // Default to struct field name
			Type:            field.Type,
		}

		// Parse field tags
		tag := field.Tag.Get("tony")
		if tag != "" {
			parsed, err := ParseStructTag(tag)
			if err != nil {
				return nil, fmt.Errorf("failed to parse tag on field %s: %w", field.Name, err)
			}

			// Check for omit flag
			if _, ok := parsed["omit"]; ok {
				info.Omit = true
			} else if val, ok := parsed["-"]; ok && val == "" {
				info.Omit = true
			}

			// Skip omitted fields
			if info.Omit {
				continue
			}

			// Check for field name override
			if name, ok := parsed["field"]; ok {
				info.SchemaFieldName = name
			}

			// Check for required/optional flags
			hasRequired := false
			hasOptional := false
			if _, ok := parsed["required"]; ok {
				hasRequired = true
				info.Required = true
			}
			if _, ok := parsed["optional"]; ok {
				hasOptional = true
				info.Optional = true
			}

			if hasRequired && hasOptional {
				return nil, fmt.Errorf("field %s cannot have both 'required' and 'optional' tags", field.Name)
			}
		}

		// Infer required/optional from type if not explicitly set
		if !info.Required && !info.Optional {
			info.Optional = isOptionalType(field.Type)
		}

		fields = append(fields, info)
	}

	return fields, nil
}

// isOptionalType determines if a Go type is optional (nullable or can be empty)
func isOptionalType(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Array, reflect.Map, reflect.Interface:
		return true
	default:
		return false
	}
}
