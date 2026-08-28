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
// a patch -- against absence, so what results is the value -- so a value which
// holds a merge operation as data -- a stored rule, a stored patch -- would be
// interpreted rather than stored when the diff was applied.  Such a value is
// escaped with !raw, under which nothing is interpreted at any depth.
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

// Escape makes node safe to carry in a patch as DATA.
//
// A value which holds a merge operation anywhere in it -- a stored rule, a
// stored patch, a charter -- would be interpreted rather than stored when the
// patch was applied, so it gets !raw, under which nothing is read as an
// instruction at any depth.  A value holding no operation is returned as it is:
// there is nothing to protect it from, and wrapping it would change what a
// reader sees.
//
// Anything that builds a patch out of MATERIALIZED data has to do this, and a
// diff is not the only thing that does: logd re-states a scope's owned paths
// from the scoped view when building an overlay, and that value came from a
// document, where an operation tag means data.
func Escape(node *ir.Node) *ir.Node {
	if !hasOpTag(node) {
		return node
	}
	return node.Clone().WithTag(ir.TagCompose(RawTag, nil, node.Tag))
}

// escaped writes node into a diff under op -- !insert or !delete -- carrying
// its own tag.  A value holding no merge operation keeps the long standing
// shape, its tag as the operation's argument; one which does gets !raw, which
// preserves the tag where it is and stops anything beneath being read as an
// instruction.
func escaped(node *ir.Node, op string) *ir.Node {
	// An operation belongs on the VALUE, and a head comment is a wrapper node AROUND
	// the value. A tag on the wrapper is seen by nothing: mergeop walks past a comment
	// before it looks for an operation, so the operation never ran -- an !insert
	// merged instead of replacing -- and the log writes a comment as its lines and its
	// child, so the tag was not even serialized. A !delete on a commented value
	// reached the log as the value, and the delta reinstated what it was meant to
	// remove (xqpvk3ehh12ks89mj5n0).
	if node.Type == ir.CommentType && len(node.Values) == 1 {
		w := node.Clone()
		w.Values[0] = escaped(w.Values[0], op)
		w.Values[0].Parent = w
		w.Values[0].ParentIndex = 0
		return w
	}
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
