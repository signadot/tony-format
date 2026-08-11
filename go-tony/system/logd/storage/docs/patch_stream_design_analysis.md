# Patch-Stream Interface Design Analysis

## Patch Semantics (from `patch.go`)

### JSON Merge Patch Base (RFC 7396-like)

**Object Merging** (`objPatchY`):
- Patch structure mirrors document structure
- Field-by-field recursive merge:
  - If patch field exists in doc: `Patch(docField, patchField)` (recursive)
  - If patch field doesn't exist in doc: `Patch(ir.Null(), patchField)` (creates new)
  - Doc fields not in patch: kept as-is
- Result: Merged object with doc fields + patched fields + new fields

**Array Merging** (lines 66-87):
- If doc is not array: replace with patch
- If both arrays: 
  - Patch elements pairwise by index: `Patch(doc[i], patch[i])`
  - Append remaining patch elements: `patch[n:]` added to result
- Result: Array with patched elements + new elements

**Primitive Replacement** (line 90):
- Replace doc with patch: `patch.Clone()`

**Tag Operations** (lines 43-60):
- If patch has tag (`!replace`, `!insert`, etc.): use `mergeop` operations
- Operations can override default merge behavior

## Key Insight: Recursive Structural Matching

A patch `{ "users": { "123": { "name": "Alice" } } }` applies at multiple levels:

1. **Root level**: Merge `{ "users": ... }` into doc
   - If doc has `users` field: recursively patch `doc.users` with `patch.users`
   - If doc doesn't have `users`: create new `users` field

2. **`users` level**: When patching `doc.users`, merge `{ "123": ... }` into it
   - If `doc.users` has `123` field: recursively patch `doc.users.123` with `patch.users.123`
   - If not: create new `123` field

3. **`users.123` level**: When patching `doc.users.123`, merge `{ "name": "Alice" }` into it
   - If `doc.users.123` has `name` field: replace with `"Alice"`
   - If not: create new `name` field

4. **`users.123.name` level**: Replace with `"Alice"`

## Implications for Streaming

### Challenge: When Does a Patch Apply?

During event streaming:
- We're building structure incrementally: `BeginObject` → `Key` → `String` → `EndObject`
- We track current path via `State.CurrentPath()`
- Patches apply recursively at container boundaries

### Questions:

1. **Detection Point**: When do we check if a patch applies?
   - **Answer**: At container start (`BeginObject`/`BeginArray`) - path is already known from previous `Key`/index event, can check `patch.GetKPath(currentPath)` immediately
   - Path is known because:
     - For objects: parent's `Key` event sets path (e.g., `Key("users")` → path="users", then `BeginObject` enters that container)
     - For arrays: array index sets path before `BeginArray`
   - We don't need to wait until `EndObject` because `GetKPath` only checks patch structure existence, not node content
   - Application still needs complete container, but detection happens early

2. **Scope Matching**: How do we match current path with patch structure?
   - Patch `{ "users": { "123": ... } }` affects paths: `""`, `"users"`, `"users.123"`, `"users.123.*"`
   - Need to check: does current path match any prefix of patch paths?

3. **Buffering Strategy**: What do we buffer?
   - **FUNDAMENTAL QUESTION**: Why buffer events at all?
   - **Constraint**: `Patch(doc *ir.Node, patch *ir.Node)` requires complete nodes
   - **But**: Do we need to apply patches during snapshot building?
   - **Alternatives**:
     - Option A: Apply patches when **reading** snapshots (no buffering during building)
     - Option B: Apply patches **before** streaming (patch source document first)
     - Option C: Buffer during building (current assumption - needs justification)
   - **Question**: What is the actual use case? When should patches be applied?

4. **Nested Application**: How do we handle recursive patching?
   - If patch applies at `users` level, we need to:
     - Buffer `users` container events
     - Convert to node
     - Apply patch recursively
     - Convert back to events
   - But patches also apply at `users.123` level - do we re-buffer?

## Design Space Exploration

### Approach A: Container-Level Buffering with Path Matching

**Concept**: 
- Track current path via `State`
- At container start (`BeginObject`/`BeginArray`), check if patch structure matches current path
- If match: buffer container, apply patch when complete, emit patched events
- If no match: emit events normally

