# Code Organization for Patch System

## Overview

The patch system consists of 3 pieces:
1. **Write-time patch simplification**: Coordinator creates simple patches from complex ones
2. **Read-time patch application**: Apply simple patches to streaming events during snapshot reads
3. **Compaction patch writing**: Modify patch application to write patches instead of full snapshots

## Proposed Organization

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
│       ├── compaction.go           # Compaction patch writer (piece 3)
│       ├── compaction_test.go     # Tests for compaction
│       └── doc.go                  # Package documentation
│
└── storage.go                       # Existing storage layer
```

## Package: `tx` (Transaction/Coordinator)

### `tx/simplify.go`

**Purpose**: Simplify complex patches into simple patches (`!insert`, `!delete`, `!replace`)

**Responsibilities**:
- Execute complex mergeops during transaction coordination
- Convert complex patches to simple patches
- Ensure patches are in canonical form (only `!insert`, `!delete`, `!replace`)

**Key Functions**:
```go
// SimplifyPatches executes complex patches and returns simple patches
func SimplifyPatches(complexPatches []*ir.Node) ([]*ir.Node, error)

// IsSimplePatch checks if a patch only contains simple operations
func IsSimplePatch(patch *ir.Node) bool
```

**Integration**:
- Called by `tx/coord.go` after collecting patches from participants
- Before committing, simplify patches and store simplified versions

## Package: `internal/patches` (Patch Application)

### Design: Chainable Processor with Pluggable Output

**Key Insight**: Patch processors are chainable, and the final output handler differs:
- **Snap writing**: Chain ends with event writer (writes events to snapshot)
- **Compaction**: Chain ends with patch writer (writes patches instead of events)

### `internal/patches/processor.go`

**Purpose**: Core patch application logic (chainable, reusable)

**Responsibilities**:
- Process events with patch operations (`!insert`, `!delete`, `!replace`)
- Coordinate patch paths with streaming paths
- Maintain sorted order for insertions
- Handle array index shifting
- Output events to next processor in chain (or output handler)

**Key Types**:
```go
// EventHandler handles events (can be another processor or output handler)
type EventHandler interface {
    HandleEvent(event *stream.Event) error
    Close() error
}

// Processor applies a single patch to streaming events
type Processor struct {
    patch *ir.Node
    state *stream.State
    next  EventHandler  // Next in chain (processor or output handler)
    // ... operation state
}

// ProcessEvent processes a single event, applying patches incrementally
func (p *Processor) ProcessEvent(event *stream.Event) error

// HandleEvent implements EventHandler - processes event and forwards to next
func (p *Processor) HandleEvent(event *stream.Event) error

// NewProcessor creates a new patch processor, chained to next handler
func NewProcessor(patch *ir.Node, next EventHandler) *Processor
```

**Usage**:
- Chain multiple processors: `processor1 → processor2 → processor3 → output`
- Each processor applies one patch
- Final handler writes output (events or patches)

### `internal/patches/writer.go`

**Purpose**: Output handler for snap writing (writes events to snapshot)

**Responsibilities**:
- Receive patched events from processor chain
- Write events to snapshot builder
- Close snapshot when done

**Key Types**:
```go
// EventWriter writes events to snapshot builder
type EventWriter struct {
    builder *snap.Builder
}

// HandleEvent writes event to snapshot builder
func (w *EventWriter) HandleEvent(event *stream.Event) error

// Close closes the snapshot builder
func (w *EventWriter) Close() error

// NewEventWriter creates an event writer for snap writing
func NewEventWriter(builder *snap.Builder) *EventWriter
```

**Usage**:
- End of chain for snap writing: `processor → processor → EventWriter`
- Writes final patched events to snapshot

### `internal/patches/patchwriter.go`

**Purpose**: Output handler for compaction (writes patches instead of events)

**Responsibilities**:
- Receive operation metadata from processor chain
- Generate patch structure (`!insert`, `!delete`, `!replace`) from operations
- Write patch format

**Key Types**:
```go
// Operation represents a patch operation (insert/replace/delete)
// Note: Unchanged elements are not reported as operations (memory constraint)
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
    // Note: OpKeep removed - would violate memory constraint (storing unchanged nodes)
    // Unchanged elements are not reported as operations
)

// PatchWriter writes patches during compaction
type PatchWriter struct {
    operations []Operation  // Operations to write as patch
    // ... patch structure building
}

// HandleOperation records an operation for patch generation
func (w *PatchWriter) HandleOperation(op Operation) error

// Finalize generates and returns patch structure
func (w *PatchWriter) Finalize() (*ir.Node, error)

// Close implements EventHandler
func (w *PatchWriter) Close() error

// NewPatchWriter creates a patch writer for compaction
func NewPatchWriter() *PatchWriter
```

**Usage**:
- End of chain for compaction: `processor → processor → PatchWriter`
- Processors call `HandleOperation` when they apply operations
- Generates patches from recorded operations

### `internal/patches/processor.go` (Updated)

**Purpose**: Core patch application logic (chainable, reusable)

**Responsibilities**:
- Process events with patch operations (`!insert`, `!delete`, `!replace`)
- Coordinate patch paths with streaming paths
- Maintain sorted order for insertions
- Handle array index shifting
- Output events to next processor in chain (or output handler)
- **Report operations** to operation-aware handlers

**Key Types** (Updated):
```go
// EventHandler handles events (can be another processor or output handler)
type EventHandler interface {
    HandleEvent(event *stream.Event) error
    Close() error
}

