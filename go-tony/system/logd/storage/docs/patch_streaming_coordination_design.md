# Coordinating Simple Patch Paths with Streaming Events

## Goal

Design a system that:
1. Applies a single simple patch (`!insert`, `!delete`, `!replace`) to streaming events
2. Coordinates patch paths with streaming paths
3. Can be repeated efficiently for subsequent patches
4. **Explicit constraint**: Does NOT load containers into memory - streaming is horizontal within containers as well

## Key Concepts

### Simple Patch Structure

A simple patch is an `ir.Node` tree where operations are tagged with `!insert`, `!delete`, `!replace`:

```
Patch: { "users": { "123": !replace { "to": { "name": "Alice" } } } }
```

**Syntax notes**:
- `!replace` operation replaces the entire value at the path
- `to:` field contains the replacement value
- `from:` field is omitted (matches all/null - no validation)
- Operations apply at container boundaries (field/element level)

This patch affects paths:
- `"users"`: Container path (has sub-patch)
- `"users.123"`: Field path (has `!replace` operation - replaces entire value)

### Streaming Context

During event streaming:
- Events: `BeginObject`, `Key("users")`, `BeginObject`, `Key("123")`, `Key("name")`, `String("Bob")`, `EndObject`, `EndObject`
- Path tracking: `""` → `"users"` → `"users.123"` → `"users.123.name"`
- State: `State.CurrentPath()` gives us current path at any point

### Coordination Challenge

**Problem**: Patch operations (`!insert`, `!delete`, `!replace`) need to be applied at specific paths, but:
- We're streaming events (incremental)
- **Constraint**: Cannot buffer containers into memory (streaming must be horizontal)
- Operations need to know what to insert/delete/replace
- **Critical**: Insertion paths don't exist in source document - must detect from patch structure

