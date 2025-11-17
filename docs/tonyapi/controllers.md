# Controllers

This document describes how controllers work in TonyAPI. For the overall design and architecture, see [design.md](./design.md).

## Mount Controllers

A **mount** associates a controller process with a mount point:

- **Process**: The controller is a separate process that handles MATCH/PATCH/WATCH operations
- **Mount point**: Controller handles operations for a specific path/level in the document tree

**Example mount configuration (core controller):**
```yaml
# Document server configuration file
core_controllers:
  users:
    controller: "user-controller"
    executable: "/usr/bin/user-controller"
    config:
      source: database
      table: users
      key: id
    schema:  # Schema contribution for core controller
      define:
        User:
          id: .string
          name: .string
          email: .string
          createdAt: .string
      accept:
        users: !key(id) .array(.User)
```

**Example remote controller registration (controller-initiated MOUNT):**
```http
# Controller connects to document server and requests mount
MOUNT /.mount/posts HTTP/1.1
Content-Type: application/tony
Host: document-server:8080

mount:
  controller: "post-controller"
  path: "/posts"  # Mount point path
  config:
    source: database
    table: posts
    key: id
  schema:  # Schema contribution for this mount point
    define:
      Post:
        id: .string
        title: .string
        content: .string
        authorId: .string
        published: .bool
        createdAt: .string
    accept:
      posts: !key(id) .array(.Post)
```

**Controller responsibilities:**
- Handle MATCH operations (queries) for paths under its mount point
- Handle PATCH operations (mutations) for paths under its mount point
- Handle WATCH operations (subscriptions) for paths under its mount point

**Multiple mounts:**
```tony
# Document with multiple mount points, each with its own controller
users:          # Mounted via user-controller
- id: "123"
    name: "Alice"

posts:          # Mounted via post-controller
- id: "post1"
    title: "First Post"

metrics:        # Mounted via metrics-controller (real-time)
  cpu: 45.2
  memory: 1024
```

### Metadata Paths

Metadata is accessed via explicit paths, not HTTP methods:

```tony
# Metadata for a path
/.meta/users/123

# Returns:
type: object
size: 1024
modified: "2024-01-15T10:30:00Z"
controller: "user-controller"
mount:
  path: "users"

# Schema metadata
/.meta/users/schema

# Returns schema document (see Schema Integration section)
```

**Common metadata paths:**
- `/.meta/{path}` - General metadata (type, size, modified, controller, etc.)
- `/.meta/schema` - Unified schema definition for entire virtual document

This is more filesystem-like - metadata is just another path you can read, not a separate HTTP method.

### Implementation Considerations

1. **Mount Points**: 
   - **Core controllers**: Defined in document server configuration (kernel-level, filesystem, database mounts)
   - **Remote controllers**: Controllers connect to document server and request `MOUNT /.mount/path` with mount point path and configuration
   - Controller process handles operations for that mount point after mount is accepted

2. **Lazy Loading**: Controllers load data on-demand
   - Like procfs, data loaded when path is accessed
   - Controller decides when to materialize data

3. **Caching**: Controller-level caching
   - Each controller manages its own cache
   - Cache invalidation per controller

4. **Permissions**: Controller-level permissions
   - Controllers enforce access control
   - Permissions checked at controller level

5. **Protocol**: Simple operations
   - MATCH: Query path using `!apiop` document (no `patch:`)
   - PATCH: Write/mutate path using `!apiop` document with `patch:`
   - WATCH: Watch path for changes using `!apiop` document (no `patch:`)
   - MATCH `/.meta/path`: Get metadata using `!apiop` document
   - MOUNT `/.mount/path`: Register remote controller for mount point

## Controller Communication

Controllers are separate processes that communicate with:
- **LogD server**: Direct communication for reading/writing diffs and state
- **Document server**: Via stdin/stdout (local) or network (remote) for routing client requests

- **Local controllers**: Communicate with document server via **stdin/stdout** (spawned by document server)
- **Remote controllers**: Communicate with document server via **network connection** (controller connects to document server and requests MOUNT)

For mutations: Controllers receive diffs from document server (routed from client), apply directly to logd server, return revision numbers.
For queries: Controllers receive query documents from document server, read state from logd server, return result documents.

### Communication Model: Diffs via stdin/stdout or network

