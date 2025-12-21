# Patch Application at Read Time - Design Evaluation

## Proposed Design

### Phase 1: Coordinator Simplifies Patches (During TX Writes)
- Coordinator "executes" complex patches (complex mergeops, diffs, etc.)
- Leaves behind only **simple patches**: `!insert`, `!delete`, `!replace` operations
- Uses merge patching (not diffs which may fail)
- **Result**: Complex mergeops problem solved at write time
- **Simple patches**: Only basic operations that are guaranteed to work, no complex logic

### Phase 2: Apply Simple Patches at Read Time
- **Motivation**: Serves both `ReadStateAt` and snapshotting
- **Operations**: `!insert`, `!delete`, `!replace` (via mergeops)
- **Key insight**: These operations are explicit and localized - know exactly what to do at specific paths
- **No complex logic**: Simple operations avoid complex mergeops that might fail or require full context

### Phase 3: Patch Organization
- Patches arranged as **ordered list per shallowest affected path**
- Example: All patches affecting `"users"` grouped together, ordered
- Example: All patches affecting `"users.123"` grouped together, ordered

### Phase 4: Snap Reader Logic
1. **Detect first patch** for a given path
2. **Read and drop** the source node (as events? or in memory?)
3. **Produce result node** from first patch
4. **Re-apply same logic** while re-reading result node as events for subsequent patches

## Evaluation

### Advantages ✅

1. **No buffering during snapshot building**
   - Snapshot stores unpatched events
   - Patches applied only when reading
   - Simpler snapshot building process

2. **Serves multiple use cases**
   - `ReadStateAt`: Apply patches when reading state
   - Snapshotting: Apply patches when reading snapshots
   - Single implementation for both

3. **Simple operations don't need full nodes**
   - Insertion: Add field/element (know where to insert)
   - Deletion: Remove field/element (know what to remove)
   - Replacement: Replace field/element (know what to replace)
   - Merge style: Can be done incrementally?

4. **Streaming-friendly**
   - Can process patches incrementally
   - Don't need to load entire document

### Questions & Concerns ❓

#### 1. How do `!insert`/`!delete`/`!replace` work without full nodes?

**Understanding the Operations:**
- `!insert`: Adds new field/element at specific position (explicit operation)
- `!delete`: Removes field/element at specific position (explicit operation)
- `!replace`: Replaces field/element at specific position (explicit operation)

**Key Insight**: These operations are **explicit and localized** - they know exactly:
- What path to operate on
- What position/index to operate at
- What value to insert/replace with

**For Objects:**
- `!insert`: Know field name and position - can stream events, insert at right point
- `!delete`: Know field name - can stream events, skip deleted field
- `!replace`: Know field name - can stream events, replace field value

**For Arrays:**
- `!insert`: Know index position - can stream events, insert at index
- `!delete`: Know index position - can stream events, skip deleted element
- `!replace`: Know index position - can stream events, replace element

**Question**: Can we apply these operations incrementally to streaming events?
- We know the path (from `State.CurrentPath()`)
- We know the operation (from patch structure)
- Can we modify the event stream as we read it?

#### 2. "Read and drop the source node"

**Interpretation A**: Read source node as events, process with patches, then drop
- Stream events from snapshot
- Apply patches incrementally as events stream
- Don't accumulate full node in memory
- **Question**: How do we apply patches to streaming events?

**Interpretation B**: Read source node into memory, apply patch, then drop
- Load node from snapshot (via `NodeAt(offset)`)
- Apply patch to node
- Drop node from memory
- **Question**: This still requires loading the node - contradicts "don't need full nodes"?

#### 3. "Re-reading the result node as events for subsequent patches"

**Process**:
1. Apply first patch → get result node
2. Convert result node → events (`NodeToEvents`)
3. Stream events through next patch application
4. Convert patched events → node
5. Repeat for subsequent patches

**Questions**:
- Is this efficient? (node → events → node → events...)
- Or can we apply patches directly to events without round-trip conversion?
- How many patches typically apply to the same path?

#### 4. "Ordered list per shallowest affected path"

**Example**:
```
Patches affecting "users":
  - Patch 1: { "users": { "123": { "name": "Alice" } } }
  - Patch 2: { "users": { "123": { "age": 30 } } }
  - Patch 3: { "users": { "456": { "name": "Bob" } } }

Patches affecting "users.123":
  - Patch 1: { "name": "Alice" }
  - Patch 2: { "age": 30 }
```

**Questions**:
- How are patches organized? Map from path → []*ir.Node?
- What is "shallowest affected path"? The top-level path in the patch structure?
- How do we detect which patches apply to a given path during reading?

#### 5. Merge Style Operations (CLARIFIED)

**Current `Patch()` behavior**:
- Uses `mergeop` operations via tags (`!insert`, `!delete`, `!replace`, etc.)
- Supports insertion: `!insert` tag adds new fields/elements
- Supports deletion: `!delete` tag removes fields/elements
- Supports replacement: `!replace` tag replaces fields/elements
- Also supports recursive structural merge (when no tag)

**Correction**: Current `Patch()` already supports insert/delete/replace via mergeops.

**Question**: What makes patches "simple" after coordinator execution?
- Are they patches that only use `!insert`, `!delete`, `!replace` (no complex mergeops)?
- Are they patches that have been "flattened" to only affect specific paths?
- Do they avoid recursive structural merging in favor of explicit operations?

**Key Insight**: If patches are simplified to only use `!insert`, `!delete`, `!replace`, they might be more amenable to incremental application because:
- `!insert`: Know exactly where to insert (path + position)
- `!delete`: Know exactly what to delete (path)
- `!replace`: Know exactly what to replace (path)
- No recursive merging needed - operations are explicit and localized

## Proposed Evaluation Framework

### 1. Clarify Simple Patch Operations ✅ (CLARIFIED)
- **Simple patches**: `!insert`, `!delete`, `!replace` operations only
- **How they differ**: Explicit operations vs. recursive structural merge
- **Can they be applied incrementally?**: This is the key question - need to design how

### 2. Understand Patch Organization
- How are patches stored/organized?
- How do we look up patches for a given path?
- What is "shallowest affected path"?

### 3. Design Read-Time Application
- How do we detect patches for current path?
- How do we apply patches to streaming events?
- Can we avoid node ↔ events round-trips?

### 4. Evaluate Efficiency
- Compare: buffering during build vs. patching during read
- Memory usage: buffering vs. streaming patches
- Performance: multiple patches per path

## Next Steps

1. ✅ **Clarify simple patch semantics**: `!insert`, `!delete`, `!replace` operations
2. **Design patch organization**: How are patches stored and looked up by shallowest path?
3. **Design streaming patch application**: How to apply `!insert`/`!delete`/`!replace` to streaming events?
   - Can we modify event stream as we read it?
   - Or do we need to load nodes (but only one at a time)?
4. **Evaluate "re-reading result node as events"**: Is node → events → node round-trip acceptable?
5. **Prototype**: Test with simple cases to validate approach
