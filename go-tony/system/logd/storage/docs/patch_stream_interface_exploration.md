# Patch-Stream Interface Exploration

## Current State

### mergeop.patchOp
- Works with `*ir.Node` (complete document structure)
- `Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, df libdiff.DiffFunc) (*ir.Node, error)`
- Requires full node in memory
- Returns complete patched node

### stream.Event
- Incremental, streaming representation
- Events: `BeginObject`, `Key`, `String`, `EndObject`, etc.
- `stream.State` tracks current path during processing
- Events are processed one at a time

## Design Space: patchStreamOp

### Option 1: Event-Level Streaming Interface

```go
type StreamOp interface {
    // ProcessEvent processes a single event and returns events to emit
    // Returns: (eventsToEmit, shouldSkipBase, error)
    // - eventsToEmit: Events to output (could be 0, 1, or many)
    // - shouldSkipBase: If true, skip the input event (patch replaces it)
    ProcessEvent(
        event *stream.Event,
        state *stream.State,
        patches []*ir.Node, // Patches that might apply
        mf MatchFunc,
        pf PatchFunc,
    ) (eventsToEmit []stream.Event, shouldSkipBase bool, err error)
}
```

**Pros:**
- True streaming: processes events one at a time
- Memory efficient: no buffering needed
- Natural fit for event-based snapshots

**Cons:**
- Complex: patches operate on nodes, not events
- Need to detect patch boundaries during streaming
- Hard to handle nested patches

---

### Option 2: Node-Buffering Streaming Interface

```go
type StreamOp interface {
    // ProcessEvent buffers events until a complete node is available
    // When a complete node is detected (depth == 0), applies patches
    ProcessEvent(
        event *stream.Event,
        state *stream.State,
        buffer *EventBuffer, // Accumulates events for current node
        patches []*ir.Node,
        mf MatchFunc,
        pf PatchFunc,
    ) (eventsToEmit []stream.Event, err error)
}

type EventBuffer struct {
    events []stream.Event
    state  *stream.State
    depth  int // Track when we have a complete node
}
```

**Pros:**
- Can use existing mergeop logic (convert buffer → node → patch → events)
- Simpler integration with existing patch operations
- Handles nested structures naturally

**Cons:**
- Requires buffering (memory overhead)
- Need to detect node boundaries
- Less "pure" streaming

---

### Option 3: Pre-Converted Patch Events

```go
// Convert patches to events upfront
type PatchEventStream struct {
    patches map[string][]stream.Event // kpath → events
    applied map[string]bool           // Track which patches applied
}

// During event processing:
// 1. Track current path via State
// 2. Check if path matches a patch
// 3. If match: emit patch events, skip base events until patch complete
// 4. If no match: emit base event normally
```

**Pros:**
- Simple: patches are already events
- Fast: O(1) lookup by kpath
- Clear separation: patch conversion separate from merging

**Cons:**
- Memory: all patch events in memory
- Complex merge logic: handle insertions/deletions at event level
- Need to track "inside patch" vs "outside patch" state

---

### Option 4: Hybrid: StreamOp with Node Conversion

```go
type StreamOp interface {
    // ProcessEvent with ability to request node conversion
    ProcessEvent(
        event *stream.Event,
        state *stream.State,
        patches []*ir.Node,
        mf MatchFunc,
        pf PatchFunc,
    ) (action StreamAction, err error)
}

type StreamAction interface {
    EmitEvents() []stream.Event
    ShouldSkip() bool
    NeedsNode() bool // If true, buffer until complete node
}

// When NeedsNode() == true:
// 1. Buffer events until depth == 0 (complete node)
// 2. Convert buffer → node
// 3. Apply patch
// 4. Convert patched node → events
// 5. Emit patched events
```

**Pros:**
- Flexible: can handle both event-level and node-level patches
- Efficient: only buffers when needed
- Can optimize: simple patches don't need buffering

**Cons:**
- Complex: multiple code paths
- Need to detect when buffering is needed

---

## Analysis: Comparing Options

### Option 1: Event-Level Streaming
**Key Question**: Can patches be applied at event granularity?
- **Challenge**: Patches operate on complete nodes, not individual events
- **Challenge**: Need to detect patch boundaries during streaming (complex)
- **Challenge**: Nested patches require tracking multiple patch contexts
- **Verdict**: **RULED OUT** - Patches fundamentally require complete nodes

### Option 2: Node-Buffering Streaming
**Key Question**: Can we buffer until complete nodes, then apply patches?
- **Advantage**: Reuses existing `mergeop` logic (proven correctness)
- **Advantage**: Patches operate on complete nodes (matches patch semantics)
- **Advantage**: Clear separation: buffer → patch → emit
- **Challenge**: Requires buffering (memory overhead, but bounded by container size)
- **Challenge**: Need to detect container boundaries (when depth == 0)
- **Verdict**: **PROMISING** - Aligns with patch semantics, but needs analysis of buffering strategy

