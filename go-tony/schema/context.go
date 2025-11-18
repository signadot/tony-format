package schema

type Context struct {
	// OutIn maps URI to short names (which short names belong to this URI)
	OutIn map[string]map[string]bool

	// InOut maps short name to URI
	InOut map[string]string

	// URI is the primary/long name for this context (e.g., "tony-format/context/match")
	// If not set, will be inferred from OutIn
	URI string

	// ShortName is the short name for this context (e.g., "match")
	ShortName string

	// Tags defines which tags are available in this context
	// Map of tag name -> TagDefinition
	Tags map[string]*TagDefinition

	// Extends lists URIs of parent contexts (for inheritance/composition)
	Extends []string
}

// TagDefinition describes a tag and its behavior
type TagDefinition struct {
	// Name is the tag name (e.g., "or", "and")
	Name string

	// Contexts lists which contexts this tag belongs to (URIs)
	Contexts []string

	// SchemaRef optionally references a schema that defines this tag's behavior
	// Empty if no schema defines it (built-in tag)
	SchemaRef string

	// Description of what this tag does
	Description string
}

const (
	TonyFormatContextURI = "tony-format/context"
	TonyFormatSchemaURI  = "tony-format/schema"
)

func DefaultContext() *Context {
	return &Context{
		OutIn: map[string]map[string]bool{
			TonyFormatContextURI: {
				"encoding": true,
				"eval":     true,
				"match":    true,
				"patch":    true,
				"diff":     true,
				"schema":   true,
			},
		},
		InOut: map[string]string{
			"encoding": TonyFormatContextURI,
			"eval":     TonyFormatContextURI,
			"match":    TonyFormatContextURI,
			"patch":    TonyFormatContextURI,
			"diff":     TonyFormatContextURI,
			"schema":   TonyFormatContextURI,
		},
	}
}
