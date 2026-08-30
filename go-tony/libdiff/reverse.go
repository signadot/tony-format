package libdiff

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
)

func Reverse(diff *ir.Node) (*ir.Node, error) {
	tmp := diff.Clone()
	err := tmp.Visit(func(node *ir.Node, isPost bool) (bool, error) {
		if !isPost {
			// Reversing rewrites operation names in place, so it may only
			// descend through the diff's own structure.  Beneath these four
			// lies the document: what !insert and !delete carry, the from: and
			// to: of a !replace, and everything under a !raw are values, and an
			// operation name in a value is data.  Rewriting one would change
			// what the diff says the document is.  Their own tags are still
			// reversed, on the way back out.
			for _, valueTag := range [...]string{InsertTag, DeleteTag, ReplaceTag, RawTag} {
				if hasTag(node.Tag, valueTag) {
					return false, nil
				}
			}
			return true, nil
		}
		// The operation is not always the head of the chain. A tag composes, and what
		// comes BEFORE the operation is the value's own labels -- presentation among
		// them. A patch written in flow style carries one: `!replace {from: 1, to: 5}`
		// parses as `!bracket.replace`, where the same patch computed by Diff carries a
		// bare `!replace`. Reading only the head found the operation in the second and
		// not the first, so Reverse returned such a patch UNCHANGED and said nothing --
		// and its caller then applied the patch it had asked to invert. The failure was
		// silent for !insert and !delete, which apply backwards perfectly well.
		//
		// So the labels ahead of the operation are set aside and put back afterwards,
		// outermost, in the order they were written. This defer is registered before the
		// container one below, so it runs last and restores them outside it.
		if pre, opTag := splitBeforeOp(node.Tag); pre != "" {
			node.Tag = opTag
			defer func() { node.Tag = joinTags(pre, node.Tag) }()
		}
		headTag, args, rest := ir.TagArgs(node.Tag)
		if headTag == StringDiffTag || headTag == ArrayDiffTag {
			// these reverse by reversing what is beneath them, but a tag diff
			// composed after one is still a tag diff and has to be reversed
			// itself
			containerTag, containerArgs := headTag, args
			node.Tag = rest
			defer func() {
				node.Tag = ir.TagCompose(containerTag, containerArgs, node.Tag)
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

// reversibleOps are the operations Reverse rewrites. A label that is not one of them, and
// not one of the four whose contents Reverse must not enter, is the value's own.
var reversibleOps = map[string]bool{
	DeleteTag: true, InsertTag: true, ReplaceTag: true,
	TagDeleteTag: true, TagInsertTag: true, TagReplaceTag: true,
	StringDiffTag: true, ArrayDiffTag: true,
}

// splitBeforeOp divides a tag chain at the first operation Reverse handles: the labels
// before it, and the chain from it onward. It answers pre == "" when the chain opens with
// an operation, or holds none at all, which is every chain a diff builds for itself.
func splitBeforeOp(tag string) (pre, opTag string) {
	rest := tag
	for rest != "" {
		head, _, next := ir.TagArgs(rest)
		if reversibleOps[head] {
			return pre, rest
		}
		pre = joinTags(pre, headWithArgs(rest))
		rest = next
	}
	return "", tag // no operation here; leave the chain as it was
}

// headWithArgs is the first label of a chain, arguments and all, as its own tag.
func headWithArgs(tag string) string {
	head, args, rest := ir.TagArgs(tag)
	if rest == "" {
		return tag
	}
	return ir.TagCompose(head, args, "")
}

// joinTags composes b after a, either of which may be empty.
func joinTags(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "." + b[1:]
}