// OperationHandler handles operation metadata (for patch writing)
type OperationHandler interface {
    HandleOperation(op Operation) error
}

// CombinedHandler combines event and operation handling
type CombinedHandler interface {
    EventHandler
    OperationHandler
}

// Processor applies a single patch to streaming events
type Processor struct {
    patch *ir.Node
    state *stream.State
    next  EventHandler  // Next in chain (processor or output handler)
    opHandler OperationHandler  // Optional: for operation reporting
    // ... operation state
}

// ProcessEvent processes a single event, applying patches incrementally
func (p *Processor) ProcessEvent(event *stream.Event) error

// HandleEvent implements EventHandler - processes event and forwards to next
func (p *Processor) HandleEvent(event *stream.Event) error

// NewProcessor creates a new patch processor, chained to next handler
func NewProcessor(patch *ir.Node, next EventHandler) *Processor

// NewProcessorWithOps creates a processor that reports operations
func NewProcessorWithOps(patch *ir.Node, next CombinedHandler) *Processor
```

**Operation Reporting**:
- When applying `!insert`: Call `opHandler.HandleOperation(Operation{Type: OpInsert, Path: path, From: nil, To: value})`
- When applying `!replace`: Call `opHandler.HandleOperation(Operation{Type: OpReplace, Path: path, From: from, To: to})`
- When applying `!delete`: Call `opHandler.HandleOperation(Operation{Type: OpDelete, Path: path, From: original, To: nil})`
- When no patch applies: **Don't report operation** (avoids storing unchanged nodes in memory)

## Integration Points

### 1. Write-Time Simplification (`tx/simplify.go`)

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

**Operation Flow**:
1. Processor applies `!insert` at path "users.123.name" → calls `patchWriter.HandleOperation(OpInsert, "users.123.name", nil, value)`
2. Processor applies `!replace` at path "users.123.age" → calls `patchWriter.HandleOperation(OpReplace, "users.123.age", from, to)`
3. Processor applies `!delete` at path "users.123.old" → calls `patchWriter.HandleOperation(OpDelete, "users.123.old", original, nil)`
4. Processor encounters unchanged element → **no operation reported** (memory efficient)
5. PatchWriter accumulates operations and builds patch structure (only changes are recorded)

## Alternative Organization (If patches are tightly coupled to snap)

### Option B: Keep patches in `internal/snap`

```
internal/snap/
├── snap.go
├── builder.go
├── patches.go              # NEW: Patch processor (piece 2)
├── patches_test.go
├── compaction.go           # NEW: Compaction patch writer (piece 3)
└── compaction_test.go
```

**Pros**: Patches are closely related to snapshots
**Cons**: `snap` package might become too large

## Recommended: Separate `internal/patches` Package

**Rationale**:
1. **Separation of concerns**: Patch application is distinct from snapshot structure
2. **Reusability**: Patch processor can be used for `ReadStateAt` and snapshots
3. **Testability**: Easier to test patch logic independently
4. **Clarity**: Clear boundary between snapshot storage and patch application

## File Structure Details

### `tx/simplify.go`
- Simplifies complex mergeops to simple operations
- Validates patches are simple
- May use `tony.Patch` internally to execute complex patches

### `internal/patches/processor.go`
- Core patch application logic (chainable)
- Implements incremental patch application
- Handles object and array operations
- Maintains sorted order and index shifting
- Forwards events to next handler in chain

### `internal/patches/writer.go`
- Event output handler for snap writing
- Writes events to snapshot builder
- Simple pass-through to builder

### `internal/patches/patchwriter.go`
- Patch output handler for compaction
- Receives operation metadata (`Operation` structs) from processors
- Generates patch structure (`!insert`, `!delete`, `!replace`) from operations
- Builds patch tree structure organized by path
- Implements `CombinedHandler` (both `EventHandler` and `OperationHandler`)

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

## Chain Composition Pattern

**Key Design**: Processors are chainable, output handlers are pluggable

```
Snap Writing Chain:
  events → processor1 → processor2 → processor3 → EventWriter → builder
  (processors forward events, EventWriter writes to snapshot)

Compaction Chain:
  baseEvents → processor1 → processor2 → processor3 → PatchWriter → patch
  (processors forward events AND report operations, PatchWriter builds patch)
