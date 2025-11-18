package schema

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
)

// Schema represents a Tony schema document
type Schema struct {
	// The context in which this schema lives (handles @context and with)
	Context *Context

	// Signature defines how the schema can be referenced
	Signature *Signature `tony:"signature"`

	// Tags defines tags that this schema introduces
	// Map of tag name -> TagDefinition
	//
	// When reading: If signature.name exists, a tag with that name is automatically
	// added to Tags (if not already present) for programmatic access. Writers don't
	// need to duplicate signature.name in the tags field.
	//
	// When encoding: Tags matching signature.name with no additional fields
	// (only Name set, no Contexts/SchemaRef/Description) are elided to avoid duplication.
	Tags map[string]*TagDefinition `tony:"tags"`

	// Define provides a place for value definitions, like json-schema $defs
	Define map[string]*ir.Node `tony:"define"`

	// Accept defines what documents this schema accepts
	Accept *ir.Node `tony:"accept"`
}

// Signature defines how a schema can be referenced
type Signature struct {
	// Name is the schema name, so we can use '!name' to refer to this
	Name string `tony:"name"`

	// Args are the schema arguments (for parameterized schemas)
	Args []Arg `tony:"args"`
}

type Arg struct {
	Name  string
	Match *ir.Node
}

// Validate validates a document against this schema
func (s *Schema) Validate(doc *ir.Node) error {
	if s.Accept == nil {
		return nil // No accept clause means everything is accepted
	}

	// TODO: Implement validation logic using match operations
	return fmt.Errorf("validation not yet implemented")
}

// ToIR converts a Schema to an IR node
// Elides tags that match signature.name and have no additional fields (auto-injected tags)
func (s *Schema) ToIR() (*ir.Node, error) {
	node := &ir.Node{
		Type:   ir.ObjectType,
		Fields: make([]*ir.Node, 0),
		Values: make([]*ir.Node, 0),
	}

	// Add context field
	if s.Context != nil {
		ctxNode, err := s.Context.ToIR()
		if err != nil {
			return nil, fmt.Errorf("failed to convert context to IR: %w", err)
		}
		node.Fields = append(node.Fields, ir.FromString("@context"))
		node.Values = append(node.Values, ctxNode)
	}

	// Add signature field
	if s.Signature != nil {
		sigNode, err := s.signatureToIR()
		if err != nil {
			return nil, fmt.Errorf("failed to convert signature to IR: %w", err)
		}
		node.Fields = append(node.Fields, ir.FromString("signature"))
		node.Values = append(node.Values, sigNode)
	}

	// Add tags field (eliding auto-injected tags)
	if tagsNode := s.tagsToIR(); tagsNode != nil {
		node.Fields = append(node.Fields, ir.FromString("tags"))
		node.Values = append(node.Values, tagsNode)
	}

	// Add define field
	if defineNode := s.defineToIR(); defineNode != nil {
		node.Fields = append(node.Fields, ir.FromString("define"))
		node.Values = append(node.Values, defineNode)
	}

	// Add accept field
	if s.Accept != nil {
		node.Fields = append(node.Fields, ir.FromString("accept"))
		node.Values = append(node.Values, s.Accept)
	}

	return node, nil
}

