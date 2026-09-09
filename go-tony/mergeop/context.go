package mergeop

// OpContext provides schema context and behavioral options for match/patch/diff operations.
// It is threaded through the operation tree to enable schema-aware matching and
// consistent behavioral configuration.
type OpContext struct {
	// === Schema Resolution ===

	// DefEnv maps definition names to their bodies for .[ref] expansion.
	// Values are typically *ir.Node for non-parameterized definitions,
	// or func(...any) any for parameterized definitions.
	DefEnv map[string]any

	// EvalOpts contains options for expression evaluation, particularly
	// for handling parameterized definitions (auto-calling bare refs).
	// This is passed through to eval.ExpandIRWithOptions.
	EvalOpts any // *eval.EvalOptions - using any to avoid import cycle

	// SchemaRegistry for resolving cross-schema references like !from(schema,def).
	// Optional - only needed for cross-schema validation.
	SchemaRegistry any // *schema.SchemaRegistry - using any to avoid import cycle

	// expanding tracks definitions currently being expanded for cycle detection.
	// This is internal state managed by Expand/Unexpand methods.
	expanding map[string]bool

	// === Behavioral Options ===

	// Config holds user-facing behavioral options for patch operations.
	// May be nil if no options were specified.
	Config *PatchConfig

	// data says the nodes beneath this point are DATA: no tag names an operation, and
	// the patch walk merges structure and dispatches nothing. !raw sets it for its
	// subtree (AsData). It is what makes the escape an escape and not a replacement: a
	// merge which reads operator tags as data is still a merge in which nothing beneath
	// is interpreted. It is not a caller's option -- a caller who wants a value held as
	// data writes !raw -- which is why it is not on PatchConfig.
	data bool
}

// Clone creates a shallow copy of the context with a fresh expanding map.
// Use this when you need isolation for parallel or independent operations.
func (c *OpContext) Clone() *OpContext {
	if c == nil {
		return &OpContext{
			expanding: make(map[string]bool),
		}
	}
	return &OpContext{
		DefEnv:         c.DefEnv,
		EvalOpts:       c.EvalOpts,
		SchemaRegistry: c.SchemaRegistry,
		expanding:      make(map[string]bool),
		Config:         c.Config,
		data:           c.data,
	}
}

// AsData answers the context for the subtree of a !raw: this one, with every tag
// beneath read as data. Nil-safe, since a patch applied with no context still meets
// !raw.
func (c *OpContext) AsData() *OpContext {
	out := c.Clone()
	out.data = true
	return out
}

// IsData says whether the walk is inside a !raw, where nothing is an operation.
func (c *OpContext) IsData() bool {
	return c != nil && c.data
}

// Expand marks a definition as currently being expanded.
// Returns false if the definition is already being expanded (cycle detected).
// Use with Unexpand in a defer pattern:
//
//	if !ctx.Expand(name) {
//	    return // cycle detected
//	}
//	defer ctx.Unexpand(name)
func (c *OpContext) Expand(name string) bool {
	if c == nil {
		return true // no context = no cycle tracking
	}
	if c.expanding == nil {
		c.expanding = make(map[string]bool)
	}
	if c.expanding[name] {
		return false // cycle detected
	}
	c.expanding[name] = true
	return true
}

// Unexpand removes the expanding mark for a definition.
// Should be called (typically via defer) after Expand returns true.
func (c *OpContext) Unexpand(name string) {
	if c == nil || c.expanding == nil {
		return
	}
	delete(c.expanding, name)
}

// IsExpanding checks if a definition is currently being expanded.
func (c *OpContext) IsExpanding(name string) bool {
	if c == nil || c.expanding == nil {
		return false
	}
	return c.expanding[name]
}

// EnsureExpanding returns the expanding map, initializing it if needed.
// This is for internal use by expansion code.
func (c *OpContext) EnsureExpanding() map[string]bool {
	if c == nil {
		return nil
	}
	if c.expanding == nil {
		c.expanding = make(map[string]bool)
	}
	return c.expanding
}