```

**Operation Reporting**:
- Processors report operations as they apply patches
- `PatchWriter` receives operation metadata (not just events)
- Operations include: type (insert/replace/delete), path, values
- `PatchWriter` builds patch structure from operations

**Benefits**:
- **Separation**: Core logic (processor) separate from output (writer)
- **Composition**: Chain multiple processors easily
- **Flexibility**: Different output handlers for different use cases
- **Operation-aware**: PatchWriter gets explicit operation info, not inferred from events
- **Memory efficient**: Only changes reported (no unchanged nodes stored)
- **Consistent naming**: From/To used consistently (not Value/From)
- **Testability**: Each component testable independently
- **No inheritance**: Functional composition, not extension

## Operation Metadata Design

### Operation Structure

```go
type Operation struct {
    Type OperationType  // Insert, Replace, Delete (no Keep - violates memory constraint)
    Path string         // kpath where operation applies
    From *ir.Node       // Source value (nil for insert, original for replace/delete)
    To   *ir.Node       // Target value (inserted/replaced value, nil for delete)
}

type OperationType int

const (
    OpInsert OperationType = iota  // From = nil, To = inserted value
    OpReplace                      // From = original, To = replacement
    OpDelete                       // From = original, To = nil
    // Note: OpKeep removed - would violate memory constraint (storing unchanged nodes)
    // Unchanged elements are not reported as operations
)
```

### When Operations Are Reported

**`!insert`**:
- When insertion is emitted (at correct sorted position)
- `Operation{Type: OpInsert, Path: "users.123.name", From: nil, To: insertedNode}`

**`!replace`**:
- When replacement happens (original skipped, replacement emitted)
- `Operation{Type: OpReplace, Path: "users.123.age", From: originalNode, To: replacementNode}`

**`!delete`**:
- When deletion happens (element skipped)
- `Operation{Type: OpDelete, Path: "users.123.old", From: originalNode, To: nil}`

**Unchanged elements**:
- When element is unchanged (no patch applies)
- **Don't report operation** - no `OpKeep` to avoid storing nodes in memory
- PatchWriter infers unchanged elements from absence of operations
- Memory efficient: only changes are reported

### PatchWriter Operation Handling

```go
func (w *PatchWriter) HandleOperation(op Operation) error {
    // All operations have uniform structure: Type, Path, From, To
    // Organize operations by path
    // Build patch tree structure
    // For arrays: use !arraydiff format with index keys
    // For objects: use field keys with operation tags
    // Only changes are reported (no OpKeep operations)
    
    // Build patch structure based on operation type
    // OpInsert: create !insert node with To value
    // OpReplace: create !replace node with From/To
    // OpDelete: create !delete node
}
```

**Uniform Input - Memory Constraint Issue**:
- **Problem**: Reporting `OpKeep` with `From` and `To` nodes violates out-of-memory constraint
- **Solution Options**:
  1. **Don't report `OpKeep`**: Only report actual changes (Insert/Replace/Delete)
     - PatchWriter infers unchanged elements from absence of operations
     - Lower memory usage (no nodes stored for unchanged elements)
  2. **Report `OpKeep` without nodes**: `OpKeep` only has path, no `From`/`To`
     - Indicates path exists and is unchanged
     - Still uniform structure, but `From`/`To` are nil for Keep
  3. **Report `OpKeep` with path only**: Just track paths, not values
     - Minimal memory footprint
     - PatchWriter knows what exists but doesn't need values

**Recommended**: Option 1 - Don't report `OpKeep` operations
- Only report changes (Insert/Replace/Delete)
- PatchWriter builds patch from changes only
- Unchanged elements inferred (not in patch = unchanged)
- Memory efficient: no nodes stored for unchanged elements

## Next Steps

1. Create `internal/patches` package structure
2. Define `EventHandler` and `OperationHandler` interfaces
3. Define `Operation` type and `OperationType` enum
4. Implement `processor.go` (chainable core logic with operation reporting)
5. Implement `writer.go` (event output handler for snap writing)
6. Implement `patchwriter.go` (patch output handler that receives operations)
7. Add `tx/simplify.go` for patch simplification
8. Integrate with existing snapshot and storage code

## Implementation Order

1. **Interfaces and types** (`processor.go`): Define `EventHandler`, `OperationHandler`, `Operation` (no OpKeep)
2. **Core processor** (`processor.go`): Implement patch application logic with operation reporting (only changes reported)
3. **Event writer** (`writer.go`): Simple output handler for testing (ignores operations, just forwards events)
4. **Patch writer** (`patchwriter.go`): Receives change operations only, builds patch structure
5. **Simplification** (`tx/simplify.go`): Can be done in parallel
6. **Integration**: Wire up with snapshots and storage

## Memory Constraint Compliance

- **No `OpKeep` operations**: Avoids storing unchanged node values
- **Change-only reporting**: Only Insert/Replace/Delete operations reported
- **Streaming-friendly**: Unchanged elements flow through without storage
- **PatchWriter efficiency**: Only processes changes, infers unchanged from absence

## Key Design Decisions

1. **Change-Only Reporting**: Only changes reported (Insert/Replace/Delete), unchanged elements not reported
2. **Memory Efficient**: No `OpKeep` operations to avoid storing unchanged nodes in memory
3. **From/To naming**: Consistent naming (not Value/From) - From is source, To is target
4. **Operation types**: Insert (nil→value), Replace (from→to), Delete (value→nil)
5. **Unchanged inference**: PatchWriter infers unchanged elements from absence of operations
