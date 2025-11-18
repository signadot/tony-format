package schema

import (
	"fmt"
	"sync"
)

// ContextRegistry manages all known execution contexts
type ContextRegistry struct {
	mu sync.RWMutex

	// Map of URI -> Context
	contexts map[string]*Context

	// Map of short name -> URI (for resolution)
	shortNames map[string]string

	// Map of tag name -> []*Context (which contexts have this tag)
	tagContexts map[string][]*Context
}

// NewContextRegistry creates a new context registry with built-in contexts
func NewContextRegistry() *ContextRegistry {
	reg := &ContextRegistry{
		contexts:    make(map[string]*Context),
		shortNames:  make(map[string]string),
		tagContexts: make(map[string][]*Context),
	}
	reg.registerBuiltinContexts()
	return reg
}

// RegisterContext registers a context
func (r *ContextRegistry) RegisterContext(ctx *Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Determine URI if not set
	uri := ctx.URI
	if uri == "" {
		// Try to infer from OutIn
		if len(ctx.OutIn) > 0 {
			for u := range ctx.OutIn {
				uri = u
				break
			}
		}
		if uri == "" {
			return fmt.Errorf("context URI cannot be empty and cannot be inferred")
		}
		ctx.URI = uri
	}

	if ctx.ShortName == "" {
		return fmt.Errorf("context short name cannot be empty")
	}

	// Check for URI conflicts
	if existing, exists := r.contexts[uri]; exists {
		return fmt.Errorf("context with URI %q already registered: %q", uri, existing.ShortName)
	}

	// Check for short name conflicts
	if existingURI, exists := r.shortNames[ctx.ShortName]; exists {
		return fmt.Errorf("context with short name %q already registered: %q", ctx.ShortName, existingURI)
	}

	r.contexts[uri] = ctx
	r.shortNames[ctx.ShortName] = uri

	// Update tag-to-context mapping
	if ctx.Tags != nil {
		for tagName := range ctx.Tags {
			r.tagContexts[tagName] = append(r.tagContexts[tagName], ctx)
		}
	}

	return nil
}

// ResolveContext resolves a context by URI or short name
func (r *ContextRegistry) ResolveContext(name string) (*Context, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Try as URI first
	if ctx, exists := r.contexts[name]; exists {
		return ctx, nil
	}

	// Try as short name
	if uri, exists := r.shortNames[name]; exists {
		return r.contexts[uri], nil
	}

	return nil, fmt.Errorf("context %q not found", name)
}

// GetTagContexts returns all contexts that have a given tag
func (r *ContextRegistry) GetTagContexts(tagName string) []*Context {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.tagContexts[tagName]
}

// GetContext returns a context by URI (must be exact match)
func (r *ContextRegistry) GetContext(uri string) (*Context, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ctx, exists := r.contexts[uri]
	return ctx, exists
}

// AllContexts returns all registered contexts
func (r *ContextRegistry) AllContexts() []*Context {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Context, 0, len(r.contexts))
	for _, ctx := range r.contexts {
		result = append(result, ctx)
	}
	return result
}
