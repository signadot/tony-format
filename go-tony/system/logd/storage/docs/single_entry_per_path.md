# Single Entry Per Path: Why Not Repeat Entries?

## The Question

**User's Insight**: "Why repeat the entries for every level?"

If we write `/a/b/c` at commit 2, do we really need:
- Entry at `/a/b` with `{c: {z: 3}}`
- Entry at `/a` with `{b: {c: {z: 3}}}`
- Entry at `/` with `{a: {b: {c: {z: 3}}}}`

That's a lot of repetition! Why not write ONE entry?

## Option A: One Entry Per Path (Current Thinking)

**Write**: `/a/b/c` at commit 2
- Write entry at `/a/b` with `{c: {z: 3}}`
- Index `/a/b/c` → points to `/a/b` entry, ExtractKey = "c"

**Write**: `/a/b` at commit 2
- Write entry at `/a` with `{b: {y: 2}}`
- Index `/a/b` → points to `/a` entry, ExtractKey = "b"

**Problem**: Multiple entries, one per level. Repetitive.

## Option B: One Entry Per Commit (User's Suggestion?)

**Write**: `/a/b/c` at commit 2
- Write ONE entry at `/a` with `{b: {c: {z: 3}}}`
- Index `/a` → points to `/a` entry, ExtractKey = ""
- Index `/a/b` → points to `/a` entry, ExtractPath = "b"
- Index `/a/b/c` → points to `/a` entry, ExtractPath = "b.c"

**Benefit**: Single entry, no repetition.

**But**: Now we need ExtractPath (not just ExtractKey) to extract nested values!

## Option C: One Entry Per Path, But At Topmost Parent

**Write**: `/a/b/c` at commit 2
- Write ONE entry at `/a` with `{b: {c: {z: 3}}}`
- Index `/a/b/c` → points to `/a` entry, ExtractPath = "b.c"

**Write**: `/a/b` at commit 2 (separate write)
- Write ONE entry at `/a` with `{b: {y: 2}}`
- Index `/a/b` → points to `/a` entry, ExtractPath = "b"

**Problem**: What if both `/a/b` and `/a/b/c` are written in same commit?
- Entry 1: `/a` with `{b: {y: 2}}`
- Entry 2: `/a` with `{b: {c: {z: 3}}}`
- Need to merge: `/a` with `{b: {y: 2, c: {z: 3}}}`

**Solution**: Merge at commit time (already doing this for multiple children).

## The Real Question: What Level Do We Write At?

### Current Assumption: Immediate Parent

**Write**: `/a/b/c`
- Write at `/a/b` (immediate parent)
- ExtractKey = "c" (single key)

### User's Suggestion: Topmost Parent (No Repetition)

**Write**: `/a/b/c`
- Write at `/a` (topmost parent that needs update)
- ExtractPath = "b.c" (nested path)

**Benefit**: 
- Single entry instead of multiple
- No repetition
- Simpler write logic

**Cost**:
- Need ExtractPath (not just ExtractKey)
- Need to extract nested values from parent diff

## Analysis: Which Approach?

### Scenario: Multiple Paths in Same Commit

**Write**: `/a/b` and `/a/b/c` at commit 2

**Option A (Immediate Parent)**:
- Entry 1: `/a` with `{b: {y: 2}}`
- Entry 2: `/a/b` with `{c: {z: 3}}`
- Index `/a/b` → Entry 1, ExtractKey = "b"
- Index `/a/b/c` → Entry 2, ExtractKey = "c"

**Option B (Topmost Parent)**:
- Entry 1: `/a` with `{b: {y: 2, c: {z: 3}}}` (merged)
- Index `/a/b` → Entry 1, ExtractPath = "b"
- Index `/a/b/c` → Entry 1, ExtractPath = "b.c"

**Option B is simpler**: One entry, merge children at commit time.

### Scenario: Different Depths

**Write**: `/a/b` and `/a/x/y` at commit 2

