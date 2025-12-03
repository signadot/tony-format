# Parent Diff Write Contention Analysis

## User's Questions

1. **Multiple children in same commit?** → Yes, write `/a` with `{b: {...}, c: {...}}`
2. **Combining with previous diffs?** → Need to switch between levels at which we apply diffs easily
3. **Write contention?** → There might be extra contention merging writes to the root

## Current Transaction Model

From `tx.go`:
- **Multi-participant transactions**: Multiple goroutines can write to different paths in the same transaction
- **Serialized by commit**: Each commit has a set of paths that are updated
- **Last participant commits**: When all participants have added patches, transaction commits atomically

**Key**: Writes within a transaction are concurrent (multiple participants), but commits are serialized.

## Write Contention Analysis

### Scenario: Multiple Children in Same Transaction

**Example**:
- Transaction with 3 participants:
  - Participant 1: Write `/a/b` → needs parent diff at `/a` with `{b: {...}}`
  - Participant 2: Write `/a/c` → needs parent diff at `/a` with `{c: {...}}`
  - Participant 3: Write `/x/y` → needs parent diff at `/x` with `{y: {...}}`

**Problem**: 
- Participants 1 and 2 both need to update `/a` diff
- If we're building parent diffs incrementally, we need to merge:
  - Participant 1: `/a` diff = `{b: {...}}`
  - Participant 2: `/a` diff = `{c: {...}}`
  - Final: `/a` diff = `{b: {...}, c: {...}}`

**Contention Points**:
1. **Within transaction**: Multiple participants writing to children of same parent
2. **At commit time**: Need to merge all parent diffs before writing to log
3. **Root path**: Most contention (every path is a child of root `/`)

### Solution: Accumulate in Transaction Buffer

**Current Design** (from redesign outline):
- Accumulate entries in in-memory buffer during transaction
- Atomically append to log on commit

**For Parent Diffs** (merge to root):
- **During transaction**: Each participant adds their patch
  - Participant 1: `/a/b` → add to buffer: `{Path: "/a/b", Diff: {y: 2}}`
  - Participant 2: `/a/c` → add to buffer: `{Path: "/a/c", Diff: {z: 3}}`
- **At commit time**: Merge all patches into root diff
  - Merge all paths into single root diff: `{a: {b: {y: 2}, c: {z: 3}}}`
  - Write entry: `{Path: "/", Diff: {a: {b: {y: 2}, c: {z: 3}}}}`
  - Index all paths to this root entry with ExtractPath:
    - `/a` → ExtractPath = "a"
    - `/a/b` → ExtractPath = "a.b"
    - `/a/c` → ExtractPath = "a.c"

**Benefits**:
- No contention during transaction (each participant adds independently)
- Single-threaded merge at commit time (serialized by commit)
- All parent diffs computed atomically before write

**Contention**: Only at commit time, but commits are already serialized, so no additional contention.

### Root Path Contention

**Problem**: Root path `/` is parent of everything:
- Write `/a/b` → needs diff at `/` with `{a: {b: {...}}}`
- Write `/x/y` → needs diff at `/` with `{x: {y: {...}}}`
- Write `/z` → needs diff at `/` with `{z: {...}}`

**At commit time**: Need to merge all root-level changes:
```go
// All paths in transaction
/a/b → root diff: {a: {b: {...}}}
/x/y → root diff: {x: {y: {...}}}
/z   → root diff: {z: {...}}

// Merge at commit
rootDiff := {
    a: {b: {...}},
    x: {y: {...}},
    z: {...}
}
```

**Contention**: 
- **Within transaction**: None (each participant adds independently)
- **At commit**: Single-threaded merge (already serialized by commit)
- **Root path**: Most work, but still single-threaded at commit time

**Conclusion**: No additional contention beyond what already exists (commits are serialized).

## Level Switching (Question 2)

**User's Insight**: "We need to be able to switch between levels at which we apply diffs from the stored data easily."

**What this means**:
- Read `/a` → get full diff `{b: {...}, c: {...}}`
- Read `/a/b` → extract `b` from same entry
- Read `/a/c` → extract `c` from same entry
- But also: Combine diffs from different commits:
  - Commit 1: `/a` → `{x: 1}`
  - Commit 2: `/a` → `{b: {y: 2}}`
  - Read `/a` at commit 2 → apply both: `{x: 1, b: {y: 2}}`

