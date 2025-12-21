# Streaming Patch Processor - Integration Design

## Overview

The streaming patch processor integrates multiple packages to apply patches while streaming. This document maps the integration points, responsibilities, and data flows.

## Component Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                    storage.ReadStateAt()                    │
│                  (Entry point, orchestrator)                │
└────────────┬────────────────────────────────────────────────┘
             │
             ├─► findSnapshotBaseReader() → EventReader
             │   (snap package integration)
             │
             ├─► index.LookupRange() → []LogSegment
             │   (index package integration)
             │
             ├─► dlog.ReadEntryAt() → Entry with Patch
             │   (dlog package integration)
             │
             └─► StreamingProcessor.Process()
                 │
                 ├─► stream.State (path tracking)
                 ├─► PatchIndex (which commits affect path)
                 ├─► IR tree navigation (extract subtrees)
                 ├─► tony.Patch (apply patches)
                 └─► stream.EventWriter (output)
```

## Package Responsibilities

### `storage` (Orchestrator)
**Owns:** High-level read/write operations, transaction coordination

**Responsibilities:**
- Find snapshot base (delegate to `findSnapshotBaseReader`)
- Query index for affected patches (delegate to `index.LookupRange`)
- Load patch entries from dlog (delegate to `dlog.ReadEntryAt`)
- Build `PatchIndex` from entries
- Invoke `StreamingProcessor`
- Strip internal tags before returning

**Interfaces Used:**
- `patches.EventReadCloser` (from snap)
- `index.LogSegment` (from index)
- `dlog.Entry` (from dlog)
- `stream.EventWriter` (to output)

### `stream` (Event Streaming)
**Owns:** Event serialization, path tracking

**Responsibilities:**
- Track current kinded path during streaming (`State.CurrentPath()`)
- Process events to update path state (`State.ProcessEvent()`)
- Convert IR nodes ↔ events (`NodeToEvents`, `EventsToNode`)
- Read/write binary event format

**Key Type:** `stream.State`
```go
type State struct {
    // Internal: bracket stack, array indices, current path
}

func (s *State) CurrentPath() string        // Get kinded path
func (s *State) ProcessEvent(e Event)       // Update state
```

**Critical Questions:**
- Does `ProcessEvent` update state before or after the event is "at" a position?
- Is State cloneable/restorable for lookahead?
- What happens to State when we skip events?

### `ir` (IR Tree Navigation)
**Owns:** IR node structure, tree relationships

**Responsibilities:**
- Provide Parent pointers for walking up tree
- Support path-based navigation (`GetKPath`, if exists)
- Tag storage and manipulation

**Key Type:** `ir.Node`
```go
type Node struct {
    Parent *Node
    Tag    string
    // ... fields, values, type
}
```

**Critical Questions:**
- Are Parent pointers always set after `tony.Patch`?
- Are Parent pointers set after `MergePatches`?
- Are Parent pointers preserved during clone?
- How do we get kinded path from a node? (walk Parents)

### `tony` (Patch Application)
**Owns:** Merge-patch semantics

**Responsibilities:**
- Apply patch to base node (`tony.Patch(base, patch)`)
- Handle `!delete`, `!arraydiff`, other mergeops
- Merge logic for nested structures

**Key Function:** `tony.Patch(base, patch *ir.Node) *ir.Node`

**Critical Questions:**
- Does it mutate base or return new node?
- What happens with `tony.Patch(nil, patch)`? (creates new)
- What happens with `tony.Patch(base, !delete)`? (returns nil)
- Are Parent pointers set in returned tree?

### `dlog` (Log Entry Storage)
**Owns:** Persistent log files, entry serialization

**Responsibilities:**
- Read entries by log file + position
- Store full patch tree from root in Entry

**Key Type:** `dlog.Entry`
```go
type Entry struct {
    Commit    int64
    TxID      int64
    Patch     *ir.Node  // Full tree from root
    Timestamp string
    // ...
}
```

**Interface:** Readonly for streaming processor

### `index` (Patch Location Index)
**Owns:** Path → LogSegment mapping

**Responsibilities:**
- Find segments affecting a path and commit range
- Return all ancestors of a path (hierarchical index)

**Key Function:** `index.LookupRange(path string, from, to *int64) []LogSegment`

**Returns:** Segments where path or descendants are affected

### `snap` (Snapshot Storage)
**Owns:** Event-based snapshot format

**Responsibilities:**
- Provide event stream reader for snapshot
- Path-based seeking within snapshot

**Key Type:** `snap.Snapshot`
```go
type Snapshot struct {
    R         io.ReadSeekCloser
    Index     *Index
    EventSize uint64
}

