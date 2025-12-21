# Snapshot as Pair of ir.Nodes: Index + Data

## Design Overview

**Snapshot Structure**:
- **First ir.Node**: Index node (mirrors snapshot structure, contains offsets)
- **Second ir.Node**: Snapshot data (actual document state)

**Key Insight**: Use `ir.Node` infrastructure for both index and data.

## Index Node Structure

The index node mirrors the snapshot structure, but:
- **Leaves** (or path boundaries) contain `!snap-offset` tagged values
- **Offset values** point to byte positions in the second node
- **Optionally**: Also store `!snap-size` for each path

## Example

**Snapshot Data** (second node):
```
{
  a: {
    b: "value1"
    c: "value2"
  }
  x: {
    y: "value3"
  }
}
```

**Index Node** (first node):
```
{
  a: {
    b: !snap-offset 0      // Points to "a" in snapshot
    c: !snap-offset 50     // Points to "a.b" in snapshot
  }
  x: {
    y: !snap-offset 100    // Points to "a.c" in snapshot
  }
}
```

**Or with sizes**:
```
{
  a: {
    b: {
      offset: !snap-offset 0
      size: !snap-size 50
    }
    c: {
      offset: !snap-offset 50
      size: !snap-size 50
    }
  }
  x: {
    y: {
      offset: !snap-offset 100
      size: !snap-size 50
    }
  }
}
```

## Benefits

### ✅ **Uses Existing Infrastructure**

1. **ir.Node parsing**: Index is just an ir.Node, can use existing parsers
2. **ir.Node encoding**: Index can be encoded/decoded like any node
3. **Path navigation**: Use existing `GetKPath()` to navigate index
4. **Tag system**: Use existing tag system (`!snap-offset`, `!snap-size`)

### ✅ **Self-Contained**

1. **No separate format**: Index format is Tony format
2. **Debuggable**: Can inspect index like any other node
3. **Consistent**: Same tools work for index and data

### ✅ **Efficient Lookup**

1. **Path-based navigation**: Navigate index using same paths as snapshot
2. **Direct offset access**: Get offset via `GetKPath()` on index node
3. **No binary search needed**: Tree structure provides O(depth) lookup

## Index Node Format Options

### Option 1: Offset at Each Path Boundary

**Structure**: Index mirrors snapshot exactly, offsets at path boundaries:
```
{
  a: !snap-offset 0        // Offset to "a" subtree
  a.b: !snap-offset 50     // Offset to "a.b" value
  a.c: !snap-offset 100    // Offset to "a.c" value
  x: !snap-offset 150      // Offset to "x" subtree
  x.y: !snap-offset 200    // Offset to "x.y" value
}
```

**Pros**: Simple, direct mapping
**Cons**: Flat structure loses hierarchy

### Option 2: Hierarchical with Offsets at Boundaries

**Structure**: Index mirrors snapshot hierarchy, offsets mark boundaries:
```
{
  a: {
    _offset: !snap-offset 0     // Offset to "a" subtree start
    b: {
      _offset: !snap-offset 50  // Offset to "a.b" value
    }
    c: {
      _offset: !snap-offset 100 // Offset to "a.c" value
    }
  }
  x: {
    _offset: !snap-offset 150
    y: {
      _offset: !snap-offset 200
    }
  }
}
```

**Pros**: Preserves hierarchy, can navigate like snapshot
**Cons**: Need special field name (`_offset`) or tag

### Option 3: Tagged Values with Offset + Size

**Structure**: Use tags on values, include both offset and size:
```
{
  a: {
    b: {
      !snap-offset: 0
      !snap-size: 50
      _value: "placeholder"  // Or omit, just tags?
    }
    c: {
      !snap-offset: 50
      !snap-size: 50
    }
  }
}
```

**Pros**: Includes size, can use tag system
**Cons**: More complex structure

### Option 4: Simple Tagged Offsets (Recommended)

**Structure**: Index mirrors snapshot, offsets stored as tagged integers:
```
{
  a: !snap-offset 0
  "a.b": !snap-offset 50
  "a.c": !snap-offset 100
  x: !snap-offset 150
  "x.y": !snap-offset 200
}
```

