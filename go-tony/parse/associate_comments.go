package parse

import (
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
)

func associateComments(node *ir.Node) *ir.Node {
	if node == nil {
		return nil
	}
	switch node.Type {
	case ir.CommentType:
		if len(node.Values) > 0 {
			node.Values[0] = associateComments(node.Values[0])
		}
	case ir.ObjectType, ir.ArrayType:
		var (
			lastLineComment *ir.Node
			eltWrap         *ir.Node
		)
		for i, elt := range node.Values {
			elt = associateComments(elt)
			if elt.Type == ir.CommentType {
				eltWrap = elt
				if len(elt.Values) == 0 {
					// Empty comment node - skip processing
					continue
				}
				elt = elt.Values[0]
			} else {
				eltWrap = nil
			}
			if !hasExtraContent(lastLineComment) {
				lastLineComment = elt.Comment
				continue
			}
			carry := splitLineCommentLines(lastLineComment)
			if eltWrap == nil {
				eltWrap = &ir.Node{
					Type:        ir.CommentType,
					ParentIndex: i,
					Parent:      node,
					Values:      []*ir.Node{elt},
				}
				node.Values[i] = eltWrap
				elt.Parent = eltWrap
				elt.ParentIndex = 0
			}
			eltWrap.Lines = append(eltWrap.Lines, carry...)
			if !hasContent(lastLineComment) {
				lastLineComment.Parent.Comment = nil
			}
			lastLineComment = elt.Comment
		}
		if !hasExtraContent(lastLineComment) {
			return node
		}

		if node.Comment == nil {
			node.Comment = &ir.Node{
				Type:   ir.CommentType,
				Parent: node,
				Lines:  []string{""},
			}
		}
		carry := splitLineCommentLines(lastLineComment)
		node.Comment.Lines = append(node.Comment.Lines, carry...)
		if !hasContent(lastLineComment) {
			lastLineComment.Parent.Comment = nil
		}
	}
	return node
}

func hasContent(lnComment *ir.Node) bool {
	if lnComment == nil {
		return false
	}
	for _, ln := range lnComment.Lines {
		if ln != "" {
			return true
		}
	}
	return false
}

func hasExtraContent(lnComment *ir.Node) bool {
	if lnComment == nil {
		return false
	}
	if len(lnComment.Lines) == 0 {
		return false
	}
	if lnComment.Parent == nil || lnComment.Parent.Type != ir.StringType {
		return len(lnComment.Lines) > 1
	}
	strParent := lnComment.Parent
	n := len(strParent.Lines)
	if n == 0 {
		// not mstring
		return len(lnComment.Lines) > 1
	}
	return len(lnComment.Lines) > n
}

func splitLineCommentLines(lnComment *ir.Node) []string {
	if lnComment == nil {
		return nil
	}
	if len(lnComment.Lines) <= 1 {
		return nil
	}
	if lnComment.Parent == nil || lnComment.Parent.Type != ir.StringType {
		lns, rest := lnComment.Lines[:1], lnComment.Lines[1:]
		lnComment.Lines = lns
		return rest
	}
	strNode := lnComment.Parent
	if len(strNode.Lines) == 0 {
		lns, rest := lnComment.Lines[:1], lnComment.Lines[1:]
		lnComment.Lines = lns
		return rest
	}
	n := len(strNode.Lines)
	lns, rest := lnComment.Lines[:n], lnComment.Lines[n:]
	lnComment.Lines = lns
	return rest
}

func getLC(n *ir.Node) string {
	if n == nil {
		return ""
	}
	if n.Comment == nil {
		return ""
	}
	return strings.Join(n.Comment.Lines, "|")
}