**Key Insight**: Simple operations can be applied incrementally to events:
- `!delete`: Skip events for deleted field/element (path exists in source)
- `!replace`: Skip original value events, emit replacement events (path exists in source)
- `!insert`: Emit insert events at insertion point (**path doesn't exist in source** - detect from patch)

## Design: Incremental Event Stream Processor (No Container Buffering)

### Core Idea

Create a processor that:
1. **Tracks current path** during event streaming (via `State`)
2. **Checks patch applicability** as we stream (at field/element boundaries)
3. **Applies operations incrementally** when reaching affected paths
4. **Emits modified events** (skipping deleted, inserting new, replacing values)
5. **Never buffers containers** - operations applied horizontally as we stream

### Data Structures

```go
type PatchProcessor struct {
    patch *ir.Node           // The patch to apply
    state *stream.State      // Tracks current path during streaming
    output []stream.Event    // Events to emit (or stream to writer)
}

// PatchPath tracks patch operations at specific paths
type PatchPath struct {
    path      string         // kpath where operation applies
    operation string         // "!insert", "!delete", "!replace"
    value     *ir.Node       // Value for insert/replace (nil for delete)
    parent    *PatchPath     // Parent path (for nested patches)
}
```

### Algorithm: Single Patch Application

#### Phase 1: Extract Patch Operations

**Key Insight**: Mergeops (`!insert`, `!delete`, `!replace`) operate on complete nodes, not events.

**Process**:
- Traverse patch tree recursively
- At each node, check if it has a tag (`!insert`, `!delete`, `!replace`)
- Record path → sub-patch mapping (not just operation, but the full sub-patch node)
- Use `patch.GetKPath(path)` to get sub-patch for any path

**Example**:
```go
patch := { "users": { "123": { "name": "!replace", "from": "Bob", "to": "Alice" } } }

// Check at path "users"
subPatch := patch.GetKPath("users")  // Returns: { "123": { "name": "!replace", ... } }

// Check at path "users.123"
subPatch := patch.GetKPath("users.123")  // Returns: { "name": "!replace", ... }

// Check at path "users.123.name"
subPatch := patch.GetKPath("users.123.name")  // Returns: nil (no direct operation here)
```

**Note**: Operations are at container boundaries, not leaf paths. The `!replace` operation applies to the `"name"` field, which is a container (object field).

#### Phase 2: Incremental Operation Application (No Buffering)

**Key Constraint**: Cannot buffer containers - must apply operations incrementally to events.

**Algorithm**:
```go
func (p *PatchProcessor) ProcessEvent(event *stream.Event) error {
    // Update state (track path)
    p.state.ProcessEvent(event)
    currentPath := p.state.CurrentPath()
    
    // Check if patch applies at current path (for delete/replace - paths exist in source)
    subPatch := p.patch.GetKPath(currentPath)
    
    if subPatch != nil {
        // Patch applies - check operation type
        if subPatch.Tag == "!delete" {
            return p.handleDelete(event, currentPath)
        } else if subPatch.Tag == "!replace" {
            return p.handleReplace(event, currentPath, subPatch)
        }
        // Note: !insert not checked here - insertion paths don't exist in source
    }
    
    // Check for insertions: look at parent path to see if insertions should happen
    // Insertions are detected when we're in a container that has insert operations
    if event.Type == EventEndObject || event.Type == EventEndArray {
        // Container ending - check if there are insertions for this container
        parentPath := p.getParentPath(currentPath)
        parentPatch := p.patch.GetKPath(parentPath)
        if parentPatch != nil {
            // Check for insertions in this container
            return p.checkAndHandleInsertions(parentPatch, currentPath)
        }
    }
    
    // Nested patch (has sub-patches) - continue streaming, check at deeper paths
    p.output = append(p.output, *event)
    return nil
}
```

#### Phase 3: Handle Operations Incrementally

**`!delete`**: Skip events for deleted field/element
- **Detection**: 
  - **Objects**: When we see `Key("field")` at path that exists in source
  - **Arrays**: When we reach source array index that matches deletion index
- **Action**: 
  - **Objects**: Skip all events until we've passed the deleted value (track depth)
  - **Arrays**: Skip element at source index, subsequent elements shift left
- **Result**: Don't emit any events for the deleted field/element

**`!replace`**: Replace value with patch value
- **Detection**: When we see `Key("field")` or array element at path that exists in source
- **Action**: Skip original value events (until value complete)
- **Result**: Emit replacement events: `NodeToEvents(patchOp.to)`

**`!insert`**: Insert new value at position (maintains sorted order)
- **Detection**: **Path doesn't exist in source** - detect from patch structure
  - Track fields/elements seen in source as we stream
  - Compare with patch structure to find insertions
  - Insertions are fields/elements in patch but not in source
- **Action**: Emit insertion events at correct sorted position
  - For objects: Insert before first field that comes after insertion key (alphabetically)
  - For arrays: Insert at correct index position
  - Maintains sorted order just like `tony.Patch` applied to a diff
- **Result**: Inserted fields/elements appear in output at correct sorted position

**Insertion Detection Example** (sorted order):
```
Source: { "users": { "123": { "age": 30, "zip": "12345" } } }
Patch: { "users": { "123": { "age": 30, "name": !insert { "to": "Alice" }, "zip": "12345" } } }

When processing "users.123" container:
- Source fields seen: "age", "zip"
- Patch fields: "age", "name" (with !insert), "zip"
- Insertion: "name" is in patch but not in source
- Sorted order: "age" < "name" < "zip"
- Emit insertion after "age" but before "zip" (when we see "zip" key)
```

**Insertion Timing**:
- As we stream through container, when we encounter a field/element that comes after insertion point (in sorted order), emit insertion first
- Then continue with current field/element
- This maintains sorted order without buffering

### Incremental Operation Application

**Key Insight**: Operations apply at field/element boundaries, can be applied incrementally to events.

**Example**:
```
Patch: { "users": { "123": !replace { "to": { "name": "Alice" } } } }
Events: BeginObject, Key("users"), BeginObject, Key("123"), BeginObject, Key("name"), String("Bob"), EndObject, EndObject, EndObject
```

**Process** (horizontal streaming, no buffering):
1. `BeginObject` (path="") → emit
2. `Key("users")` (path="users") → check patch: `patch.GetKPath("users")` → `{ "123": !replace ... }` → nested patch, emit
3. `BeginObject` (path="users") → emit
4. `Key("123")` (path="users.123") → check patch: `patch.GetKPath("users.123")` → `!replace { "to": ... }` → **operation detected**
5. **Handle `!replace`**:
   - Skip original value events (track depth, skip until value complete)
   - Emit replacement events: `BeginObject`, `Key("name")`, `String("Alice")`, `EndObject`
6. Continue with remaining events

**Insertion Example - Object** (maintaining sorted order):
```
Source: { "users": { "123": { "age": 30, "zip": "12345" } } }
Patch: { "users": { "123": { "age": 30, "name": !insert { "to": "Alice" }, "zip": "12345" } } }

Process (sorted: "age" < "name" < "zip"):
1. `BeginObject` (path="") → emit
2. `Key("users")` → emit
3. `BeginObject` (path="users") → emit
4. `Key("123")` → emit
5. `BeginObject` (path="users.123") → emit
6. `Key("age")` → emit (source field, "age" < "name")
7. `Int(30)` → emit
8. `Key("zip")` → **check insertions before emitting**:
   - "zip" > "name" (insertion key)
   - Emit insertion first: `Key("name")`, `String("Alice")`
   - Then emit: `Key("zip")`
9. `String("12345")` → emit
10. `EndObject` (path="users.123") → check for any remaining insertions
11. `EndObject` → emit
12. `EndObject` → emit
```

**Insertion Example - Array** (maintaining correct indices):
```
Source: [1, 2, 4]  (source indices: 0=1, 1=2, 2=4)
Patch: !arraydiff
  2: !insert 3

Process (insert 3 at source index 2, shift original index 2 to output index 3):
1. `BeginArray` (path="") → emit
2. Source index 0: `Int(1)` → emit → output index 0
3. Source index 1: `Int(2)` → emit → output index 1
4. Source index 2: **check insertions before emitting**:
   - Current source index (2) == insertion index (2)
   - Emit insertion first: `Int(3)` → output index 2
   - Original element at source index 2 shifts to output index 3
5. Source index 2 (original): `Int(4)` → emit → output index 3 (shifted)
6. `EndArray` → emit

Result: [1, 2, 3, 4]  (output indices: 0=1, 1=2, 2=3, 3=4) ✅
```

**Delete Example - Array** (maintaining correct indices):
```
Source: [1, 2, 3, 4]  (source indices: 0=1, 1=2, 2=3, 3=4)
Patch: !arraydiff
  2: !delete 3

Process (delete at source index 2, shift subsequent elements left):
1. `BeginArray` (path="") → emit
2. Source index 0: `Int(1)` → emit → output index 0
3. Source index 1: `Int(2)` → emit → output index 1
4. Source index 2: **check deletions**:
   - Current source index (2) == deletion index (2)
   - Skip this element (don't emit)
   - Subsequent elements shift left (source index 3 becomes output index 2)
5. Source index 3: `Int(4)` → emit → output index 2 (shifted left)
6. `EndArray` → emit

Result: [1, 2, 4]  (output indices: 0=1, 1=2, 2=4) ✅
```

**Key Points**:
- **Array insertions**: When we insert at source index `i`, original element at `i` shifts to output index `i+1`
- **Array deletions**: When we delete at source index `i`, subsequent elements shift left (source index `i+1` becomes output index `i`)
- Track source array index separately from output position

**Key Points**:
- Operations detected at field/element boundaries (`Key` event or array element)
- Value events skipped/replaced incrementally (no container buffering)
- Replacement events emitted immediately
- Streaming continues horizontally through container

### Design: Incremental Operation Application (NO CONTAINER BUFFERING)

**Key Constraint**: Cannot buffer containers - must stream horizontally and apply operations incrementally.

**Algorithm**:
1. Stream events, tracking path via `State`
2. At field/element boundaries (`Key` event or array element):
   - Check if patch applies: `subPatch := patch.GetKPath(currentPath)`
   - If operation detected (`!delete`, `!replace`, `!insert`):
     - Apply operation incrementally (skip/replace/insert events)
   - If nested patch (has sub-patches):
     - Continue streaming, check at deeper paths
3. Operations applied immediately:
   - `!delete`: Skip value events until value complete
   - `!replace`: Skip original value events, emit replacement events
   - `!insert`: Emit insert events at insertion point

**Memory**: No container buffering - only tracks current path and operation state

**Nested Patches**: Checked incrementally as we stream deeper into structure

### Repeatability for Subsequent Patches

**Process for Multiple Patches**:

**Option A: Sequential Streaming**
1. Apply first patch → get result events
2. Stream result events through second patch processor
3. Apply second patch → get result events
4. Repeat for subsequent patches

**Efficiency**: 
- Events → buffer → node → patch → events → buffer → node → patch → events
- Multiple round-trips: `EventsToNode` and `NodeToEvents` for each patch
- But: Only buffers containers that need patching (memory efficient)

**Option B: Batch Application**
1. Convert events → node once (`EventsToNode`)
2. Apply all patches sequentially to node (`Patch(node, patch1)`, `Patch(result, patch2)`, ...)
3. Convert final node → events once (`NodeToEvents`)

**Efficiency**:
- Events → node → patch → patch → patch → events
- Single round-trip: `EventsToNode` once, `NodeToEvents` once
- But: Requires full document in memory (if patches affect root)

**Hybrid Approach** (Recommended):
- For containers with patches: Use Option A (streaming, one container at a time)
- For root-level patches: Use Option B (batch, but only if root needs patching)
- **Key**: Only buffer what needs patching, apply patches incrementally

## Proposed Design: Incremental Operation Application with Sequential Processing

### For Single Patch

**Incremental operation application** (NO container buffering):
- Stream events, track path via `State`
- At field/element boundaries: check `patch.GetKPath(currentPath)`
- If operation detected (`!delete`, `!replace`, `!insert`):
  - Apply operation incrementally to events
  - Skip/replace/insert events as needed
- If nested patch: continue streaming, check at deeper paths

**Memory**: No container buffering - only operation state

### For Multiple Patches

**Sequential application**:
1. Apply first patch incrementally → result events
2. Stream result events through second patch processor
3. Apply second patch incrementally (same logic)
4. Repeat for subsequent patches

**Why Sequential**:
- Consistent: Same incremental logic for all patches
- Memory efficient: No container buffering ever
- Simple: Same process for all patches
- Efficient: Operations applied directly to events, no node conversion needed

**Optimization**: Can skip `NodeToEvents` between patches if we keep result as node
- But: Need to track which containers were patched
- Complexity: May not be worth it

## Implementation Details

### Operation State (No Container Buffering)

```go
type PatchProcessor struct {
    patch *ir.Node
    state *stream.State
    
    // Operation state (for current field/element being processed)
    skipping bool          // Skipping events for !delete or !replace
    skipDepth int          // Depth when skipping started
    replacing bool         // Replacing value
    replacement []stream.Event  // Replacement events to emit
    
    // Insertion tracking: track which fields/elements we've seen in source
    containerFields map[string]bool  // Fields seen in current container (for objects)
    containerElements int            // Elements seen in current container (for arrays)
    
    // Pending insertions: fields to insert (for objects), sorted by key
    pendingInsertions []Insertion  // Sorted insertions for current object container
    
    // Array operations: operations indexed by source array index (for arrays)
    arrayOperations map[int]ArrayOperation  // Source index → operation
    
    // Output
    output []stream.Event  // Or: stream to writer
}

type Insertion struct {
    key     string         // Field name (for objects)
    events  []stream.Event // Events to emit for insertion
}

type ArrayOperation struct {
    opType  string         // "!insert", "!delete", "!replace"
    value   *ir.Node       // Value for insert/replace (nil for delete)
    from    *ir.Node       // From value for replace (nil for insert/delete)
    to      *ir.Node       // To value for replace (nil for insert/delete)
}

// Note: Array operations are indexed by SOURCE array index
// - Insertion at source i: original element at i shifts to output i+1
// - Deletion at source i: subsequent elements shift left (source i+1 → output i)
// - Replacement at source i: same output index (no shifting)
```

### Detection and Application Logic

```go
func (p *PatchProcessor) ProcessEvent(event *stream.Event) error {
    // Update state (track path)
    p.state.ProcessEvent(event)
    currentPath := p.state.CurrentPath()
    
    // Check for insertions before processing current event (maintain sorted order)
    if event.Type == EventKey {
        // For objects: check if we need to emit insertions before this key
        if err := p.emitPendingInsertionsBeforeKey(event.Key); err != nil {
            return err
        }
        p.containerFields[event.Key] = true
    } else if p.state.IsInArray() && (event.Type == EventBeginObject || event.Type == EventBeginArray || 
              event.Type == EventString || event.Type == EventInt || event.Type == EventFloat || 
              event.Type == EventBool || event.Type == EventNull) {
        // For arrays: check if we need to emit insertions before this element
        // containerElements is the current index in source array
        if err := p.emitPendingInsertionsBeforeIndex(p.containerElements); err != nil {
            return err
        }
        // After potential insertions, increment source array index
        p.containerElements++
    }
    
    // Check if we're at a field/element boundary (for delete/replace - paths exist in source)
    if event.Type == EventKey {
        // Object field - check if patch applies at this path
        subPatch := p.patch.GetKPath(currentPath)
        
        if subPatch != nil {
            if subPatch.Tag == "!delete" {
                p.startSkipping()  // Skip value events
                return nil
            } else if subPatch.Tag == "!replace" {
                to := ir.Get(subPatch, "to")
                if to == nil {
                    return fmt.Errorf("!replace missing 'to:' at %s", currentPath)
                }
                replacementEvents, err := stream.NodeToEvents(to)
                if err != nil {
                    return err
                }
                p.startReplacing(replacementEvents)  // Skip original, emit replacement
                return nil
            }
            // Note: !insert not handled here - insertion paths don't exist in source
        }
    } else if p.state.IsInArray() && (event.Type == EventBeginObject || event.Type == EventBeginArray || 
              event.Type == EventString || event.Type == EventInt || event.Type == EventFloat || 
              event.Type == EventBool || event.Type == EventNull) {
        // Array element - check if patch applies at current source index
        // Note: For arrays, we need to check !arraydiff structure, not GetKPath
        if err := p.checkArrayOperation(p.containerElements); err != nil {
            return err
        }
    }
    
    // Initialize insertions when entering container
    if event.Type == EventBeginObject || event.Type == EventBeginArray {
        if err := p.initializeInsertions(currentPath); err != nil {
            return err
        }
    }
    
    // Emit remaining insertions when container ends (for insertions after last field/element)
    if event.Type == EventEndObject || event.Type == EventEndArray {
        return p.emitRemainingInsertions()
    }
    
    // Handle skipping/replacing state
    if p.skipping {
        // Check if we've passed the value (depth returned)
        if p.state.Depth() <= p.skipDepth {
            p.skipping = false
        }
        return nil  // Don't emit skipped events
    }
    
    if p.replacing {
        // Check if we've passed the original value
        if p.state.Depth() <= p.skipDepth {
            // Emit replacement events
            p.output = append(p.output, p.replacement...)
            p.replacing = false
            p.replacement = nil
        }
        return nil  // Don't emit original events
    }
    
    // Normal event - emit
    p.output = append(p.output, *event)
    return nil
}
```

## Implementation Details: Insertion Ordering

### Initializing Array Operations

When entering an array container (`BeginArray`):
1. Get sub-patch for container: `subPatch := patch.GetKPath(containerPath)`
2. Check if sub-patch has `!arraydiff` tag
3. Parse `!arraydiff` structure to extract operations by source index:
   - Format: `index: !insert value` or `index: !delete value` or `index: !replace { from: X, to: Y }`
   - Build map: `sourceIndex → operation`
4. Store operations indexed by source array index

**Array Operation Parsing**:
```
Patch: !arraydiff
  2: !insert 3
  4: !delete 5
  6: !replace
    from: 7
    to: 8

Extract:
- Source index 2: insertion (value 3)
- Source index 4: deletion (value 5)
- Source index 6: replacement (from 7, to 8)
```

### Initializing Object Insertions

When entering an object container (`BeginObject`):
1. Get sub-patch for container: `subPatch := patch.GetKPath(containerPath)`
2. Extract insertion operations from sub-patch:
   - Find fields with `!insert` tag that aren't in source
3. Sort insertions by key (alphabetically, matching `tony.Patch` behavior)
4. Store in `pendingInsertions` (sorted list)

### Emitting Insertions at Correct Position

**For Objects**:
- When we see a `Key` event, check if any pending insertions have keys < current key
- Emit those insertions first (maintains sorted order)
- Then process current key

**For Arrays**:
- Track current source array index (`containerElements`)
- Parse `!arraydiff` structure to get operations by source index
- When we see an array element at source index `i`:
  - **Insertion**: If insertion at index `i`, emit insertion first → output index `i`, then original element → output index `i+1`
  - **Deletion**: If deletion at index `i`, skip element (don't emit), subsequent elements shift left
  - **Replacement**: If replacement at index `i`, skip original, emit replacement → same output index `i`
- **Index shifting**: 
  - Insertion at `i`: original element at `i` → output index `i+1`
  - Deletion at `i`: subsequent elements shift left (source `i+1` → output `i`)

**Example Array Operations**:
```
Insertion:
Source: [1, 2, 4]  (source: 0, 1, 2)
Patch: insert 3 at source index 2
- Source 0: emit → output 0
- Source 1: emit → output 1
- Source 2: insert 3 → output 2, then emit 4 → output 3
Result: [1, 2, 3, 4] ✅

Deletion:
Source: [1, 2, 3, 4]  (source: 0, 1, 2, 3)
Patch: delete at source index 2
- Source 0: emit → output 0
- Source 1: emit → output 1
- Source 2: skip (deleted)
- Source 3: emit → output 2 (shifted left)
Result: [1, 2, 4] ✅
```

**When Container Ends**:
- Emit any remaining insertions (for insertions after last field/element)

## Open Questions

1. **Value boundary detection**: How do we know when a value is complete? (Track depth - when depth returns, value is complete)
2. **Insertion initialization**: How do we extract insertions from patch structure?
   - **Objects**: Traverse patch tree to find fields with `!insert` tag
   - **Arrays**: Parse `!arraydiff` structure (format: `index: !insert value`)
   - Compare with source structure (tracked during streaming)
   - Sort by key (objects) or insertion index (arrays)
3. **Array index tracking**: How do we track source vs output indices?
   - Track source array index (`containerElements`) as we stream
   - **Insertion** at source `i`: emit insertion → output `i`, then original element → output `i+1`
   - **Deletion** at source `i`: skip element, subsequent elements shift left (source `i+1` → output `i`)
   - **Replacement** at source `i`: skip original, emit replacement → output `i` (no shifting)
   - Output indices are correct because operations are applied at correct source positions
4. **Array operation parsing**: How do we parse `!arraydiff` structure?
   - Parse integer keys as source indices
   - Extract operation type (`!insert`, `!delete`, `!replace`) from tag
   - Build map: `sourceIndex → ArrayOperation`
3. **Nested operations**: If patch applies at `users` and `users.123`, how do we handle both? (Check incrementally as we stream deeper)
4. **Multiple patches**: Do we create new processor for each patch, or reuse? (New processor with result events as input)
5. **Operation timing**: When exactly do we detect operations?
   - `!delete`/`!replace`: At `Key` event for objects, at element start for arrays
   - `!insert`: Initialize when container starts, emit at correct sorted position as we stream

## Next Steps

1. **Design container buffering**: How to detect and buffer containers with patches
2. **Design operation application**: How to apply `!insert`/`!delete`/`!replace` to buffered containers
3. **Design repeatability**: How to chain patch applications efficiently
4. **Prototype**: Test with simple cases
