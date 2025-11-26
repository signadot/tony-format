package codegen

import (
	"fmt"
	"reflect"
)

// FlattenEmbeddedFields flattens embedded fields in all structs.
// It recursively resolves embedded fields and adds their fields to the embedding struct.
func FlattenEmbeddedFields(structs []*StructInfo) error {
	// Build a map for quick lookup
	structMap := make(map[string]*StructInfo)
	for _, s := range structs {
		structMap[s.Name] = s
	}

	// Process each struct
	for _, s := range structs {
		if err := flattenStruct(s, structMap, make(map[string]bool)); err != nil {
			return fmt.Errorf("failed to flatten struct %q: %w", s.Name, err)
		}
	}

	return nil
}

// flattenStruct flattens embedded fields for a single struct.
// visited is used to detect cycles.
func flattenStruct(s *StructInfo, structMap map[string]*StructInfo, visited map[string]bool) error {
	if visited[s.Name] {
		return fmt.Errorf("cycle detected involving struct %q", s.Name)
	}
	visited[s.Name] = true
	defer delete(visited, s.Name)

	var newFields []*FieldInfo
	for _, field := range s.Fields {
		if field.IsEmbedded {
			// Resolve embedded struct
			embeddedStructName := field.StructTypeName
			if embeddedStructName == "" && field.Type != nil {
				// Try to get name from type if StructTypeName is empty
				if field.Type.Kind() == reflect.Struct {
					embeddedStructName = field.Type.Name()
				} else if field.Type.Kind() == reflect.Ptr {
					embeddedStructName = field.Type.Elem().Name()
				}
			}

			// If still empty, try AST type name (fallback)
			if embeddedStructName == "" {
				name, err := getEmbeddedFieldName(field.ASTType)
				if err == nil {
					embeddedStructName = name
				}
			}

			embeddedStruct, ok := structMap[embeddedStructName]
			if !ok {
				// If not found in our map, it might be an external type or not a struct we care about.
				// For now, we only flatten structs that we are generating code for.
				// If we can't find it, we'll just keep the embedded field.
				newFields = append(newFields, field)
				continue
			}

			// Recursively flatten the embedded struct first
			if err := flattenStruct(embeddedStruct, structMap, visited); err != nil {
				return err
			}

			// Add fields from embedded struct
			for _, embeddedField := range embeddedStruct.Fields {
				// We append the embedded fields directly.
				// Note: This modifies the struct definition to include fields from embedded structs directly.
				// This matches how Go promotes fields.
				newFields = append(newFields, embeddedField)
			}
		} else {
			newFields = append(newFields, field)
		}
	}

	// Replace fields
	s.Fields = newFields
	return nil
}
