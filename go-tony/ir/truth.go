package ir

// Truth is what a node says when read as a condition.
//
// It sees through a comment, which is what was SAID about a value and not part of
// it: a commented true used to be false, because CommentType fell to the
// everything-else branch. Nothing answers false, rather than panicking, since a
// condition on a node which is not there is a question with an answer.
func Truth(node *Node) bool {
	node = Uncomment(node)
	if node == nil {
		return false
	}
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