**Implementation**: 
- **Index structure**: Points to entries with `ExtractKey`
- **Read logic**: Can read at parent level and extract children, or read children and reconstruct parent
- **Flexibility**: Same entry supports reading at multiple levels

**Example**:
```go
// Entry at offset 200: {Path: "/a", Diff: {b: {y: 2}, c: {z: 3}}}

// Read /a
segments := index.Query("/a", commit=2)
// Returns: [{LogPosition: 200, ExtractKey: ""}]
entry := ReadEntryAt(200)
return entry.Diff  // {b: {y: 2}, c: {z: 3}}

// Read /a/b
segments := index.Query("/a/b", commit=2)
// Returns: [{LogPosition: 200, ExtractKey: "b"}]
entry := ReadEntryAt(200)
return entry.Diff.GetField("b")  // {y: 2}

// Read /a/c
segments := index.Query("/a/c", commit=2)
// Returns: [{LogPosition: 200, ExtractKey: "c"}]
entry := ReadEntryAt(200)
return entry.Diff.GetField("c")  // {z: 3}
```

**Level Switching**: Same entry, different extraction. Easy to switch between reading parent vs child.

## Design Implications

### Write Path (No Contention)

1. **During transaction**: Participants add patches independently
   ```go
   buffer.AddPatch("/a/b", diff={y: 2})
   buffer.AddPatch("/a/c", diff={z: 3})
   buffer.AddPatch("/x/y", diff={w: 4})
   ```

2. **At commit time** (single-threaded, serialized by commit, merge to root):
   ```go
   // Merge all patches into root diff
   rootDiff := &ir.Node{Type: ir.ObjectType}
   
   for patch := range buffer.patches {
       // Merge patch.Path into root diff
       mergePathIntoRoot(rootDiff, patch.Path, patch.Diff)
   }
   
   // Write ONE entry at root (Path field removed - always root)
   entry := LogEntry{Diff: rootDiff, Commit: commit, Seq: txSeq, Timestamp: timestamp}
   logPosition := AppendEntry(entry)
   
   // Index all paths to root entry with ExtractPath
   for patch := range buffer.patches {
       extractPath := pathToExtractPath(patch.Path)  // "/a/b" → "a.b"
       index.Add(patch.Path, logPosition, ExtractPath: extractPath)
   }
   
   // Also index root
   index.Add("/", logPosition, ExtractPath: "")
   ```

3. **Root path**: Always write at root (simplest, consistent)
   ```go
   // Merge all patches into root diff
   rootDiff := mergeAllPatchesToRoot(allPatches)
   // Write ONE entry at root, index all paths to it
   ```

### Read Path (Level Switching)

1. **Query index**: Get segments with `ExtractKey`
2. **Read entry**: Read at `LogPosition`
3. **Extract if needed**: If `ExtractKey != ""`, extract from parent diff
4. **Combine**: Apply multiple entries in commit order

**Level switching**: Same entry supports multiple read levels via `ExtractKey`.

## Conclusion

### Write Contention: ✅ Resolved

- **Within transaction**: No contention (independent patches)
- **At commit**: Single-threaded merge (already serialized by commit)
- **Root path**: Most work, but still single-threaded
- **No additional contention**: Commits are already serialized

### Level Switching: ✅ Supported

- **Same entry**: Supports reading at parent and child levels
- **ExtractKey**: Allows extracting children from parent diff
- **Flexible**: Easy to switch between reading `/a` vs `/a/b` from same entry

### Multiple Children: ✅ Handled

- **Same commit**: Write one entry at `/a` with `{b: {...}, c: {...}}`
- **Index**: Maps both `/a/b` and `/a/c` to same entry with different `ExtractKey`
- **Efficient**: One entry instead of multiple

## Recommendation

**Adopt subtree-inclusive parent diff approach**:
1. ✅ No write contention (commits serialized, merge at commit time)
2. ✅ Level switching supported (ExtractKey allows reading at different levels)
3. ✅ Multiple children handled (merge into single parent diff)

**Implementation**:
- Accumulate patches in transaction buffer
- At commit: Compute parent diffs, write entries, index with `ExtractKey`
- Root path: Special case, but still single-threaded