**Or with size**:
```
{
  a: {
    offset: !snap-offset 0
    size: !snap-size 150    // Size of "a" subtree
  }
  "a.b": {
    offset: !snap-offset 50
    size: !snap-size 10      // Size of "a.b" value
  }
}
```

**Pros**: Simple, uses existing tag system
**Cons**: Need to decide on structure

## Lookup Process

### Step 1: Navigate Index Node

```go
func (idx *SnapshotIndex) GetPathOffset(kindedPath string) (int64, int64, error) {
    // Navigate index node using same path
    indexNode, err := idx.indexNode.GetKPath(kindedPath)
    if err != nil {
        return 0, 0, err
    }
    
    // Extract offset from tagged value
    offset := extractSnapOffset(indexNode)
    size := extractSnapSize(indexNode)  // If stored
    
    return offset, size, nil
}
```

### Step 2: Read from Snapshot Data

```go
func (snap *Snapshot) ReadPath(kindedPath string) (*ir.Node, error) {
    // Get offset from index
    offset, size, err := snap.index.GetPathOffset(kindedPath)
    if err != nil {
        return nil, err
    }
    
    // Seek to offset in snapshot data
    reader := io.NewSectionReader(snap.dataReader, offset, size)
    
    // Parse node at that offset
    return parse.Parse(reader)
}
```

## Building the Index

### During Snapshot Write

```go
func BuildSnapshotIndex(snapshotWriter *SnapshotWriter, snapshotReader SnapshotReader) (*ir.Node, error) {
    indexBuilder := &indexNodeBuilder{}
    
    // Traverse snapshot in pre-order DFS
    traverseSnapshot(snapshotReader, func(path string, offset int64, size int64) {
        // Build index node mirroring snapshot structure
        indexBuilder.AddPath(path, offset, size)
    })
    
    return indexBuilder.Build(), nil
}
```

**Process**:
1. Stream snapshot data to second node (track offsets as we write)
2. Build index node mirroring structure (store offsets at boundaries)
3. Write index node as first node

## Storage Format

### On Disk

```
[Index Node in Tony Format]
[Snapshot Data Node in Tony Format]
```

**Or**:
```
[Index Node Length: varuint]
[Index Node Data: Tony Format]
[Snapshot Data Node Length: varuint]
[Snapshot Data Node Data: Tony Format]
```

**Entry.SnapPos** points to start of index node.

## Size Considerations

### Index Node Size

**For 1M paths**:
- Index node structure: mirrors snapshot (same number of paths)
- Offset values: integers (8 bytes each, or varuint if smaller)
- **Estimated**: ~10-50 MB depending on structure

**Max node size 1M**: Index node must fit in 1M, so:
- **Constraint**: Index node size < 1M
- **Implication**: May need to limit paths or use compression

### Snapshot Data Size

- **No constraint**: Snapshot data can be larger than 1M
- **Streaming**: Can read paths without loading full snapshot

## Questions

1. **Index Structure**: Which format? (Option 1-4 above)
2. **Size Storage**: Include size in index, or compute from offsets?
3. **Index Size Limit**: What if index node > 1M? (Split index? Compression?)
4. **Tag Names**: `!snap-offset` and `!snap-size`? Or different?
5. **Empty Paths**: How to represent root path (`""`) in index?

## Recommendation

**Option 4: Simple Tagged Offsets** seems best:

```go
// Index node structure
{
  a: {
    offset: !snap-offset 0
    size: !snap-size 150    // Optional but nice
  }
  "a.b": {
    offset: !snap-offset 50
    size: !snap-size 10
  }
}
```

**Benefits**:
- ✅ Uses existing ir.Node infrastructure
- ✅ Self-contained (index is just a node)
- ✅ Includes size (convenient)
- ✅ Navigable via GetKPath()
- ✅ Debuggable (can inspect like any node)

**Implementation**:
- Build index node during snapshot write
- Store as first node, snapshot as second node
- Navigate index to find offsets, then read from snapshot
