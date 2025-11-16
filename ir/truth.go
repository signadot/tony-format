package ir

func Truth(node *Node) bool {
	switch node.Type {
	case ObjectType:
		return len(node.Fields) != 0
	case ArrayType:
		return len(node.Values) != 0
	case StringType:
		return node.String != ""
	case NumberType:
		if node.Int64 != nil {
			return *node.Int64 != 0
		}
		if node.Float64 != nil {
			return *node.Float64 != 0.0
		}
		return node.Number != ""
	case BoolType:
		return node.Bool
	case NullType, CommentType:
		return false
	default:
		panic("type")
	}
}