func (s *Snapshot) ReadPath(path string) (*ir.Node, error)
```

**Integration:** Returns EventReader via wrapper in `snap_storage.go`

## Data Flow

### Phase 1: Setup (in `ReadStateAt`)

```
1. findSnapshotBaseReader(kPath, commit)
   │
   ├─► index.IterAtPath(kPath)
   ├─► Find snapshot segment (StartCommit == EndCommit)
   ├─► dlog.OpenReaderAt(logFile, snapPos)
   ├─► snap.Open(reader) → Snapshot
   ├─► snapshot.ReadPath(kPath) → ir.Node
   └─► stream.NodeToEvents(node) → []Event
       └─► Return as EventReader (sliceEventReader)

2. index.LookupRange(kPath, &startCommit, &commit)
   └─► Return []LogSegment (patches affecting kPath)

3. For each segment:
   dlog.ReadEntryAt(logFile, position) → Entry
   └─► Collect Entry.Patch nodes

4. Build PatchIndex
   └─► Walk each patch tree, find !logd-patch-root tags
```

### Phase 2: Streaming (in `StreamingProcessor.Process`)

```
Loop over source events:
  │
  ├─► state.CurrentPath() → "users{42}.status"
  │
  ├─► patchIndex.lookup("users{42}.status")
  │   └─► Returns [] (no patches) OR [commit1, commit2, ...]
  │
  ├─── NO PATCHES ─────────────────────────────────┐
  │    │                                           │
  │    ├─► sink.WriteEvent(event)                 │
  │    └─► state.ProcessEvent(event)              │
  │                                                │
  └─── HAS PATCHES ────────────────────────────────┤
       │                                           │
       ├─► collectEventsForSubtree()              │
       │   └─► Track depth, collect until exit    │
       │                                           │
       ├─► stream.EventsToNode(events) → base     │
       │                                           │
       ├─► For each patch (in commit order):      │
       │   ├─► navigateTo(entry.Patch, path)      │
       │   │   └─► Extract subtree at path        │
       │   └─► result = tony.Patch(result, subtree)
       │                                           │
       ├─► stream.NodeToEvents(result) → events   │
       │                                           │
       └─► For each event:                        │
           ├─► sink.WriteEvent(event)             │
           └─► state.ProcessEvent(event)          │
```

### Phase 3: Cleanup (in `ReadStateAt`)

```
stream.EventsToNode(all output events) → final node
stripInternalTags(final node)
return final node
```

## Critical Integration Points

### 1. Event Collection for Subtree

**Challenge:** Know when a subtree is complete

```go
func collectEventsForSubtree(
    reader EventReader,
    state *stream.State,
    rootPath string,
) ([]Event, error) {
    var events []Event
    depth := 0

    for {
        event, err := reader.ReadEvent()
        if err != nil {
            return nil, err
        }

        // Track container depth
        if event.Type == BeginObject || event.Type == BeginArray {
            depth++
        }
        if event.Type == EndObject || event.Type == EndArray {
            depth--
        }

        events = append(events, event)
        state.ProcessEvent(event)

        // Exit when back to original depth
        if depth == 0 {
            // Check: are we still under rootPath?
            if !strings.HasPrefix(state.CurrentPath(), rootPath) {
                break
            }
        }
    }

    return events, nil
}
```

**Questions:**
- When is depth == 0 the right condition?
- What about leaf values (no Begin/End)?
- Does state.CurrentPath() update before or after ProcessEvent?

### 2. IR Tree Navigation to Path

**Challenge:** Extract subtree at kinded path from full tree

```go
func navigateTo(root *ir.Node, kPath string) (*ir.Node, error) {
    if kPath == "" {
        return root, nil
    }

    parsed, err := kpath.Parse(kPath)
    if err != nil {
        return nil, err
    }

    // Use existing ir.Node.GetKPath if available
    return root.GetKPath(kPath)

    // OR manual walk:
    // current := root
    // for seg := parsed; seg != nil; seg = seg.Next {
    //     switch {
    //     case seg.Field != nil:
    //         current = findField(current, *seg.Field)
    //     case seg.Index != nil:
    //         current = findIndex(current, *seg.Index)
    //     // ... etc
    //     }
    // }
    // return current, nil
}
```

**Questions:**
- Does `ir.Node.GetKPath` exist?
- If not, do we need to implement full navigation?
- What about error handling for "path not found"?

### 3. Getting Path from IR Node

**Challenge:** Know kinded path of a node (for patch index building)

```go
func getPathFromNode(node *ir.Node) string {
    // Walk up Parent pointers
    var segments []string

    current := node
    for current.Parent != nil {
        parent := current.Parent

        // Find which field/index we are in parent
        seg := findSegmentInParent(current, parent)
        segments = append([]string{seg}, segments...)

        current = parent
    }

    return strings.Join(segments, "")
}

