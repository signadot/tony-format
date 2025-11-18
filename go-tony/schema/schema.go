package schema

import (
	"fmt"

	"github.com/signadot/tony-format/tony/ir"
)

// Schema represents a Tony schema document
type Schema struct {
	// The context in which this schema lives
	Context *Context

	// Signature defines how the schema can be referenced
	Signature *Signature `tony:"signature"`

	// Map of short name schema to fully qualified name
	With map[string]string

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