**Example**:
```
Events: BeginObject, Key("users"), BeginObject, Key("123"), String("Bob"), EndObject, EndObject
Path: "" → "users" → "users.123"
Patch: { "users": { "123": { "name": "Alice" } } }

At BeginObject (path=""): Check patch root - has "users" field? Yes → start buffering
At BeginObject (path="users"): Check patch.users - has "123" field? Yes → continue buffering  
At EndObject (path="users.123"): Complete node → apply patch recursively → emit patched events
At EndObject (path=""): Complete root → apply patch → emit patched events
```

**Challenges**:
- Need to extract patch structure/paths upfront
- Need to match paths during streaming
- Buffering overhead (but bounded by container size)

### Approach B: Node-Level Buffering (Complete Node Required)

**Concept**:
- Always buffer until complete node (depth == 0)
- When complete, check if patch applies to current path
- Apply patch, convert to events, emit

**Example**:
```
Events: BeginObject, Key("users"), BeginObject, Key("123"), String("Bob"), EndObject, EndObject
Buffer accumulates all events until depth == 0
At EndObject (depth=0): Have complete root node
Check: Does patch apply? Yes → Patch(rootNode, patch) → convert to events → emit
```

**Challenges**:
- Always buffers (even when no patch applies)
- But simpler logic - always same flow

### Approach C: Pre-Compute Patch Paths

**Concept**:
- Extract all paths that patch affects: `["", "users", "users.123", "users.123.name"]`
- During streaming, check current path against patch paths
- If match: buffer from that point, apply when complete

**Example**:
```
Patch paths: ["", "users", "users.123", "users.123.name"]
Events: BeginObject (path="") → matches "" → start buffering
        Key("users") (path="users") → matches "users" → continue
        BeginObject (path="users") → continue
        Key("123") (path="users.123") → matches "users.123" → continue
        String("Bob") (path="users.123") → continue
        EndObject (path="users") → complete users.123 node → apply patch → emit
        EndObject (path="") → complete root → apply patch → emit
```

**Challenges**:
- How to extract paths from patch structure?
- Need to understand patch structure traversal

## Key Questions to Resolve

### 1. Sub-Patch Extraction (SOLVED)

**Answer**: Use `node.GetKPath(kp string)` to traverse patch structure.

