# Patch System Design Reference

## Overview

The patch system consists of 3 pieces for persistent storage:

1. **Coordinator Simplification** (write-time): Converts complex patches to simple ones before storage
2. **Streaming Patch Application** (read-time): Applies simple patches when reading snapshots
3. **Compaction Patch Writing** (write-time): Writes patches instead of full snapshots during compaction

### Key Principles

- **Memory efficient**: No container buffering - streaming is horizontal within containers
- **Change-only**: Only report changes (Insert/Replace/Delete), not unchanged elements
- **Chainable**: Processors compose cleanly, output handlers are pluggable
- **Simple operations**: `!insert`, `!delete`, `!replace` are well-defined mergeops

### Storage and Ordering

- **Patch storage**: Storage log (as now)
- **Patch ordering**: Maintained by storage log (patches cannot arrive out of order)
- **Patch lookup**: Index package
- **Contract**: Simplified patches written to storage commit log

---

## Piece 1: Coordinator Simplification (Write-Time)

### Purpose

Simplify complex mergeops (`!dive`, `!pipe`, etc.) into simple operations (`!insert`, `!delete`, `!replace`) before storing patches.

### Location

`tx/simplify.go` (new file)

### Semantics

**What simplification does**:
1. Apply complex patch to base document (via `ReadStateAt`)
2. Compute diff between base and result
3. Remove matching conditions (replace `from:` fields with match-all)

**Integration point**:
- Called from `tx/coord.go` after `MergePatches` (line 209), before `WriteAndIndex` (line 245)
- `simplifiedPatch := simplify.SimplifyPatches(mergedPatch)`
- Write `simplifiedPatch` instead of `mergedPatch`