func findSegmentInParent(child, parent *ir.Node) string {
    // Search parent.Values for child
    for i, val := range parent.Values {
        if val == child {
            // Is this an object field, array index, sparse index?
            if parent.Type == ir.ObjectType {
                return "." + parent.Fields[i].String
            }
            if parent.Type == ir.ArrayType {
                return fmt.Sprintf("[%d]", i)
            }
            // ... sparse array
        }
    }
    panic("child not found in parent")
}
```

**Questions:**
- Are Parent pointers reliably set?
- How do we distinguish array vs sparse array?
- What if node is root (no parent)?

### 4. State Management After Skipping Events

**Challenge:** Keep stream.State in sync when we don't emit original events

Two approaches:

**Option A: Replay Events Through State (without emitting)**
```go
// Collect events (advances reader)
events := collectEventsForSubtree(reader, state, path)

// Convert, patch, re-emit
base := stream.EventsToNode(events)
result := applyPatches(base, patches)
newEvents := stream.NodeToEvents(result)

for _, evt := range newEvents {
    sink.WriteEvent(evt)
    state.ProcessEvent(evt)  // Keep state in sync
}
```

State tracks the patched events, not original.

**Option B: Parallel State Tracking**
Keep two states:
- `sourceState`: tracks position in source stream
- `outputState`: tracks position in output stream

Only output state matters for patch root detection.

**Likely Choice:** Option A (single state tracking output)

### 5. Parent Pointer Assumptions

**Parent pointers should be reliable** - Scott has been careful about this:

**Confirmed OK:**
- ✅ `stream.EventsToNode` - sets Parent in `addNodeToParent` (stream/conversion.go:19-21, 26-27)
- ✅ `ir.Clone` - copies and sets Parent pointers (ir/node.go:41-43, 51-53, 59-61)
- ✅ `ir.FromMap/FromIntKeysMap` - sets Parent pointers (ir/node.go:152-158, 183-190)
- ✅ `tony.Patch` - confirmed Parent pointers are correct

**Should verify during implementation:**
- ⚠️ `tx.MergePatches` output - uses FromMap/FromIntKeysMap, should be okay but verify

**If Parent pointers are missing:**
- DO NOT add workaround helpers
- FIX THE SOURCE - find which function is not setting Parent and fix it there
- Parent pointer discipline is critical for the codebase, don't paper over bugs

## Error Handling Strategy

### Fail Fast vs Graceful Degradation

**Fail Fast Errors (return error):**
- Cannot find snapshot
- Cannot read dlog entry
- Cannot parse kinded path
- tony.Patch returns error (if it can)

**Graceful Degradation:**
- Patch root tag missing → treat as unpatch, stream through
- Parent pointer missing → log warning, skip optimization
- Path navigation fails → log warning, apply patch to whole subtree

### Error Propagation

```
StreamingProcessor.Process
  └─► returns error
      ↓
ReadStateAt
  └─► wraps with context, returns error
      ↓
