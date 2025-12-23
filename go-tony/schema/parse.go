package schema

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
)

// ParseSchema parses a schema from an IR node
func ParseSchema(node *ir.Node) (*Schema, error) {
	if node.Type != ir.ObjectType {
		return nil, fmt.Errorf("schema must be an object")
	}

	s := &Schema{
		Tags:   make(map[string]*TagDefinition),
		Define: make(map[string]*ir.Node),
	}

	// Parse context field (can be "@context", "context", or "contexts")
	var ctxNode *ir.Node
	if ctxNode = ir.Get(node, "@context"); ctxNode == nil {
		if ctxNode = ir.Get(node, "context"); ctxNode == nil {
			ctxNode = ir.Get(node, "contexts")
		}
	}

	if ctxNode != nil {
		ctx := &Context{}
		if err := ctx.FromIR(ctxNode); err != nil {
			return nil, fmt.Errorf("failed to parse context: %w", err)
		}
		s.Context = ctx
	} else {
		// Default context if not specified
		s.Context = DefaultContext()
	}

	// Parse signature field
	if sigNode := ir.Get(node, "signature"); sigNode != nil {
		sig, err := parseSignature(sigNode)
		if err != nil {
			return nil, fmt.Errorf("failed to parse signature: %w", err)
		}
		s.Signature = sig
	}

	// Parse tags field
	if tagsNode := ir.Get(node, "tags"); tagsNode != nil {
		if tagsNode.Type != ir.ObjectType {
			return nil, fmt.Errorf("tags must be an object")
		}
		for i := range tagsNode.Fields {
			tagName := tagsNode.Fields[i].String
			tagDefNode := tagsNode.Values[i]
			tagDef, err := parseTagDefinition(tagName, tagDefNode)
			if err != nil {
				return nil, fmt.Errorf("failed to parse tag %q: %w", tagName, err)
			}
			s.Tags[tagName] = tagDef
		}
	}

	// Parse define field
	if defineNode := ir.Get(node, "define"); defineNode != nil {
		if defineNode.Type != ir.ObjectType {
			return nil, fmt.Errorf("define must be an object")
		}
		for i := range defineNode.Fields {
			name := defineNode.Fields[i].String
			def := defineNode.Values[i]
			s.Define[name] = def
		}
	}

	// Parse accept field
	if acceptNode := ir.Get(node, "accept"); acceptNode != nil {
		s.Accept = acceptNode
	}

	// If schema has a signature name, ensure it's in Tags (for programmatic reading)
	// Writers don't need to duplicate signature.name in tags field
	if s.Signature != nil && s.Signature.Name != "" {
		if s.Tags == nil {
			s.Tags = make(map[string]*TagDefinition)
		}
		if _, exists := s.Tags[s.Signature.Name]; !exists {
			// Create a tag definition for the signature name
			s.Tags[s.Signature.Name] = &TagDefinition{
				Name: s.Signature.Name,
				// Contexts, SchemaRef, and Description can be set explicitly in tags if needed
			}
		}
	}

	// SAT-based satisfiability check for accept field and reachable definitions
	if err := CheckAcceptSatisfiability(s); err != nil {
		return nil, err
	}

	return s, nil
}

// parseSignature parses a signature from an IR node
func parseSignature(node *ir.Node) (*Signature, error) {
	if node.Type != ir.ObjectType {
		return nil, fmt.Errorf("signature must be an object")
	}

	sig := &Signature{}

	// Parse name field
	if nameNode := ir.Get(node, "name"); nameNode != nil {
		if nameNode.Type != ir.StringType {
			return nil, fmt.Errorf("signature.name must be a string")
		}
		sig.Name = nameNode.String
	}

	// Parse args field
	if argsNode := ir.Get(node, "args"); argsNode != nil {
		if argsNode.Type != ir.ArrayType {
			return nil, fmt.Errorf("signature.args must be an array")
		}
		sig.Args = make([]Arg, 0, len(argsNode.Values))
		for _, argNode := range argsNode.Values {
			arg, err := parseArg(argNode)
			if err != nil {
				return nil, fmt.Errorf("failed to parse arg: %w", err)
			}
			sig.Args = append(sig.Args, arg)
		}
	}

	return sig, nil
}

// parseArg parses an argument from an IR node
func parseArg(node *ir.Node) (Arg, error) {
	if node.Type != ir.ObjectType {
		return Arg{}, fmt.Errorf("arg must be an object")
	}

	arg := Arg{}

	// Parse name field
	if nameNode := ir.Get(node, "name"); nameNode != nil {
		if nameNode.Type != ir.StringType {
			return Arg{}, fmt.Errorf("arg.name must be a string")
		}
		arg.Name = nameNode.String
	}

	// Parse match field
	if matchNode := ir.Get(node, "match"); matchNode != nil {
		arg.Match = matchNode
	}

	return arg, nil
}

// parseTagDefinition parses a tag definition from an IR node
func parseTagDefinition(tagName string, node *ir.Node) (*TagDefinition, error) {
	if node.Type != ir.ObjectType {
		return nil, fmt.Errorf("tag definition must be an object")
	}

	def := &TagDefinition{
		Name: tagName,
	}

	// Parse contexts field
	if contextsNode := ir.Get(node, "contexts"); contextsNode != nil {
		if contextsNode.Type != ir.ArrayType {
			return nil, fmt.Errorf("tag.contexts must be an array")
		}
		def.Contexts = make([]string, 0, len(contextsNode.Values))
		for _, ctxNode := range contextsNode.Values {
			if ctxNode.Type != ir.StringType {
				return nil, fmt.Errorf("tag.contexts entries must be strings")
			}
			def.Contexts = append(def.Contexts, ctxNode.String)
		}
	}

	// Parse schema field (schema reference)
	if schemaNode := ir.Get(node, "schema"); schemaNode != nil {
		if schemaNode.Type != ir.StringType {
			return nil, fmt.Errorf("tag.schema must be a string")
		}
		def.SchemaRef = schemaNode.String
	}

	// Parse description field
	if descNode := ir.Get(node, "description"); descNode != nil {
		if descNode.Type != ir.StringType {
			return nil, fmt.Errorf("tag.description must be a string")
		}
		def.Description = descNode.String
	}

	return def, nil
}
