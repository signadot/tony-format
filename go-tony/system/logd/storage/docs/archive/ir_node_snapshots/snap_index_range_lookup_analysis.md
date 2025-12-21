# snap.Index Analysis: Efficient Range Lookups

## Question

Does `snap.Index` contain everything needed for efficient lookup over ranges of sorted keys and arrays?

## Current Structure

```go
type Index struct {
    StartField *string      // Field name where index starts
    Start      int          // Start position (array index or field range)
    End        int          // End position
    Offset     int64        // Byte offset in snapshot
    Size       int64        // Size in bytes

    ParentField       string   // Parent field name
    ParentSparseIndex int      // Parent sparse array index
    ParentIndex       int      // Parent array index
    Parent            *Index   // Parent index entry

    Children []Index           // Child index entries
}
```

## Requirements for Efficient Range Lookups

### 1. **Sorted Children**
- For objects: Children sorted by `StartField` (lexicographically)
- For arrays: Children sorted by `Start` index (numerically)
- Enables binary search: O(log n) instead of O(n) linear scan

### 2. **Binary Search Capability**
- Find child by key name (objects): `FindChildByField(fieldName string)`
- Find child containing index (arrays): `FindChildContainingIndex(index int)`
- Find range boundaries: `FindChildrenInRange(start, end)`

### 3. **Range Query Support**
- Objects: "All keys between 'a' and 'z'"
- Arrays: "All indices between 100 and 200"
- Requires sorted children + binary search

## What's Missing

### ❌ **1. No Sorting Guarantee**

`Children []Index` is a slice with no guarantee of sorted order:
- For objects: Children may not be sorted by `StartField`
- For arrays: Children may not be sorted by `Start` index
- **Impact**: Cannot use binary search, must scan linearly O(n)

### ❌ **2. No Key/Index Mapping**

For objects:
- Have `StartField` on each child
- But no way to quickly map `fieldName → childIndex`
- **Impact**: Must scan `Children` slice to find child by field name

For arrays:
- Have `Start`/`End` on each child
- But no way to quickly find which child contains a specific index
- **Impact**: Must scan `Children` slice to find containing child

### ❌ **3. No Range Query Methods**

Missing methods like:
- `FindChildrenInFieldRange(startField, endField string) []*Index`
- `FindChildrenInIndexRange(startIdx, endIdx int) []*Index`
- `FindChildContainingIndex(index int) *Index`
- `FindChildByField(fieldName string) *Index`

### ❌ **4. No Sparse Array Handling**

For sparse arrays:
- `Start`/`End` represent sparse indices (e.g., `{100: val, 200: val}`)
- But no way to efficiently find children by sparse index
- **Impact**: Must scan to find sparse index ranges

## What Would Be Needed

### Option 1: **Sorted Children + Binary Search**

**Requirement**: Ensure `Children` slice is always sorted:
- Objects: Sort by `StartField` (lexicographically)
- Arrays: Sort by `Start` index (numerically)

**Implementation**:
```go
// When building index, ensure children are sorted
func (idx *Index) AddChild(child Index) {
    idx.Children = append(idx.Children, child)
    // Sort after adding
    if idx.isObject() {
        sort.Slice(idx.Children, func(i, j int) bool {
            return *idx.Children[i].StartField < *idx.Children[j].StartField
        })
    } else if idx.isArray() {
        sort.Slice(idx.Children, func(i, j int) bool {
            return idx.Children[i].Start < idx.Children[j].Start
        })
    }
}

// Binary search for child by field
func (idx *Index) FindChildByField(fieldName string) *Index {
    i := sort.Search(len(idx.Children), func(i int) bool {
        return *idx.Children[i].StartField >= fieldName
    })
    if i < len(idx.Children) && *idx.Children[i].StartField == fieldName {
        return &idx.Children[i]
    }
    return nil
}

// Binary search for child containing index
func (idx *Index) FindChildContainingIndex(index int) *Index {
    i := sort.Search(len(idx.Children), func(i int) bool {
        return idx.Children[i].Start >= index
    })
    // Check if previous child contains index
    if i > 0 {
        prev := &idx.Children[i-1]
        if index >= prev.Start && index < prev.End {
            return prev
        }
    }
    // Check current child
    if i < len(idx.Children) {
        curr := &idx.Children[i]
        if index >= curr.Start && index < curr.End {
            return curr
        }
    }
    return nil
}
```

**Pros**: 
- Uses existing `Children []Index` structure
- O(log n) lookup after sorting
- Minimal changes to structure

**Cons**:
- Must maintain sorted order when building
- Insertion is O(n log n) if sorting after each add
- Better to sort once after all children added

### Option 2: **Separate Maps for Fast Lookup**

**Requirement**: Add maps alongside `Children` slice:
```go
type Index struct {
    // ... existing fields ...
    Children []Index
    
    // Fast lookup maps (built from Children)
    ChildrenByField map[string]*Index  // For objects
    ChildrenByStart map[int]*Index     // For arrays (index → child)
}
```

**Implementation**:
```go
func (idx *Index) BuildLookupMaps() {
    idx.ChildrenByField = make(map[string]*Index)
    idx.ChildrenByStart = make(map[int]*Index)
    
    for i := range idx.Children {
        child := &idx.Children[i]
        if child.StartField != nil {
            idx.ChildrenByField[*child.StartField] = child
        }
        idx.ChildrenByStart[child.Start] = child
    }
}

func (idx *Index) FindChildByField(fieldName string) *Index {
    return idx.ChildrenByField[fieldName]
}
```

**Pros**:
- O(1) lookup by key/index
- Fast range queries (can iterate map)

**Cons**:
- Extra memory (duplicates `Children` data)
- Must maintain maps when children change
- More complex structure

### Option 3: **Hybrid: Sorted Slice + Range Index**

**Requirement**: Keep sorted `Children` slice, add range index:
```go
type Index struct {
    // ... existing fields ...
    Children []Index  // Sorted by StartField or Start
    
    // Range index for fast range queries
    RangeIndex *RangeIndex  // Optional, built on demand
}

type RangeIndex struct {
    FieldBounds []FieldBound  // For objects: sorted field ranges
    IndexBounds []IndexBound  // For arrays: sorted index ranges
}
```

**Pros**:
- Fast range queries
- Minimal memory overhead
- Can build on demand

**Cons**:
- More complex
- Must maintain range index

## Recommendation

**For efficient range lookups, `snap.Index` needs:**

1. ✅ **Sorted `Children` slice** - Ensure children are sorted when building index
2. ✅ **Binary search methods** - Add `FindChildByField()`, `FindChildContainingIndex()`, etc.
3. ✅ **Range query methods** - Add `FindChildrenInRange()` methods
4. ⚠️ **Optional**: Maps for O(1) lookup if needed (adds memory overhead)

**Current state**: `snap.Index` structure has the fields needed (`StartField`, `Start`, `End`), but:
- No guarantee of sorted children
- No lookup methods
- No range query support

**Next steps**: 
1. Define sorting requirements (when building index, ensure children sorted)
2. Implement binary search methods
3. Implement range query methods
4. Test with real snapshot data
