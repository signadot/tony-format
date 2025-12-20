# Query Operations

This document provides comprehensive examples and format specifications for query operations in TonyAPI. For the overall design and architecture, see [design.md](./design.md).

## Virtual Document Structure

All examples in this document operate on the blog API example structure. For the complete virtual document structure, schema definition, and design decisions, see [blog-api-example.md](./blog-api-example.md).

## Matching vs Selection: Unified Query Schema

Operations express three key aspects:

1. **Path**: WHERE to operate (document path specified in `path:`)
2. **Matching**: WHAT to filter/find within that path (criteria in `match:`)
3. **Selection** (for queries): HOW to return results, controlled by `!trim` / `!notrim` tags on match
   - `!trim`: Return only matched sub-documents/fields
   - `!notrim`: Return full documents
4. **Mutation** (when `patch:` present): `patch:` modifies matched documents

### Key Set Notation

Tony supports **key set notation** `{a b c}` as syntactic sugar for `a: null, b: null, c: null`. This creates an object value.

**Important**: Key set notation `{a b c}` creates an **object value**, not keys at the same level. It cannot be used directly as a sibling to other keys in block-mode objects.

**Key feature**: Within key set notation, you can **mix explicit key-value pairs with bare keys**:
```tony
{id: "123" name email}  # Equivalent to: id: "123", name: null, email: null
{id: .idMatch name email}  # Equivalent to: id: .idMatch, name: null, email: null
```

**How tags work in key set notation**: Tags like `!as` work because bare keys are syntactic sugar for `key: null`. When you add a tag, it becomes `key: !as(xxx) null`:
```tony
{id name !as(userName)}  # Equivalent to: id: null, name: !as(userName) null
{id: "123" name !as(userName) email}  # Equivalent to: id: "123", name: !as(userName) null, email: null
```

**Valid uses of key set notation:**

- ✅ As a standalone object: `{a b c}` 
- ✅ As a value: `fields: {id name email}` creates `fields: {id: null, name: null, email: null}`
- ✅ As an array element: `- {a b c}` or `[{a b c}]`
- ✅ With tags: `{id !as(foo-id) location displayName}` - tags apply to the key before them (e.g., `id !as(foo-id)` becomes `id: !as(foo-id) null`)
- ✅ **Mixing explicit values with bare keys**: `{id: "123" name email}`

**Invalid use:**

- ❌ Using key set notation as a sibling to other keys in block-mode (must be the only value):
  ```tony
  match: !trim
    id: "123"
    {name email}  # ERROR: cannot use key set notation here (sibling to id:)
  ```
  Or:
  ```tony
  match: !trim
    {id: "123" name}  # ERROR: cannot use key set notation here (sibling to posts:)
    posts:
      !apiop
      path: /posts
  ```
  Instead, use explicit syntax when you have sibling fields:
  ```tony
  match: !trim
    id: "123"  # Match criteria
    name: null  # Field selection
    posts:
      !apiop
      path: /posts
  ```
  Or use key set notation when it's the only value:
  ```tony
  match: !trim
    {id: "123" name email}  # ✅ Works! No sibling fields
  ```

**For field selection in queries**, you can use key set notation when it's a value:
```tony
match: !trim
  {id: "123" name email}  # Mix match criteria and field selection
```

**Key insight**: 
- **Query** = no `patch:` (read-only)
- **Mutation** = has `patch:` (modifies data)
- Selection is controlled by trim behavior on the match (for queries)

**How it works:**

