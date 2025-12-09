package schema

// registerBuiltinContexts registers all built-in execution contexts
func (r *ContextRegistry) registerBuiltinContexts() {
	// Match context - for matching operations
	matchContext := &Context{
		URI:       "tony-format/context/match",
		ShortName: "match",
		Tags: map[string]*TagDefinition{
			"or":      {Name: "or", Contexts: []string{"tony-format/context/match"}},
			"and":     {Name: "and", Contexts: []string{"tony-format/context/match"}},
			"not":     {Name: "not", Contexts: []string{"tony-format/context/match"}},
			"type":    {Name: "type", Contexts: []string{"tony-format/context/match"}},
			"glob":    {Name: "glob", Contexts: []string{"tony-format/context/match"}},
			"field":   {Name: "field", Contexts: []string{"tony-format/context/match"}},
			"tag":     {Name: "tag", Contexts: []string{"tony-format/context/match"}},
			"subtree": {Name: "subtree", Contexts: []string{"tony-format/context/match"}},
			"has-path": {Name: "has-path", Contexts: []string{"tony-format/context/match"}},
			"all":     {Name: "all", Contexts: []string{"tony-format/context/match"}},
			"let":     {Name: "let", Contexts: []string{"tony-format/context/match"}},
			"if":      {Name: "if", Contexts: []string{"tony-format/context/match"}},
			"pass":    {Name: "pass", Contexts: []string{"tony-format/context/match"}},
			"quote":   {Name: "quote", Contexts: []string{"tony-format/context/match"}},
			"unquote": {Name: "unquote", Contexts: []string{"tony-format/context/match"}},
		},
	}

	// Patch context - for patching operations
	patchContext := &Context{
		URI:       "tony-format/context/patch",
		ShortName: "patch",
		Tags: map[string]*TagDefinition{
			"nullify":  {Name: "nullify", Contexts: []string{"tony-format/context/patch"}},
			"jsonpatch": {Name: "jsonpatch", Contexts: []string{"tony-format/context/patch"}},
			"pipe":     {Name: "pipe", Contexts: []string{"tony-format/context/patch"}},
			"insert":   {Name: "insert", Contexts: []string{"tony-format/context/patch"}},
			"delete":   {Name: "delete", Contexts: []string{"tony-format/context/patch"}},
			"replace":  {Name: "replace", Contexts: []string{"tony-format/context/patch"}},
			"rename":   {Name: "rename", Contexts: []string{"tony-format/context/patch"}},
			"strdiff":  {Name: "strdiff", Contexts: []string{"tony-format/context/patch"}},
			"arraydiff": {Name: "arraydiff", Contexts: []string{"tony-format/context/patch"}},
			"addtag":   {Name: "addtag", Contexts: []string{"tony-format/context/patch"}},
			"rmtag":    {Name: "rmtag", Contexts: []string{"tony-format/context/patch"}},
			"retag":    {Name: "retag", Contexts: []string{"tony-format/context/patch"}},
			"dive":     {Name: "dive", Contexts: []string{"tony-format/context/patch"}},
			"embed":    {Name: "embed", Contexts: []string{"tony-format/context/patch"}},
		},
	}

	// Eval context - for evaluation operations
	evalContext := &Context{
		URI:       "tony-format/context/eval",
		ShortName: "eval",
		Tags: map[string]*TagDefinition{
			"eval":     {Name: "eval", Contexts: []string{"tony-format/context/eval"}},
			"file":     {Name: "file", Contexts: []string{"tony-format/context/eval"}},
			"exec":     {Name: "exec", Contexts: []string{"tony-format/context/eval"}},
			"tostring": {Name: "tostring", Contexts: []string{"tony-format/context/eval"}},
			"toint":    {Name: "toint", Contexts: []string{"tony-format/context/eval"}},
			"tovalue":  {Name: "tovalue", Contexts: []string{"tony-format/context/eval"}},
			"b64enc":   {Name: "b64enc", Contexts: []string{"tony-format/context/eval"}},
			"script":   {Name: "script", Contexts: []string{"tony-format/context/eval"}},
			"osenv":    {Name: "osenv", Contexts: []string{"tony-format/context/eval"}},
		},
	}

	// Diff context - for diff operations
	diffContext := &Context{
		URI:       "tony-format/context/diff",
		ShortName: "diff",
		Tags: map[string]*TagDefinition{
			"strdiff":  {Name: "strdiff", Contexts: []string{"tony-format/context/diff"}},
			"arraydiff": {Name: "arraydiff", Contexts: []string{"tony-format/context/diff"}},
		},
	}

	// Register all contexts (ignore errors for built-ins)
	_ = r.RegisterContext(matchContext)
	_ = r.RegisterContext(patchContext)
	_ = r.RegisterContext(evalContext)
	_ = r.RegisterContext(diffContext)
}
