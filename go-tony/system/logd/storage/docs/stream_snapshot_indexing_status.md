# Stream Package and snap.Index

## Overview

This document describes how the `stream` package relates to building `snap.Index` structures for snapshot indexing.

**Note**: The `snap.Index` structure may evolve based on requirements. This document focuses on the current structure as a starting point.

## snap.Index Structure

The `snap.Index` structure (defined in `internal/snap/index.go`):

```go
//tony:schemagen=index
type Index struct {
    StartField *string
    Start      int
    End        int
    Offset     int64
    Size       int64

    ParentField       string
    ParentSparseIndex int
    ParentIndex       int
    Parent            *Index

    Children []Index
}
```

**Current state**: `NewIndex()` is a stub (returns `nil, nil`). The structure is defined but not yet implemented.

**Fields**:
- `Offset` / `Size`: Byte position and size in snapshot
- `Start` / `End`: Array indices or field ranges  
- `StartField`: Object field name where index starts
- `Parent*`: Parent relationship fields
- `Children`: Child index entries forming a tree

## Stream Package Status

### ✅ What Exists (Ready to Use)

**`stream.Encoder`** - Complete implementation
- Structural encoding: `BeginObject()`, `EndObject()`, `BeginArray()`, `EndArray()`
- Value writing: `WriteKey()`, `WriteString()`, `WriteInt()`, `WriteFloat()`, `WriteBool()`, `WriteNull()`
- State queries: `CurrentPath()`, `ParentPath()`, `Depth()`, `IsInObject()`, `IsInArray()`, `CurrentKey()`, `CurrentIndex()`, `Offset()`
- **Offset tracking**: `Offset()` returns accurate byte position in output stream

**`stream.Decoder`** - Complete implementation
- Event reading: `ReadEvent()` returns structural events
- State queries: Same query methods as Encoder
- Event types: `EventBeginObject`, `EventEndObject`, `EventBeginArray`, `EventEndArray`, `EventKey`, `EventString`, `EventInt`, `EventFloat`, `EventBool`, `EventNull`
- ⚠️ **Exception**: `dec.Offset()` returns 0 (explicitly deferred)

**`stream.State`** - Complete implementation
- Path tracking: `CurrentPath()`, `ParentPath()` return correct kpaths
- Depth tracking: `Depth()` accurate
- Structure tracking: `IsInObject()`, `IsInArray()`, `CurrentKey()`, `CurrentIndex()` work correctly

### ⚠️ What Needs Implementation

**Building `snap.Index` from Stream Events**

The stream package provides the necessary hooks, but the logic to build `snap.Index` structures doesn't exist yet:

1. **Index Building Logic**
   - Code to create `snap.Index` nodes as structures are encoded
   - Code to track start/end offsets for each path
   - Code to populate `StartField`, `Start`, `End`, `Offset`, `Size` fields
   - Code to maintain parent/child relationships

2. **Size Tracking**
   - Code to track start offset when a path begins
   - Code to calculate size when a path completes
   - Data structure to store per-path start offsets

3. **Range Descriptor Support** (if needed)
   - Code to track array ranges
   - Code to create range descriptors for unindexed ranges
   - Threshold application logic

## How Stream Package Enables snap.Index Building

### Event-Based Processing

The stream package provides structural events that correspond to `snap.Index` structure:

```go
dec, _ := stream.NewDecoder(sourceFile, stream.WithBrackets())
enc, _ := stream.NewEncoder(destFile, stream.WithBrackets())

var indexStack []*snap.Index  // Track index tree as we build

for {
    event, err := dec.ReadEvent()
    if err == io.EOF {
        break
    }
    
    // Write event to new snapshot
    switch event.Type {
    case stream.EventBeginObject:
        enc.BeginObject()
        // Create new snap.Index node
        idx := &snap.Index{
            Offset: enc.Offset(),
            // ... populate from enc state
        }
        indexStack = append(indexStack, idx)
        
    case stream.EventEndObject:
        enc.EndObject()
        // Finalize current index node
        current := indexStack[len(indexStack)-1]
        current.Size = enc.Offset() - current.Offset
        indexStack = indexStack[:len(indexStack)-1]
        
    case stream.EventKey:
        enc.WriteKey(event.Key)
        // Update current index with field name
        if len(indexStack) > 0 {
            indexStack[len(indexStack)-1].StartField = &event.Key
        }
        
    // ... etc
    }
}
```

### Queryable State

The stream package provides state queries that map directly to `snap.Index` fields:

- `enc.CurrentPath()` → Can derive `StartField` and path structure
- `enc.Offset()` → Maps to `Index.Offset`
- `enc.CurrentIndex()` → Maps to `Index.Start`/`End` for arrays
- `enc.CurrentKey()` → Maps to `Index.StartField` for objects
- `enc.Depth()` → Helps maintain parent/child relationships

## Next Steps

1. **Understand `snap.Index` Requirements**
   - Review how `snap.Index` structure should be populated
   - Determine mapping from stream events to index fields
   - Clarify when to create index nodes vs. update existing ones

2. **Design Index Building Logic**
   - Design data structures to track index tree during encoding
   - Determine how to populate `StartField`, `Start`, `End`, `Offset`, `Size`
   - Design parent/child relationship tracking

3. **Implement `NewIndex()`**
   - Implement index building from stream events
   - Integrate with `stream.Encoder` for offset/size tracking
   - Test with various snapshot structures

## Related Documentation

- `snapshot_index_summary.md` - Overview of range descriptors concept
- `snapshot_index_range_descriptors.md` - Detailed range descriptor design (may inform `snap.Index` evolution)
- `internal/snap/doc.go` - Package documentation for snap package
