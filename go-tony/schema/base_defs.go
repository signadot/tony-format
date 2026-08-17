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

// MergeBaseDefinitions merges base.tony definitions into a schema node, before
// ParseSchema reads it.  A definition the schema makes itself wins: the base
// ones fill in what it did not say.
//
// _ir and ir are left out.  They describe the IR encoding of a document in
// terms of itself, and expansion of a definition reference is eager, so a
// schema which mentions them expands forever.
func MergeBaseDefinitions(node *ir.Node) error {
	baseDefs, err := BaseDefinitions()
	if err != nil {
		return err
	}
	if baseDefs == nil {
		return nil
	}

	skipDefs := map[string]bool{
		"_ir": true,
		"ir":  true,
	}

	// Get or create the define section
	defineNode := ir.Get(node, "define")
	if defineNode == nil {
		defineNode = ir.FromMap(map[string]*ir.Node{})
		node.Fields = append(node.Fields, ir.FromString("define"))
		node.Values = append(node.Values, defineNode)
	}

	// Build a set of existing definition names
	existing := make(map[string]bool)
	for _, field := range defineNode.Fields {
		if field.Type == ir.StringType {
			existing[field.String] = true
		}
	}

	// Add base definitions that don't already exist (except skip list)
	for name, def := range baseDefs {
		if !existing[name] && !skipDefs[name] {
			defineNode.Fields = append(defineNode.Fields, ir.FromString(name))
			defineNode.Values = append(defineNode.Values, def)
		}
	}

	return nil
}