**Local controllers** (spawned processes):
- **Read from stdin**: Receive queries (MATCH) or diffs (PATCH) from document server (routed from clients)
- **Write to stdout**: Emit result documents (MATCH) or revision numbers (PATCH) to document server
- **Direct communication with logd server**: Controllers read state and apply diffs directly to logd server
- This is very Plan 9-like - controllers are processes that communicate via standard streams with document server, but directly with logd server

**Remote controllers** (network connections):
- **Read from network connection**: Receive queries (MATCH) or diffs (PATCH) from document server (routed from clients)
- **Write to network connection**: Emit result documents (MATCH) or revision numbers (PATCH) to document server
- **Direct communication with logd server**: Controllers read state and apply diffs directly to logd server
- Controller initiates connection and requests MOUNT, then communicates directly with logd server for all operations

### Controller Operation

**Initial State:**

When a controller connects, it reads all diffs directly from the logd server and reconstructs the current state. The controller communicates directly with the logd server - it does not receive initial state from the document server.

```tony
# Controller reads directly from logd server and reconstructs state
# Controller has current state, ready to handle operations
users: !key(id)
- id: "123"
    name: "Alice"
  - id: "456"
    name: "Bob"
```

The document server can query the logd server to get current state for its virtual document cache. Controllers maintain their own state by reading from the logd server.

**Applying Diffs (Mutations):**

When a client sends a PATCH request to the document server, the document server routes the diff to the appropriate controller based on the mount point. The controller receives the diff and applies it directly to the logd server:

```tony
# Document server routes client's diff to controller
# Controller receives diff via stdin (local) or network (remote)
users: !key(id)
- id: "123"
    name: !replace
      from: "Alice"
      to: "Alice Updated"
```

Controller applies diff directly to logd server (logd server stores diff in its log). Controller returns revision number or error:

```tony
# Success response - revision number at which changes were applied
revision: 42
```

```tony
# Error response
!error
code: conflict
message: "Revision mismatch: expected 40, got 41"
revision: 41
```

**Key points:**
- Controller communicates **directly** with logd server (not through document server)
- Controller returns **revision number** (or error), not the diff itself
- Document server doesn't store diffs - logd server is the source of truth
- Document server can query logd server for state at specific revisions if needed

**Backend Storage:**
- LogD server stores diffs, not full state (e.g., etcd stores sequence of diffs)
- Current state = apply all diffs from base point
- All writes are diffs: `backend.ApplyDiff(diff)` returns revision number
- Reads reconstruct state: `state = backend.GetState()` or `backend.GetStateAt(revision)` (applies all diffs up to revision)

**Querying (MATCH Operations):**

For MATCH operations, document server sends a query document (what to select):

```tony
# Query - select user 123 with specific fields
users: !key(id)
- id: "123"
    name: null
    posts:
      id: null
      title: null
```

Controller reads current state from backend (reconstructed from all diffs), filters to match query, writes result:

```tony
# Controller writes result
users: !key(id)
- id: "123"
    name: "Alice"
    posts: !key(id)
      - id: "post1"
        title: "First Post"
      - id: "post2"
        title: "Second Post"
```

**Note**: Controller reads state directly from logd server (which reconstructs from diffs), not from stored full state.

**Watching (Subscriptions):**

For WATCH operations, controllers watch the logd server directly for changes:

1. Client sends WATCH request to document server with query
2. Document server registers the watch and notifies the appropriate controller
3. Controller watches logd server directly for new diffs affecting its mount point
4. When logd server has new diffs, controller reads them and computes state changes
5. Controller sends diffs to document server, which forwards them to the client:

```tony
# Controller watches logd server, detects new diff at revision 42
# Controller computes diff and sends to document server
users: !key(id)
- id: "123"
    name: !replace
      from: "Alice"
      to: "Alice Updated"
```

**Key point**: Controllers watch the logd server directly - they don't receive watch updates from the document server. The document server only routes the initial watch request and forwards diffs from controllers to clients.

### Diff Format

All communication uses Tony diff format (from `tony.Diff()`):

**Insert:**
```tony
users: !key(id)
- !insert
    id: "789"
    name: "Charlie"
```

**Delete:**
```tony
users: !key(id)
- !delete
    id: "456"
```

**Replace:**
```tony
users: !key(id)
- id: "123"
    name: !replace
      from: "Alice"
      to: "Alice Updated"
```

