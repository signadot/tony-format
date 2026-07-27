package libdiff

import "github.com/signadot/tony-format/go-tony/ir"

// IsOp reports whether a tag head, "!" and all, names a registered merge
// operation.  It is installed by package mergeop, which libdiff cannot import.
// Until then it answers no, which is the safe default: a diff that escapes
// nothing is what libdiff produced before the escape existed.
var IsOp = func(tag string) bool { return false }

// MakeDiff builds the patch node that turns from into to at one position.
//
// An inserted value goes into the patch as a value, and !insert applies it as
// a patch, so a value which holds a merge operation as data -- a stored rule,
// a stored patch -- would be interpreted rather than stored when the diff was
// applied.  Such a value is escaped with !raw, under which nothing is
// interpreted at any depth.
//
// A deleted value needs the escape just as much, though !delete never applies
// what it carries: Reverse turns the !delete into an !insert, which does, and
// a diff which cannot be reversed is not one.  !replace is the exception --
// it compares its from: and installs its to: whole, in either direction.
func MakeDiff(from, to *ir.Node) *ir.Node {
	switch {
	case from == nil:
		return escaped(to, InsertTag)
	case to == nil:
		return escaped(from, DeleteTag)
	default:
		return ir.FromMap(map[string]*ir.Node{
			"from": from,
			"to":   to,
		}).WithTag(ReplaceTag)
	}
}

// escaped writes node into a diff under op -- !insert or !delete -- carrying
// its own tag.  A value holding no merge operation keeps the long standing
// shape, its tag as the operation's argument; one which does gets !raw, which
// preserves the tag where it is and stops anything beneath being read as an
// instruction.
func escaped(node *ir.Node, op string) *ir.Node {
	switch {
	case hasOpTag(node):
		return node.Clone().WithTag(
			ir.TagCompose(op, nil, ir.TagCompose(RawTag, nil, node.Tag)))
	case node.Tag == "":
		return node.Clone().WithTag(op)
	default:
		return node.Clone().WithTag(op + "(" + node.Tag[1:] + ")")
	}
}

func MakeTagDiff(from, to string) string {
	switch {
	case from == "":
		return TagInsertTag + "(" + to[1:] + ")"
	case to == "":
		return TagDeleteTag + "(" + from[1:] + ")"
	default:
		return TagReplaceTag + "(" + from[1:] + "," + to[1:] + ")"
	}
}

// hasTag reports whether tag has what among its composed labels.  what carries
// its "!", as tag does.
func hasTag(tag, what string) bool {
	for tag != "" {
		head, _, rest := ir.TagArgs(tag)
		if head == what {
			return true
		}
		tag = rest
	}
	return false
}

// hasOpTag reports whether node or anything beneath it carries a tag naming a
// merge operation, and so would be interpreted rather than stored if the node
// reached a patch unescaped.
func hasOpTag(node *ir.Node) bool {
	if node == nil {
		return false
	}
	for tag := node.Tag; tag != ""; {
		head, _, rest := ir.TagArgs(tag)
		if IsOp(head) {
			return true
		}
		tag = rest
	}
	for _, f := range node.Fields {
		if hasOpTag(f) {
			return true
		}
	}
	for _, v := range node.Values {
		if hasOpTag(v) {
			return true
		}
	}
	return false
}
