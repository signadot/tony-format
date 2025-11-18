# Contexts, Tags, and Schema: Coherent Design

## Problem Statement

We need a coherent system to coordinate:
1. **Contexts**: Execution contexts (match, patch, eval, diff, etc.) - JSON-LD like, with short/long names
2. **Tags**: Operations that work within specific contexts (e.g., `!or` for match, `!nullify` for patch)
3. **Schema**: Define names, tags, and reference other schemas

This design should reduce refactoring for tasks like cycle detection and provide a foundation for schema validation and Go struct tag integration.

## Core Principles

1. **Contexts are execution contexts** - disjoint from data types, they define where operations execute
2. **JSON-LD like contexts** - use `@context` field in schema documents, similar to JSON-LD
3. **Short and long names** - like Go modules (e.g., `match` vs `tony-format/context/match`)
4. **Schema define tags** - schemas can define new tags and their behavior
5. **Schema reference schema** - schemas can reference other schemas by name/URI

## Design

### 1. Context Structure

```go
// Context represents an execution context (like JSON-LD @context)
type Context struct {
    // URI is the long name (e.g., "tony-format/context/match")
    URI string
    
    // ShortName is the short name (e.g., "match")
    ShortName string
    
    // Tags defines which tags are available in this context
    // Map of tag name -> TagDefinition
    Tags map[string]*TagDefinition
    
    // Parent contexts (for inheritance/composition)
    Extends []string // URIs of parent contexts
}

// TagDefinition describes a tag and its behavior
type TagDefinition struct {
    // Name is the tag name (e.g., "or", "and")
    Name string
    
    // Contexts lists which contexts this tag belongs to
    Contexts []string // URIs
    
    // Schema optionally references a schema that defines this tag's behavior
    SchemaRef string // Schema name/URI, empty if no schema
    
    // Description of what this tag does
    Description string
}
```

### 2. Schema Structure (Enhanced)

```go
// Schema represents a Tony schema document
type Schema struct {
    // @context field (JSON-LD style) - defines the context URI
    ContextURI string `tony:"@context"`
    
    // Context reference - resolved from ContextURI
    Context *Context
    
    // Signature defines how the schema can be referenced
    Signature *Signature `tony:"signature"`
    
    // With provides short name -> fully qualified name mappings
    // (like Go imports: "match" -> "tony-format/context/match")
    With map[string]string `tony:"with"`
    
    // Tags defines tags that this schema introduces
    // Map of tag name -> TagDefinition
    Tags map[string]*TagDefinition `tony:"tags"`
    
    // Define provides a place for value definitions
    Define map[string]*ir.Node `tony:"define"`
    
    // Accept defines what documents this schema accepts
    Accept *ir.Node `tony:"accept"`
}
```

### 3. Context Registry

```go
// ContextRegistry manages all known contexts
type ContextRegistry struct {
    // Map of URI -> Context
    contexts map[string]*Context
    
    // Map of short name -> URI (for resolution)
    shortNames map[string]string
    
    // Map of tag name -> []Context (which contexts have this tag)
    tagContexts map[string][]*Context
}

// RegisterContext registers a context
func (r *ContextRegistry) RegisterContext(ctx *Context) error

// ResolveContext resolves a context by URI or short name
func (r *ContextRegistry) ResolveContext(name string) (*Context, error)

// GetTagContexts returns all contexts that have a given tag
func (r *ContextRegistry) GetTagContexts(tagName string) []*Context
```

### 4. Schema Reference System

```go
// SchemaReference represents a reference to another schema
type SchemaReference struct {
    // Name is the schema name (from signature.name)
    Name string
    
    // URI is the fully qualified schema URI (optional, for cross-context refs)
    URI string
    
    // Args are schema arguments (for parameterized schemas)
    Args []*ir.Node
    
    // Context is the context in which this reference is used
    Context *Context
}

// ResolveSchemaReference resolves a schema reference
// Examples:
//   - "!schema example" -> SchemaReference{Name: "example"}
//   - "!schema tony-format/schema/base" -> SchemaReference{URI: "..."}
//   - "!schema match:or" -> SchemaReference{Name: "or", Context: matchContext}
func ResolveSchemaReference(ref string, currentSchema *Schema, registry *SchemaRegistry) (*Schema, error)
```

### 5. Schema Registry

