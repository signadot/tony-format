# Comparison with GraphQL

This document provides a detailed comparison between TonyAPI and GraphQL, highlighting similarities, differences, advantages, and use cases.

## Similarities

1. **Unified API**: Both present a single, unified API interface
2. **Schema-driven**: Both use schemas to describe data structure
3. **Field selection**: Both allow clients to select which fields to retrieve
4. **Nested queries**: Both support querying nested/related data
5. **Subscriptions**: Both support real-time updates (GraphQL subscriptions vs Tony WATCH)
6. **Type system**: Both have strong type systems with schema definitions and validation

## Key Differences

**Note**: One of the most significant differences is **transaction support** - TonyAPI has built-in protocol-level transactions, while GraphQL has no transaction support in its specification. See [Transactions](#7-transactions) below for details.

### 1. **Mental Model**

**GraphQL:**

- Presents a **graph** of related types
- Relations are **virtual** - defined in schema, resolved on-demand
- Queries traverse the graph by explicitly requesting relations

**TonyAPI:**

- Presents **one unified document** (like a filesystem)
- Relations are **concrete** - exist as nested structures in the document
- Queries navigate the document structure naturally

### 2. **Query Language**

**GraphQL:**

- Custom query language (GraphQL syntax)
- Separate syntax for queries vs mutations
- Field selection via explicit field names
- Variables: `$userId: ID!`

**TonyAPI:**

- Uses Tony format (YAML-like, JSON-compatible)
- Same document structure for queries and mutations (distinguished by presence of `patch:`)
- Field selection via `field: null` in `match:` with `!trim`
- Dynamic values embedded directly in the document

### 3. **Mutations**

**GraphQL:**

- Separate mutation syntax
- Input types for mutation arguments
- Returns specified fields

**TonyAPI:**
- Same document structure as queries, but with `patch:` section
- Uses Tony diff/patch operations (`!replace`, `!insert`, `!delete`, etc.)
- Returns revision numbers (not diffs) for successful mutations

### 4. **Subscriptions**

**GraphQL:**
- Uses WebSocket or Server-Sent Events
- Query-based subscriptions (subscribe to query results)
- Server pushes updates when query results change

**TonyAPI:**
- Uses new HTTP method `WATCH`
- Query-based subscriptions (same as MATCH queries)
- Server streams diffs as document changes

### 5. **Schema**

**GraphQL:**
- Schema defines types and relations
- Schema is separate from data
- Introspection via `__schema` query
- Strong type system with validation

**TonyAPI:**
- Schema describes the virtual document structure
- Schema accessed as metadata at `/.meta/schema`
- Schema mirrors document structure (paths in `accept:`)
- Strong type system with validation (similar to GraphQL)

### 6. **Backend Architecture**

**GraphQL:**
- Resolvers fetch data on-demand
- N+1 problem requires dataloaders/batching
- Backend complexity in resolvers

**TonyAPI:**
- Controllers maintain virtual document structure
- Document server caches computed view
- Backend complexity in document construction/maintenance
- Diff-based storage (like etcd) for history/audit

### 7. **Transactions**

**GraphQL:**
- **No built-in transaction support** in the GraphQL specification
- Multiple mutations in a single request may or may not be atomic (implementation-dependent)
- Atomicity depends on underlying database/backend, not the GraphQL layer
- No protocol-level guarantee of atomicity across multiple mutations
- Some implementations provide transaction support via custom directives or server configuration, but it's not standardized

**Example GraphQL:**
```graphql
mutation {
  updateUser(id: "123", name: "Alice Updated") {
    id
    name
  }
  updatePost(id: "post1", title: "New Title") {
    id
    title
  }
}
```
*May or may not be atomic - depends on server implementation*

**TonyAPI:**
- **Built-in transaction protocol** with explicit transaction IDs
- Document server coordinates transactions across multiple mount points
- LogD server ensures atomicity at the storage layer
- Controllers write with transaction IDs and participant counts
- **Guaranteed atomicity**: Either all updates succeed or none do
- Protocol-level transaction support works across distributed controllers

**Example TonyAPI:**
```tony
!apiop
path: /
patch:
  users: !key(id)
    - id: "123"
      name: !replace
        from: "Alice"
        to: "Alice Updated"
  posts: !key(id)
    - id: "post1"
      title: !replace
        from: "Old Title"
        to: "New Title"
```
*Guaranteed atomic - all updates succeed or all fail*

**Key Difference:**
- GraphQL: Transaction support is implementation-dependent and not part of the spec
- TonyAPI: Transaction support is built into the protocol and storage layer, providing guaranteed atomicity across multiple mount points

### 8. **Relations**

**GraphQL:**
- Relations defined in schema
- Explicitly requested in queries
- Resolved by resolvers (can be from different sources)

**TonyAPI:**
- Relations exist in document structure
- Implicitly available by navigating document
- Concrete structures (can use nested `!apiop` queries for joins)

### 9. **Error Handling**

**GraphQL:**
- Errors in `errors` array separate from `data`
- Error types, codes, paths

**TonyAPI:**
- Errors can be represented in diff format with `!error` tag
- Errors at specific paths in document

### 10. **Transport**

**GraphQL:**
- HTTP POST for queries/mutations
- WebSocket/SSE for subscriptions
- Single endpoint (usually `/graphql`)

**TonyAPI:**
- HTTP MATCH for queries
- HTTP PATCH for mutations
- HTTP WATCH for subscriptions
- HTTP MOUNT for controller registration
- Path-based routing (like REST, but with unified document model)

### 11. **File System Analogy**

**GraphQL:**
- Like a database query language
- Graph traversal

**TonyAPI:**
- Like a filesystem (Plan 9's "everything is a file" + `9p` protocol)
- Mount points, paths, metadata
- Controllers as "filesystem drivers" (similar to how Linux's `/proc` mounts process information)

## Comparison: Relations

### GraphQL Approach

In GraphQL, relations are defined at the **schema level** and explicitly requested in queries:

```graphql
# Schema
type User {
  id: ID!
  name: String!
  posts: [Post!]!  # Relation defined here
}

type Post {
  id: ID!
  title: String!
  author: User!    # Relation defined here
}

# Query - must explicitly request nested relations
query {
  user(id: "123") {
    id
    name
    posts {        # Explicitly requesting the relation
      id
      title
    }
  }
}
```

**Characteristics:**
- Relations are **schema-level declarations**
- Queries **explicitly request** which relations to follow
- Resolvers handle fetching related data (N+1 problem, dataloaders)
- Relations are **virtual** - the data might come from different sources

### TonyAPI Approach

In TonyAPI, relations are **data-level structures** in the virtual document. Relations are just nested data - `users[].posts` and `posts[].author` are concrete structures in the document.

**Query - just follows the structure:**

```tony
# Query naturally follows the nested structure
!apiop
path: /users
match: !trim
  id: "123"  # Match criteria - also includes id in result
  name: null
  posts:                      # Just navigate the structure
    id: null
    title: null
```

**Characteristics:**
- Relations are **data-level structures** in the virtual document
- Queries **naturally follow** nested structures - no special syntax
- Relations are **concrete** - they exist in the document structure
- No N+1 problem at query level - the document already has the structure

### Key Differences in Relations

1. **Explicitness**: 
   - GraphQL: Must explicitly request each relation in query
   - TonyAPI: Relations are implicit in the document structure

2. **Schema vs Data**:
   - GraphQL: Relations defined in schema, data fetched separately
   - TonyAPI: Relations exist in the virtual document itself

3. **Flexibility**:
   - GraphQL: Can request different relations per query
   - TonyAPI: Document structure determines available relations (but queries can select subsets)

4. **Backend Complexity**:
   - GraphQL: Resolvers must handle relation fetching, batching, etc.
   - TonyAPI: Backend maintains the virtual document structure (complexity moved to document construction)

5. **Querying Relations from Different Starting Points**:
   - GraphQL: Can query relations from different starting points (user.posts, or posts filtered by authorId)
   - TonyAPI: Can query related data from different starting points (query /users with nested posts, or query /posts with nested user query)

### Example: Querying Relations from Different Starting Points

**GraphQL:**
```graphql
query {
  user(id: "123") {
    posts { title }
  }
  posts(authorId: "123") {
    title
  }
}
```

**TonyAPI:**
```tony
# Query user and get their posts via nested query
!apiop
path: /users
match: !let
  let:
    - idMatch: "123"
  in: !trim
    id: .idMatch
    posts:
      !apiop
      path: /posts
      match: !trim
        authorId: .idMatch
        title: null

# Or query posts and get the author via nested query
!apiop
path: /posts
match: !let
  let:
    - authorIdMatch: "123"
  in: !trim
    authorId: .authorIdMatch
    title: null
    author:
      !apiop
      path: /users
      match: !trim
        id: .authorIdMatch
        name: null
```

Both query patterns access the same underlying data (posts by user "123"), but use different starting points in the query. The document structure itself has a single representation (posts reference users via `authorId`), but queries can navigate from either direction.

## Advantages of TonyAPI

1. **Unified Format**: Queries, mutations, and subscriptions all use the same Tony document format
2. **Diff-Based**: Mutations return revision numbers, enabling efficient change tracking and history
3. **Filesystem Model**: Familiar mental model (paths, mounts, metadata)
4. **Composable**: Diffs compose naturally, controllers communicate via diffs
5. **Storage**: Diff-based backend enables history, time-travel, audit trails
6. **No N+1 at Query Level**: Document structure already contains relations
7. **Plan 9-like**: Simple, composable, process-based architecture
8. **Schema-Driven**: Unified schema system with type definitions and validation (similar type safety to GraphQL)
9. **Built-in Transactions**: Protocol-level transaction support with guaranteed atomicity across multiple mount points (see [Transactions](#7-transactions) comparison)

## Advantages of GraphQL

1. **Mature Ecosystem**: Extensive tooling, libraries, and community
2. **Type Safety**: Strong typing with code generation (both GraphQL and TonyAPI have schemas, but GraphQL has more mature code generation tooling)
3. **Explicit Relations**: Clear schema definition of relations
4. **Flexible Resolvers**: Can fetch from multiple sources per query
5. **Standard Protocol**: Well-established specification and practices
6. **Client Libraries**: Rich client libraries with caching, normalization

## Trade-offs

**TonyAPI:**
- Requires maintaining virtual document structure (complexity in controllers)
- Less mature ecosystem
- Document structure determines available relations
- New HTTP methods (MATCH, WATCH, MOUNT) may need proxy support

**GraphQL:**
- Resolver complexity (N+1, batching)
- Separate query/mutation syntax
- No built-in diff/history capabilities
- WebSocket complexity for subscriptions

## Use Cases

**TonyAPI might be better for:**
- Systems needing change tracking/history
- File-like data access patterns
- Systems already using Tony format
- When diff-based communication is valuable
- Plan 9-like architectures

**GraphQL might be better for:**
- Existing GraphQL ecosystems
- When explicit relation control is important
- When mature code generation tooling is critical (both have schemas, but GraphQL has more established tooling)
- When standard protocols are required

## Related Documentation

- [Design Overview](./design.md) - High-level architecture and concepts
- [Query Operations](./queries.md) - Query format examples
- [Mutation Formats](./mutations.md) - Mutation format examples
- [Blog API Example](./blog-api-example.md) - Complete example API
