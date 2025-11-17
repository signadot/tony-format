package mergeop

import (
	"github.com/signadot/tony-format/go-tony/ir"
)

func SplitChild(opDoc *ir.Node) (tag string, args []string, child *ir.Node, err error) {
	if opDoc.Tag == "" {
		return "", nil, nil, nil
	}
	var rest = ""
	tag = opDoc.Tag
	for {
		if tag == "" {
			return "", nil, nil, nil
		}
		tag, args, rest = ir.TagArgs(tag)
		tag = tag[1:]
		if Lookup(tag) == nil {
			tag = rest
			continue
		}
		child = opDoc.Clone()
		child.Tag = rest
		return tag, args, child, nil
	}
}
