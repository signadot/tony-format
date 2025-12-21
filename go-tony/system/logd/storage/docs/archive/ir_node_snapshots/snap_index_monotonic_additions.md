# Monotonic Index Additions: Impact on Incremental Flow

## The Flow

```
1. Read existing index (in-memory)
2. Read snapshot incrementally (stream.Decoder)
3. Apply patches
4. Write result snapshot incrementally (stream.Encoder)
5. Write index incrementally (add entries as we write)
```

**Key characteristic**: Additions to write-index are **monotonically increasing** - entries added in the order they appear in the snapshot.

## The Problem

### Monotonic ≠ Sorted

When we add index entries in document order (monotonically increasing), they may **not be sorted** by key name or index:

**Example - Object**:
```
Snapshot: {z: val, a: val, m: val}
Add to index: z, a, m  (document order)
For lookup: Need a, m, z  (sorted order)
```

**Example - Array**:
```
Snapshot: [val0, val1, val2, ...]
Add to index: 0, 1, 2, ...  (document order)
For lookup: Need 0, 1, 2, ...  (already sorted - OK!)
```

**Example - Sparse Array**:
```
Snapshot: {100: val, 5: val, 200: val}
Add to index: 100, 5, 200  (document order)
For lookup: Need 5, 100, 200  (sorted order)
```

### Impact on Lookup Efficiency

**If children are unsorted**:
- ❌ Cannot use binary search: O(log n)
- ❌ Must use linear scan: O(n)
- ❌ Range queries inefficient: O(n) to find boundaries

**If children are sorted**:
- ✅ Binary search: O(log n)
- ✅ Range queries efficient: O(log n) to find boundaries

## Analysis: What Order Do We Get?

### Pre-order DFS Traversal

When reading/writing incrementally, we typically traverse in **pre-order DFS**:

```
Snapshot: {
  z: {z1: val, z2: val},
  a: {a1: val},
  m: {m1: val, m2: val}
}
```

**Pre-order DFS order**: `z, z.z1, z.z2, a, a.a1, m, m.m1, m.m2`

**Lexicographic order**: `a, a.a1, m, m.m1, m.m2, z, z.z1, z.z2`

**Result**: Pre-order DFS is **lexicographically sorted** for paths! ✅

**Why**: Pre-order DFS visits parents before children, and siblings in document order. If document order matches lexicographic order (which it often does in Tony format), then pre-order DFS order is lexicographically sorted.

### But: Object Keys May Not Be Sorted

**Problem**: Within a single object, keys may appear in **any order**:

```
Snapshot: {z: val, a: val, m: val}
```

**Pre-order DFS adds children**: `z, a, m` (document order)

**For efficient lookup**: Need `a, m, z` (sorted)

**Impact**: Children of an object node are **not guaranteed sorted** by `StartField`.

### Arrays: Usually Sorted

**Arrays**: Typically processed sequentially:
```
Snapshot: [val0, val1, val2, ...]
Add to index: 0, 1, 2, ...  (sequential)
```

**Result**: Array children are **already sorted** by `Start` index. ✅

**Exception**: Sparse arrays may have non-sequential indices:
```
Snapshot: {100: val, 5: val, 200: val}
Add to index: 100, 5, 200  (document order)
```

**Result**: Sparse array children are **not sorted** by `Start` index. ❌

## Solutions

### Option 1: Sort After Building (Recommended)

**Approach**: Add children in document order (monotonic), then sort once after all children added.

**Implementation**:
```go
func (idx *Index) Finalize() {
    // Sort children once after all added
    if idx.isObject() {
        sort.Slice(idx.Children, func(i, j int) bool {
            return *idx.Children[i].StartField < *idx.Children[j].StartField
        })
    } else if idx.isArray() {
        sort.Slice(idx.Children, func(i, j int) bool {
            return idx.Children[i].Start < idx.Children[j].Start
        })
    }
    
    // Recursively finalize children
    for i := range idx.Children {
        idx.Children[i].Finalize()
    }
}
```

**Usage in incremental flow**:
```go
// During incremental write
for {
    event, _ := dec.ReadEvent()
    
    switch event.Type {
    case stream.EventBeginObject:
        enc.BeginObject()
        currentIndex = &Index{Offset: enc.Offset()}
        indexStack = append(indexStack, currentIndex)
        
    case stream.EventEndObject:
        enc.EndObject()
        currentIndex.Size = enc.Offset() - currentIndex.Offset
        currentIndex.Finalize()  // Sort children
        indexStack = indexStack[:len(indexStack)-1]
        
    // ... etc
    }
}
```

**Pros**:
- ✅ Simple: Add in document order, sort once
- ✅ Efficient: O(n log n) sort once vs O(n log n) per insertion
- ✅ Works with monotonic additions
- ✅ Minimal changes to incremental flow

