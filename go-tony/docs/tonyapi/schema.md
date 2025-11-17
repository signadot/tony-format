# Schema Formats

Schemas use the Tony Schema format (see `docs/tonyschema.md`). Key features:

- **Type definitions** in `define:` section
- **References** using `.TypeName` to refer to defined types
- **Type parameters** like `array(t)` for generic types
- **Validation** via `accept:` using match operations
- **Schema references** using `!schema schema-name` to compose schemas

## Schema as Metadata

Since the API exposes **one giant Tony document**, there is **one unified schema** that describes the entire virtual document structure. The schema is accessed via the metadata path:

```tony
# Query unified schema for entire virtual document
!apiop
path: /.meta/schema
match: !trim
```

## Example Unified Schema

```tony
# GET /.meta/schema returns:
!schema
signature:
  name: blog-api
  args: []
define:
  User:
    id: .string
    name: .string
    email: .string
    createdAt: .string
  Post:
    id: .string
    title: .string
    content: .string
    authorId: .string
    published: .bool
    createdAt: .string
accept:
  users: .array(.User)
  posts: .array(.Post)
```

## Schema Structure Matches Virtual Document Structure

The unified schema mirrors the virtual document structure, with each top-level path (like `users`, `posts`) defined in the schema's `accept:` section:

```tony
# Virtual document structure:
users: !key(id)
- id: "123"
  name: "Alice"
posts: !key(id)
- id: "post1"
  title: "First Post"

# Unified schema structure:
!schema
accept:
  users: !key(id) .array(.User)  # Matches /users path
  posts: !key(id) .array(.Post)  # Matches /posts path
```

## Querying Schema for a Specific Path

While there's one unified schema, you can query what the schema says about a specific path:

```tony
# Get schema definition for /users path
!apiop
path: /.meta/schema
match: !trim
  accept:
    users: null  # Select users definition from schema

# Returns:
accept:
  users: .array(.User)
```

Or query the type definition:

```tony
# Get User type definition
!apiop
path: /.meta/schema
match: !trim
  define:
    User: null

# Returns:
define:
  User:
    id: .string
    name: .string
    email: .string
    createdAt: .string
```

## Query Validation Against Schema

Queries can be validated against schemas to ensure:
- Requested fields exist in schema
- Types match expected types
- Relationships are valid

### Option 1: Schema reference in query

```tony
# Query validated against unified schema
!apiop
path: /users
match: !trim
  !schema blog-api
  id: null
  name: null
  posts:
    id: null
    title: null
```

### Option 2: Fetch schema separately and validate

```tony
# First, get unified schema
!apiop
path: /.meta/schema
match: !trim

# Then validate query against schema (client-side or server-side)
```

## Schema Composition

The unified schema is composed from multiple controllers' schemas. Each controller provides its schema contribution when it mounts:

```tony
# User controller mounts and provides schema:
mount:
  controller: "user-controller"
  path: "/users"
  schema:
    define:
      User:
        id: .string
        name: .string
        email: .string
    accept:
      users: .array(.User)

# Post controller mounts and provides schema:
mount:
  controller: "post-controller"
  path: "/posts"
  schema:
    define:
      Post:
        id: .string
        title: .string
        authorId: .string
    accept:
      posts: .array(.Post)

# Document server composes unified schema:
!schema
define:
  User:
    id: .string
    name: .string
    email: .string
  Post:
    id: .string
    title: .string
    authorId: .string
accept:
  users: !key(id) .array(.User)  # From user-controller
  posts: !key(id) .array(.Post)  # From post-controller
```

## Schema Storage

- Controllers provide schema contribution in MOUNT request
- Document server composes unified schema from all mounted controllers
- Unified schema stored in backend as metadata at `/.meta/schema` in backend diff log
- Document server updates unified schema when controllers mount/unmount
- Document server caches unified schema for validation

## Related Documentation

- [Design Overview](./design.md) - High-level architecture and concepts
- [HTTP Protocols](./http-protocols.md) - How to query schema via HTTP
- [Controllers](./controllers.md) - How controllers contribute schemas during MOUNT
