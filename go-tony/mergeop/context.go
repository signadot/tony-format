package mergeop

// OpContext carries behavioral options for match/patch/diff operations, threaded
// through the operation tree.
type OpContext struct {
	// === Schema Resolution ===

	// DefEnv maps definition names to their bodies for .[ref] expansion.
	// Values are typically *ir.Node for non-parameterized definitions,
	// or func(...any) any for parameterized definitions.
	//
	// Nothing reads DefEnv or EvalOpts. A schema's references are expanded
	// before a match starts -- schema.Validate matches against its accept clause
	// with every .[ref] already resolved -- so no operation needs definitions
	// during one (addsgv1yh12kszdxmdn0).
	DefEnv map[string]any

	// EvalOpts contains options for expression evaluation, particularly
	// for handling parameterized definitions (auto-calling bare refs).
	EvalOpts any // *eval.EvalOptions - using any to avoid import cycle

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

// Clone creates a shallow copy of the context. Nil-safe: the copy of no
// context is an empty one.
func (c *OpContext) Clone() *OpContext {
	if c == nil {
		return &OpContext{}
	}
	return &OpContext{
		DefEnv:   c.DefEnv,
		EvalOpts: c.EvalOpts,
		Config:   c.Config,
		data:     c.data,
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