// signatureToIR converts a Signature to an IR node
func (s *Schema) signatureToIR() (*ir.Node, error) {
	if s.Signature == nil {
		return nil, nil
	}

	sigNode := &ir.Node{
		Type:   ir.ObjectType,
		Fields: make([]*ir.Node, 0),
		Values: make([]*ir.Node, 0),
	}

	if s.Signature.Name != "" {
		sigNode.Fields = append(sigNode.Fields, ir.FromString("name"))
		sigNode.Values = append(sigNode.Values, ir.FromString(s.Signature.Name))
	}

	if len(s.Signature.Args) > 0 {
		sigNode.Fields = append(sigNode.Fields, ir.FromString("args"))
		argsNode := &ir.Node{
			Type:   ir.ArrayType,
			Values: make([]*ir.Node, 0, len(s.Signature.Args)),
		}
		for _, arg := range s.Signature.Args {
			argNode := &ir.Node{
				Type:   ir.ObjectType,
				Fields: make([]*ir.Node, 0),
				Values: make([]*ir.Node, 0),
			}
			if arg.Name != "" {
				argNode.Fields = append(argNode.Fields, ir.FromString("name"))
				argNode.Values = append(argNode.Values, ir.FromString(arg.Name))
			}
			if arg.Match != nil {
				argNode.Fields = append(argNode.Fields, ir.FromString("match"))
				argNode.Values = append(argNode.Values, arg.Match)
			}
			argsNode.Values = append(argsNode.Values, argNode)
		}
		sigNode.Values = append(sigNode.Values, argsNode)
	}

	return sigNode, nil
}

// tagsToIR converts Tags to an IR node, eliding auto-injected tags
func (s *Schema) tagsToIR() *ir.Node {
	if s.Tags == nil || len(s.Tags) == 0 {
		return nil
	}

	tagsNode := &ir.Node{
		Type:   ir.ObjectType,
		Fields: make([]*ir.Node, 0),
		Values: make([]*ir.Node, 0),
	}

	for tagName, tagDef := range s.Tags {
		// Skip auto-injected tags: if tag name matches signature.name and has no additional fields
		if s.Signature != nil && tagName == s.Signature.Name {
			// Check if this is just the auto-injected tag (only Name field set)
			if len(tagDef.Contexts) == 0 && tagDef.SchemaRef == "" && tagDef.Description == "" {
				continue // Elide this auto-injected tag
			}
		}
		// Encode the tag definition
		tagDefNode := s.tagDefinitionToIR(tagDef)
		tagsNode.Fields = append(tagsNode.Fields, ir.FromString(tagName))
		tagsNode.Values = append(tagsNode.Values, tagDefNode)
	}

	// Only return if there are tags to include
	if len(tagsNode.Fields) == 0 {
		return nil
	}

	return tagsNode
}

// tagDefinitionToIR converts a TagDefinition to an IR node
func (s *Schema) tagDefinitionToIR(tagDef *TagDefinition) *ir.Node {
	tagDefNode := &ir.Node{
		Type:   ir.ObjectType,
		Fields: make([]*ir.Node, 0),
		Values: make([]*ir.Node, 0),
	}

	if len(tagDef.Contexts) > 0 {
		tagDefNode.Fields = append(tagDefNode.Fields, ir.FromString("contexts"))
		contextsNode := &ir.Node{
			Type:   ir.ArrayType,
			Values: make([]*ir.Node, 0, len(tagDef.Contexts)),
		}
		for _, ctx := range tagDef.Contexts {
			contextsNode.Values = append(contextsNode.Values, ir.FromString(ctx))
		}
		tagDefNode.Values = append(tagDefNode.Values, contextsNode)
	}

	if tagDef.SchemaRef != "" {
		tagDefNode.Fields = append(tagDefNode.Fields, ir.FromString("schema"))
		tagDefNode.Values = append(tagDefNode.Values, ir.FromString(tagDef.SchemaRef))
	}

	if tagDef.Description != "" {
		tagDefNode.Fields = append(tagDefNode.Fields, ir.FromString("description"))
		tagDefNode.Values = append(tagDefNode.Values, ir.FromString(tagDef.Description))
	}

	return tagDefNode
}

// defineToIR converts Define to an IR node
func (s *Schema) defineToIR() *ir.Node {
	if s.Define == nil || len(s.Define) == 0 {
		return nil
	}

	defineNode := &ir.Node{
		Type:   ir.ObjectType,
		Fields: make([]*ir.Node, 0, len(s.Define)),
		Values: make([]*ir.Node, 0, len(s.Define)),
	}

	for name, def := range s.Define {
		defineNode.Fields = append(defineNode.Fields, ir.FromString(name))
		defineNode.Values = append(defineNode.Values, def)
	}

	return defineNode
}
