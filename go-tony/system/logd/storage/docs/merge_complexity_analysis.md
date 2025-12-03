# Merge Complexity Analysis

## Question: Is merging all patches into root diff at commit time inefficient?

Let's compare the current design (merge at write time) with alternatives.

## Current Design: Merge at Write Time

**Operation**: Commit transaction with patches: `a.b`, `a.c`, `x.y.z`
1. Merge patches into root diff: `{a: {b: {...}, c: {...}}, x: {y: {z: {...}}}}`
2. Write ONE entry to log
3. Index all paths to this entry

**Cost**:
- Merge: O(number of patches × average depth)
- Write: 1 disk write
- Index: O(number of paths) index operations

**Example**:
- Patches: `a.b`, `a.c`, `x.y.z`
- Merge: Combine into `{a: {b: {...}, c: {...}}, x: {y: {z: {...}}}}`
- Write: 1 entry
- Index: 3 entries (`a.b`, `a.c`, `x.y.z`)

## Alternative 1: Write Separate Entries Per Path

**Operation**: Commit transaction with patches: `a.b`, `a.c`, `x.y.z`
1. Write separate entry for each path:
   - Entry 1: `{a: {b: {...}}}`
   - Entry 2: `{a: {c: {...}}}`
   - Entry 3: `{x: {y: {z: {...}}}}`
2. Index each entry separately

**Cost**:
- Merge: 0 (no merge needed)
- Write: N disk writes (N = number of patches)
- Index: O(number of paths) index operations

**Example**:
- Patches: `a.b`, `a.c`, `x.y.z`
- Write: 3 entries
- Index: 3 entries

**Comparison**:
- ✅ No merge cost
- ❌ **But**: Multiple disk writes (slower, not atomic)
- ❌ **But**: Reading `a` requires reading entries 1 and 2 and merging
- ❌ **But**: More log entries (harder to compact, more to scan)

**Verdict**: Faster write (no merge), but:
- Slower reads (must merge multiple entries)
- Not atomic (multiple writes)
- More log entries

## Alternative 2: Merge at Read Time

**Operation**: Commit transaction with patches: `a.b`, `a.c`, `x.y.z`
1. Write separate entries (no merge)
2. Index each entry separately

**Read Operation**: Read `a` at commit 2
1. Query index → get entries for `a.b` and `a.c`
2. Read both entries
3. Parse both entries
4. Merge: `{a: {b: {...}}} + {a: {c: {...}}} → {a: {b: {...}, c: {...}}}`

**Cost** (write):
- Merge: 0
- Write: N disk writes
- Index: O(number of paths)

**Cost** (read):
- Read: N disk reads (N = number of paths)
- Parse: N parses
- Merge: O(number of paths × depth)

**Comparison**:
- ✅ No merge cost at write time
- ❌ **But**: Merge cost at read time (done many times)
- ❌ **But**: Multiple disk reads per read operation
- ❌ **But**: More log entries to scan

**Verdict**: Faster writes, but MUCH slower reads (merge done repeatedly)

## Alternative 3: Lazy Merge (Merge on First Read)

**Operation**: Commit transaction with patches: `a.b`, `a.c`, `x.y.z`
1. Write separate entries (no merge)
2. Index each entry separately
3. On first read of `a`, merge entries and cache result

**Cost** (write):
- Merge: 0
- Write: N disk writes
- Index: O(number of paths)

**Cost** (read, first time):
- Read: N disk reads
- Parse: N parses
- Merge: O(number of paths × depth)
- Cache: Store merged result

**Cost** (read, subsequent):
- Read: 0 (cached)
- Parse: 0 (cached)
- Merge: 0 (cached)

**Comparison**:
- ✅ No merge cost at write time
- ✅ Fast reads after cache warm-up
- ❌ **But**: Cache invalidation complexity
- ❌ **But**: Multiple disk writes (not atomic)
- ❌ **But**: Cache management overhead

**Verdict**: Good if reads are repeated, but adds complexity

## Analysis: Is Current Design Actually Inefficient?

### For Write Performance

**Current**: Merge at write time
- Merge cost: O(patches × depth) - done once
- Write cost: 1 disk write - atomic
- Index cost: O(paths)

**Alternative (separate entries)**: No merge at write
- Merge cost: 0
- Write cost: N disk writes - not atomic
- Index cost: O(paths)

**Verdict**: 
- Current: Higher CPU cost (merge), but atomic write
- Alternative: Lower CPU cost, but multiple writes (slower, not atomic)

**Trade-off**: Merge cost is typically small (few patches, shallow depth), and atomic write is valuable

### For Read Performance

**Current**: Merge already done
- Read: 1 disk read
- Parse: 1 parse
- Extract: O(depth)

**Alternative (separate entries)**: Merge at read time
- Read: N disk reads
- Parse: N parses
- Merge: O(patches × depth)

**Verdict**: Current design is MUCH more efficient (one read vs many reads)

### For Transaction Atomicity

**Current**: Single write = atomic
- ✅ All-or-nothing (single write succeeds or fails)

**Alternative**: Multiple writes
- ❌ Partial writes possible (some succeed, some fail)
- ❌ Need complex rollback logic

**Verdict**: Current design is better (atomicity is critical)

### For Log Size

**Current**: One entry per commit
- Log entries: O(commits)

**Alternative**: Multiple entries per commit
- Log entries: O(commits × paths)

**Verdict**: Current design is better (fewer entries, easier to compact)

## Conclusion: Current Design is Actually MORE Efficient

**Why**:
1. **Merge once vs many times**: Merge at write (once) vs merge at read (many times)
2. **Atomic writes**: Single write is atomic, multiple writes are not
3. **Fewer disk operations**: One write vs many writes
4. **Better read performance**: One read gets all paths vs many reads
5. **Smaller log**: One entry per commit vs many entries

**Trade-off**:
- Slightly more CPU at write time (merge cost)
- But this is offset by:
  - Much better read performance (no merge needed)
  - Atomic writes (critical for correctness)
  - Smaller log (easier to manage)

**When Alternative Might Be Better**:
- If writes are very frequent and reads are very rare
- If merge cost is prohibitive (very large transactions)
- But even then, atomicity concerns may outweigh performance

**Recommendation**: 
- ✅ Current design is efficient and correct
- ⚠️ Merge cost is typically small (few patches, shallow depth)
- ⚠️ If merge becomes bottleneck, consider:
  - Transaction size limits (prevent very large transactions)
  - Streaming merge for very large transactions (future optimization)
  - But don't sacrifice atomicity for performance
