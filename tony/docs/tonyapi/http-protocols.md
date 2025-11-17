# HTTP Protocol Formats

This document describes the HTTP request and response formats for TonyAPI operations. TonyAPI uses four HTTP methods: `MATCH` (queries), `PATCH` (mutations), `WATCH` (subscriptions), and `MOUNT` (controller registration).

## MATCH (Query Operation)

**Purpose**: Query/read data from the virtual document

```http
# Match operation (MATCH) - query using !apiop document
MATCH /api/query HTTP/1.1
Content-Type: application/tony

!apiop
path: /users
match:
  id: "123"

# Returns:
users: !key(id)
  - id: "123"
    name: "Alice"
    email: "alice@example.com"
```

## PATCH (Mutation Operation)

**Purpose**: Modify data in the virtual document

```http
# Write operation (PATCH) - mutation using !apiop document with patch:
PATCH /api/mutation HTTP/1.1
Content-Type: application/tony

!apiop
path: /users
match:
  id: "123"
patch:
  name: "Alice Updated"
```

## MATCH (Metadata Operation)

**Purpose**: Access metadata about document paths

```http
# Metadata operation (MATCH) - explicit metadata path
MATCH /.meta/users/123 HTTP/1.1
Content-Type: application/tony

!apiop
path: /.meta/users/123

# Returns metadata as Tony document:
type: object
size: 1024
modified: "2024-01-15T10:30:00Z"
source: "database:users"
mount:
  controller: "user-controller"
```

## MATCH (Schema Operation)

**Purpose**: Query the unified schema

```http
# Schema metadata operation (MATCH) - unified schema
MATCH /.meta/schema HTTP/1.1
Content-Type: application/tony

!apiop
path: /.meta/schema
match: !trim

# Returns unified schema document:
!schema
signature:
  name: blog-api
define:
  User:
    id: .string
    name: .string
    email: .string
  Post:
    id: .string
    title: .string
    content: .string
    authorId: .string
accept:
  users: .array(.User)
  posts: .array(.Post)
```

## WATCH (Subscription Operation)

**Purpose**: Subscribe to changes in the virtual document

```http
# Watch operation (WATCH)
WATCH /api/query HTTP/1.1
Content-Type: application/tony

!apiop
path: /posts
match: !trim
  published: true
  id: null
  title: null

# Streams diffs as document changes
```

For detailed WATCH format specifications, see [subscriptions.md](./subscriptions.md).

## MOUNT (Controller Registration)

**Purpose**: Register remote controllers with the document server

```http
# Mount operation (MOUNT) - controller requests mount from document server
# Controller initiates connection and sends MOUNT request
MOUNT /.mount/metrics HTTP/1.1
Content-Type: application/tony
Host: document-server:8080

mount:
  controller: "metrics-controller"
  path: "/metrics"  # Mount point path in document tree
  config:
    source: procfs
    path: /proc/metrics
    refresh: 1s
  schema:  # Schema contribution for this mount point
    define:
      Metric:
        cpu: .number
        memory: .number
        timestamp: .string
    accept:
      metrics: .Metric
```

**Response from document server:**
```http
HTTP/1.1 200 OK
Content-Type: application/tony

mount:
  accepted: true
  path: "/metrics"
```

After mount is accepted, the connection remains open and the document server sends diffs to the controller, which responds with result diffs.

## Related Documentation

- [Design Overview](./design.md) - High-level architecture and protocol flow
- [Query Operations](./queries.md) - Detailed query format specifications
- [Mutation Formats](./mutations.md) - Detailed mutation format specifications
- [Subscription Formats](./subscriptions.md) - Detailed WATCH format specifications
- [Controllers](./controllers.md) - Controller documentation including MOUNT details
