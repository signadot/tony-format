# Mutation Formats

Mutations use the same structure as queries but **include `patch:`** to indicate modification:
- **`path:`** - Document path to operate on
- **`match:`** - Matching criteria (what to find/select)
- **`patch:`** - Changes to apply (uses Tony diff operations)

**Key**: Mutation documents have **`patch:`** section.

**Tony diff operations**:
- `!replace` - Replace a value
- `!insert` - Insert into array
- `!delete` - Delete from array or set to null
- `!arraydiff` - Array diff operations
- `!strdiff` - String diff operations

**Basic mutation structure:**
```tony
!apiop
path: /path/to/resource
match:
  # Matching criteria
patch:
  # Changes to apply
```

## Example: Update Mutation

```tony
# Mutation: Update a post's title
!apiop
path: /posts
match:
  id: "post123"
patch:
  title: "New Title"
```

## Create Mutation

```tony
# Mutation: Create a new post
# Client sends actual values - these are dynamic
!apiop
path: /posts
match: null  # or !pass (always match) or omit match
patch:
  id: "post123"  # Client generates or server assigns
  title: "New Post"
  content: "Content here"
  author:
    id: "user456"
  published: false
```

## Delete Mutation

```tony
# Mutation: Delete a post
!apiop
path: /posts
match:
  id: "post123"
patch: !delete
```

## Conditional Update

```tony
# Mutation: Update only if condition matches
!apiop
path: /posts
match:
  id: "post123"
  author:
    id: "user456"  # Client sends the current user ID - server validates
patch:
  title: "Updated Title"
```

## Multiple Updates

```tony
# Mutation: Update multiple posts
!apiop
path: /posts
match: !all
  published: false
patch: !all
  published: true
  publishedAt: "2024-01-15T10:30:00Z"  # Client sends timestamp, or server fills in
```

## Related Documentation

- [Design Overview](./design.md) - High-level architecture and concepts
- [Query Operations](./queries.md) - Query format specifications
- [HTTP Protocols](./http-protocols.md) - HTTP request/response formats for mutations