**Failure handling**:
- Simplification cannot fail (it's just a diff computation)
- If it fails, transaction fails before write to log (consistent with existing tx failure handling)
- No need to handle both complex and simple patches

### Key Functions

```go
// SimplifyPatches executes complex patches and returns simple patches
func SimplifyPatches(complexPatches []*ir.Node) ([]*ir.Node, error)

// IsSimplePatch checks if a patch only contains simple operations
func IsSimplePatch(patch *ir.Node) bool
```

---

## Piece 2: Streaming Patch Application (Read-Time)

### Purpose

Apply simple patches (`!insert`, `!delete`, `!replace`) to streaming events during snapshot reads.

### Location

`internal/patches/processor.go` (new file)

### Key Constraint

**NO container buffering** - streaming must be horizontal within containers as well.

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

### Operation Semantics

**`!delete`**: Skip events for deleted field/element
- **Detection**: At `Key` event for objects, at element start for arrays
- **Action**: Skip all events until value complete (track depth)
- **Result**: Don't emit any events for the deleted field/element

**`!replace`**: Replace value with patch value
- **Detection**: At `Key` event for objects, at element start for arrays
- **Action**: Skip original value events, emit replacement events (`NodeToEvents(patchOp.to)`)
- **Result**: Replacement events emitted immediately

**`!insert`**: Insert new value at position (maintains sorted order)
- **Detection**: **Path doesn't exist in source** - detect from patch structure
  - Track fields/elements seen in source as we stream
  - Compare with patch structure to find insertions
  - Insertions are fields/elements in patch but not in source
- **Action**: Emit insertion events at correct sorted position
  - For objects: Insert before first field that comes after insertion key (alphabetically)
  - For arrays: Insert at correct index position
- **Result**: Inserted fields/elements appear in output at correct sorted position

### Array Index Shifting

**Insertion at source index `i`**:
- Original element at `i` shifts to output index `i+1`
- Insertion appears at output index `i`

**Deletion at source index `i`**:
- Element at `i` is skipped
- Subsequent elements shift left (source index `i+1` becomes output index `i`)

**Replacement at source index `i`**:
- Same output index (no shifting)

### Core Algorithm

```go
func (p *Processor) ProcessEvent(event *stream.Event) error {
    // Update state (track path)
    p.state.ProcessEvent(event)
    currentPath := p.state.CurrentPath()
    
    // Check for insertions before processing current event (maintain sorted order)
    if event.Type == EventKey {
        // For objects: check if we need to emit insertions before this key
        if err := p.emitPendingInsertionsBeforeKey(event.Key); err != nil {
            return err
        }
    } else if p.state.IsInArray() && isValueEvent(event) {
        // For arrays: check if we need to emit insertions before this element
        if err := p.emitPendingInsertionsBeforeIndex(p.containerElements); err != nil {
            return err
        }
        p.containerElements++
    }
    
    // Check if we're at a field/element boundary (for delete/replace)
    if event.Type == EventKey {
        subPatch := p.patch.GetKPath(currentPath)
        if subPatch != nil {
            if subPatch.Tag == "!delete" {
                p.startSkipping()  // Skip value events
                return nil
            } else if subPatch.Tag == "!replace" {
                to := ir.Get(subPatch, "to")
                replacementEvents := stream.NodeToEvents(to)
                p.startReplacing(replacementEvents)  // Skip original, emit replacement
                return nil
            }
        }
    }
    
    // Handle skipping/replacing state
    if p.skipping {
        if p.state.Depth() <= p.skipDepth {
            p.skipping = false
        }
        return nil  // Don't emit skipped events
    }
    
    if p.replacing {
        if p.state.Depth() <= p.skipDepth {
            p.output = append(p.output, p.replacement...)
            p.replacing = false
        }
        return nil  // Don't emit original events
    }
    
    // Normal event - emit
    p.output = append(p.output, *event)
    return nil
}
```

### Multiple Patches

**Sequential application**:
1. Apply first patch incrementally → result events
2. Stream result events through second patch processor
3. Apply second patch incrementally (same logic)
4. Repeat for subsequent patches

**Chain composition**:
```
events → processor1 → processor2 → processor3 → outputHandler
```

---

## Piece 3: Compaction Patch Writing (Write-Time)

### Purpose

Write patches instead of full snapshots during compaction, using operation metadata from processors.

### Location

`internal/patches/patchwriter.go` (new file)

### Key Design

**Operation reporting**: Processors report operations (`Insert`/`Replace`/`Delete`) as they apply patches, not just events.

**Change-only**: Only changes are reported - unchanged elements are not reported (avoids storing nodes in memory).

### Operation Structure

```go
type Operation struct {
    Type OperationType  // Insert, Replace, Delete (no Keep)
    Path string         // kpath where operation applies
    From *ir.Node       // Source value (nil for insert, original for replace/delete)
    To   *ir.Node       // Target value (inserted/replaced value, nil for delete)
}

type OperationType int

const (
    OpInsert OperationType = iota  // From = nil, To = inserted value
    OpReplace                      // From = original, To = replacement
    OpDelete                       // From = original, To = nil
    // Note: OpKeep removed - would violate memory constraint
    // Unchanged elements are not reported as operations
)
```

### When Operations Are Reported

**`!insert`**: When insertion is emitted (at correct sorted position)
- `Operation{Type: OpInsert, Path: "users.123.name", From: nil, To: insertedNode}`

**`!replace`**: When replacement happens (original skipped, replacement emitted)
- `Operation{Type: OpReplace, Path: "users.123.age", From: originalNode, To: replacementNode}`

**`!delete`**: When deletion happens (element skipped)
- `Operation{Type: OpDelete, Path: "users.123.old", From: originalNode, To: nil}`

**Unchanged elements**: **Don't report operation** - PatchWriter infers unchanged elements from absence of operations.

### PatchWriter Responsibilities

- Receive operation metadata from processor chain
- Generate patch structure (`!insert`, `!delete`, `!replace`) from operations
- Build patch tree structure organized by path
- Write patch format to storage log

---

## Code Organization

### Directory Structure

```
go-tony/system/logd/storage/
├── tx/
│   ├── coord.go                    # Existing coordinator
│   ├── simplify.go                  # NEW: Patch simplification logic
│   └── simplify_test.go            # NEW: Tests for simplification
│
├── internal/
│   ├── snap/                       # Existing snapshot package
│   │   ├── snap.go
│   │   ├── builder.go
│   │   └── ...
│   │
│   └── patches/                    # NEW: Patch application package
│       ├── processor.go            # Patch processor (piece 2)
│       ├── processor_test.go       # Tests for patch application
│       ├── writer.go                # Event writer for snap writing
│       ├── patchwriter.go           # Patch writer for compaction (piece 3)
│       ├── patchwriter_test.go      # Tests for patch writing
│       └── doc.go                   # Package documentation
│
└── storage.go                       # Existing storage layer
```

### Package: `tx`

**`tx/simplify.go`**:
- Simplifies complex mergeops to simple operations
- Validates patches are simple
- May use `tony.Patch` internally to execute complex patches

### Package: `internal/patches`

**`internal/patches/processor.go`**:
- Core patch application logic (chainable)
- Implements incremental patch application
- Handles object and array operations
- Maintains sorted order and index shifting
- Forwards events to next handler in chain
- Reports operations to operation-aware handlers

**`internal/patches/writer.go`**:
- Event output handler for snap writing
- Writes events to snapshot builder
- Simple pass-through to builder

**`internal/patches/patchwriter.go`**:
- Patch output handler for compaction
- Receives operation metadata (`Operation` structs) from processors
- Generates patch structure (`!insert`, `!delete`, `!replace`) from operations
- Builds patch tree structure organized by path
- Implements `CombinedHandler` (both `EventHandler` and `OperationHandler`)

---

## Key Types and Interfaces

### EventHandler Interface

```go
// EventHandler handles events (can be another processor or output handler)
type EventHandler interface {
    HandleEvent(event *stream.Event) error
    Close() error
}
```

### OperationHandler Interface

```go
// OperationHandler handles operation metadata (for patch writing)
type OperationHandler interface {
    HandleOperation(op Operation) error
}
```

### CombinedHandler Interface

```go
// CombinedHandler combines event and operation handling
type CombinedHandler interface {
    EventHandler
    OperationHandler
}
```

### Processor Type

```go
// Processor applies a single patch to streaming events
type Processor struct {
    patch *ir.Node
    state *stream.State
    next  EventHandler  // Next in chain (processor or output handler)
    opHandler OperationHandler  // Optional: for operation reporting
    // ... operation state (skipping, replacing, pendingInsertions, etc.)
}

// NewProcessor creates a new patch processor, chained to next handler
func NewProcessor(patch *ir.Node, next EventHandler) *Processor

// NewProcessorWithOps creates a processor that reports operations
func NewProcessorWithOps(patch *ir.Node, next CombinedHandler) *Processor
```

### EventWriter Type

```go
// EventWriter writes events to snapshot builder
type EventWriter struct {
    builder *snap.Builder
}

// NewEventWriter creates an event writer for snap writing
func NewEventWriter(builder *snap.Builder) *EventWriter
```

### PatchWriter Type

```go
// PatchWriter writes patches during compaction
type PatchWriter struct {
    operations []Operation  // Operations to write as patch
    // ... patch structure building
}

// NewPatchWriter creates a patch writer for compaction
func NewPatchWriter() *PatchWriter

// Finalize generates and returns patch structure
func (w *PatchWriter) Finalize() (*ir.Node, error)
```

---

## Integration Points

### 1. Write-Time Simplification

**Called from**: `tx/coord.go`

```go
// In coord.go, after collecting patches:
simplifiedPatches, err := simplify.SimplifyPatches(complexPatches)
if err != nil {
    return err
}
// Store simplified patches
```

### 2. Read-Time Application (Snap Writing)

**Called from**: `internal/snap/snap.go` or `storage.go`

```go
// Build chain: processor1 → processor2 → ... → eventWriter
builder := snap.NewBuilder(w, index, nil)
eventWriter := patches.NewEventWriter(builder)

// Chain processors (apply patches in order)
chain := eventWriter
for i := len(patches) - 1; i >= 0; i-- {
    chain = patches.NewProcessor(patches[i], chain)
}

// Process events through chain
for {
    event, err := decoder.ReadEvent()
    if err != nil {
        break
    }
    if err := chain.HandleEvent(event); err != nil {
        return err
    }
}
chain.Close()  // Closes entire chain (eventWriter closes builder)
```

### 3. Compaction Patch Writing

**Called from**: Compaction logic

```go
// Build chain: processor1 → processor2 → ... → patchWriter
patchWriter := patches.NewPatchWriter()

// Chain processors (apply patches in order, with operation reporting)
chain := patchWriter  // PatchWriter implements CombinedHandler
for i := len(patches) - 1; i >= 0; i-- {
    chain = patches.NewProcessorWithOps(patches[i], chain)
}

// Process base events through chain
// Processors will report operations as they apply patches
for {
    event, err := baseDecoder.ReadEvent()
    if err != nil {
        break
    }
    if err := chain.HandleEvent(event); err != nil {
        return err
    }
}

// Generate patch from recorded operations
patch, err := patchWriter.Finalize()
chain.Close()
```

---

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
    next EventHandler  // Next in chain
    opHandler OperationHandler  // Optional: for operation reporting
}
```

### Array Operation Parsing

When entering an array container (`BeginArray`):
1. Get sub-patch for container: `subPatch := patch.GetKPath(containerPath)`
2. Check if sub-patch has `!arraydiff` tag
3. Parse `!arraydiff` structure to extract operations by source index:
   - Format: `index: !insert value` or `index: !delete value` or `index: !replace { from: X, to: Y }`
   - Build map: `sourceIndex → operation`
4. Store operations indexed by source array index

### Object Insertion Initialization

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

**When Container Ends**:
- Emit any remaining insertions (for insertions after last field/element)

---

## Constraints and Requirements

### Memory Constraints

- **No container buffering**: Streaming must be horizontal within containers
- **Change-only reporting**: Only report changes (Insert/Replace/Delete), not unchanged elements
- **No `OpKeep` operations**: Avoids storing unchanged node values in memory

### Sorted Order Requirements

- **Object insertions**: Must maintain alphabetical key order
- **Array insertions**: Must maintain correct index order with proper shifting
- **Array deletions**: Must correctly shift subsequent elements left

### Patch Format Requirements

- **Simple operations only**: `!insert`, `!delete`, `!replace`
- **No matching conditions**: `from:` fields removed (match-all)
- **Storage format**: Same format as existing patches (storage log)

### Failure Handling

- **Simplification failure**: Transaction fails before write to log (consistent with existing pattern)
- **Patch application failure**: Propagate error through chain
- **Compaction failure**: Handled by compaction design (out of scope)

---

## Implementation Order

1. **Interfaces and types** (`processor.go`): Define `EventHandler`, `OperationHandler`, `Operation` (no OpKeep)
2. **Core processor** (`processor.go`): Implement patch application logic with operation reporting (only changes reported)
3. **Event writer** (`writer.go`): Simple output handler for testing (ignores operations, just forwards events)
4. **Patch writer** (`patchwriter.go`): Receives change operations only, builds patch structure
5. **Simplification** (`tx/simplify.go`): Can be done in parallel
6. **Integration**: Wire up with snapshots and storage

---

## Key Design Decisions

1. **Change-Only Reporting**: Only changes reported (Insert/Replace/Delete), unchanged elements not reported
2. **Memory Efficient**: No `OpKeep` operations to avoid storing unchanged nodes in memory
3. **From/To naming**: Consistent naming (not Value/From) - From is source, To is target
4. **Operation types**: Insert (nil→value), Replace (from→to), Delete (value→nil)
5. **Unchanged inference**: PatchWriter infers unchanged elements from absence of operations
6. **Chainable design**: Processors compose cleanly, output handlers are pluggable
7. **No container buffering**: Streaming is horizontal within containers as well

---

## Dependencies

```
tx/simplify.go
  └── Uses: tony.Patch, mergeop, ir

internal/patches/processor.go
  └── Uses: stream, ir
  └── Defines: EventHandler interface

internal/patches/writer.go
  └── Uses: internal/patches (EventHandler), internal/snap (Builder)
  └── Implements: EventHandler

internal/patches/patchwriter.go
  └── Uses: internal/patches (EventHandler, OperationHandler, Operation), stream, ir
  └── Implements: CombinedHandler (EventHandler + OperationHandler)

internal/snap/builder.go
  └── Uses: stream (for WriteEvent)

storage.go
  └── Uses: internal/patches (for ReadStateAt with patches)
```

---

## Summary

The 3-piece patch design provides:

- **Mechanism**: How patches are simplified, applied, and written
- **Constraints**: Sequential patch application, operation-based patch generation
- **Foundation**: For compaction design, caching strategies, performance optimization

**Established**:
- Patch storage: Storage log (as now)
- Patch ordering: Maintained by storage log (patches cannot arrive out of order)
- Patch lookup: Index package
- Contract: Simplified patches written to storage commit log
- Simplification semantics: Compute diff after applying patch, remove matching conditions
- Simplification failure: Cannot fail (it's just a diff); if it fails, tx fails before write

The design is **functionally sound** for persistent storage organization. All key design questions within scope have been clarified.
