package mergeop

import (
	"github.com/signadot/tony-format/go-tony/ir"
)

// SplitChild divides opDoc's tag chain at the first tag naming a registered operation:
// preTag is the tags before it, tag is the operation's name without the '!', args are
// its arguments, and child is a clone of opDoc carrying the rest of the chain. When no
// tag in the chain names an operation, tag is "" and child is nil.
func SplitChild(opDoc *ir.Node) (preTag, tag string, args []string, child *ir.Node, err error) {
	if opDoc.Tag == "" {
		return "", "", nil, nil, nil
	}
	var (
		rest = ""
	)
	tag = opDoc.Tag
	for {
		if tag == "" {
			return preTag, "", nil, nil, nil
		}
		tag, args, rest = ir.TagArgs(tag)
		tag = tag[1:]
		if Lookup(tag) == nil {
			preTag = ir.TagCompose("!"+tag, args, preTag)
			tag = rest
			continue
		}
		child = opDoc.Clone()
		child.Tag = rest
		return preTag, tag, args, child, nil
	}
}
