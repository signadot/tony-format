package eval

import (
	"fmt"
	"strings"

	"github.com/tony-format/tony/ir"
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

func splitArgs(tag string) (string, []string, error) {
	preArgs, rest, ok := strings.Cut(tag, "(")
	if !ok {
		return tag, nil, nil
	}
	if len(rest) == 0 || rest[len(rest)-1] != ')' {
		return "", nil, fmt.Errorf("invalid operation args: %q", tag)
	}
	rest = rest[:len(rest)-1]
	args := strings.Split(rest, ",")
	return preArgs, args, nil
}