**Cons**:
- ⚠️ Must call `Finalize()` when node completes
- ⚠️ Cannot lookup children until finalized

**Cost**: O(n log n) per node with n children. For typical snapshots, this is acceptable.

### Option 2: Insert in Sorted Position

**Approach**: Maintain sorted order as we add children (insert in correct position).

**Implementation**:
```go
func (idx *Index) AddChild(child Index) {
    if idx.isObject() {
        // Find insertion point
        i := sort.Search(len(idx.Children), func(i int) bool {
            return *idx.Children[i].StartField >= *child.StartField
        })
        // Insert at position i
        idx.Children = append(idx.Children[:i], append([]Index{child}, idx.Children[i:]...)...)
    } else if idx.isArray() {
        // Find insertion point
        i := sort.Search(len(idx.Children), func(i int) bool {
            return idx.Children[i].Start >= child.Start
        })
        idx.Children = append(idx.Children[:i], append([]Index{child}, idx.Children[i:]...)...)
    }
}
```

**Pros**:
- ✅ Always sorted (can lookup during building)
- ✅ No finalization step needed

**Cons**:
- ❌ O(n) insertion cost per child (slice insertion)
- ❌ More complex: Must find insertion point for each child
- ❌ Slower: O(n²) total cost vs O(n log n) for sort-after

**Cost**: O(n²) total for n children. Worse than Option 1.

### Option 3: Use Maps for Lookup (Hybrid)

**Approach**: Keep `Children` slice in document order, add maps for fast lookup.

**Implementation**:
```go
type Index struct {
    // ... existing fields ...
    Children []Index  // Document order (monotonic)
    
    // Lookup maps (built on demand)
    ChildrenByField map[string]*Index  // For objects
    ChildrenByStart map[int]*Index     // For arrays
    lookupBuilt bool
}

func (idx *Index) BuildLookupMaps() {
    if idx.lookupBuilt {
        return
    }
    
    idx.ChildrenByField = make(map[string]*Index)
    idx.ChildrenByStart = make(map[int]*Index)
    
    for i := range idx.Children {
        child := &idx.Children[i]
        if child.StartField != nil {
            idx.ChildrenByField[*child.StartField] = child
        }
        idx.ChildrenByStart[child.Start] = child
    }
    
    idx.lookupBuilt = true
}

func (idx *Index) FindChildByField(fieldName string) *Index {
    idx.BuildLookupMaps()  // Build on first lookup
    return idx.ChildrenByField[fieldName]
}
```

**Pros**:
- ✅ O(1) lookup by key/index
- ✅ No sorting needed
- ✅ Works with monotonic additions
- ✅ Can build maps on-demand (lazy)

**Cons**:
- ⚠️ Extra memory (maps duplicate `Children` data)
- ⚠️ Range queries still need sorted order (or scan)
- ⚠️ More complex structure

**Cost**: O(n) to build maps, O(1) lookup. Good for frequent lookups, but doesn't help with range queries.

### Option 4: Accept Unsorted, Use Linear Search

**Approach**: Keep children in document order, use linear search for lookups.

**Pros**:
- ✅ Simplest: No sorting, no maps
- ✅ Works with monotonic additions

**Cons**:
- ❌ O(n) lookup cost
- ❌ Inefficient for large objects/arrays
- ❌ Range queries inefficient

**Cost**: O(n) per lookup. Only acceptable for small objects/arrays.

## Recommendation

**For incremental flow with monotonic additions:**

1. ✅ **Add children in document order** (monotonic) - matches incremental flow
2. ✅ **Sort once when node completes** (Option 1) - efficient and simple
3. ✅ **Call `Finalize()` when `EndObject`/`EndArray` events occur**

**Implementation**:
```go
// During incremental write
case stream.EventEndObject:
    enc.EndObject()
    currentIndex.Size = enc.Offset() - currentIndex.Offset
    currentIndex.Finalize()  // Sort children once
    // Add to parent's children
    if len(indexStack) > 0 {
        parent := indexStack[len(indexStack)-1]
        parent.Children = append(parent.Children, *currentIndex)
    }
```

**Why this works**:
- ✅ Monotonic additions preserved (add in document order)
- ✅ Efficient lookups enabled (sort once per node)
- ✅ Minimal overhead (O(n log n) per node, not per child)
- ✅ Simple to implement (one `Finalize()` call per node)

**Alternative**: If range queries are rare and lookups are frequent, Option 3 (maps) may be better. But for most cases, Option 1 (sort after) is the best balance.

## Conclusion

**Monotonic additions do NOT impose problems** - we can:
1. Add children in document order (monotonic)
2. Sort once when node completes
3. Enable efficient lookups without breaking incremental flow

The key insight: **Sorting is a one-time cost per node**, not per child. This makes it efficient even with monotonic additions.
