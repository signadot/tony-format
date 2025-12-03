# Merge to Root: Why Not Merge Everything to `/`?

## The Question

**User's Question**: "Why does the example not merge to the root?"

If we're writing `/a/b` and `/a/b/c` at commit 2, why merge to `/a` instead of `/` (root)?

## Current Example

**Write**: `/a/b` and `/a/b/c` at commit 2
- ONE entry at `/a`: `{b: {y: 2, c: {z: 3}}}` (merged)
- ExtractPath for `/a/b`: "b"
- ExtractPath for `/a/b/c`: "b.c"

**Why `/a`?** Because it's the topmost common parent of both paths.

## Alternative: Merge to Root

**Write**: `/a/b` and `/a/b/c` at commit 2
- ONE entry at `/`: `{a: {b: {y: 2, c: {z: 3}}}}` (merged at root)
- ExtractPath for `/a`: "a"
- ExtractPath for `/a/b`: "a.b"
- ExtractPath for `/a/b/c`: "a.b.c"

**Benefit**: Even simpler - always write at root, no need to find common parent.

## Analysis: Merge to Root vs Merge to Common Parent

### Option A: Merge to Common Parent (Current)

**Write**: `/a/b` and `/a/b/c` at commit 2
- Entry at `/a`: `{b: {y: 2, c: {z: 3}}}`

**Write**: `/a/b` and `/x/y` at commit 2
- Entry at `/`: `{a: {b: {...}}, x: {y: {...}}}` (root is common parent)

**Pros**:
- Smaller diffs (don't include unnecessary parent levels)
- More efficient reads (shorter ExtractPath)

**Cons**:
- Need to find common parent (more complex logic)
- Different entry paths for different commits

### Option B: Always Merge to Root

**Write**: `/a/b` and `/a/b/c` at commit 2
- Entry at `/`: `{a: {b: {y: 2, c: {z: 3}}}}`

**Write**: `/a/b` and `/x/y` at commit 2
- Entry at `/`: `{a: {b: {...}}, x: {y: {...}}}`

**Pros**:
- Simpler logic (always write at root)
- Consistent (all entries at same path)
- No need to find common parent

**Cons**:
- Larger diffs (include all parent levels)
- Longer ExtractPath (e.g., "a.b.c" instead of "b.c")
- Root entry might get very large

## The Real Question: What's the Trade-off?

### Read Efficiency

**Merge to Common Parent**:
- Read `/a/b/c` → ExtractPath = "b.c" (2 levels)
- Read `/a` → ExtractPath = "" (read entire diff)

**Merge to Root**:
- Read `/a/b/c` → ExtractPath = "a.b.c" (3 levels)
- Read `/a` → ExtractPath = "a" (1 level)

**Difference**: Root approach has longer ExtractPath, but extraction is still O(depth), so not a big difference.

### Write Efficiency

**Merge to Common Parent**:
- Need to find common parent of all paths
- More complex logic

**Merge to Root**:
- Always write at `/`
- Simpler logic

**Difference**: Root approach is simpler to implement.

### Storage Efficiency

**Merge to Common Parent**:
- Smaller diffs (only include necessary levels)
- Example: `/a` entry is `{b: {...}}` (smaller)

**Merge to Root**:
- Larger diffs (include all parent levels)
- Example: `/` entry is `{a: {b: {...}}}` (larger)

**Difference**: Root approach uses more storage, but probably negligible.

### Consistency

**Merge to Common Parent**:
- Different commits might write to different parent levels
- Example: Commit 1 writes to `/a`, Commit 2 writes to `/`

**Merge to Root**:
- All commits write to `/`
- Consistent structure

**Difference**: Root approach is more consistent.

## Recommendation: Merge to Root

**Why**: Simpler logic, consistent structure, minimal performance difference.

**Implementation**:
- Always write ONE entry at `/` (root) per commit
- Merge all paths into root diff
- ExtractPath is full path from root (e.g., "a.b.c")

**Example**:
```go
// Write /a/b and /a/b/c at commit 2
rootDiff := {
    a: {
        b: {
            y: 2,
            c: {z: 3}
        }
    }
}

entry := LogEntry{
    Path: "/",  // Always root
    Diff: rootDiff,
    Commit: 2
}

// Index
index.Add("/a", LogPosition: 200, ExtractPath: "a")
index.Add("/a/b", LogPosition: 200, ExtractPath: "a.b")
index.Add("/a/b/c", LogPosition: 200, ExtractPath: "a.b.c")
```

**Read**:
```go
// Read /a/b/c
segment := index.Query("/a/b/c")
// Returns: {LogPosition: 200, ExtractPath: "a.b.c"}

entry := ReadEntryAt(200)
// Entry.Path = "/"
// Entry.Diff = {a: {b: {y: 2, c: {z: 3}}}}

childDiff := entry.Diff.GetPath(segment.ExtractPath)  // Extract "a.b.c"
// Result: {z: 3}
```

## Answer: Merge to Root

**Why**: Simpler, consistent, minimal performance difference. Always write ONE entry at `/` per commit, merge all paths into root diff.
