package schema

import (
	_ "embed"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

//go:embed base.tony
var baseTony []byte

// BaseDefinitions returns the definitions from base.tony as a map.
// These include primitive type definitions like string, number, bool, etc.
func BaseDefinitions() (map[string]*ir.Node, error) {
	node, err := parse.Parse(baseTony)
	if err != nil {
		return nil, err
	}

	defineNode := ir.Get(node, "define")
	if defineNode == nil || defineNode.Type != ir.ObjectType {
		return nil, nil
	}

	defs := make(map[string]*ir.Node)
	for i := range defineNode.Fields {
		name := defineNode.Fields[i].String
		defs[name] = defineNode.Values[i]
	}
	return defs, nil
}
