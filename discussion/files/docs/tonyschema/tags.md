# Tags

Tags are markers on IR nodes that provide type information, schema references,
or operation instructions. They're the mechanism by which schemas reference
other schemas and by which operations are invoked.

## What are Tags?

Tags appear on IR nodes using the `!tagName` syntax. They serve several purposes:

1. **Schema References**: `!person` means "this node conforms to the person schema"
2. **Type Markers**: `!irtype` marks built-in types
3. **Operations**: `!or`, `!and`, `!nullify` invoke operations
4. **Custom Tags**: Schemas can define their own tags

## Tag Syntax

Tags use the `!` prefix:

```tony
!person           # Reference to person schema
!irtype 1         # Built-in number type
!or               # OR operation
!and              # AND operation
```

Tags can be composed:

```tony
!tag1.tag2        # Composed tags
!match:or          # Context-qualified tag
```

## Schema Tags

When a schema has a `signature.name`, that name becomes available as a tag
reference. For example:

```tony
signature:
  name: person

# Now !person can be used to reference this schema
```

Other schemas can then reference it:

```tony
define:
  user: !person null        # Reference to person schema
  number: !from(base-schema,number) null  # Reference to definition in another schema
  post:                     # post definition with structure
    title: !irtype ""
    content: !irtype ""
    author: .[user]         # Reference to definition in same schema using expr-lang
```

The schema system automatically creates a tag entry for `signature.name` in
the schema's `tags` map, so you don't need to explicitly define it unless
you want to add additional metadata (like contexts or descriptions).

## Tag Definitions

Schemas can define tags in the `tags` field. This is metadata about what
tags are available and in which contexts they're valid:

```tony
tags:
  custom-or:
    contexts:
    - tony-format/context/match
    description: "Custom OR operation for matching"
    schema: .custom-or-definition
```

A `TagDefinition` can have:

- **Name**: The tag name (e.g., "or", "and")
- **Contexts**: List of context URIs where this tag is valid
- **SchemaRef**: Optional reference to a schema that defines this tag's behavior
- **Description**: Human-readable description of what the tag does

## Built-in Tags

### Type Tags

- `!irtype` - Built-in type marker (used with values like `1`, `""`, `true`, `null`, `[]`, `{}`)

### Match Context Tags

- `!or`, `!and`, `!not` - Boolean operations
- `!type`, `!glob`, `!field` - Type and field matching
- `!tag`, `!subtree`, `!all` - Structural matching
- `!let`, `!if`, `!dive`, `!embed` - Control flow

### Patch Context Tags

- `!nullify`, `!insert`, `!delete`, `!replace` - Basic operations
- `!rename`, `!strdiff`, `!arraydiff` - Transformations
- `!addtag`, `!rmtag`, `!retag` - Tag manipulation

### Eval Context Tags

- `!eval`, `!file`, `!exec` - Execution
- `!tostring`, `!toint`, `!tovalue` - Type conversion

## Tag Composition

Tags can be composed to create more specific tags:

```tony
!outer.inner      # Composed tag
```

This is useful when you have nested structures or when you want to apply
multiple tags to a single node.

## Context-Qualified Tags

You can explicitly specify which context a tag comes from:

```tony
!match:or         # !or from match context
!patch:nullify    # !nullify from patch context
```

This is useful when:

- You need to disambiguate between tags with the same name in different contexts
- You want to use a tag from a different context than your schema's default

## Tag Validation

The schema system can validate that tags are:

- Available in the current context
- Properly referenced (schema exists)
- Used correctly (right number of arguments, etc.)

This validation happens when:

- Parsing schemas
- Validating documents against schemas
- Resolving schema references

## Example: Using Tags

```tony
context: tony-format/context/match

signature:
  name: blog-post

define:
  # Use !irtype for built-in types
  title: !irtype ""
  content: !irtype ""
  
  # Use !or for unions
  status: !or
  - "draft"
  - "published"
  - "archived"
  
  # Use !and for intersections
  published-post: !and
  - .[post]
  - status: "published"
  
  # Reference other schemas
  author: !person
  
  # Use match operations
  valid-post: !and
  - .[post]
  - !not null

tags:
  blog-post:
    description: "A blog post schema"
    contexts:
    - tony-format/context/match
```