- **`path:`**: Specifies the document path to operate on (e.g., `/users`, `/posts`, `/users/123`)
- **Query (no `patch:`)**: Returns matched documents at the specified path
  - **Without `!trim`**: Return full matched documents
  - **With `!trim`**: Return only the matched sub-documents/fields (what's specified in the match)
- **Mutation (has `patch:`)**: Modifies matched documents at the specified path using patch
- **Nested `!trim` / `!notrim`**: 
  - `!trim` under `!trim`: Doesn't make sense - parent trim already constrains
  - `!notrim` under `!trim`: Overrides parent to return full nested documents
  - `!trim` under `!notrim`: Trims nested documents while parent is full

**Example structure:**
```tony
!apiop
path: /users          # WHERE: operate on /users path
match: !trim          # WHAT: match criteria + HOW: trim selection
  {id: "123" name email}  # Match user with id "123", include name and email
patch:                # Mutation: modify matched documents (if present)
  name: "Updated"     # Change name to "Updated"
```

**Key set notation**: `{id: "123" name email}` mixes explicit key-value pairs (`id: "123"`) with bare keys (`name email`), making queries more concise. Bare keys become `key: null` for field selection.

### Unified Schema

Based on `docs/match-sel.md`, we use a structure with `path:`, `match:`, and optional `patch:`:

**Key points:**

- **`!apiop`** - Tag required to mark operation documents (distinguishes from data fields)
- **`path:`** - Document path to operate on
- **`match:`** - Matching criteria (what to find)
- **`patch:`** - **Required for mutations** (modifies data), **absent for queries** (read-only)
- **Selection is implicit** - You get what matches (for queries)
- **`!trim` / `!notrim`** - Tags on match indicating whether to restrict result to matched sub-documents
- **`!as`** - Tag for field aliasing/renaming in query results. In key set notation: `name !as(userName)` is syntactic sugar for `name: !as(userName) null`. Explicitly: `name: !as(userName) null`

**Why `!apiop` is needed:**

Without `!apiop`, there's ambiguity when data fields have names like `path`, `match`, or `patch`:

```tony
# Ambiguous: Is this an operation or data?
path: /users
match:
  name: "bob"
  path: "/some/path"  # Is this a nested operation or a data field?
  match:
    something: true   # Is this a nested operation or a data field?
```

With `!apiop`, it's clear:

```tony
# Operation (has !apiop tag)
!apiop
path: /users
match:
  name: "bob"
  path: "/some/path"  # Clearly a data field
  match:
    something: true   # Clearly a data field

# Nested operation (also has !apiop tag)
!apiop
path: /users
match: !trim
  id: "123"
  posts:
    !apiop              # Nested operation - clearly marked
    path: /posts
    match:
      published: true
```

**Basic query:**
```tony
# Query: Find users matching criteria, return matched documents
!apiop
path: /users
match:
  id: "123"
# Returns: full user documents that match
```

**Mutation (has patch):**
```tony
# Mutation: Find users matching criteria, then modify them
!apiop
path: /users
match:
  age: !gt 18
patch:
  adult: true  # Modify: set adult field to true
# Mutates: matched users get 'adult: true' field
```

**With !trim (restrict to matched sub-documents):**
```tony
# Query: Find users, return only matched fields
!apiop
path: /users
match: !let
  let:
    - idMatch: "123"
  in: !trim
    {id: .idMatch name email}  # Mix match criteria with field selection
# Returns: { id: "123", name: "Alice", email: "alice@example.com" } - only matched fields
# Note: !let binds idMatch to "123", then .idMatch references it in the match
# Note: Key set notation allows mixing explicit values (id: .idMatch) with bare keys (name email)
```

**With !notrim (return full documents):**
```tony
# Query: Find users, return full documents
!apiop
path: /users
match: !notrim
  id: "123"
# Returns: full user document with all fields
```

**Nested queries with trim:**
```tony
!apiop
path: /users
match: !let
  let:
    - idMatch: "123"
  in: !trim
    id: .idMatch  # Match criteria
    name: null  # Field selection
    posts:
      !apiop
      path: /posts
      match:
        {authorId: .idMatch published: true id title}  # Mix match criteria with field selection
# Returns: user with only id and name, posts with only id and title (filtered by authorId)
# Note: !let binds idMatch to "123", then .idMatch references it in both the user match and nested posts query
# Note: nested operations need !apiop tag to distinguish from data fields
# Note: When you have sibling fields like posts:, use explicit syntax for the parent match
# Note: Key set notation works when it's the only value (like in nested match)
```

**Nested trim/notrim:**
```tony
!apiop
path: /users
match: !let
  let:
    - idMatch: "123"
  in: !trim
    id: .idMatch
    name: null  # Include name field
    posts:
      !apiop
      path: /posts
      match: !notrim
        authorId: .idMatch
# Returns: user with only id and name, but full post documents (filtered by authorId)
# Note: !let binds idMatch to "123", then .idMatch references it in both the user match and nested posts query
# Note: !notrim can be nested to override parent trim behavior
# Note: nested operations need !apiop tag
```

## Query Examples

All examples below operate on the virtual document structure defined above.

### 1. Simple query - match only

```tony
# Query: Get user matching id
# Client sends this document with the actual ID value they want
# Returns full user document
!apiop
path: /users
match:
  id: "123"
# Returns: { id: "123", name: "Alice", email: "alice@example.com" }
# Note: To get posts, use a separate query or nested operation matching author.id
```

### 2. Query with !trim (field selection)

```tony
# Query: Get user, but only return specific fields
!apiop
path: /users
match: !let
  let:
    - idMatch: "123"
  in: !trim
    id: .idMatch  # Match criteria
    name: null  # Field selection
    posts:
      !apiop
      path: /posts
      match:
        {authorId: .idMatch id title}  # Mix match criteria with field selection
# Returns: { id: "123", name: "Alice", posts: [{ id: "post1", title: "..." }] }
# Note: !let binds idMatch to "123", then .idMatch references it in both the user match and nested posts query
# Note: When you have sibling fields like posts:, use explicit syntax for the parent match
# Note: Nested match doesn't need !trim since parent !trim already constrains
# Note: Key set notation works when it's the only value (like in nested match)
```

### 3. Query with !notrim (full documents)

```tony
# Query: Get published posts, return full documents
!apiop
path: /posts
match: !notrim
  published: true
# Returns: full post documents with all fields
```

### 4. Mutation (has patch)

```tony
# Mutation: Update users, set adult field
!apiop
path: /users
match:
  age: !gt 18
patch:
  adult: true  # Modify: set adult field to true
# Mutates: matched users get 'adult: true' field
```

### 5. Query with nested match under trim

```tony
# Query: Get user with filtered posts
# Parent !trim constrains user fields, nested match constrains posts
!apiop
path: /users
match: !let
  let:
    - idMatch: "123"
  in: !trim
    id: .idMatch  # Match criteria
    name: null  # Field selection
    posts:
      !apiop
      path: /posts
      match:
        {authorId: .idMatch published: true id title}  # Mix match criteria with field selection
# Returns: user with only id and name, posts with only id and title (filtered by authorId)
# Note: !let binds idMatch to "123", then .idMatch references it in both the user match and nested posts query
# Note: nested operations need !apiop tag
# Note: Nested match doesn't need !trim since parent !trim already constrains
# Note: When you have sibling fields like posts:, use explicit syntax for the parent match
# Note: Key set notation works when it's the only value (like in nested match)
```

### 5b. Nested notrim to override parent trim

```tony
# Query: Get user (trimmed), but full posts
!apiop
path: /users
match: !let
  let:
    - idMatch: "123"
  in: !trim
    id: .idMatch
    name: null  # Include name field
    posts:
      !apiop
      path: /posts
      match: !notrim
        authorId: .idMatch
# Returns: user with only id and name, but full post documents (filtered by authorId)
# Note: !let binds idMatch to "123", then .idMatch references it in both the user match and nested posts query
# Note: !notrim overrides parent trim to return full documents
# Note: nested operations need !apiop tag
```

### 5c. Nested trim under notrim

```tony
# Query: Full user, but trimmed posts
!apiop
path: /users
match: !let
  let:
    - idMatch: "123"
  in: !notrim
    id: .idMatch
    posts:
      !apiop
      path: /posts
      match: !trim
        {authorId: .idMatch published: true id title}  # Mix match criteria with field selection
# Returns: full user document, but posts only include id and title (filtered by authorId)
# Note: !let binds idMatch to "123", then .idMatch references it in both the user match and nested posts query
# Note: !trim can be nested under !notrim to trim nested documents
# Note: nested operations need !apiop tag
# Note: Key set notation allows mixing explicit values with bare keys for concise syntax
```

### 6. Query with multiple conditions

```tony
# Query: Get posts matching multiple criteria
!apiop
path: /posts
match: !trim
  !and:
    - published: true
    - author:
        name: "Alice"
  id: null  # Include id field
  title: null  # Include title field
  author:
    name: null  # Include author.name field
# Returns: only id, title, and author.name for matching posts
# Note: When you have other keys like !and: at the same level, use explicit field: null syntax
# Note: For nested fields like author.name, use explicit syntax: author: {name: null} or author: name: null
```

### 7. Path-based query (no matching)

```tony
# Query: Get specific path - no match, just return document
!apiop
path: /users/123
# Returns: full document at that path
```

### 8. Field aliasing with !as

```tony
# Query: Rename fields in response
!apiop
path: /users
match: !let
  let:
    - idMatch: "123"
  in: !trim
    {id: .idMatch name !as(userName) email !as(userEmail)}  # Mix match criteria with aliased fields
# Returns: { id: "123", userName: "Alice", userEmail: "alice@example.com" }
# Note: !let binds idMatch to "123", then .idMatch references it in the match
# Note: Key set notation allows mixing explicit values with aliased fields: name !as(userName) is syntactic sugar for name: !as(userName) null
```

**Nested field aliasing:**
```tony
# Query: Rename nested fields
!apiop
path: /posts
match: !trim
  id: null  # Include id field
  title: null  # Include title field
  author:
    {id name !as(authorName)}  # Include author.id and rename author.name
# Returns: { id: "post1", title: "First Post", author: { id: "123", authorName: "Alice" } }
# Note: When you have sibling fields like author:, use explicit syntax for the parent match
# Note: Key set notation works for nested fields: author: {id name !as(authorName)} where name !as(authorName) is syntactic sugar for name: !as(authorName) null
```

**Aliasing with nested operations:**
```tony
# Query: Rename fields in nested query
!apiop
path: /users
match: !let
  let:
    - idMatch: "123"
  in: !trim
    id: .idMatch  # Match criteria
    name: !as(userName) null  # Rename name field
    posts:
      !apiop
      path: /posts
      match:
        {authorId: .idMatch id title !as(postTitle)}  # Mix match criteria with aliased field
# Returns: { id: "123", userName: "Alice", posts: [{ id: "post1", postTitle: "First Post" }] }
# Note: !let binds idMatch to "123", then .idMatch references it in both the user match and nested posts query
# Note: Nested match doesn't need !trim since parent !trim already constrains
# Note: When you have sibling fields like posts:, use explicit syntax for the parent match
# Note: Key set notation works when it's the only value (like in nested match)
```

### 9. Query all user-post pairs

```tony
# Query: Get all pairs of user-id and post-id
!apiop
path: /posts
match: !trim
  {id authorId}  # Include id and authorId fields
# Returns:
# posts: !key(id)
#   - id: "post1"
#     authorId: "123"
#   - id: "post2"
#     authorId: "123"
# This gives all (user-id, post-id) pairs via posts[key].authorId and posts[key].id
# Note: When you have only field selection (no match criteria), bare keys work: {id authorId}
```

### 9b. Query all (post-id, user) pairs

Query posts and use a nested operation to fetch the user for each post. Use `!let` at the parent level to bind the `authorId`:

```tony
# Query: Get all pairs of post-id and user
!apiop
path: /posts
match: !trim
  id: null  # Include id field
  authorId: null  # Include authorId field
  author:
    !apiop
    path: /users
    match: !let
      let:
        - authorIdMatch: null  # Bound from parent post's authorId field
      in: !trim
        {id: .authorIdMatch name email}  # Mix match criteria with field selection
# Returns:
# posts: !key(id)
#   - id: "post1"
#     authorId: "123"
#     author:
#       id: "123"
#       name: "Alice"
#       email: "alice@example.com"
#   - id: "post2"
#     authorId: "123"
#     author:
#       id: "123"
#       name: "Alice"
#       email: "alice@example.com"
```

**How it works**: When `!let` is used in a nested `!apiop`, variables can reference fields from the parent match context. The `authorIdMatch: null` binds the value from the parent post's `authorId` field, which is then referenced as `.authorIdMatch` to query `/users`. This effectively joins posts with users.

**Note**: When you have sibling fields like `author:`, use explicit syntax for the parent match. Key set notation works when it's the only value (like in nested match).
