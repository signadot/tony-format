package schema

import (
	"fmt"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
)

// SchemaReference represents a reference to another schema
type SchemaReference struct {
	// Name is the schema name (from signature.name)
	Name string

	// URI is the fully qualified schema URI (optional, for cross-context refs)
	URI string

	// Args are schema arguments (for parameterized schemas)
	Args []*ir.Node
}

// FromReference represents a reference to a definition in another schema
type FromReference struct {
	// SchemaName is the name of the schema containing the definition
	SchemaName string

	// DefName is the name of the definition within that schema
	DefName string

	// SchemaArgs are schema arguments (for parameterized schemas)
	SchemaArgs []*ir.Node
}

// ParseSchemaRefFromTag parses X from !schema(X) tag string
// Returns the first argument from the !schema tag
func ParseSchemaRefFromTag(tag string) (string, error) {
	if tag == "" {
		return "", fmt.Errorf("tag cannot be empty")
	}

	// Use ir.TagArgs to parse the tag
	head, args, _ := ir.TagArgs(tag)

	// Check if this is a schema tag
	if head != "!schema" {
		return "", fmt.Errorf("expected !schema tag, got %q", head)
	}

	// Extract the first argument
	if len(args) == 0 {
		return "", fmt.Errorf("!schema tag requires an argument")
	}

	return args[0], nil
}

// ParseSchemaReference parses a schema reference from an IR node with a !schema(X) tag
// Examples:
//   - !schema(example) -> SchemaReference{Name: "example"}
//   - !schema(tony-format/schema/base) -> SchemaReference{URI: "tony-format/schema/base"}
//   - !schema(p(1,2,3)) -> SchemaReference{Name: "p", Args: ["1", "2", "3"]}
func ParseSchemaReference(node *ir.Node) (*SchemaReference, error) {
	if node.Tag == "" {
		return nil, fmt.Errorf("schema reference node must have a tag")
	}

	// Extract the schema reference from the tag using ParseSchemaRefFromTag
	schemaRef, err := ParseSchemaRefFromTag(node.Tag)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema reference from tag: %w", err)
	}

	ref := &SchemaReference{}

	// Parse the schema reference - it might be parameterized like "p(1,2,3)"
	// Use ir.TagArgs to parse the schema reference (it might have parentheses)
	// TagArgs expects a tag starting with "!", so prepend it
	schemaHead, schemaArgs, _ := ir.TagArgs("!" + schemaRef)
	
	// Remove the "!" prefix to get the actual schema name/URI
	schemaNameOrURI := schemaHead
	if strings.HasPrefix(schemaHead, "!") {
		schemaNameOrURI = schemaHead[1:]
	}

	// Parse schemaNameOrURI as name or URI
	parts := strings.Split(schemaNameOrURI, ":")
	if len(parts) == 1 {
		// Local reference: "example" or "p"
		ref.Name = parts[0]
	} else {
		// Full URI: "tony-format/schema/base"
		// Join all parts as they might contain colons
		ref.URI = strings.Join(parts, ":")
	}

	// Convert schema argument strings to IR nodes
	if len(schemaArgs) > 0 {
		ref.Args = make([]*ir.Node, 0, len(schemaArgs))
		for _, argStr := range schemaArgs {
			// Parse argument string as an IR node
			// For simple string args, create string nodes
			argNode := ir.FromString(argStr)
			ref.Args = append(ref.Args, argNode)
		}
	}

	return ref, nil
}

// ParseFromRefFromTag parses schema-name (possibly parameterized), def-name from !from(schema-name(...), def-name) tag string
// Returns the schema name, definition name, and schema arguments (if the schema is parameterized)
func ParseFromRefFromTag(tag string) (string, string, []string, error) {
	if tag == "" {
		return "", "", nil, fmt.Errorf("tag cannot be empty")
	}

	// Use ir.TagArgs to parse the tag
	head, args, _ := ir.TagArgs(tag)

	// Check if this is a from tag
	if head != "!from" {
		return "", "", nil, fmt.Errorf("expected !from tag, got %q", head)
	}

	// Extract the two required arguments
	if len(args) < 2 {
		return "", "", nil, fmt.Errorf("!from tag requires two arguments: schema-name and def-name")
	}

	// First argument is the schema name, which might be parameterized like "my-schema(1,2,3)"
	schemaArg := args[0]
	defName := args[1]

	// Parse the schema name and its arguments from the first argument
	// Use ir.TagArgs to parse the schema reference (it might have parentheses)
	// TagArgs expects a tag starting with "!", so prepend it
	schemaHead, schemaArgs, _ := ir.TagArgs("!" + schemaArg)
	
	// Remove the "!" prefix to get the actual schema name
	schemaName := schemaHead
	if strings.HasPrefix(schemaHead, "!") {
		schemaName = schemaHead[1:]
	}

	return schemaName, defName, schemaArgs, nil
}

// ParseFromReference parses a from reference from an IR node with a !from(schema-name, def-name, ...) tag
// Examples:
//   - !from(base-schema, number) -> FromReference{SchemaName: "base-schema", DefName: "number"}
//   - !from(param-schema, def, arg1, arg2) -> FromReference with schema args
func ParseFromReference(node *ir.Node) (*FromReference, error) {
	if node.Tag == "" {
		return nil, fmt.Errorf("from reference node must have a tag")
	}

	// Extract the schema name, definition name, and schema arguments from the tag
	schemaName, defName, schemaArgs, err := ParseFromRefFromTag(node.Tag)
	if err != nil {
		return nil, fmt.Errorf("failed to parse from reference from tag: %w", err)
	}

	ref := &FromReference{
		SchemaName: schemaName,
		DefName:    defName,
	}

	// Convert schema argument strings to IR nodes
	if len(schemaArgs) > 0 {
		ref.SchemaArgs = make([]*ir.Node, 0, len(schemaArgs))
		for _, argStr := range schemaArgs {
			// Parse argument string as an IR node
			// For simple string args, create string nodes
			argNode := ir.FromString(argStr)
			ref.SchemaArgs = append(ref.SchemaArgs, argNode)
		}
	}

	return ref, nil
}
