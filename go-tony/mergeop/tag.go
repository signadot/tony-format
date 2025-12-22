package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
)

var tagSym = &tagSymbol{matchName: tagName}

func Tag() Symbol {
	return tagSym
}

const (
	tagName matchName = "tag"
)

type tagSymbol struct {
	matchName
}

func (s tagSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("tag op has no args, got %v", args)
	}
	return &tagOp{matchOp: matchOp{op: op{name: s.matchName, child: child}}}, nil
}

type tagOp struct {
	matchOp
}

func (g tagOp) Match(doc *ir.Node, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("tag op called on %s with tag %q\n", doc.Path(), doc.Tag)
	}

	// Parse tag into name and args
	// e.g., "!key(name)" -> name="key", args=["name"]
	tag := doc.Tag
	if tag == "" {
		// No tag - match against empty structure
		tagNode := ir.FromMap(map[string]*ir.Node{
			"name": ir.FromString(""),
			"args": ir.FromSlice(nil),
		})
		return f(tagNode, g.child)
	}

	head, args, _ := ir.TagArgs(tag)
	// head includes the !, strip it
	name := head
	if len(name) > 0 && name[0] == '!' {
		name = name[1:]
	}

	// Build args array
	argsNodes := make([]*ir.Node, len(args))
	for i, arg := range args {
		argsNodes[i] = ir.FromString(arg)
	}

	// Build structured match object: {name: "key", args: ["name"]}
	tagNode := ir.FromMap(map[string]*ir.Node{
		"name": ir.FromString(name),
		"args": ir.FromSlice(argsNodes),
	})

	return f(tagNode, g.child)
}