**Example**: 
- Patch: `{ "users": { "123": { "name": "Alice" } } }`
- Current path: `"users.123"`
- Sub-patch: `patch.GetKPath("users.123")` → returns `{ "name": "Alice" }` (or `nil` if path doesn't exist)

**Implications**:
- No need to pre-extract paths - can check lazily during streaming
- If `patch.GetKPath(currentPath)` returns non-nil → patch applies at this path
- If returns `nil` → no patch applies at this path
- Simple and unambiguous: `ir.Node` is a tree, traverse by kpath

### 2. Path Matching During Streaming

**Answer**: Use `patch.GetKPath(currentPath)` to check if patch applies.

**Logic**:
- At current path `"users.123"`: `patch.GetKPath("users.123")` → returns sub-patch or `nil`
- If non-nil: patch applies at this path → buffer container, apply when complete
- If nil: no patch applies → emit events normally

**Key insight**: 
- We check at container boundaries (when we have a complete path)
- `GetKPath` handles the traversal - if path exists in patch, it returns the sub-patch
- No need for prefix matching - we check exactly at the container level where patch applies

**Example**:
- Current path: `"users.123"` (at `EndObject` for `users.123` container)
- `patch.GetKPath("users.123")` → `{ "name": "Alice" }` (sub-patch exists)
- Buffer: events for `users.123` container
- Apply: `Patch(users123Node, subPatch)` → merged result
- Emit: patched events

### 3. Buffering Boundaries

**Question**: What are the boundaries for buffering?

**Option A: Buffer from patch application point**
- If patch applies at `users.123`, buffer from `BeginObject` at `users.123`
- Apply patch when `EndObject` at `users.123`
- But: Patch also applies at `users` level - do we buffer `users` separately?

**Option B: Buffer from root**
- Always buffer from root until complete
- Apply all applicable patches at once
- Simpler but more memory

**Option C: Hierarchical buffering**
- Buffer at each container level
- Apply patches at each level as containers complete
- Complex but memory-efficient

### 4. Patch Application Scope (SOLVED)

**Answer**: Use `patch.GetKPath(currentPath)` to get sub-patch, then `Patch(docNode, subPatch)`.

**Example**: 
- Current path: `"users.123"`
- Patch: `{ "users": { "123": { "name": "Alice" } } }`
- Sub-patch: `patch.GetKPath("users.123")` → `{ "name": "Alice" }`
- Current node: `{ "age": 30 }` (from buffered events → `EventsToNode`)
- Apply: `Patch({ "age": 30 }, { "name": "Alice" })` → `{ "age": 30, "name": "Alice" }`
- Emit: `NodeToEvents(patchedNode)`

**Process**:
1. Buffer events until container complete (depth == 0 at container boundary)
2. Convert buffer → node: `EventsToNode(bufferEvents)`
3. Get sub-patch: `subPatch := patch.GetKPath(currentPath)`
4. Apply patch: `patchedNode := Patch(node, subPatch)`
5. Convert to events: `NodeToEvents(patchedNode)`
6. Emit patched events

## Remaining Open Questions

### 1. Why Buffer Events? (FUNDAMENTAL QUESTION)
**Question**: Do we actually need to buffer events at all?

**Current Assumption**: We buffer events to convert to nodes, apply patches, then convert back to events.

**But Why?**
- **Constraint**: `Patch()` requires complete `ir.Node` structures
- **Therefore**: Need complete nodes to apply patches
- **Therefore**: Must accumulate events until complete node

**Alternatives**:
- **Apply patches when reading**: Read base events from snapshot, apply patches on-the-fly when reading
- **Apply patches before building**: Patch source document first, then stream patched document
- **Different patch model**: Could patches work incrementally? (Probably not - JSON Merge Patch is structural)

**Key Question**: **When should patches be applied?**
- During snapshot building? (write patched events to snapshot)
- During snapshot reading? (read base events, apply patches on-the-fly)
- Before snapshot building? (patch source document first)

**Until this is resolved, buffering boundaries are premature.**

### 2. Buffering Boundaries (DEFERRED - depends on answer to #1)
**Question**: If we do need to buffer, what are the exact boundaries?

**Options**:
- **Option A**: Buffer from container start (`BeginObject`/`BeginArray`)
  - When we see `BeginObject` at path `"users"`, start buffering
  - Apply patch when `EndObject` completes the container
  - **Challenge**: Patch also applies at `users.123` level - do we buffer `users` separately from `users.123`?
  
- **Option B**: Buffer from root, apply all patches at once
  - Always buffer from root until complete document
  - Apply all applicable patches recursively
  - **Challenge**: More memory, but simpler logic
  
- **Option C**: Hierarchical buffering at each container level
  - Buffer at each container level independently
  - Apply patches at each level as containers complete
  - **Challenge**: Complex state management, but memory-efficient

**Analysis Needed**: 
- How does recursive patch application (`Patch` calls `Patch` recursively) affect buffering?
- If patch applies at `users` level, does it also need to handle `users.123` patches?
- Or are patches applied independently at each level?

### 2. Detection Timing (PARTIALLY RESOLVED)
**Answer**: We can detect patch applicability at container start (`BeginObject`/`BeginArray`).

**Remaining Question**: 
- Do we check at container start AND at container end?
- Or only at container start (then buffer until end)?
- What if patch structure changes during streaming? (Unlikely, but need to confirm)

### 3. Multiple Patches (DEFERRED PER USER REQUEST)
**Status**: User requested to "punt on that and assume for the moment we need to figure out how to do 1 patch correctly."

**Future Consideration**: 
- Eventually inputs will be a list of patches
- Need to determine: sequential application vs. merging first
- This is "pretty inevitable and pretty immediate" per user

### 4. Nested Patch Application (NEEDS CLARIFICATION)
**Question**: How do nested patches interact with buffering?

**Scenario**:
- Patch: `{ "users": { "123": { "name": "Alice" } } }`
- Document: `{ "users": { "123": { "age": 30 } } }`
- Patch applies at both `users` level and `users.123` level

**Questions**:
- Do we buffer `users` container, apply patch, then re-buffer `users.123`?
- Or does recursive `Patch` handle nested application automatically?
- If `Patch` handles it recursively, do we only need to buffer at the top-level patch boundary?

**Analysis Needed**: 
- Review `patch.go` to understand recursive application
- Determine if we need separate buffering for nested patches
- Or if single buffering at patch root is sufficient
