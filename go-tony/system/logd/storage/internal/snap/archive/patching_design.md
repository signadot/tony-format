# Patching Design for Chunked Snapshots

## Overview

This document describes the design for applying patches to chunked snapshots while maintaining the constraint that snapshots can be too large for memory.

## Core Principle

**Rebuild everything, let compaction handle cleanup later.**

Rather than trying to preserve references to unchanged ranges, we rebuild all ranges. This simplifies the logic significantly, and compaction can clean up old chunks later.

## Algorithm

### Outer Loop: Per Chunked Container

For each chunked container in the index:

1. **Patch Collection**: Find all patches that affect this container (or its descendants)
   - Patches are processed **in external order** (cannot reorder)

2. **Track Container State**:
   - Track if container has been replaced (by a replacement patch)
   - If replaced, all subsequent patches targeting this container are ignored

3. **Categorize Patches** (respecting external order):
   - For each patch in order:
     - **If replacement patch** (replaces entire container):
       - Mark container as replaced
       - Write new container to sink
       - Skip remaining patches for this container
     - **If merge patch**:
       - Determine target child index (from kpath)
       - **If index falls within existing range**: Categorize to that range
       - **If index is between ranges or outside all ranges**: Mark as insertion with position
       - If container was replaced, skip this patch
   - **Categorization output**:
     - Map: range → list of patches (in order) affecting that range
     - List: insertions with positions (in order) - position specifies where relative to ranges

4. **Range Processing** (left-to-right, respecting patch order):
   - For each range in order:
     - **Before range**: Process insertions with position < range start
     - **Range processing**:
       - Get patches categorized to this range (in order)
       - **Project**: Transform patch kpaths from global to local (relative to range content)
       - **Load**: Read the range chunk
       - **Apply**: Apply projected patches in order to loaded data
       - **Stream**: Send result to sink
     - **After range**: Process insertions with position between this range and next
   - For unchanged ranges: Still load and stream to sink (no patches applied)
   - **Insertions**: Stream to sink at correct positions, maintaining order
   - Sink creates new ranges dynamically based on size

5. **Sink Mechanism**:
   - Receives children sequentially (in order)
   - Tracks cumulative size
   - **New capability**: Can group small nodes with nearby chunks
   - When `size >= threshold`: Finalize current range (write chunk, create `!snap-range` node with range descriptors), start new range
   - **Range descriptors**: Can add multiple `[from, to, offset, size]` descriptors to a single `!snap-range` node
   - Maintains order (objects: field/value pairs; arrays: elements)

6. **Finalize**: Write new index structure for container

## Key Components

- **Range Processor**: Iterates ranges left-to-right, categorizes/projects patches, loads/patches/streams
- **Patch Categorizer**: Determines which patches affect a given range
- **Patch Projector**: Transforms global kpaths to local kpaths within a range
- **Sink**: Streaming range builder that creates ranges dynamically based on actual data sizes

## Benefits

- **Simple**: No tracking of unchanged vs changed ranges
- **Memory-efficient**: Process one range at a time
- **Natural rebalancing**: Ranges rebuild based on actual data sizes
- **Compaction handles cleanup**: Old chunks deleted later
- **Flexible chunking**: Small nodes can be grouped with nearby chunks (new structure)

## Implementation Implications of New Structure

### Changes Required

1. **Remove `!snap-loc` handling**:
   - `load.go`: Remove `loadSnapLoc()` function
   - `from_ir.go`: Don't create `!snap-loc` nodes, use `!snap-range` with single descriptor

2. **Update `!snap-range` structure**:
   - **Tag**: Just `!snap-range` (no `(from, to)` arguments)
   - **Body**: Array of descriptors `[[from1, to1, offset1, size1], [from2, to2, offset2, size2], ...]`
   - Each descriptor: `[from, to, offset, size]` (4 int64 values)

3. **Update `load.go`**:
   - `loadSnapRange()`: Parse array of descriptors
   - Find descriptor matching target index (or load all descriptors)
   - Read chunk at `offset` with `size`
   - Parse and return container node

4. **Update `from_ir.go`**:
   - `buildIndexRecursive()`: Create `!snap-range` nodes with descriptor arrays
   - **New capability**: Group small nodes with nearby chunks
   - Sink can add multiple descriptors to same `!snap-range` node

5. **Update `from_snap.go`**:
   - Categorization: Check if patch index falls in any descriptor's `[from, to)` range
   - Projection: Use descriptor's `from` value to adjust indices
   - Loading: Find matching descriptor and load its chunk

6. **Sink enhancement**:
   - Can accumulate multiple small nodes and group them with a nearby large chunk
   - Create `!snap-range` node with multiple descriptors pointing to same chunk
   - **Grouping logic**: Multiple descriptors can share the same `offset` (point to same chunk)
   - Descriptors don't need to be contiguous - can have gaps between ranges that share the same chunk
   - Example: `[[100, 200, 1024, 4096], [300, 301, 1024, 4096]]` - two non-contiguous ranges in same chunk

## Revised Snapshot Structure

### New Design: Single `!snap-range` Node Type

