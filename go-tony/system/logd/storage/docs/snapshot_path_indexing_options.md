# Snapshot Path Indexing Options

## Requirements

- Store snapshot `ir.Node` in Tony wire format
- Store index with offset and size for every path
- Support efficient path lookups during snapshot reads

## Current Context

- Snapshots are stored at `SnapPos` in dlog files (logA/logB)
- `Entry.SnapPos` points to the byte offset where snapshot data starts
- Snapshot data is in Tony wire format (same as patches)

## Option 1: Appended Index (Snapshot Data + Index)

**Structure**:
```
[Snapshot Data in Tony Format]
[Index Data]
```

**Format**:
- Snapshot data starts at `SnapPos`
- Index appended after snapshot data
- Need to know where snapshot ends / index starts

**Pros**:
- ✅ Simple: Write snapshot, then write index
- ✅ Can read snapshot without reading index
- ✅ Index can be built incrementally as snapshot is written

**Cons**:
- ⚠️ Need to track snapshot size to know where index starts
- ⚠️ Reading index requires seeking to end of snapshot

**Implementation**:
```go
// Write snapshot
snapshotWriter := dlog.WriteSnapshot()
snapshotWriter.StreamFrom(snapshotReader)
snapshotPos, err := snapshotWriter.Close() // Returns position where snapshot starts

// Write index after snapshot
indexWriter := dlog.WriteIndexAt(snapshotPos + snapshotSize)
for each path {
    indexWriter.AddPath(path, offset, size)
}
indexPos, err := indexWriter.Close()

// Store both positions in Entry
entry.SnapPos = snapshotPos
entry.IndexPos = indexPos  // New field needed
```

---

## Option 2: Prepend Index (Index + Snapshot Data)

**Structure**:
```
[Index Data]
[Snapshot Data in Tony Format]
```

**Format**:
- Index starts at `SnapPos`
- Snapshot data starts at `SnapPos + indexSize`
- Need to know index size to read snapshot

**Pros**:
- ✅ Can read index first, then seek to specific paths
- ✅ Index size known after writing index

**Cons**:
- ⚠️ Need to buffer snapshot data while building index
- ⚠️ Or need two passes: build index, then write snapshot

**Implementation**:
```go
// Build index first (need snapshot data to know paths)
index := buildIndex(snapshotData)

// Write index
indexWriter := dlog.WriteIndex()
for each path {
    indexWriter.AddPath(path, offset + indexSize, size) // Offset includes index size
}
indexPos, err := indexWriter.Close()
indexSize := getIndexSize(indexPos)

// Write snapshot after index
snapshotWriter := dlog.WriteSnapshotAt(indexPos + indexSize)
snapshotWriter.StreamFrom(snapshotReader)
snapshotPos, err := snapshotWriter.Close()

// Store position in Entry
entry.SnapPos = indexPos  // Points to index start
entry.SnapshotOffset = indexSize  // Offset from SnapPos to snapshot data
```

---

## Option 3: Separate Index File

**Structure**:
```
logA/logB: [Snapshot Data in Tony Format]
index file: [Index Data]
```

**Format**:
- Snapshot stored in dlog at `SnapPos`
- Index stored in separate file (e.g., `snapshot_indexes.gob` or per-snapshot file)
- Index file maps `(logFile, snapPos) → pathIndex`

**Pros**:
- ✅ Snapshot data stays in wire format (no changes)
- ✅ Index can be loaded separately
- ✅ Can rebuild index without touching snapshot data

**Cons**:
- ⚠️ Need separate file management
- ⚠️ Need to keep index file in sync with snapshots
- ⚠️ More complex file structure

**Implementation**:
```go
// Write snapshot
snapshotWriter := dlog.WriteSnapshot()
snapshotWriter.StreamFrom(snapshotReader)
snapshotPos, err := snapshotWriter.Close()

// Build and store index separately
index := buildIndex(snapshotData)
indexFile.StoreIndex(logFile, snapshotPos, index)

// Entry only needs SnapPos
entry.SnapPos = snapshotPos
```

---

## Option 4: Embedded Index in Entry

