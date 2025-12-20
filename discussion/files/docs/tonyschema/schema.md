# Schema Structure

A Tony Schema document defines the structure and constraints for Tony documents.
It consists of several key fields that work together to describe what documents
are valid.

## Core Fields

### `context`

The `context` field specifies the execution context in which this schema operates.
Contexts define which tags are available and how operations execute.

```tony
context: tony-format/context
```

See [Contexts](contexts.md) for more details on how contexts work.

### `signature`

The `signature` field defines how the schema can be referenced by name.
This allows other schemas and documents to reference this schema using tags
like `!schemaName`.

```tony
signature:
  name: example  # so we can use '!example' to refer to this
  args: []       # schema arguments for parameterized schemas
```

The `name` field is required and becomes the schema's identifier. When a schema
has a signature name, that name is automatically available as a tag reference.

The `args` field is optional and allows schemas to be parameterized. For example,
a schema might define `array(t)` where `t` is a type parameter.

### `define`

The `define` field provides a place for value definitions, similar to JSON Schema's
`$defs`. Each key in `define` is a definition name, and each value describes what
that definition accepts.

```tony
define:
  ttl:
    offsetFrom: !or
    - createdAt
    - updatedAt
    duration: .[duration]
  
  duration: !regexp |-
    \d+[mhdw]

  # recursive definitions are possible
  node:
    parent: .[node]  # .[name] refers to things under $.define using expr-lang
    children: .array(.[node])
```

Definitions can reference other definitions using the `.[definitionName]` syntax
(expr-lang format). This allows for recursive structures and composition.

### `accept`

The `accept` field defines what documents this schema accepts. It uses match
operations to describe the constraints that valid documents must satisfy.

```tony
accept:
  !or
  - !and
    - !not .[ttl]
    - !not .[node]
  - !example  # reference to this schema itself
```

If `accept` is omitted, the schema accepts all documents (no validation).

See [Validation](validation.md) for more details on how acceptance works.

### `tags`

The `tags` field defines tags that this schema introduces. This is metadata
about what tags are available and in which contexts they're valid.

```tony
tags:
  custom-or:
    contexts:
    - tony-format/context/match
    description: "Custom OR operation for matching"
    schema: .custom-or-definition
```

See [Tags](tags.md) for more details on tag definitions.

## Example Schema

Here's a complete example that brings these fields together:

```tony
# Example schema for a blog post
context: tony-format/context

signature:
  name: blog-post
  args: []

define:
  # A blog post has a title, content, and optional tags
  post:
    title: !irtype ""
    content: !irtype ""
    tags: .array(string)
    author: .[author]
    published: !or
    - null
    - .[timestamp]
  
  author:
    name: !irtype ""
    email: !irtype ""
  
  timestamp: !irtype 1

accept:
  .[post]
```

This schema defines:

- A `blog-post` schema that can be referenced as `!blog-post`
- Definitions for `post`, `author`, and `timestamp`
- An `accept` clause that requires documents to match the `post` definition

## Schema References

Schemas can reference other schemas using the `!schemaName` syntax. When a schema
has a `signature.name`, that name becomes available as a tag reference.

```tony
define:
  user: !person null      # reference to person schema
  company: !company null   # reference to company schema
  number: !from(base-schema,number) null  # reference to definition in another schema
  post:                    # post definition with structure
    title: !irtype ""
    content: !irtype ""
    author: .[user]        # reference to definition in same schema using expr-lang
```

References can be:

- **Local definition**: `.[name]` (within the same schema's `define:` section, using expr-lang)
- **Schema reference**: `!person` (reference to another schema by name)
- **Cross-schema definition**: `!from(schema-name,def-name)` (reference to a specific definition in another schema)
- **Cross-context**: `!match:or` (from a specific context)