**Option A (Immediate Parent)**:
- Entry 1: `/a` with `{b: {y: 2}}`
- Entry 2: `/a/x` with `{y: {z: 3}}`
- Two entries (different parents)

**Option B (Topmost Parent)**:
- Entry 1: `/a` with `{b: {y: 2}, x: {y: {z: 3}}}` (merged)
- One entry (same parent)

**Option B is simpler**: One entry at `/a`, merge all children.

## Conclusion: Write At Topmost Common Parent

**Pattern**: Write ONE entry at the topmost parent that contains all changed paths.

**Example**:
- Write `/a/b` and `/a/b/c` → ONE entry at `/a` with merged `{b: {y: 2, c: {z: 3}}}`
- Write `/a/b` and `/a/x/y` → ONE entry at `/a` with merged `{b: {...}, x: {y: {...}}}`

**Index**:
- `/a` → ExtractPath = "" (read entire diff)
- `/a/b` → ExtractPath = "b" (extract `b` from diff)
- `/a/b/c` → ExtractPath = "b.c" (extract `b.c` from diff)

**ExtractPath Format**:
- Relative to entry path
- Empty string "" = read entire diff
- "b" = extract `b` field
- "b.c" = extract `b.c` nested path

**Implementation**:
```go
type LogSegment struct {
    RelPath     string  // "/a/b/c" (the path being queried)
    LogPosition int64   // Points to entry at topmost parent "/a"
    ExtractPath string  // "b.c" (relative path to extract from parent diff)
    // ... other fields
}
```

**Extraction Logic**:
```go
// Read /a/b/c
segment := index.Query("/a/b/c")
entry := ReadEntryAt(segment.LogPosition)
// Entry.Path = "/a"
// Entry.Diff = {b: {c: {z: 3}}}

if segment.ExtractPath == "" {
    return entry.Diff  // Read entire diff
} else {
    // Extract nested path "b.c" from diff
    return entry.Diff.GetPath(segment.ExtractPath)  // Extract "b.c"
}
```

## Answer: Use ExtractPath, Not ExtractKey

**Why**: We write ONE entry at topmost parent, need to extract nested paths.

**ExtractPath Format**: Relative path from entry path to queried path.
- Entry at `/a`, query `/a/b/c` → ExtractPath = "b.c"
- Entry at `/a`, query `/a/b` → ExtractPath = "b"
- Entry at `/a`, query `/a` → ExtractPath = ""

**Benefit**: No repetition, single entry per commit (at topmost parent).

## Updated Design: Merge to Root

**Write Pattern** (always merge to root):
- Write `/a/b/c` → ONE entry at `/` with `{a: {b: {c: {z: 3}}}}`
- Write `/a/b` and `/a/b/c` → ONE entry at `/` with merged `{a: {b: {y: 2, c: {z: 3}}}}`
- Write `/a/b` and `/x/y` → ONE entry at `/` with merged `{a: {b: {...}}, x: {y: {...}}}`

**Why Root?**
- Simpler logic (always write at `/`, no need to find common parent)
- Consistent structure (all entries at same path)
- Minimal performance difference (ExtractPath is longer but extraction is still O(depth))

**Index Structure**:
```go
type LogSegment struct {
    RelPath     string  // "/a/b/c" (the path being queried)
    LogPosition int64   // Points to entry at root "/"
    ExtractPath string  // "a.b.c" (full path from root to extract)
    StartCommit int64
    EndCommit   int64
    StartTx     int64
    EndTx       int64
}
```

**Extraction Logic**:
```go
// Read /a/b/c
segment := index.Query("/a/b/c")
entry := ReadEntryAt(segment.LogPosition)
// Entry.Path = "/" (always root)
// Entry.Diff = {a: {b: {c: {z: 3}}}}

if segment.ExtractPath == "" {
    return entry.Diff  // Read entire diff (root)
} else {
    // Extract nested path "a.b.c" from root diff
    return entry.Diff.GetPath(segment.ExtractPath)  // Extract "a.b.c"
}
```

**Key Change**: Always write at root `/`, use ExtractPath (full path from root) to extract nested values.