```go
// SchemaRegistry manages all known schemas
type SchemaRegistry struct {
    // Map of schema name -> Schema (within a context)
    schemas map[string]*Schema
    
    // Map of schema URI -> Schema (cross-context)
    schemasByURI map[string]*Schema
    
    // Context registry for resolving contexts
    contexts *ContextRegistry
}

// RegisterSchema registers a schema
func (r *SchemaRegistry) RegisterSchema(schema *Schema) error

// ResolveSchema resolves a schema by name or URI
func (r *SchemaRegistry) ResolveSchema(ref *SchemaReference) (*Schema, error)
```

## Example Schema Document

```tony
# Example schema with context and tag definitions
@context: tony-format/context/match

with:
  patch: tony-format/context/patch
  eval: tony-format/context/eval

signature:
  name: example
  args: []

# Define tags that this schema introduces
tags:
  custom-or:
    contexts:
    - tony-format/context/match
    description: "Custom OR operation for matching"
    schema: .custom-or-definition

define:
  custom-or-definition:
    # Schema definition for the custom-or tag
    ...
  
  ttl:
    offsetFrom: !or  # Uses built-in !or tag from match context
    - createdAt
    - updatedAt
    duration: .duration
  
  duration: !regexp |-
    \d+[mhdw]
  
  node:
    parent: .node
    children: .array(node)
    description: \.startswith.

accept:
  !or
  - !and
    - !not .ttl
    - !not .node
  - !schema other-example  # Reference to another schema
  - !schema patch:nullify  # Reference to tag from patch context
```

## Context Definitions

### Built-in Contexts

```go
// Match context - for matching operations
matchContext := &Context{
    URI: "tony-format/context/match",
    ShortName: "match",
    Tags: map[string]*TagDefinition{
        "or": {Name: "or", Contexts: []string{"tony-format/context/match"}},
        "and": {Name: "and", Contexts: []string{"tony-format/context/match"}},
        "not": {Name: "not", Contexts: []string{"tony-format/context/match"}},
        "type": {Name: "type", Contexts: []string{"tony-format/context/match"}},
        "glob": {Name: "glob", Contexts: []string{"tony-format/context/match"}},
        "field": {Name: "field", Contexts: []string{"tony-format/context/match"}},
        "tag": {Name: "tag", Contexts: []string{"tony-format/context/match"}},
        "subtree": {Name: "subtree", Contexts: []string{"tony-format/context/match"}},
        "all": {Name: "all", Contexts: []string{"tony-format/context/match"}},
        "let": {Name: "let", Contexts: []string{"tony-format/context/match"}},
        "if": {Name: "if", Contexts: []string{"tony-format/context/match"}},
        "dive": {Name: "dive", Contexts: []string{"tony-format/context/match"}},
        "embed": {Name: "embed", Contexts: []string{"tony-format/context/match"}},
    },
}

// Patch context - for patching operations
patchContext := &Context{
    URI: "tony-format/context/patch",
    ShortName: "patch",
    Tags: map[string]*TagDefinition{
        "nullify": {Name: "nullify", Contexts: []string{"tony-format/context/patch"}},
        "jsonpatch": {Name: "jsonpatch", Contexts: []string{"tony-format/context/patch"}},
        "pipe": {Name: "pipe", Contexts: []string{"tony-format/context/patch"}},
        "insert": {Name: "insert", Contexts: []string{"tony-format/context/patch"}},
        "delete": {Name: "delete", Contexts: []string{"tony-format/context/patch"}},
        "replace": {Name: "replace", Contexts: []string{"tony-format/context/patch"}},
        "rename": {Name: "rename", Contexts: []string{"tony-format/context/patch"}},
        "strdiff": {Name: "strdiff", Contexts: []string{"tony-format/context/patch"}},
        "arraydiff": {Name: "arraydiff", Contexts: []string{"tony-format/context/patch"}},
        "addtag": {Name: "addtag", Contexts: []string{"tony-format/context/patch"}},
        "rmtag": {Name: "rmtag", Contexts: []string{"tony-format/context/patch"}},
        "retag": {Name: "retag", Contexts: []string{"tony-format/context/patch"}},
    },
}

// Eval context - for evaluation operations
evalContext := &Context{
    URI: "tony-format/context/eval",
    ShortName: "eval",
    Tags: map[string]*TagDefinition{
        "eval": {Name: "eval", Contexts: []string{"tony-format/context/eval"}},
        "file": {Name: "file", Contexts: []string{"tony-format/context/eval"}},
        "exec": {Name: "exec", Contexts: []string{"tony-format/context/eval"}},
        "tostring": {Name: "tostring", Contexts: []string{"tony-format/context/eval"}},
        "toint": {Name: "toint", Contexts: []string{"tony-format/context/eval"}},
        "tovalue": {Name: "tovalue", Contexts: []string{"tony-format/context/eval"}},
        "b64enc": {Name: "b64enc", Contexts: []string{"tony-format/context/eval"}},
        "script": {Name: "script", Contexts: []string{"tony-format/context/eval"}},
        "osenv": {Name: "osenv", Contexts: []string{"tony-format/context/eval"}},
    },
}

// Diff context - for diff operations
diffContext := &Context{
    URI: "tony-format/context/diff",
    ShortName: "diff",
    Tags: map[string]*TagDefinition{
        "strdiff": {Name: "strdiff", Contexts: []string{"tony-format/context/diff"}},
        "arraydiff": {Name: "arraydiff", Contexts: []string{"tony-format/context/diff"}},
    },
}
```