**Keyed Object Diff:**
```tony
users: !key(id)
- id: "123"
    name: !replace
      from: "Alice"
      to: "Alice Updated"
```

### Controller Lifecycle

**Local Controllers (spawned processes):**
1. Document server spawns controller process locally
2. Controller reads mount configuration from environment or initial stdin message
3. Controller initializes connection to logd server (e.g., etcd)
4. Controller reads all diffs directly from logd server and reconstructs current state
5. Controller is ready to handle operations - communicates directly with logd server
6. For WATCH operations, controller watches logd server directly for changes

**Remote Controllers (network endpoints):**
1. Remote controller initiates connection to document server endpoint
2. Remote controller sends MOUNT request with mount point path and configuration
3. Document server accepts mount and registers controller for mount point
4. Remote controller initializes connection to logd server (e.g., etcd)
5. Remote controller reads all diffs directly from logd server and reconstructs current state
6. Controller is ready to handle operations - communicates directly with logd server
7. For WATCH operations, controller watches logd server directly for changes

**Operation (PATCH - Mutations):**

**Single-path PATCH:**
1. Client sends PATCH request with diff to document server
2. Document server routes diff to appropriate controller (based on mount point) via stdin (local) or network (remote)
3. Controller receives diff and applies it directly to logd server: `revision, err := logd.ApplyDiff(diff)` - adds diff to logd server's diff log
4. If successful, controller writes revision number to document server (via stdout for local, network for remote)
5. If error (e.g., conflict, validation failure), controller writes error response to document server
6. Document server receives revision number (or error) and responds to client - document server does not store diffs

**Multi-path PATCH (Transaction):**
1. Client sends PATCH request affecting multiple mount points to document server
2. Document server requests transaction ID from logd server with participant count (number of controllers)
3. LogD server generates transaction ID with participant count (e.g., `tx-12345-2`)
4. Document server sends diffs to all relevant controllers with the transaction ID
5. Controllers validate diffs and apply directly to logd server with transaction ID and participant count
6. LogD server waits until all expected participants have written their diffs
7. LogD server commits atomically when all participants complete
8. Controllers return revision numbers to document server
9. Document server responds to client with success
10. If any controller validation fails before writing:
    - Controller returns error to document server
    - Document server aborts transaction at logd server
    - Document server responds to client with error

**Operation (MATCH - Queries):**
1. Client sends MATCH request with query to document server
2. Document server routes query to appropriate controller (based on mount point) via stdin (local) or network (remote)
3. Controller reads current state directly from logd server: `state = logd.GetState()` - reconstructs from all diffs
4. Controller filters state to match query criteria
5. Controller writes result document to document server (via stdout for local, network for remote)
6. Document server forwards result to client

**Shutdown:**
1. Document server sends shutdown signal
2. Controller flushes pending operations
3. Controller exits

### Example: User Controller

```bash
#!/bin/bash
# user-controller

# Read mount config from stdin
read CONFIG

# Initialize connection to logd server (e.g., etcd)
LOGD_SERVER="etcd://logd-server:2379/users"
init_logd "$LOGD_SERVER"

# Read all diffs directly from logd server and reconstruct current state
CURRENT_STATE=$(logd_get_state "$LOGD_SERVER")

# Controller has current state, ready to handle operations
# Controller communicates directly with logd server

# Main loop: read queries/diffs from document server
while IFS= read -r REQUEST; do
  # Parse request to check if it has a transaction ID
  TRANSACTION_ID=$(echo "$REQUEST" | get_field "transaction_id")
  DIFF=$(echo "$REQUEST" | get_field "diff")
  
  if [ -n "$TRANSACTION_ID" ]; then
    # Multi-path transaction - validate and apply with transaction ID
    VALIDATION_RESULT=$(validate_diff "$DIFF")
    
    if [ $? -ne 0 ]; then
      # Validation failed - return error before writing to logd server
      echo "transaction_id: $TRANSACTION_ID"
      echo "!error"
      echo "code: validation_failed"
      echo "message: $VALIDATION_RESULT"
      continue
    fi
    
    # Extract participant count from transaction ID (e.g., "tx-12345-2" -> 2)
    PARTICIPANT_COUNT=$(echo "$TRANSACTION_ID" | extract_participant_count)
    
    # Apply diff to logd server with transaction ID and participant count
    REVISION=$(logd_apply_diff "$LOGD_SERVER" "$DIFF" "$TRANSACTION_ID" "$PARTICIPANT_COUNT")
    
    if [ $? -eq 0 ]; then
      # Success - return revision
      echo "transaction_id: $TRANSACTION_ID"
      echo "revision: $REVISION"
    else
      # Error applying to logd server
      echo "transaction_id: $TRANSACTION_ID"
      echo "!error"
      echo "code: $(logd_get_error_code "$LOGD_SERVER")"
      echo "message: $(logd_get_error_message "$LOGD_SERVER")"
    fi
    
  else
    # Regular single-path diff (no transaction)
    REVISION=$(logd_apply_diff "$LOGD_SERVER" "$REQUEST")
    
    if [ $? -eq 0 ]; then
      # Success - write revision number to document server
      echo "revision: $REVISION"
    else
      # Error - write error response
      echo "!error"
      echo "code: $(logd_get_error_code "$LOGD_SERVER")"
      echo "message: $(logd_get_error_message "$LOGD_SERVER")"
      echo "revision: $(logd_get_last_revision "$LOGD_SERVER")"
    fi
  fi
done
```

