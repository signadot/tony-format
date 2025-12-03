# ExtractKey vs ExtractPath: Which Do We Need?

## The Question

When reading `/a/b/c`, do we need:
- **ExtractKey**: Just `"c"` (single key to extract from parent diff)
- **ExtractPath**: Full path `"/a/b/c"` (full path to extract)

## Scenario Analysis

### Scenario 1: Simple Two-Level Path

**Write**: `/a/b` at commit 2
- Write entry at `/a` with `{b: {y: 2}}`
- Index `/a/b` → points to entry at `/a`, extract `"b"`

**Read**: `/a/b` at commit 2
- Query index → get `{LogPosition: 200, ExtractKey: "b"}`
- Read entry at `/a` → `{b: {y: 2}}`
- Extract `"b"` → `{y: 2}` ✅

**Answer**: `ExtractKey = "b"` is sufficient (single key)

### Scenario 2: Three-Level Path

**Write**: `/a/b/c` at commit 2

**Question**: What entry do we write?
- **Option A**: Write entry at `/a` with `{b: {c: {z: 3}}}`
- **Option B**: Write entry at `/a/b` with `{c: {z: 3}}`

**User's Insight**: "The diff/patch at `/a` should include the subtree change `{b: !insert ...}`"

This suggests we write at the **immediate parent** level:
- Write `/a/b/c` → entry at `/a/b` with `{c: {z: 3}}`
- Index `/a/b/c` → points to entry at `/a/b`, extract `"c"`

**Read**: `/a/b/c` at commit 2
- Query index → get `{LogPosition: 200, ExtractKey: "c"}`
- Read entry at `/a/b` → `{c: {z: 3}}`
- Extract `"c"` → `{z: 3}` ✅

**Answer**: `ExtractKey = "c"` is sufficient (single key)

### Scenario 3: Multiple Parent Levels

**Write**: `/a/b/c` at commit 2

**Question**: Do we write entries at multiple parent levels?
- Entry at `/a/b` with `{c: {z: 3}}` (immediate parent)
- Entry at `/a` with `{b: {c: {z: 3}}}` (grandparent)

**If we write at multiple levels**:
- Index `/a/b` → points to entry at `/a`, extract `"b"`
- Index `/a/b/c` → points to entry at `/a/b`, extract `"c"`

**Read**: `/a/b/c` at commit 2
- Query index → get `{LogPosition: 300, ExtractKey: "c"}` (points to `/a/b` entry)
- Read entry at `/a/b` → `{c: {z: 3}}`
- Extract `"c"` → `{z: 3}` ✅

**Answer**: Still `ExtractKey = "c"` (single key)

### Scenario 4: What If Entry Is At Grandparent?

**Hypothetical**: What if we only wrote at `/a` level?
- Write `/a/b/c` → entry at `/a` with `{b: {c: {z: 3}}}`

**Read**: `/a/b/c` at commit 2
- Query index → get `{LogPosition: 200, ExtractKey: ???}`
- Read entry at `/a` → `{b: {c: {z: 3}}}`
- Need to extract `b.c` → but `ExtractKey` is single key!

**Problem**: If entry is at grandparent, we need to extract nested path `"b.c"`, not just `"c"`.

**Solution Options**:
1. **Always write at immediate parent** → `ExtractKey` (single key) is sufficient
2. **Write at multiple levels** → `ExtractKey` (single key) is sufficient
3. **Write at arbitrary level** → Need `ExtractPath` (full path) to extract nested value

## The Real Question: Where Do We Write Parent Diffs?

### Option A: Write at Immediate Parent Only

**Write**: `/a/b/c` at commit 2
- Write entry at `/a/b` with `{c: {z: 3}}`
- Index `/a/b/c` → points to `/a/b` entry, `ExtractKey = "c"`

**Write**: `/a/b` at commit 2
- Write entry at `/a` with `{b: {y: 2}}`
- Index `/a/b` → points to `/a` entry, `ExtractKey = "b"`

**Read**: `/a/b/c` at commit 2
- Query index → get entry at `/a/b`, extract `"c"` ✅

**Answer**: `ExtractKey` (single key) is sufficient

### Option B: Write at All Parent Levels

**Write**: `/a/b/c` at commit 2
- Write entry at `/a/b` with `{c: {z: 3}}`
- Write entry at `/a` with `{b: {c: {z: 3}}}` (computed from child)
- Index `/a/b/c` → points to `/a/b` entry, `ExtractKey = "c"`
- Index `/a/b` → points to `/a` entry, `ExtractKey = "b"`

**Read**: `/a/b/c` at commit 2
- Query index → get entry at `/a/b`, extract `"c"` ✅

