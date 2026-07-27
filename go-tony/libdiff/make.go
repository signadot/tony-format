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
// interpreted at any depth.  A deleted value is never applied, and !replace
// installs its to: verbatim, so neither needs the escape.
func MakeDiff(from, to *ir.Node) *ir.Node {
	switch {
	case from == nil:
		if hasOpTag(to) {
			return to.Clone().WithTag(
				ir.TagCompose(InsertTag, nil, ir.TagCompose(RawTag, nil, to.Tag)))
		}
		if to.Tag == "" {
			return to.Clone().WithTag(InsertTag)
		}
		return to.Clone().WithTag(InsertTag + "(" + to.Tag[1:] + ")")
	case to == nil:
		if from.Tag == "" {
			return from.Clone().WithTag(DeleteTag)
		}
		return from.Clone().WithTag(DeleteTag + "(" + from.Tag[1:] + ")")
	default:
		return ir.FromMap(map[string]*ir.Node{
			"from": from,
			"to":   to,
		}).WithTag(ReplaceTag)
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