**Document server interaction (local controller):**

```go
// Document server spawns local controller
cmd := exec.Command("user-controller")
cmd.Stdin = pipeIn
cmd.Stdout = pipeOut

// Send diff to controller
diff := tony.Diff(oldState, newState)
encode.Encode(diff, pipeIn)

// Read revision number (or error) from controller
response := parse.Parse(pipeOut)
if response.Error != nil {
    // Handle error (conflict, validation, etc.)
    return response.Error
}
revision := response.Revision
// Document server can query logd server at this revision if needed
// Document server does not store the diff - logd server is source of truth
```

**Remote controller (initiates connection and requests mount):**

```go
// Remote controller connects to document server
conn, err := net.Dial("tcp", "document-server:8080")

// Send MOUNT request with schema contribution
mountReq := tony.MountRequest{
    Controller: "post-controller",
    Path: "/posts",
    Config: config,
    Schema: tony.Schema{
        Define: map[string]interface{}{
            "Post": map[string]interface{}{
                "id": ".string",
                "title": ".string",
                "content": ".string",
                "authorId": ".string",
                "published": ".bool",
                "createdAt": ".string",
            },
        },
        Accept: map[string]interface{}{
            "posts": "!key(id) .array(.Post)",
        },
    },
}
encode.Encode(mountReq, conn)

// Read mount acceptance
mountResp := parse.Parse(conn)
if mountResp.Accepted {
    // Document server has composed unified schema with this controller's contribution
    // Control loop established - read queries/diffs/transaction commands from document server
    for {
        request := parse.Parse(conn)
        
        // Check if this is a transaction request
        if request.TransactionID != "" {
            // Multi-path transaction - validate and apply with transaction ID
            err := validateDiff(request.Diff)
            if err != nil {
                // Validation failed - return error before writing to logd server
                encode.Encode(ErrorResponse{
                    TransactionID: request.TransactionID,
                    Error: err,
                }, conn)
                continue
            }
            
            // Extract participant count from transaction ID (e.g., "tx-12345-2" -> 2)
            participantCount := extractParticipantCount(request.TransactionID)
            
            // Apply diff to logd server with transaction ID and participant count
            revision, err := logd.ApplyDiff(request.Diff, request.TransactionID, participantCount)
            if err != nil {
                encode.Encode(ErrorResponse{
                    TransactionID: request.TransactionID,
                    Error: err,
                }, conn)
            } else {
                encode.Encode(RevisionResponse{
                    TransactionID: request.TransactionID,
                    Revision: revision,
                }, conn)
            }
        } else {
            // Regular single-path diff (no transaction)
            revision, err := logd.ApplyDiff(request.Diff)
            if err != nil {
                encode.Encode(ErrorResponse{
                    Error: err,
                    Revision: logd.GetLastRevision(),
                }, conn)
            } else {
                encode.Encode(RevisionResponse{Revision: revision}, conn)
            }
        }
    }
}
```

**Document server (accepts mount and routes requests):**