**Structure**:
```
Entry structure contains index data
logA/logB: [Snapshot Data in Tony Format]
```

**Format**:
- Snapshot stored at `SnapPos` (wire format)
- Index stored in `Entry` structure (e.g., `Entry.PathIndex`)
- Index serialized as part of Entry

**Pros**:
- ✅ Index travels with Entry metadata
- ✅ No separate file management
- ✅ Can read index without reading snapshot

**Cons**:
- ⚠️ Entry structure grows large (many paths = large index)
- ⚠️ Index must fit in memory when reading Entry
- ⚠️ Need to serialize/deserialize index

**Implementation**:
```go
type Entry struct {
    // ... existing fields ...
    PathIndex *SnapshotPathIndex  // New field
}

type SnapshotPathIndex struct {
    Paths []PathIndexEntry
}

type PathIndexEntry struct {
    Path   string
    Offset int64
    Size   int64
}

// Write snapshot and build index
snapshotWriter := dlog.WriteSnapshot()
indexBuilder := NewIndexBuilder()
for each path {
    offset, size := snapshotWriter.GetPathBounds(path)
    indexBuilder.AddPath(path, offset, size)
}
snapshotPos, err := snapshotWriter.Close()
index := indexBuilder.Build()

// Store in Entry
entry.SnapPos = snapshotPos
entry.PathIndex = index
```

---

## Option 5: Hybrid: Index Footer in Snapshot

**Structure**:
```
[Snapshot Data in Tony Format]
[Index Footer: offset to index start]
[Index Data]
```

**Format**:
- Snapshot data starts at `SnapPos`
- Index footer at end (fixed size, e.g., 8 bytes)
- Footer contains offset to index start (or index size)
- Index data before footer

**Pros**:
- ✅ Can read snapshot without reading index
- ✅ Can seek to index using footer
- ✅ Single file, no separate management

**Cons**:
- ⚠️ Need to update footer after writing index
- ⚠️ Slightly more complex write process

**Implementation**:
```go
// Write snapshot
snapshotWriter := dlog.WriteSnapshot()
snapshotWriter.StreamFrom(snapshotReader)
snapshotEnd, err := snapshotWriter.Close()

// Reserve space for footer (8 bytes)
footerPos := snapshotEnd
dlog.Seek(footerPos + 8)

// Write index
indexWriter := dlog.WriteIndex()
for each path {
    indexWriter.AddPath(path, offset, size)
}
indexStart, err := indexWriter.Close()
indexSize := indexStart - footerPos - 8

// Write footer (offset to index start, or index size)
dlog.WriteAt(footerPos, indexSize) // Or indexStart

// Entry points to snapshot start
entry.SnapPos = snapshotStart
```

---

## Comparison

| Option | Complexity | Read Efficiency | Write Efficiency | File Management |
|--------|-----------|----------------|------------------|-----------------|
| **1. Appended Index** | Low | Medium (seek to end) | High (stream write) | Simple |
| **2. Prepend Index** | Medium | High (read index first) | Low (need buffer/2 passes) | Simple |
| **3. Separate File** | High | High (load separately) | Medium (2 files) | Complex |
| **4. Embedded in Entry** | Low | High (in Entry) | Medium (build during write) | Simple |
| **5. Index Footer** | Medium | High (seek via footer) | Medium (update footer) | Simple |

## Recommendation

**Option 1: Appended Index** seems best for initial implementation:
- ✅ Simplest to implement
- ✅ Can stream snapshot write
- ✅ Index can be built incrementally
- ✅ Snapshot readable without index

**Trade-off**: Need to track snapshot size, but this is manageable.

**Alternative**: **Option 4: Embedded in Entry** if index is small enough:
- ✅ Simplest read path (index in Entry)
- ⚠️ But index may be large for big snapshots

## Questions

1. **Index Size**: How many paths per snapshot? (affects Option 4 viability)
2. **Index Format**: What format for index? (Gob, custom binary, Tony format?)
3. **Rebuild**: Can we rebuild index from snapshot if lost? (affects Option 3)
4. **Update Frequency**: Do snapshots change after creation? (affects all options)
