# Subscription Formats (WATCH)

Subscriptions use a new HTTP method `WATCH` that streams diffs when query results change, similar to `o diff -loop` but with server-side push instead of polling.

## WATCH Request Format

```http
WATCH /api/query HTTP/1.1
Content-Type: application/tony

# Query document (no patch: means query)
!apiop
path: /posts
match: !trim
  published: true
  id: null
  title: null
  author:
    name: null
```

## WATCH Response Stream

The server maintains the query result state and pushes diffs when changes occur:

```tony
# Initial state (first response)
---
posts: !key(id)
- id: "post1"
    title: "First Post"
    author:
      name: "Alice"
  - id: "post2"
    title: "Second Post"
    author:
      name: "Bob"

# Diff when post3 is published (assuming it matches the query criteria)
---
# difference found at 2024-01-15T10:30:00Z
posts: !key(id)
- !insert
    id: "post3"
    title: "New Post"
    author:
      name: "Charlie"

# Diff when post1's title changes
---
# difference found at 2024-01-15T10:35:00Z
posts: !key(id)
- id: "post1"
    title: !replace
      from: "First Post"
      to: "Updated First Post"
```

## WATCH Behavior

- Server maintains a snapshot of the query result
- When underlying data changes, server computes diff using `tony.Diff()`
- Server pushes diffs as they occur (server-sent events or similar)
- Client can reconstruct current state by applying diffs
- Connection remains open until client closes or timeout

## WATCH with Variables

```http
WATCH /api/query HTTP/1.1
Content-Type: application/tony

!apiop
path: /posts
match: !trim
  author:
    id: "user123"
  id: null
  title: null
```

Server maintains the query state and streams diffs as changes occur.

**WATCH Response Format:**
- Initial state sent as first response
- Subsequent responses are diffs (using Tony diff format)
- Connection remains open until client closes or timeout

## Related Documentation

- [Design Overview](./design.md) - High-level architecture and concepts
- [Query Operations](./queries.md) - Query format specifications (WATCH uses same query format)
- [HTTP Protocols](./http-protocols.md) - HTTP request/response formats for WATCH
