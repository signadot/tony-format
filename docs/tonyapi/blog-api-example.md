# Blog API Example

This document provides a complete example of a blog API using TonyAPI. This example is used throughout the documentation to illustrate concepts.

## Virtual Document Structure

The virtual document has this structure:

```tony
# The virtual document the API exposes
users: !key(id)
- id: "123"
  name: "Alice"
  email: "alice@example.com"
- id: "456"
  name: "Bob"
  email: "bob@example.com"

posts: !key(id)
- id: "post1"
  title: "First Post"
  content: "Content here"
  authorId: "123"  # Reference to user, not embedded user data
  published: true
  createdAt: "2024-01-15T10:00:00Z"
- id: "post2"
  title: "Second Post"
  content: "More content"
  authorId: "123"  # Reference to user
  published: false
  createdAt: "2024-01-15T11:00:00Z"
```

**Key design decisions:**

- **Posts contain only post-specific information**: Posts reference users via `authorId`, not embedded user data
- **User data exists only under `/users`**: To get (post-id, user) pairs, query posts and join with users via `authorId`
- **Arrays use `!key(id)`**: This allows arrays to be treated as keyed objects for efficient diff operations while maintaining list syntax
- **Relationships are references**: The document structure uses references (`authorId`) rather than nested objects to avoid duplication

## Schema

The API exposes a **unified schema** that describes the entire virtual document structure. The schema is accessed via `/.meta/schema`:

```tony
# Query unified schema
!apiop
path: /.meta/schema
match: !trim

# Returns unified schema:
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
  users: !key(id) .array(.User)
  posts: !key(id) .array(.Post)
```

The unified schema mirrors the virtual document structure - each top-level path (like `users`, `posts`) is defined in the schema's `accept:` section.

## Mount Points

In this example:
- `/users` is handled by the user-controller
- `/posts` is handled by the post-controller
- Both controllers read from/write to the same logd server
- The document server presents a unified view

For detailed examples using this structure, see [queries.md](./queries.md) and [mutations.md](./mutations.md).
