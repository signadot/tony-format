# Parent Diff Indexing: Design Question

## The Question

When we write to `/a` at commit 1 and `/a/b` at commit 2, and read `/a/b` at commit 2:

**User's Insight**: 
- The diff/patch at `/a` should include the subtree change: `{b: !insert ...}`
- The index should point BOTH `/a/b` AND `/a` to the same log entry (or related entries)

**Current Thinking** (from sanity check):
- Each path gets its own log entry
- Parent diffs are computed separately
- Index maps each path to its own log entries

## Example Scenario

### Writes:
- Commit 1: Write diff at `/a` (e.g., `{x: 1}`)
- Commit 2: Write diff at `/a/b` (e.g., `{y: 2}`)

### Read `/a/b` at commit 2:

**What should happen?**
- Read `/a` at commit 2 → should include change from `/a/b`
- Read `/a/b` at commit 2 → should get the value

**User's Approach**:
- When writing `/a/b` at commit 2, also write/update diff at `/a` that includes: `{b: <diff from /a/b>}`
- Index maps `/a` → log entry (contains `{b: ...}`)
- Index maps `/a/b` → same log entry (or references it)

## Two Possible Designs

### Design A: Separate Entries (Current Thinking)

**How it works**:
- Write `/a` at commit 1 → log entry 1: `{Path: "/a", Diff: {x: 1}}`
- Write `/a/b` at commit 2 → log entry 2: `{Path: "/a/b", Diff: {y: 2}}`
- Write computed parent diff at `/a` → log entry 3: `{Path: "/a", Diff: {b: {y: 2}}}`

**Index**:
- `/a` → [log entry 1, log entry 3]
- `/a/b` → [log entry 2]

**Read `/a/b` at commit 2**:
- Query index for `/a/b` → get log entry 2
- Read log entry 2 → `{y: 2}`

**Read `/a` at commit 2**:
- Query index for `/a` → get log entries 1, 3
- Read entries, apply in order → `{x: 1, b: {y: 2}}`

**Problem**: Two separate entries for `/a` (commit 1 and commit 2), need to apply both.

### Design B: Subtree-Inclusive Parent Diff (User's Suggestion)

**How it works**:
- Write `/a` at commit 1 → log entry 1: `{Path: "/a", Diff: {x: 1}}`
- Write `/a/b` at commit 2 → log entry 2: `{Path: "/a", Diff: {b: {y: 2}}}` (parent diff includes subtree)

**Index**:
- `/a` → [log entry 1, log entry 2]
- `/a/b` → [log entry 2] (points to same entry, extracts `b` part)

**Read `/a/b` at commit 2**:
- Query index for `/a/b` → get log entry 2
- Read log entry 2 → `{b: {y: 2}}`
- Extract `b` part → `{y: 2}`

**Read `/a` at commit 2**:
- Query index for `/a` → get log entries 1, 2
- Read entries, apply in order → `{x: 1, b: {y: 2}}`

**Benefit**: Single entry covers both `/a` and `/a/b`, index points both paths to it.

## Key Differences

### Design A: Separate Entries
- **Pros**: 
  - Clear separation: each path has its own entries
  - Simpler indexing: one path → one set of entries
- **Cons**:
  - Need to write separate parent diff entry
  - More entries in log
  - Need to apply multiple entries for parent

### Design B: Subtree-Inclusive Parent Diff
- **Pros**:
  - Single entry covers parent and child
  - Index can point both paths to same entry
  - Fewer entries in log
  - More efficient reads (one entry instead of two)
- **Cons**:
  - Need to extract subtree from parent diff
  - Index needs to handle "extract `b` from entry at `/a`"
  - More complex indexing logic

## Implementation Questions

### For Design B:

1. **Log Entry Structure**:
   ```go
   type LogEntry struct {
       Commit    int64      // Commit number
       Seq       int64      // Transaction sequence
       Timestamp string     // Timestamp
       Diff      *ir.Node   // Root diff (always at "/") - {a: {b: {y: 2}}}
       // Path field removed - always root "/"
   }
   ```

2. **Index Structure**:
   ```go
   // Index maps /a/b → log entry at /a, with "extract b" instruction?
   type LogSegment struct {
       RelPath     string  // "/a/b"
       LogPosition int64   // Points to entry at /a
       ExtractPath string  // "b" (extract this key from parent diff)
   }
   ```

3. **Read Logic**:
   ```go
   // Read /a/b at commit 2
   segments := index.Query("/a/b", commit=2)
   // Returns: [{LogPosition: 200, ExtractPath: "b"}]
   
   entry := ReadEntryAt(logFile, 200)
   // Entry.Path = "/a", Entry.Diff = {b: {y: 2}}
   
   // Extract b part
   childDiff := entry.Diff.GetField("b")  // {y: 2}
   ```

### Alternative: Multiple Index Entries, Same Log Entry

**Index Structure**:
```go
// Index maps both paths to same log entry
/a   → [{LogPosition: 200, Path: "/a"}]
/a/b → [{LogPosition: 200, Path: "/a/b", ExtractKey: "b"}]
```

**Read Logic**:
```go
// Read /a/b
segments := index.Query("/a/b", commit=2)
// Returns: [{LogPosition: 200, ExtractKey: "b"}]

entry := ReadEntryAt(logFile, 200)
// Entry.Path = "/a", Entry.Diff = {b: {y: 2}}

if segment.ExtractKey != "" {
    // Extract child from parent diff
    childDiff := entry.Diff.GetField(segment.ExtractKey)
    return childDiff
} else {
    return entry.Diff
}
```

