package snap

import "github.com/signadot/tony-format/go-tony/ir"

// estimateNodeSize estimates the encoded size of a node using heuristics.
// This does not encode the node - it uses size estimation based on node structure.
// Returns an estimated size in bytes.
func estimateNodeSize(node *ir.Node) (int64, error) {
	if node == nil {
		return 0, nil
	}

	var size int64

	// Add overhead for tags
	if node.Tag != "" {
		size += int64(len(node.Tag) + 2) // tag + ": " or similar
	}

	switch node.Type {
	case ir.NullType:
		size += 4
	case ir.BoolType:
		size += 4
	case ir.NumberType:
		if node.Number != "" {
			size += int64(len(node.Number))
		} else if node.Int64 != nil {
			size += 20 // Estimate: int64 can be up to 20 digits + sign
		} else if node.Float64 != nil {
			size += 24 // Estimate: float64 representation
		} else {
			size += 10 // fallback estimate
		}
	case ir.StringType:
		size += int64(len(node.String))
		size += int64(len(node.String) / 10) // 10% overhead for escaping
		size += 2                            // quotes
	case ir.ArrayType:
		size += 2 // "[" and "]"
		for _, child := range node.Values {
			childSize, err := estimateNodeSize(child)
			if err != nil {
				return 0, err
			}
			size += childSize
			size += 2 // comma + space overhead
		}
	case ir.ObjectType:
		size += 2 // "{" and "}"
		maxPairs := len(node.Fields)
		if len(node.Values) < maxPairs {
			maxPairs = len(node.Values)
		}
		for i := 0; i < maxPairs; i++ {
			field := node.Fields[i]
			value := node.Values[i]

			if field != nil {
				size += int64(len(field.String))
				size += 3 // quotes around field name + ": "
			}

			valueSize, err := estimateNodeSize(value)
			if err != nil {
				return 0, err
			}
			size += valueSize
			size += 2 // comma + space overhead
		}
	case ir.CommentType:
		if len(node.Lines) > 0 {
			for _, line := range node.Lines {
				size += int64(len(line) + 2) // line + "# " + newline
			}
		}
		if len(node.Values) > 0 {
			valueSize, err := estimateNodeSize(node.Values[0])
			if err != nil {
				return 0, err
			}
			size += valueSize
		}
	default:
		size += 10 // Unknown type - conservative estimate
	}

	return size, nil
}
