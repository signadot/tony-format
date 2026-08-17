package api

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

const (
	autoIDTag = "!logd-auto-id"
	// keyTag declares an array keyed on a field the CLIENT supplies, where !logd-auto-id
	// declares one the server generates. Both mean "merged and indexed by identity".
	keyTag = "!logd-key"
)

// ParseSchemaFromNode extracts logd schema from a Tony schema node.
// It walks the "define" section looking for fields tagged with !logd-auto-id.
//
// Example Tony schema:
//
//	define:
//	  users:
//	    id: !logd-auto-id
//	    name: ...
//	  orders:
//	    items:
//	      sku: !logd-auto-id
//	      qty: ...
//
// This produces AutoIDFields:
//
//	{Path: "users", Field: "id"}
//	{Path: "orders.items", Field: "sku"}
func ParseSchemaFromNode(node *ir.Node) *Schema {
	if node == nil || node.Type != ir.ObjectType {
		return nil
	}

	defineNode := ir.Get(node, "define")
	if defineNode == nil || defineNode.Type != ir.ObjectType {
		return nil
	}

	var fields []AutoIDField
	var keys []KeyField
	walkDefine(defineNode, "", &fields, &keys)

	if len(fields) == 0 && len(keys) == 0 {
		return nil
	}

	return &Schema{AutoIDFields: fields, KeyFields: keys}
}

// walkDefine recursively walks the define tree to find !logd-auto-id tagged fields.
func walkDefine(node *ir.Node, parentPath string, fields *[]AutoIDField, keys *[]KeyField) {
	if node == nil || node.Type != ir.ObjectType {
		return
	}

	for i, field := range node.Fields {
		fieldName := field.String
		value := node.Values[i]

		// An explicitly declared key: the parent path is the array, this field identifies
		// its elements, and the client supplies the value.
		if ir.TagHas(value.Tag, keyTag) {
			*keys = append(*keys, KeyField{Path: parentPath, Field: fieldName})
			continue
		}

		// Check if this field has !logd-auto-id tag
		if ir.TagHas(value.Tag, autoIDTag) {
			// Found an auto-id field
			// The parent path is the keyed array, fieldName is the key field
			*fields = append(*fields, AutoIDField{
				Path:  parentPath,
				Field: fieldName,
			})
			continue
		}

		// Recurse into object children
		if value.Type == ir.ObjectType {
			childPath := kpath.ChildField(parentPath, fieldName)
			walkDefine(value, childPath, fields, keys)
		}
	}
}
