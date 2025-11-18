# Contexts

Contexts are execution environments that define where operations execute and
which tags are available. They provide a namespace system similar to JSON-LD's
`@context`, allowing tags to be scoped and preventing naming conflicts.

## What are Contexts?

Contexts are execution contexts - they define where operations execute, not
what data looks like. Think of them as different "modes" or "environments"
for operations:

- **match**: Matching operations (`!or`, `!and`, `!not`, etc.)
- **patch**: Patching operations (`!nullify`, `!insert`, `!delete`, etc.)
- **eval**: Evaluation operations (`!eval`, `!file`, `!exec`, etc.)
- **diff**: Diff operations (`!strdiff`, `!arraydiff`, etc.)

Each context defines which tags are available in that context. This allows
the same tag name to mean different things in different contexts, or to
restrict certain operations to specific contexts.

## Context Structure

A context has:

- **URI**: The fully qualified name (e.g., `tony-format/context/match`)
- **ShortName**: A short name for convenience (e.g., `match`)
- **Tags**: Map of tag name to `TagDefinition` - which tags are available
- **Extends**: List of parent context URIs (for inheritance)

## Using Contexts in Schemas

Schemas specify their context using the `context` field:

```tony
context: tony-format/context

signature:
  name: example

define:
  # ... definitions ...
```

The `context` field tells the schema system which execution context to use
when interpreting tags and operations in this schema.

## Built-in Contexts

Tony Format provides several built-in contexts:

### Match Context (`tony-format/context/match`)

Tags for matching operations:

- `!or`, `!and`, `!not` - boolean operations
- `!type`, `!glob`, `!field` - type and field matching
- `!tag`, `!subtree`, `!all` - structural matching
- `!let`, `!if`, `!dive`, `!embed` - control flow

### Patch Context (`tony-format/context/patch`)

Tags for patching operations:

- `!nullify`, `!insert`, `!delete`, `!replace` - basic operations
- `!rename`, `!strdiff`, `!arraydiff` - transformation operations
- `!addtag`, `!rmtag`, `!retag` - tag manipulation
- `!jsonpatch`, `!pipe` - advanced operations

### Eval Context (`tony-format/context/eval`)

Tags for evaluation operations:

- `!eval`, `!file`, `!exec` - execution
- `!tostring`, `!toint`, `!tovalue` - type conversion
- `!b64enc`, `!script`, `!osenv` - utilities

### Diff Context (`tony-format/context/diff`)

Tags for diff operations:

- `!strdiff`, `!arraydiff` - diff generation

## Context Resolution

When a schema references a tag, the system resolves it within the schema's
context. For example:

```tony
context: tony-format/context/match

accept:
  !or          # This !or is from the match context
  - .[something]
  - .[something-else]
```

If you need to reference a tag from a different context, you can use
context-qualified references:

```tony
!match:or      # Explicitly use !or from match context
!patch:nullify # Use !nullify from patch context
```

## Default Context

If a schema doesn't specify a `context` field, it uses the default context
(`tony-format/context`), which includes mappings for common short names like
`match`, `patch`, `eval`, and `diff`.

## Context Registry

The system maintains a `ContextRegistry` that tracks:

- All registered contexts (by URI and short name)
- Which contexts have which tags
- Context inheritance relationships

This allows the system to:

- Validate that tags are available in the current context
- Resolve context-qualified tag references
- Provide tooling support (autocomplete, validation)

## Example: Schema with Context

```tony
# Schema that uses match context operations
context: tony-format/context/match

signature:
  name: user-schema

define:
  user:
    name: !irtype ""
    age: !and
    - .[number]
    - age: !not null

accept:
  !and
  - .[user]
  - !not null
```

This schema uses the match context, so all the match operations (`!and`,
`!not`, etc.) are available for use in the `accept` clause and definitions.
