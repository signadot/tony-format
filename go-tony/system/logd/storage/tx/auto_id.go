package tx

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/autoid"
)

// InjectAutoIDs walks each patcher's patch data and injects auto-generated IDs
// for arrays that match schema AutoIDFields.
//
// For each matching array element:
// - If the key field is null or missing, a new ID is generated
// - IDs are generated using the commit number to ensure monotonicity
// - Index tracks position within the commit for uniqueness
//
// Returns the number of IDs injected.
func InjectAutoIDs(commit int64, schema *api.Schema, data []*PatcherData) int {
	if schema == nil || len(schema.AutoIDFields) == 0 {
		return 0
	}

	count := 0
	idx := 0 // Global index across all patches

	for _, pd := range data {
		if pd.API == nil || pd.API.Patch.Data == nil {
			continue
		}

		// Use the patch path as the starting kpath
		injected := injectAutoIDsRec(commit, schema, pd.API.Patch.Data, pd.API.Patch.Path, &idx)
		count += injected
	}

	return count
}

// injectAutoIDsRec recursively walks a node tree and injects auto-IDs.
func injectAutoIDsRec(commit int64, schema *api.Schema, node *ir.Node, kpath string, idx *int) int {
	if node == nil {
		return 0
	}

	count := 0

	switch node.Type {
	case ir.ObjectType:
		for i, field := range node.Fields {
			if i >= len(node.Values) {
				continue
			}
			childPath := field.String
			if kpath != "" {
				childPath = kpath + "." + childPath
			}
			count += injectAutoIDsRec(commit, schema, node.Values[i], childPath, idx)
		}

	case ir.ArrayType:
		// Check if this array path has an auto-id field
		aid := schema.AutoID(kpath)
		if aid != nil {
			// This array should have auto-generated IDs
			for _, elem := range node.Values {
				if elem.Type != ir.ObjectType {
					continue
				}

				// Check if the key field is null or missing
				keyVal := ir.Get(elem, aid.Field)
				if keyVal == nil || keyVal.Type == ir.NullType {
					// Generate and inject ID
					id := autoid.Generate(commit, *idx)
					*idx++

					if keyVal == nil {
						// Field doesn't exist - add it
						elem.Fields = append([]*ir.Node{{Type: ir.StringType, String: aid.Field}}, elem.Fields...)
						elem.Values = append([]*ir.Node{{Type: ir.StringType, String: id}}, elem.Values...)
					} else {
						// Field exists but is null - replace the value
						for j, f := range elem.Fields {
							if f.String == aid.Field && j < len(elem.Values) {
								elem.Values[j] = &ir.Node{Type: ir.StringType, String: id}
								break
							}
						}
					}
					count++
				}

				// Recurse into the element (for nested auto-id fields)
				elemPath := kpath // Elements inherit the array path for recursion
				count += injectAutoIDsRec(commit, schema, elem, elemPath, idx)
			}
		} else {
			// No auto-id for this array, but still recurse for nested arrays
			for i, elem := range node.Values {
				elemPath := kpath + "[" + itoa(i) + "]"
				count += injectAutoIDsRec(commit, schema, elem, elemPath, idx)
			}
		}
	}

	return count
}

// itoa is a simple int to string conversion without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