```go
// Document server receives MOUNT request
mountReq := parse.Parse(conn)
if validateMount(mountReq) {
    // Accept mount
    encode.Encode(MountResponse{Accepted: true}, conn)
    
    // Compose unified schema with controller's schema contribution
    if mountReq.Schema != nil {
        composeSchema(mountReq.Path, mountReq.Schema)
        // Unified schema at /.meta/schema now includes this controller's contribution
    }
    
    // Register controller for mount point
    registerController(mountReq.Path, mountReq.Controller, conn)
    
    // Controller now communicates directly with logd server
    // Document server routes client requests to controller when needed:
    // - PATCH: Route diff to controller, receive revision
    // - MATCH: Route query to controller, receive result
    // - WATCH: Register watch, controller watches logd server and sends diffs
}

// When client sends PATCH request:
func handlePATCH(paths []string, diff tony.Diff) {
    if len(paths) > 1 {
        // Multi-path transaction
        // Request transaction ID from logd server with participant count
        transactionResp := requestTransactionFromLogD(len(paths))
        transactionID := transactionResp.TransactionID
        
        // Send diffs to all controllers with transaction ID
        errors := make(map[string]error)
        revisions := make(map[string]int64)
        
        for _, path := range paths {
            controller := getControllerForPath(path)
            pathDiff := extractDiffForPath(diff, path)
            encode.Encode(DiffRequest{
                TransactionID: transactionID,
                Diff: pathDiff,
            }, controller.Conn)
            response := parse.Parse(controller.Conn)
            
            if response.Error != nil {
                errors[path] = response.Error
                // Abort transaction at logd server
                abortTransactionAtLogD(transactionID)
                // Continue reading responses from other controllers (they may have already written)
                // But transaction is aborted, so their writes will be discarded
            } else {
                revisions[path] = response.Revision
            }
        }
        
        // If any errors occurred, return error
        if len(errors) > 0 {
            // Return first error
            for _, err := range errors {
                return ErrorResponse{
                    TransactionID: transactionID,
                    Error: err,
                }
            }
        }
        
        // All succeeded - return revision (all should be same due to logd server transaction)
        return RevisionResponse{
            TransactionID: transactionID,
            Revision: revisions[paths[0]],
        }
    } else {
        // Single-path operation (no transaction)
        controller := getControllerForPath(paths[0])
        encode.Encode(diff, controller.Conn)
        response := parse.Parse(controller.Conn)
        return response
    }
}
```

**Key point**: Remote controllers **initiate** the connection to the document server and request a MOUNT. After the mount is accepted, controllers communicate **directly** with the logd server. The document server only routes client requests (PATCH/MATCH/WATCH) to controllers and forwards responses. Controllers read from and write to the logd server directly - the document server does not store diffs or act as an intermediary for logd operations.

**Using o commands:**
- `o patch <diff> <state>` - Applies diff to state, outputs new state (used for computing state from diffs)
- `o diff <old> <new>` - Computes diff between two states
- Controller uses backend to store/retrieve diffs, uses `o` commands to reconstruct state and compute diffs

### Example: Controller Adding Computed Fields

Controllers can add computed/derived fields by applying an additional diff to the logd server:

```bash
#!/bin/bash
# user-controller with computed fields

BACKEND="etcd://backend:2379/users"

while IFS= read -r DIFF; do
  # Apply original diff to logd server
  REVISION=$(logd_apply_diff "$LOGD_SERVER" "$DIFF")
  
  if [ $? -ne 0 ]; then
    # Error applying original diff
    echo "!error"
    echo "code: $(logd_get_error_code "$LOGD_SERVER")"
    echo "message: $(logd_get_error_message "$LOGD_SERVER")"
    echo "revision: $(logd_get_last_revision "$LOGD_SERVER")"
    continue
  fi
  
  # Read current state to determine what changed
  CURRENT_STATE=$(logd_get_state_at "$LOGD_SERVER" "$REVISION")
  
  # Create computed fields diff (adds status and timestamp to changed items)
  COMPUTED_DIFF=$(cat <<TONY
users: !key(id)
  - id: "123"
    status: "updated"
    lastModified: !eval \$[now().Format("2006-01-02T15:04:05Z")]
TONY
  )
  
  # Apply computed fields diff to logd server
  FINAL_REVISION=$(logd_apply_diff "$LOGD_SERVER" "$COMPUTED_DIFF")
  
  if [ $? -eq 0 ]; then
    # Success - return final revision number
    echo "revision: $FINAL_REVISION"
  else
    # Error applying computed fields (unlikely, but handle it)
    echo "!error"
    echo "code: $(logd_get_error_code "$LOGD_SERVER")"
    echo "message: $(logd_get_error_message "$LOGD_SERVER")"
    echo "revision: $REVISION"
  fi
done
```