**Changes from previous design**:
1. **Remove `!snap-loc`**: No longer needed, everything uses `!snap-range`
2. **Remove `(from, to)` from tag**: Tag is just `!snap-range` (no arguments)
3. **Add range list to body**: `!snap-range` node contains an array of range descriptors

**Structure**:
```
!snap-range [
  [from1, to1, offset1, size1],
  [from2, to2, offset2, size2],
  ...
]
```

Where:
- Each element is `[from, to, offset, size]` (4 int64 values)
- `from, to`: Indices in parent container (inclusive `from`, exclusive `to`)
- `offset, size`: Location and size of chunk data in the snapshot file
- **Constraint**: Multiple descriptors can share the same chunk (same `offset`), but they must be **contiguous** in the array
  - Example: `[[100, 200, 1024, 4096], [200, 250, 1024, 4096]]` ✅ (contiguous, same offset)
  - Example: `[[100, 200, 1024, 4096], [300, 400, 1024, 4096]]` ❌ (not contiguous, even if same offset)

**Benefits**:
- **Group small nodes**: Small nodes can be grouped with nearby large chunks into a single chunk
- **Single node type**: Simpler than having both `!snap-loc` and `!snap-range`
- **Flexible chunking**: Multiple non-contiguous ranges can share one chunk

**Example**:
- Container has children at indices 0-999
- `!snap-range [[100, 200, 1024, 4096], [250, 251, 5120, 128]]`
  - First range: children 100-199 in chunk at offset 1024, size 4096
  - Second range: child 250 (small node) grouped with first chunk at offset 5120, size 128

### Understanding Ranges (Updated)

A `!snap-range` node contains multiple range descriptors:
- Each descriptor: `[from, to, offset, size]`
- **`(from, to)`**: Indices in the parent container's `Values` array (inclusive `from`, exclusive `to`)
- **`(offset, size)`**: Location and size of chunk data
- **Loading**: When loading a range descriptor, read `size` bytes at `offset`, parse as container node containing children `from` to `to-1`

### Patch Structure

Patches are `[]*ir.Node` where each node has an **implicit kpath** based on its tree structure:
- Patch `{ "users": { "123": { "name": "Alice" } } }` means: set value at path `users.123.name` to `"Alice"`
- Patch `{ "items": [ { "id": 42 } ] }` means: set value at path `items[0].id` to `42`

### 1. Patch Categorization

**Question**: Given a range with `(from, to)` boundaries, which patches affect this range?

**Answer**: A patch affects a range if:
- The patch's kpath targets a child at index `i` where `from <= i < to`
- OR the patch targets an ancestor that contains this range (e.g., patch targets parent container)

**Challenges**:
- Need to extract kpath from patch node structure
- Need to map kpath to child index in parent container
- For objects: kpath like `users.123` → need to find which index corresponds to field `"123"`
- For arrays: kpath like `items[150]` → index is `150`