User code
  └─► handles error
```

## Testing Strategy

### Unit Tests (per component)

1. **PatchIndex building**
   - Walk trees with !logd-patch-root tags
   - Verify path extraction
   - Test dominated root filtering

2. **Event collection**
   - Simple objects: `{a: 1, b: 2}`
   - Nested: `{a: {b: {c: 1}}}`
   - Arrays: `[1, 2, 3]`
   - Mixed: `{users: [{id: 1}, {id: 2}]}`
   - Edge: empty objects/arrays

3. **IR navigation**
   - Navigate to various paths
   - Handle missing paths
   - Verify Parent pointers

### Integration Tests (cross-component)

1. **Simple patch application**
   - Snapshot: `{a: 1, b: 2}`
   - Patch at `a`: `10`
   - Expected: `{a: 10, b: 2}`

2. **Nested patch**
   - Snapshot: `{users: [{id: 1, name: "alice"}]}`
   - Patch at `users[0].name`: `"bob"`
   - Expected: `{users: [{id: 1, name: "bob"}]}`

3. **Delete then recreate**
   - Snapshot: `{a: {b: 1}}`
   - Patch 1 at `a`: `!delete`
   - Patch 2 at `a.c`: `2`
   - Expected: `{a: {c: 2}}`

4. **Multiple disjoint patches**
   - Snapshot: `{a: 1, b: 2, c: 3}`
   - Patch at `a`: `10`
   - Patch at `c`: `30`
   - Expected: `{a: 10, b: 2, c: 30}`

5. **Large streaming (performance)**
   - Snapshot: 1000 element array
   - Patch element 500
   - Verify memory ≠ O(1000)

## Implementation Phases

### Phase 1: Infrastructure
- [ ] Add `!logd-patch-root` tag constant
- [ ] Implement tag stripping helper
- [ ] Verify/fix Parent pointer handling in existing code
- [ ] Add IR navigation helpers (`navigateTo`, `getPathFromNode`)

### Phase 2: Patch Index
- [ ] Implement PatchIndex type
- [ ] Build index from tagged patches
- [ ] Test dominated root filtering
- [ ] Test path lookup

### Phase 3: Event Collection
- [ ] Implement `collectEventsForSubtree`
- [ ] Test depth tracking
- [ ] Test state synchronization
- [ ] Handle edge cases (empty, null)

### Phase 4: Streaming Processor
- [ ] Implement StreamingProcessor type
- [ ] Integrate with PatchIndex
- [ ] Apply patches sequentially
- [ ] Emit patched events

### Phase 5: Integration
- [ ] Tag patches at commit time
- [ ] Use StreamingProcessor in ReadStateAt
- [ ] Update tests
- [ ] Performance validation

## Open Questions

1. **Does `ir.Node.GetKPath(path string)` exist?**
   - If yes: use it
   - If no: implement navigation

2. **Are Parent pointers reliable?**
   - Test tony.Patch output
   - Test EventsToNode output
   - Add fixing if needed

3. **When does stream.State.CurrentPath() update?**
   - Before ProcessEvent?
   - After ProcessEvent?
   - Document and test

4. **Do we need lookahead for patch root detection?**
   - Or is CurrentPath() sufficient?
   - What about events that span multiple paths?

5. **How does tony.Patch handle errors?**
   - Return error?
   - Panic?
   - Return nil?

## Success Criteria

1. **Correctness:** All existing snapshot tests pass with new processor
2. **Memory:** Reading 1M array with 1 patch uses O(1) memory, not O(1M)
3. **Performance:** Similar or better than InMemoryApplier for small docs
4. **Tag hygiene:** No `!logd-patch-root` tags in user-visible results

## Debugging Checklist

When tests fail:
- [ ] Check stream.State.CurrentPath() value
- [ ] Verify Parent pointers are set
- [ ] Check depth tracking in event collection
- [ ] Verify tag was inserted at commit
- [ ] Check patch index contains expected commits
- [ ] Verify navigateTo returns correct subtree
- [ ] Check tony.Patch semantics (mutate vs return)
- [ ] Verify state sync after skipped events
- [ ] Check for off-by-one in path comparisons