**Example interaction:**

**Server sends:**
```tony
users: !key(id)
- id: "123"
    name: !replace
      from: "Alice"
      to: "Alice Updated"
```

**Controller responds with revision:**
```tony
revision: 42
```

The computed fields (status, lastModified) are stored in the logd server at revision 42. The document server can query the logd server at this revision to see the full state including computed fields.

This allows controllers to add metadata, computed fields, or side effects (like status tracking, timestamps, validation results) directly to the logd server.

### MATCH Operations

For MATCH operations, the query is a selection pattern:

```tony
# Query - what to select
users: !key(id)
- id: "123"
    name: null
```

Controller interprets `null` as "match anything, include this field" and returns:

```tony
# Result
users: !key(id)
- id: "123"
    name: "Alice"
```

### Error Handling

Errors are communicated as error responses with revision information:

```tony
# Error response
!error
code: conflict
message: "Revision mismatch: expected 40, got 41"
revision: 41
```

```tony
# Error response (validation failure)
!error
code: validation_failed
message: "User email must be valid"
revision: 40
```

The revision number in the error response indicates the last known revision from the logd server, which can help with conflict resolution and retry logic.

### Cross-Controller Communication

For MATCH operations spanning multiple controllers, document server:
1. Sends query to first controller
2. Receives result
3. Sends sub-query to second controller
4. Composes results

Controllers don't directly communicate - document server orchestrates.

### Atomicity for Multi-Mount PATCH Operations

When a single PATCH operation affects multiple mount points (e.g., updating both `/users/123` and `/posts/post1`), the document server coordinates with controllers and the logd server to ensure atomicity:

**Problem:**
```tony
# Single PATCH affecting both users and posts
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

**Solution: LogD Server Generates Transaction ID with Participant Count**

The logd server generates a transaction ID that includes the number of controllers that must complete writing diffs:

1. **Document server receives PATCH** affecting multiple mount points
2. **Document server requests transaction ID from logd server** with the number of participants
3. **LogD server generates transaction ID** with participant count (e.g., `tx-12345-2` where `2` means 2 controllers must complete)
4. **Document server sends diff to each controller** with the transaction ID
5. **Controllers validate and apply diff directly to logd server** with the transaction ID
6. **LogD server waits** until all expected diffs for the transaction are received
7. **LogD server commits atomically** when all participants have written their diffs
8. **Controllers return revision numbers** to document server
9. **Document server responds to client** with success

**Transaction Flow:**

```tony
# Step 1: Document server requests transaction ID from logd server
request_transaction:
  participant_count: 2

# LogD server responds with transaction ID
transaction_id: "tx-12345-2"
participant_count: 2

# Step 2: Document server sends diff to user-controller with transaction ID
transaction_id: "tx-12345-2"
diff:
  users: !key(id)
    - id: "123"
      name: !replace
        from: "Alice"
        to: "Alice Updated"

# Step 3: User-controller validates and applies to logd server
# User-controller to logd server:
write:
  transaction_id: "tx-12345-2"
  participant_count: 2
  path: "/users"
  diff:
    users: !key(id)
      - id: "123"
        name: !replace
          from: "Alice"
          to: "Alice Updated"

# LogD server acknowledges (but doesn't commit yet - waiting for 2nd participant)
write:
  success: true
  transaction_id: "tx-12345-2"
  participants_received: 1
  participants_expected: 2
  pending: true

# Step 4: Document server sends diff to post-controller with same transaction ID
transaction_id: "tx-12345-2"
diff:
  posts: !key(id)
    - id: "post1"
      title: !replace
        from: "Old Title"
        to: "New Title"

# Step 5: Post-controller validates and applies to logd server
# Post-controller to logd server:
write:
  transaction_id: "tx-12345-2"
  participant_count: 2
  path: "/posts"
  diff:
    posts: !key(id)
      - id: "post1"
        title: !replace
          from: "Old Title"
          to: "New Title"

# LogD server now has all participants - commits atomically
write:
  success: true
  transaction_id: "tx-12345-2"
  participants_received: 2
  participants_expected: 2
  seq: 42
  committed: true

# Controllers return revision to document server
transaction_id: "tx-12345-2"
revision: 42

