package libdiff

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
)

func Reverse(diff *ir.Node) (*ir.Node, error) {
	tmp := diff.Clone()
	err := tmp.Visit(func(node *ir.Node, isPost bool) (bool, error) {
		if !isPost {
			// Everything under a !raw is the document's data, not
			// instructions, so the operation names in it must be left alone:
			// reversing them would rewrite the values themselves.  The !raw
			// node's own tag is still reversed, on the way back out.
			return !hasTag(node.Tag, RawTag), nil
		}
		headTag, args, rest := ir.TagArgs(node.Tag)
		if headTag == StringDiffTag {
			// a strdiff reverses by reversing the pieces beneath it, but a
			// tag diff composed after it still has to be reversed itself
			strDiffArgs := args
			node.Tag = rest
			defer func() {
				node.Tag = ir.TagCompose(StringDiffTag, strDiffArgs, node.Tag)
			}()
			headTag, args, rest = ir.TagArgs(rest)
		}
		switch headTag {
		case DeleteTag:
			node.Tag = ir.TagCompose(InsertTag, args, rest)
		case InsertTag:
			node.Tag = ir.TagCompose(DeleteTag, args, rest)
		case ReplaceTag:
			if node.Type != ir.ObjectType {
				return false, fmt.Errorf("wrong type for !diff: %s at %s", node.Type, node.Path())
			}
			fromIndex, toIndex := -1, -1
			for i := range node.Fields {
				switch node.Fields[i].String {
				case "from":
					fromIndex = i
					if toIndex != -1 {
						goto found
					}
				case "to":
					toIndex = i
					if fromIndex != -1 {
						goto found
					}
				}
			}
			return false, fmt.Errorf("missing from/to in %s at %s", ReplaceTag, node.Path())
		found:
			node.Values[fromIndex], node.Values[toIndex] = node.Values[toIndex], node.Values[fromIndex]
			node.Values[fromIndex].ParentIndex = fromIndex
			node.Values[toIndex].ParentIndex = toIndex
			return true, nil

		case TagDeleteTag:
			node.Tag = ir.TagCompose(TagInsertTag, args, rest)
		case TagInsertTag:
			node.Tag = ir.TagCompose(TagDeleteTag, args, rest)
		case TagReplaceTag:
			if len(args) != 2 {
				return false, fmt.Errorf("wrong number of args for %s: %d", TagReplaceTag, len(args))
			}
			args[0], args[1] = args[1], args[0]
			node.Tag = ir.TagCompose(TagReplaceTag, args, rest)
		default:
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return tmp, nil
}