### Option 3: Pre-Converted Patch Events
**Key Question**: Can we convert patches to events upfront and merge at event level?
- **Advantage**: Simple: patches are already events
- **Advantage**: Fast: O(1) lookup by kpath
- **Challenge**: Memory: all patch events in memory
- **Challenge**: Complex merge logic: handle insertions/deletions at event level
- **Challenge**: Need to track "inside patch" vs "outside patch" state
- **Verdict**: **NEEDS ANALYSIS** - Simpler conceptually, but merge logic complexity unclear

### Option 4: Hybrid: StreamOp with Node Conversion
**Key Question**: Can we optimize by only buffering when needed?
- **Advantage**: Flexible: can handle both event-level and node-level patches
- **Advantage**: Efficient: only buffers when needed
- **Challenge**: Complex: multiple code paths
- **Challenge**: Need to detect when buffering is needed (same as Option 2)
- **Verdict**: **DEFERRED** - Optimization over Option 2, but adds complexity

---

## Key Design Questions

### 1. Patch Applicability Check
- **Clarification**: A patch is an `ir.Node` tree structure, not a single path
- A patch like `{ "users": { "123": { "name": "Alice" } } }` affects multiple paths:
  - Root level (`""`): has `"users"` field
  - `"users"` level: has `"123"` field  
  - `"users.123"` level: has `"name"` field
  - `"users.123.name"` level: leaf value
- **Question**: How do we check if a patch applies to the current path?
- **Answer**: Use `patch.GetKPath(currentPath)` - returns sub-patch if path exists, `nil` otherwise
- No need to "extract" paths from patches - patches are queried at paths

### 2. Patch Scope
- **Question**: When does a patch "apply" to a path?
- **Answer**: When `patch.GetKPath(currentPath)` returns non-nil
- This means the patch structure contains a sub-tree at that path
- The sub-patch returned is what gets applied to the document node at that path

### 3. Nested Patches
- What if patch modifies a nested structure?
- Do we need to re-buffer after applying a patch?
- Or: patches are always at leaf level?

### 4. Multiple Patches
- What if multiple patches apply to same path?
- Apply sequentially? Merge first?
- Need: patch ordering/collapsing logic

### 5. Performance
- Is buffering acceptable? (one node at a time)
- Can we optimize common cases? (e.g., simple value patches don't need buffering)

---

## Open Questions to Resolve

### 1. Patch Applicability Check (RESOLVED)
- **Answer**: Use `patch.GetKPath(currentPath)` to check if patch applies
- **Clarification**: Patches are tree structures (`ir.Node`), not single paths
- A patch affects all paths where `patch.GetKPath(path)` returns non-nil
- **Example**: 
  - Patch: `{ "users": { "123": { "name": "Alice" } } }`
  - At path `"users"`: `patch.GetKPath("users")` → `{ "123": { "name": "Alice" } }` (non-nil, applies)
  - At path `"users.123"`: `patch.GetKPath("users.123")` → `{ "name": "Alice" }` (non-nil, applies)
  - At path `"other"`: `patch.GetKPath("other")` → `nil` (doesn't apply)
- **No pre-extraction needed**: Query patches lazily during streaming

### 2. Patch Application Scope (RESOLVED)
- **Answer**: Patches apply at the exact path where `GetKPath` returns non-nil
- The returned sub-patch is what gets merged into the document node at that path
- **Recursive application**: `Patch()` handles nested merging automatically
- **Example**: 
  - Current path: `"users.123"`, document node: `{ "age": 30 }`
  - Sub-patch: `patch.GetKPath("users.123")` → `{ "name": "Alice" }`
  - Apply: `Patch({ "age": 30 }, { "name": "Alice" })` → `{ "age": 30, "name": "Alice" }`
  - The patch at `"users"` level will also apply recursively when patching the `users` container

### 3. Buffering Boundaries
- **Question**: What are the boundaries for buffering events?
- **Analysis Needed**:
  - Buffer from container start (`BeginObject`/`BeginArray`)?
  - Buffer only when patch applies (from detection point)?
  - How do nested patches affect buffering boundaries?

### 4. Multiple Patches
- **Question**: How do we handle multiple patches that apply to the same path?
- **Analysis Needed**:
  - Apply sequentially using `Patch`?
  - Merge patches first, then apply?
  - What is the correct ordering?

### 5. Performance Trade-offs
- **Question**: Is buffering acceptable for our use case?
- **Analysis Needed**:
  - What is the typical container size?
  - Can we optimize common cases (e.g., simple value patches)?
  - What are the memory implications?

## Decision Framework

Before implementing, we need to:
1. **Understand patch semantics**: How do patches apply recursively? (See `patch_stream_design_analysis.md`) ✅
2. **Determine detection point**: When can we detect patch applicability? (At container start? End?) ✅ (At container start)
3. **Clarify buffering strategy**: What do we buffer and when? ❌ (CRITICAL - see buffering boundaries question)
4. **Resolve path matching**: How do we match current path with patch paths? ✅ (Use `patch.GetKPath(currentPath)`)
5. **Handle multiple patches**: What is the correct application order? ⏸️ (Deferred per user request)

Once these are resolved, we can arrive at a single clear objective for implementation.