# Document server responds to client
revision: 42
```

**Error Handling:**

```tony
# If user-controller validation fails before writing to logd server
transaction_id: "tx-12345-2"
error: validation_failed
message: "User 123 not found"

# Document server can abort transaction at logd server
abort_transaction:
  transaction_id: "tx-12345-2"

# LogD server discards any partial writes for this transaction
abort_transaction:
  success: true
  transaction_id: "tx-12345-2"
  aborted: true

# Document server responds to client with error
!error
code: validation_failed
message: "User 123 not found"
transaction_id: "tx-12345-2"
```

**LogD Server Transaction Support:**

The logd server tracks transactions by transaction ID:
- When a diff is written with a transaction ID and participant count, it's stored in a pending state
- The logd server waits until `participant_count` diffs with the same transaction ID are received
- Once all participants have written, the logd server commits all diffs atomically
- All diffs in a transaction receive the same revision number
- If a transaction is aborted, all pending diffs for that transaction are discarded
- Other operations don't see partial transaction state (only committed transactions are visible)

**Key Guarantees:**
- **Atomicity**: Either all paths are updated or none are (logd server commits only when all participants complete)
- **Consistency**: All paths see the same revision number
- **Isolation**: Other operations don't see partial updates (pending transactions are not visible)
- **Durability**: Once committed, all changes are persisted
- **Simplicity**: No prepare/commit phases - controllers write directly with transaction ID
- **Controller autonomy**: Controllers remain the only ones communicating with logd server

**Single-Path Operations:**

For PATCH operations affecting only one mount point, no transaction is needed:
1. Document server sends diff to controller (no transaction ID)
2. Controller applies directly to logd server
3. Controller returns revision number
4. Document server responds to client

### Benefits of Diff-Based Communication (for Mutations)

1. **Coherent**: Uses same diff format as rest of Tony system
2. **Simple**: Just read/write diffs, no complex protocol
3. **Plan 9-like**: Processes communicate via stdin/stdout
4. **Composable**: Diffs compose naturally
5. **Watch-friendly**: Controllers naturally stream diffs for subscriptions

**Note**: MATCH operations use query documents (not diffs) to express what to select. Only mutations (PATCH) and subscriptions (WATCH) use diffs.

### Storage Architecture: Diff-Based Backend

**Key principle**: Backend stores diffs, not full state. Current state is reconstructed by applying all diffs.

**Backend Interface:**
- `revision, err := backend.ApplyDiff(diff)` - Add diff to backend's diff log, returns revision number or error
- `state := backend.GetState()` - Reconstruct current state by applying all diffs from base point
- `state := backend.GetStateAt(revision)` - Reconstruct state at specific revision
- `state := backend.GetStateAt(path)` - Reconstruct state for specific path

**Benefits:**
- **No duplicate storage** - Single source of truth (diff log in backend)
- **History/audit trail** - All changes preserved as diffs
- **Time-travel** - Reconstruct state at any point in time
- **Efficient** - Only store changes, not full state snapshots
- **Conflict resolution** - Can merge diffs intelligently
- **Event sourcing** - Natural fit for event-driven architectures

**Example backend (etcd-like):**
```go
// Backend stores diffs in log
revision, err := backend.ApplyDiff(diff)  // Append diff to log, returns revision number
if err != nil {
    return 0, err  // Conflict, validation error, etc.
}

// Reconstruct current state
state := ir.Null()
for _, diff := range backend.GetAllDiffs() {
    state = tony.Patch(state, diff)
}
return state

// Reconstruct state at specific revision
state := ir.Null()
for _, diff := range backend.GetDiffsUpTo(revision) {
    state = tony.Patch(state, diff)
}
return state
```

**Controller Initialization:**
```go
// Controller reads all diffs directly from logd server
currentState := logd.GetState()  // Reconstructs from all diffs

// Controller has current state, ready to handle operations
// Controller communicates directly with logd server for all operations
// Document server can query logd server independently if needed
```

**Virtual Document:**
- Computed on-demand from backend diffs
- Document server caches the computed view for performance
- No separate storage needed - virtual document is ephemeral view

This works for:
- **WATCH operations**: Controller watches logd server directly, sends diffs to document server as changes occur
- **MATCH operations**: Controller reads state directly from logd server, filters, returns result to document server
- **PATCH operations**: Controller receives diff from document server (routed from client), applies to logd server, returns revision