## The Real Question

**When writing `/a/b` at commit 2:**

1. **Do we write one entry or two?**
   - One entry at `/a` with `{b: <diff>}`? (Design B)
   - Two entries: one at `/a/b`, one at `/a`? (Design A)

2. **How does the index handle this?**
   - Map `/a/b` → entry at `/a` with extraction instruction?
   - Map `/a/b` → separate entry at `/a/b`?
   - Map both paths to same entry?

3. **How do we read `/a/b`?**
   - Read entry at `/a`, extract `b` part?
   - Read entry at `/a/b` directly?

## User's Insight (Reinterpreted)

**The key insight**: When `/a/b` changes, the diff at `/a` should include the subtree change `{b: ...}`. The index should allow reading `/a/b` by pointing to the entry at `/a` and extracting the `b` part.

**This means**:
- Write one entry at `/a` with subtree-inclusive diff
- Index maps `/a` → entry (read entire diff)
- Index maps `/a/b` → same entry (read and extract `b` part)

**Benefits**:
- Single source of truth: parent diff includes all child changes
- Efficient: one entry instead of multiple
- Consistent: reading `/a` and `/a/b` from same entry ensures consistency

## Answers to Open Questions

1. **What if multiple children change in same commit?** ✅ **YES**
   - Write `/a/b` and `/a/c` at commit 2
   - Parent diff at `/a`: `{b: {...}, c: {...}}` (merged into single entry)
   - Index maps both `/a/b` and `/a/c` to same entry with different `ExtractKey`

2. **What if child changes in later commit?** ✅ **Level Switching**
   - Write `/a` at commit 1: `{x: 1}`
   - Write `/a/b` at commit 2: `{y: 2}`
   - Parent diff at `/a` at commit 2: `{b: {y: 2}}`
   - **Solution**: Apply entries in commit order, can switch between levels easily
   - Read `/a` at commit 2: Apply commit 1 entry (`{x: 1}`), then commit 2 entry (`{b: {y: 2}}`)
   - Read `/a/b` at commit 2: Extract `b` from commit 2 entry (`{y: 2}`)
   - **Level switching**: Same entry supports reading at parent and child levels via `ExtractKey`

3. **Write contention?** ✅ **No Additional Contention**
   - **Within transaction**: No contention (participants add patches independently)
   - **At commit**: Single-threaded merge (commits already serialized)
   - **Root path**: Most work, but still single-threaded at commit time
   - **Solution**: Accumulate patches in transaction buffer, compute parent diffs at commit time
   - See `parent_diff_write_contention.md` for detailed analysis

4. **Index structure**: ✅ **Add `ExtractKey` to `LogSegment`**
   - `ExtractKey`: "" if reading parent, "b" if reading `/a/b`
   - Allows level switching: same entry supports multiple read levels

## Recommendation (User's Insight)

**Adopt Design B (Subtree-Inclusive Parent Diff)**:

1. **Write**: When `/a/b` changes, write diff at `/a` with `{b: <diff>}` (subtree-inclusive)
2. **Index**: Map both `/a` and `/a/b` to the same log entry
3. **Read**: 
   - Reading `/a`: Read entry, use entire diff
   - Reading `/a/b`: Read same entry, extract `b` part from parent diff

**Key Insight**: The diff at `/a` includes the subtree change `{b: !insert ...}`. The index points both `/a` and `/a/b` to the same log entry.

**Index Structure** (needs `LogPosition` and `ExtractPath` fields):
```go
type LogSegment struct {
    RelPath     string  // "/a/b/c" (the path being queried)
    LogPosition int64   // Points to entry at root "/"
    ExtractPath string  // "a.b.c" (full path from root to extract; "" if reading root)
    StartCommit int64
    EndCommit   int64
    StartTx     int64
    EndTx       int64
}
```

**Why ExtractPath (not ExtractKey)?**
- We write ONE entry at root `/` per commit (no repetition, simpler logic)
- Need to extract nested paths (e.g., "a.b.c" from root entry)
- ExtractPath is full path from root to queried path
- Always write at root: simpler logic, consistent structure

**Write Example** (merge to root):
- Write `/a/b` at commit 2 → Write ONE entry at `/` with `Diff: {a: {b: {y: 2}}}`
- Index entry 1: `{RelPath: "/a", LogPosition: 200, ExtractPath: "a"}` (extract "a")
- Index entry 2: `{RelPath: "/a/b", LogPosition: 200, ExtractPath: "a.b"}` (extract "a.b")

**Read Example**:
- Read `/a/b` at commit 2:
  - Query index → get `{LogPosition: 200, ExtractPath: "a.b"}`
  - Read entry at offset 200 → `{Diff: {a: {b: {y: 2}}}}` (always at root "/")
  - Extract path "a.b" from root diff → `{y: 2}`

**Nested Path Example**:
- Write `/a/b/c` at commit 2 → Write ONE entry with `Diff: {a: {b: {c: {z: 3}}}}` (always at root)
- Index entry: `{RelPath: "/a/b/c", LogPosition: 200, ExtractPath: "a.b.c"}`
- Read `/a/b/c`: Extract path "a.b.c" from root entry → `{z: 3}`

**Multiple Paths Example**:
- Write `/a/b` and `/a/b/c` at commit 2 → Write ONE entry at `/` with merged `Diff: {a: {b: {y: 2, c: {z: 3}}}}`
- Index entries point to same root entry with different ExtractPath values

**This aligns with user's insight**: Parent diff includes subtree, index points both paths to it.
