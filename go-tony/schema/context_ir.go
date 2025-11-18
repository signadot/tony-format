package schema

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
)

// FromIR creates a Context from an IR node representing a JSON-LD style context
// The node can be:
//   - A string (URI)
//   - An object mapping terms to URIs (e.g., {"match": "tony-format/context/match"})
//   - An array of contexts
// Ensures InOut and OutIn are always consistent: OutIn[uri][short] == true iff InOut[short] == uri
func (c *Context) FromIR(node *ir.Node) error {
	// Initialize maps if nil
	if c.OutIn == nil {
		c.OutIn = make(map[string]map[string]bool)
	}
	if c.InOut == nil {
		c.InOut = make(map[string]string)
	}

	switch node.Type {
	case ir.StringType:
		// Simple URI string - no terms mapped, just record the URI
		uri := node.String
		if c.OutIn[uri] == nil {
			c.OutIn[uri] = make(map[string]bool)
		}
		// No InOut entry needed since there's no term mapping
		return nil

	case ir.ObjectType:
		// Object mapping terms to URIs
		for i := range node.Fields {
			term := node.Fields[i].String
			value := node.Values[i]
			
			if value.Type != ir.StringType {
				return fmt.Errorf("context term %q has non-string value (type %v), only string URIs are supported", term, value.Type)
			}
			
			uri := value.String
			
			// Remove any existing mapping for this term to maintain consistency
			if oldURI, exists := c.InOut[term]; exists {
				if c.OutIn[oldURI] != nil {
					delete(c.OutIn[oldURI], term)
					if len(c.OutIn[oldURI]) == 0 {
						delete(c.OutIn, oldURI)
					}
				}
			}
			
			// Set the new mapping in both directions
			c.InOut[term] = uri
			if c.OutIn[uri] == nil {
				c.OutIn[uri] = make(map[string]bool)
			}
			c.OutIn[uri][term] = true
		}
		return nil

	case ir.ArrayType:
		// Array of contexts - merge them
		for _, ctxNode := range node.Values {
			if err := c.FromIR(ctxNode); err != nil {
				return fmt.Errorf("error parsing context in array: %w", err)
			}
		}
		return nil

	default:
		return fmt.Errorf("context must be a string, object, or array, got %v", node.Type)
	}
}

// ToIR converts a Context to an IR node representing a JSON-LD style context
// Returns an object mapping terms to URIs (e.g., {"match": "tony-format/context/match"})
// If there's only one URI with no terms, returns a string URI
// Returns an error if InOut and OutIn are inconsistent
func (c *Context) ToIR() (*ir.Node, error) {
	if c == nil {
		return nil, fmt.Errorf("cannot convert nil context to IR")
	}

	// Ensure maps are initialized
	if c.InOut == nil {
		c.InOut = make(map[string]string)
	}
	if c.OutIn == nil {
		c.OutIn = make(map[string]map[string]bool)
	}

	// Verify consistency: OutIn[uri][term] == true iff InOut[term] == uri
	for term, uri := range c.InOut {
		if c.OutIn[uri] == nil || !c.OutIn[uri][term] {
			return nil, fmt.Errorf("inconsistent context: InOut[%q] = %q but OutIn[%q][%q] is not true", term, uri, uri, term)
		}
	}

	// Check OutIn for any terms that aren't in InOut
	for uri, terms := range c.OutIn {
		for term := range terms {
			if c.InOut[term] != uri {
				return nil, fmt.Errorf("inconsistent context: OutIn[%q][%q] = true but InOut[%q] = %q (expected %q)", uri, term, term, c.InOut[term], uri)
			}
		}
	}

	// Build the set of all term->URI mappings from InOut (canonical source)
	termToURI := make(map[string]string)
	for term, uri := range c.InOut {
		termToURI[term] = uri
	}

	// If we have term mappings, return an object
	if len(termToURI) > 0 {
		node := &ir.Node{
			Type:   ir.ObjectType,
			Fields: make([]*ir.Node, 0, len(termToURI)),
			Values: make([]*ir.Node, 0, len(termToURI)),
		}

		for term, uri := range termToURI {
			field := ir.FromString(term)
			value := ir.FromString(uri)
			node.Fields = append(node.Fields, field)
			node.Values = append(node.Values, value)
		}

		return node, nil
	}

	// If we only have OutIn URIs with no terms, return the first URI as a string
	if len(c.OutIn) > 0 {
		for uri := range c.OutIn {
			return ir.FromString(uri), nil
		}
	}

	// Default: return default URI as string
	return ir.FromString(TonyFormatContextURI), nil
}
