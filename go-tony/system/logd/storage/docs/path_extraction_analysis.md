# Path Extraction Efficiency Analysis

## Question: Is path extraction from root diffs inefficient?

Let's compare the current design (extract from root diff) with alternatives.

## Current Design: Extract from Root Diff

**Operation**: Read `a.b.c` at commit 2
1. Query index → get `LogPosition: 200, KindedPath: "a.b.c"`
2. Read entry at offset 200 (length-prefixed)
3. Parse entry from Tony format → `{a: {b: {y: 2, c: {z: 3}}, x: {w: 1}}}`
4. Extract `a.b.c` → traverse `a` → `b` → `c` → `{z: 3}`

**Cost**:
- Read: 1 disk read (entry at offset 200)
- Parse: 1 Tony parse (entire diff)
- Extract: O(depth) tree traversal (typically 3-5 levels)

## Alternative 1: Separate Entries Per Path

**Operation**: Read `a.b.c` at commit 2
1. Query index → get entries for `a.b.c` at commit 2
2. Read entry at offset 200 (separate entry for `a.b.c`)
3. Parse entry → `{z: 3}`

**Cost**:
- Read: 1 disk read (entry at offset 200)
- Parse: 1 Tony parse (smaller diff, just `{z: 3}`)
- Extract: None (already at target path)

**Comparison**:
- ✅ Parse cost: Smaller (only target path, not entire diff)
- ❌ **But**: Need separate entries for every path
- ❌ **But**: Reading parent `a.b` requires reading `a.b.c` entry AND merging with other children
- ❌ **But**: Reading `a` requires reading ALL child entries and merging

**Verdict**: More efficient for single-path reads, but LESS efficient for:
- Reading parent paths (must read multiple entries)
- Reading multiple paths (must read multiple entries)
- Index size (one entry per path vs one entry per commit)

## Alternative 2: Store Extracted Paths in Index

**Operation**: Read `a.b.c` at commit 2
1. Query index → get `LogPosition: 200, ExtractedValue: {z: 3}`
2. Done (value already extracted)

**Cost**:
- Read: 0 disk reads (value in index)
- Parse: 0 (value already parsed)
- Extract: 0 (already extracted)

**Comparison**:
- ✅ Fastest reads (no disk, no parse, no extract)
- ❌ **But**: Index stores entire values (not just pointers)
- ❌ **But**: Index size grows dramatically (stores all values, not just paths)
- ❌ **But**: Index must be updated on every write (extract all paths)
- ❌ **But**: Memory usage explodes (entire document state in memory)

**Verdict**: Fastest reads, but impractical due to:
- Index size (stores values, not just metadata)
- Memory usage (entire document in memory)
- Write overhead (extract all paths on every write)

## Alternative 3: Hybrid (Current Design + Caching)

**Operation**: Read `a.b.c` at commit 2
1. Check cache → miss
2. Query index → get `LogPosition: 200, KindedPath: "a.b.c"`
3. Read entry at offset 200
4. Parse entry → `{a: {b: {y: 2, c: {z: 3}}, x: {w: 1}}}`
5. Extract `a.b.c` → `{z: 3}`
6. Cache extracted value

**Cost** (first read):
- Same as current design

**Cost** (subsequent reads):
- Read: 0 (cached)
- Parse: 0 (cached)
- Extract: 0 (cached)

**Comparison**:
- ✅ Best of both worlds (efficient reads after cache warm-up)
- ✅ Index stays small (just metadata)
- ⚠️ **But**: Cache management overhead
- ⚠️ **But**: Cache invalidation on writes

**Verdict**: Good optimization if reads are repeated, but adds complexity

## Analysis: Is Current Design Actually Inefficient?

### For Single Path Reads

**Current**: Read entire diff, extract path
- Parse cost: O(size of entire diff)
- Extract cost: O(depth)

**Alternative (separate entries)**: Read only target path
- Parse cost: O(size of target path only)
- Extract cost: 0

**Verdict**: Alternative is more efficient for single-path reads

### For Parent Path Reads

**Current**: Read entire diff, extract parent path
- Parse cost: O(size of entire diff)
- Extract cost: O(depth - 1)

**Alternative (separate entries)**: Read multiple entries, merge
- Parse cost: O(sum of all child entries)
- Extract cost: 0, but merge cost: O(number of children)

**Verdict**: Current design is MORE efficient (one read vs multiple reads)

### For Multiple Path Reads

**Current**: Read one entry, extract multiple paths
- Parse cost: O(size of entire diff) - done once
- Extract cost: O(depth × number of paths)

**Alternative (separate entries)**: Read multiple entries
- Parse cost: O(sum of all entries) - done multiple times
- Extract cost: 0

**Verdict**: Current design is MORE efficient (one parse vs multiple parses)

### For Index Size

**Current**: One entry per commit
- Index entries: O(commits)

**Alternative (separate entries)**: One entry per path per commit
- Index entries: O(commits × paths)

**Verdict**: Current design is MORE efficient (smaller index)

## Conclusion: Current Design is Actually MORE Efficient

**Why**:
1. **Single read per commit**: One disk read gets all paths in that commit
2. **Single parse per commit**: Parse once, extract multiple paths
3. **Smaller index**: One entry per commit vs one per path per commit
4. **Better for parent reads**: One read gets parent + all children

**Trade-off**:
- Slightly more parse overhead for single-path reads (parse entire diff vs just target)
- But this is offset by:
  - Fewer disk reads (one vs many)
  - Smaller index (one entry vs many)
  - Better for common case (reading parents/multiple paths)

**Recommendation**: 
- ✅ Current design is efficient
- ⚠️ Consider caching if single-path reads dominate and are repeated
- ⚠️ Profile to validate (parse overhead may be negligible)