**Answer**: `ExtractKey` (single key) is sufficient

### Option C: Write at Arbitrary Level (e.g., Root)

**Write**: `/a/b/c` at commit 2
- Write entry at `/` (root) with `{a: {b: {c: {z: 3}}}}`

**Read**: `/a/b/c` at commit 2
- Query index → get entry at `/`, need to extract `"a.b.c"` ❌

**Problem**: Need `ExtractPath = "/a/b/c"` to extract nested value from root entry.

**Answer**: Need `ExtractPath` (full path) if entries can be at arbitrary levels

## Analysis: Which Approach Do We Use?

### Current Design (From User's Insight)

**User's Insight**: "When we write to `/a` at commit 1 and `/a/b` at commit 2, and read `/a/b` at 2 I think we should have written the diff/patch at `/a` in a form which includes the subtree path `{b: !insert ...}`"

This suggests:
- Write `/a/b` → entry at `/a` with `{b: {...}}`
- Write `/a/b/c` → entry at `/a/b` with `{c: {...}}` (immediate parent)

**Pattern**: Write at **immediate parent** level, not at arbitrary levels.

### Why Immediate Parent?

1. **Efficiency**: Don't need to write multiple entries
2. **Simplicity**: Each child path points to its immediate parent entry
3. **Consistency**: Reading `/a/b` and `/a/b/c` both work the same way

### Conclusion: ExtractKey is Sufficient

**Reasoning**:
- We write entries at **immediate parent** level
- When reading `/a/b/c`, we point to entry at `/a/b` (immediate parent)
- We extract the immediate child key `"c"` from that entry
- No need for full path extraction

**Example**:
```go
// Write /a/b/c
entry := LogEntry{
    Path: "/a/b",  // Immediate parent
    Diff: {c: {z: 3}}
}

// Index /a/b/c
index.Add("/a/b/c", LogPosition: 200, ExtractKey: "c")

// Read /a/b/c
segment := index.Query("/a/b/c")
// Returns: {LogPosition: 200, ExtractKey: "c"}

entry := ReadEntryAt(200)
// Entry.Path = "/a/b", Entry.Diff = {c: {z: 3}}

childDiff := entry.Diff.GetField(segment.ExtractKey)  // Extract "c"
// Result: {z: 3}
```

## Edge Case: What If Multiple Children at Different Depths?

**Write**: `/a/b` and `/a/b/c` at commit 2

**Question**: Where do we write entries?

**Option 1**: Write separate entries
- Entry at `/a` with `{b: {y: 2}}` (for `/a/b`)
- Entry at `/a/b` with `{c: {z: 3}}` (for `/a/b/c`)

**Option 2**: Write merged entry
- Entry at `/a` with `{b: {y: 2, c: {z: 3}}}` (merged)

**If Option 1** (separate entries):
- Index `/a/b` → points to `/a` entry, `ExtractKey = "b"`
- Index `/a/b/c` → points to `/a/b` entry, `ExtractKey = "c"`
- Both use `ExtractKey` ✅

**If Option 2** (merged entry):
- Index `/a/b` → points to `/a` entry, `ExtractKey = "b"`
- Index `/a/b/c` → points to `/a` entry, need to extract `"b.c"` ❌

**Conclusion**: If we merge, we might need `ExtractPath`. But merging at different depths is complex, so we probably use Option 1 (separate entries).

## Updated Answer (After User's "Why Repeat?" Question)

**Use `ExtractPath` (relative path), not `ExtractKey` (single key)**

**Reasoning**:
1. We write entries at **topmost parent** level (no repetition)
2. When reading a path, we point to the topmost parent entry
3. We extract the relative path from entry path to queried path
4. Need nested path extraction (e.g., "b.c" from entry at "/a")

**Why**: Avoids repeating entries at every level. Write ONE entry at topmost parent, extract nested paths.

**Implementation**:
```go
type LogSegment struct {
    RelPath     string  // "/a/b/c" (the path being queried)
    LogPosition int64   // Points to entry at immediate parent "/a/b"
    ExtractKey  string  // "c" (immediate child key to extract)
    // ... other fields
}
```

**Extraction Logic**:
```go
// Read /a/b/c
segment := index.Query("/a/b/c")
entry := ReadEntryAt(segment.LogPosition)
// Entry.Path = "/a/b" (immediate parent)
// Entry.Diff = {c: {z: 3}}

childDiff := entry.Diff.GetField(segment.ExtractKey)  // Extract "c"
// Result: {z: 3}
```

**Why not ExtractPath?**
- Unnecessary complexity (we always extract immediate child)
- More storage (full path vs single key)
- Harder to implement (need path parsing for extraction)