**Algorithm**:
1. **For root-level patches**: Traverse patch tree to find target node, call `.KPath()` on it
2. **For projected patches**: Call `.KPath()` on the sub-node (it's part of original tree)
3. **For ancestor patches**: Use index structure to determine kpath (range is under ancestor)
4. Parse kpath to determine target child index
5. Check if `from <= index < to` for any range descriptor in the `!snap-range` node

### 2. Patch Projection

**Question**: If a patch targets something inside a range, how do we transform it to a local kpath relative to the range content?

**Answer**: Remove the prefix that leads to the range, and adjust indices:

**For Arrays**:
- Global patch: `{ "items": [ { "id": 42 } ] }` targeting `items[150]`
- Range: `items[100:200]` (contains indices 100-199)
- Projected patch: `{ "id": 42 }` targeting index `[50]` within range (150 - 100 = 50)

**For Objects**:
- Global patch: `{ "users": { "123": { "name": "Alice" } } }` targeting `users.123.name`
- Range: Contains `users[50:150]` (field/value pairs at indices 50-149)
- Need to check: Does field `"123"` fall within this range?
- If yes: Projected patch: `{ "name": "Alice" }` (remove `users.123` prefix)
- Challenge: Need to map field name to index to determine if it's in range

**For Sparse Arrays**:
- Similar to objects, but keys are numeric indices
- Global patch: `{ "items": { "150": { "id": 42 } } }` targeting `items{150}`
- Range: `items[100:200]` (sparse indices 100-199)
- Projected patch: `{ "id": 42 }` (remove `items.150` prefix, but need to verify `150` is in range)

**Algorithm**:
1. **Get projected sub-node**: Traverse patch tree to find node corresponding to range content
   - Example: Patch `{ "users": { "123": { "name": "Alice" } } }`, range descriptor is `[100, 200, offset, size]`
   - Projected sub-node: `{ "123": { "name": "Alice" } }` (sub-tree under `users`)
2. **Call `.KPath()` on projected sub-node**: Gets relative kpath within range
   - Example: `"123.name"` (relative to range content)
3. **Adjust array indices**: If range is array, adjust indices: `index - from`
   - Example: Patch targets `items[150]`, range descriptor is `[100, 200, offset, size]` → projected index is `[50]`
4. **For objects/sparse arrays**: Verify field/key is in range (`from <= index < to`), then use projected kpath

**Loading a range descriptor**:
- Given a `!snap-range` node with multiple descriptors `[[from1, to1, offset1, size1], ...]`
- To load a specific range: Find descriptor where `from <= targetIndex < to`
- **If contiguous descriptors share same offset**: Can load them together (read once, parse multiple ranges)
- Read `size` bytes at `offset` from snapshot file
- Parse as container node containing children at indices `from` to `to-1`
- **Optimization**: When loading, check if adjacent descriptors share offset and load them together

### Open Questions

1. **How to extract kpath from patch node?** ✅ **SOLVED**
   - **Root-level patches**: Patches are `[]*ir.Node` rooted at document root
   - **Projected patches**: When projecting to a range, we get a sub-node of the original patch
   - **Sub-nodes can call `.KPath()`**: Since they're part of the patch tree, they have parent pointers
   - **Ancestor patches**: If patch targets an ancestor of a range, the index structure contains the kpath
     - Example: Patch targets `users`, range is `users[100:200]` → index tells us range is under `users`

2. **How to map object field names to indices?**
   - Need to traverse parent container's `Fields` array to find matching field
   - Or maintain a field-name-to-index mapping?
   - **For insertions**: Need to determine position relative to ranges
     - For objects: Position based on field name ordering
     - For arrays: Position is the index value
     - For sparse arrays: Position is the sparse index value

3. **How to handle patches that target ancestors?** ✅ **SOLVED**

**Example**: Patch targeting `users` container

**Case 1: Replacement patch**
- Patch: `{ "users": { "123": {...}, "456": {...} } }` (replaces entire `users`)
- Action: Write new `users` container to sink, ignore old ranges
- Old chunks become orphaned, compaction cleans up

**Case 2: Merge patch (decomposed correctly)**
- Patch: `{ "users": { "150": {...} } }` where `150` falls within existing range `users[100:200]`
- Action: Should have been categorized to that range already
- If not categorized correctly → bug in categorization logic

**Case 3: Merge patch (addition not in existing ranges)**
- Patch: `{ "users": { "999": {...} } }` where `999` doesn't fall in any existing range
- Action: 
  1. Stream existing ranges to sink (in order)
  2. Insert addition at correct position (maintain order)
  3. Sink creates new ranges dynamically based on size
- This handles additions that don't fit into existing range boundaries

**Answer**: Yes, this answers unambiguously:
- Replacement → write to sink
- Merge decomposed correctly → should be in a range already
- Merge with addition → stream ranges + insert addition at correct position

4. **How to handle structural changes?**

**The sink handles**: Size-based rebalancing
- Ranges split when they exceed threshold
- Ranges merge when they're small (though we rebuild everything, so this is less relevant)
- Dynamic range creation based on actual data sizes

**Other classes of structural changes**:

**A. Deletions**:
- Patch removes a child (field/element)
- Order preserved by processing left-to-right
- Sink handles naturally (fewer children → smaller size)

**B. Type changes**:
- Leaf → Container: Child becomes chunked (sink handles)
- Container → Leaf: Child becomes small (sink handles)
- But: Need to handle when a chunked child becomes a leaf (no longer needs chunking)

**C. Reordering**:

**Scope: Basic cases only** (for now):
- Focus on simple patches: full replacements, merge patches, and `!arraydiff` operations
- Defer complex operations: `!all`, `!if`, and other advanced patch operations

**Array diffs (`!arraydiff` tag) - if used**:
- Patch structure: Object with numeric keys (sparse array), values are operations:
  - `!delete` - delete element at position
  - `!replace` - replace element (with `from:` and `to:` fields)
  - `!insert` - insert new element
  - Recursive patches - patch element at position
- **Key insight**: Operations are processed sequentially left-to-right with two pointers:
  - `fi` (from index) - position in original document
  - `di` (diff index) - position in diff/patch
- **This means**: Array diff operations can be categorized and projected!
  - Categorize: Operations at position `di` fall in range `[from, to)` if `from <= di < to`
  - Project: Adjust `di` to local position within range: `localDi = di - from`
  - Apply: Process operations sequentially as we process ranges left-to-right

**Reordering via array diffs** (if patch uses `!arraydiff`):
- Reordering is represented as `!replace` operations that swap elements
- Or as `!delete` + `!insert` operations
- **Can be handled in-order**: Operations are sequential, can be categorized to ranges and projected
- **No need to load multiple ranges**: Each operation targets a specific position, can be applied to the range containing that position

**Note**: Other patch operations (`!all`, `!if`, etc.) may require different handling - deferred for now.

**D. Nesting depth changes**:
- Flattening: Container children become direct children of parent
- Adding nesting: Direct children become nested containers
- This affects the index structure itself

**E. Container type changes**:
- Object → Array (or vice versa)
- Regular → Sparse Array (or vice versa)
- This fundamentally changes structure

**Question**: Which of these need special handling beyond what the sink provides?