## Schema Reference Resolution

### Reference Syntax

1. **Local schema reference**: `!schema example` - references schema named "example" in current context
2. **Cross-context tag reference**: `!schema match:or` - references "or" tag from match context
3. **Full URI reference**: `!schema tony-format/schema/base` - references schema by full URI
4. **Context-qualified schema**: `!schema match:custom-schema` - references schema in specific context

### Resolution Algorithm

```go
func ResolveSchemaReference(ref string, currentSchema *Schema, registry *SchemaRegistry) (*Schema, error) {
    // Parse reference
    parts := strings.Split(ref, ":")
    
    if len(parts) == 1 {
        // Local reference: !schema example
        return registry.ResolveSchema(&SchemaReference{
            Name: parts[0],
            Context: currentSchema.Context,
        })
    } else if len(parts) == 2 {
        // Context-qualified: !schema match:or
        contextName := parts[0]
        schemaName := parts[1]
        
        ctx, err := registry.contexts.ResolveContext(contextName)
        if err != nil {
            return nil, err
        }
        
        return registry.ResolveSchema(&SchemaReference{
            Name: schemaName,
            Context: ctx,
        })
    } else {
        // Full URI: !schema tony-format/schema/base
        return registry.ResolveSchema(&SchemaReference{
            URI: ref,
        })
    }
}
```

## Benefits

1. **Clear separation**: Contexts (execution) vs Schema (data structure) vs Tags (operations)
2. **Namespace management**: Short names + URIs prevent collisions
3. **Extensibility**: Schemas can define new tags
4. **Reference clarity**: Clear syntax for referencing schemas and tags
5. **Cycle detection**: Can traverse schema references with context awareness
6. **Go struct tag integration**: Can map `tony:schema=...` to schema references with context
7. **Validation**: Can validate that tags used in schema are available in the context

## Implementation Phases

### Phase 1: Context System
- Define `Context` and `TagDefinition` structures
- Create `ContextRegistry` with built-in contexts
- Update `Schema` to include `@context` field
- Parse `@context` and `with` fields in schema documents

### Phase 2: Tag System
- Map existing tags to contexts (match, patch, etc.)
- Update `TagDefinition` to reference schemas
- Validate tags are available in context when parsing schema

### Phase 3: Schema Reference System
- Implement `SchemaReference` and resolution
- Update schema parsing to resolve references
- Support local, cross-context, and URI references

### Phase 4: Schema Registry
- Create `SchemaRegistry` to manage schemas
- Register schemas with context awareness
- Resolve schema references with context

### Phase 5: Integration
- Update cycle detection to use schema references
- Update validation to check tag availability
- Prepare for Go struct tag integration

## Migration Notes

1. **Existing `Context` type**: The current `Context` type in `schema/context.go` is different - it maps short names to URIs. This should be replaced/refactored.

2. **Existing tags**: All tags in `mergeop/register.go` need to be mapped to contexts (match vs patch).

3. **Base schema**: `schema/base.tony` already has `context:` field - this aligns with `@context` proposal.

4. **Backward compatibility**: Need to handle schemas without `@context` (default to match context?).
