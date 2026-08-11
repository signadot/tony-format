# Index Migration to KPath: Detailed Proposal

## Current Structure

**Note**: The `Index` struct field has been renamed from `Name` to `PathKey` to better reflect that it stores kpath segments (which can be field names, array indices, or sparse array indices), not just simple names.

**KPath Implementation Status**: All kpath utilities are implemented in `ir/kpath.go`:
- ✅ `ir.ParseKPath(kpath string) (*KPath, error)` - Parse kpath strings
- ✅ `ir.Split(kpath string) (firstSegment, restPath string)` - Split into first segment and rest
- ✅ `ir.SplitAll(kpath string) []string` - Split into all segments
- ✅ `ir.Join(prefix, suffix string) string` - Join segments
- ✅ `KPath.Compare(other *KPath) int` - Semantic comparison
- ✅ `KPath.Parent() *KPath` - Get parent path
- ✅ `KPath.IsChildOf(parent *KPath) bool` - Check parent-child relationship

The index uses a hierarchical tree structure where:

1. **Index Node Structure**:
   ```go
   type Index struct {
       PathKey  string              // KPath segment at this level (e.g., "a", "[0]", "{42}")
       Commits  *Tree[LogSegment]   // Segments stored at this level
       Children map[string]*Index  // Child indices keyed by kpath segment
   }
   ```

2. **Path Representation**: Uses `KindedPath` (kpath syntax: `"a.b.c"`, `"[0]"`, `"{42}"`)

3. **Key Operations**:
   - `Add(seg)`: Splits `KindedPath` using `ir.Split()`, takes first segment, recurses
   - `LookupRange(kpath, from, to)`: Splits kpath, navigates hierarchy
   - `LogSegCompare()`: Compares `KindedPath` using `KPath.Compare()` for semantic ordering

4. **Current Path Splitting**:
   ```go
   func splitPath(vp string) []string {
       return strings.Split(filepath.ToSlash(filepath.Clean(vp)), "/")
   }
   ```

## Challenges with KPath

### 1. **Segment Types**
KPath segments are not simple strings - they can be:
- **Field names**: `"a"`, `"'field name'"` (quoted if needed)
- **Dense array indices**: `"[0]"`, `"[42]"`, `"[*]"` (wildcard)
- **Sparse array indices**: `"{0}"`, `"{42}"`, `"{*}"` (wildcard)
- **Field wildcards**: `"*"` (top-level), `".*"` (nested)

**Note**: Index will NOT support wildcards initially (future work).

### 2. **Comparison Complexity**
- Cannot use simple string comparison (`a.RelPath < b.RelPath`)
- Must use `ir.KPath.Compare()` which handles:
  - Field vs Index vs SparseIndex ordering
  - Numeric index comparison
  - Wildcard ordering (if supported later)
- **Status**: ✅ `ir.ParseKPath()` and `KPath.Compare()` are implemented in `ir/kpath.go`

### 3. **Path Splitting**
- Cannot use `strings.Split(path, "/")`
- Must use `ir.Split(kpath)` which:
  - Returns `(firstSegment string, restPath string)`
  - Handles quoted fields, array indices, sparse indices
  - Each segment is a valid top-level kpath
- **Status**: ✅ `ir.Split()` and `ir.Join()` are implemented in `ir/kpath.go`

### 4. **Children Map Keys**
- Currently: `map[string]*Index` where keys are simple segment names
- Need: Keys must be segment strings (e.g., `"a"`, `"[0]"`, `"{42}"`, `"'field name'"`)
- These are valid top-level kpaths that can be parsed

## Proposed Changes

**Note**: `LogSegment` now uses only `KindedPath` (full path from root). The `RelPath` field has been removed. All index operations use `KindedPath` with kpath operations (`ir.Split()`, `ir.Join()`, `KPath.Compare()`).

### Phase 1: Replace splitPath with ir.Split

#### 1.1 Remove splitPath Function
```go
// REMOVED: func splitPath(vp string) []string
```

#### 1.2 Use ir.Split Instead
- `ir.Split(kpath)` returns `(firstSegment string, restPath string)` ✅ **Implemented**
- `firstSegment` is a valid top-level kpath (e.g., `"a"`, `"[0]"`, `"{42}"`, `"'field name'"`)
- `restPath` is the remaining kpath (e.g., `"b.c"`, `"b"`, `""`)
- Also available: `ir.SplitAll(kpath)` returns `[]string` of all segments
- Also available: `ir.Join(prefix, suffix)` to combine segments

### Phase 2: Update Index.Add()

#### 2.1 Current Implementation
```go
func (i *Index) Add(seg *LogSegment) {
    if seg.KindedPath == "" {
        i.Commits.Insert(*seg)
        return
    }
    // Split kpath into first segment and rest for navigation
    firstSegment, restPath := ir.Split(seg.KindedPath)
    child := i.Children[firstSegment]
    if child == nil {
        child = NewIndex(firstSegment)
        i.Children[firstSegment] = child
    }
    // Create a copy with relative path for recursive call
    // (but we'll store the full path, so create new segment with restPath)
    segCopy := *seg
    segCopy.KindedPath = restPath
    child.Add(&segCopy)
}
```

**Key Changes**:
- Use `ir.Split(seg.KindedPath)` to split the full kpath
- `firstSegment` is a valid top-level kpath segment (can be used as map key)
- `restPath` is the remaining kpath (may be empty)
- Create a copy for recursive call to avoid modifying the original segment
- Full path is always stored in `KindedPath` (no path restoration needed)

### Phase 3: Update Index.Remove()

#### 3.1 Proposed Implementation
```go
func (i *Index) Remove(seg *LogSegment) bool {
    if seg.RelPath == "" {
        i.Lock()
        defer i.Unlock()
        return i.Commits.Remove(*seg)
    }
    
    firstSegment, restPath := ir.Split(seg.KindedPath)
    
    i.RLock()
    defer i.RUnlock()
    c := i.Children[firstSegment]
    if c == nil {
        return false
    }
    
    // Create a copy with relative path for recursive call
    segCopy := *seg
    segCopy.KindedPath = restPath
    res := c.Remove(&segCopy)
    return res
}
```

### Phase 4: Update Index.LookupRange()

#### 4.1 Current Implementation
```go
func (i *Index) LookupRange(vp string, from, to *int64) []LogSegment {
    // ... existing code ...
    if vp == "" {
        // Return all segments at this level and children
        // ...
    }
    parts := splitPath(vp)
    hd, rest := parts[0], parts[1:]
    c := i.Children[hd]
    // ...
    cRes := c.LookupRange(strings.Join(rest, "/"), from, to)
    // Reconstruct path using path.Join(hd, ...)
}
```

#### 4.2 Proposed Implementation
```go
func (i *Index) LookupRange(vp string, from, to *int64) []LogSegment {
    i.RLock()
    defer i.RUnlock()
    
    res := []LogSegment{}
    
    // Add segments at this level
    i.Commits.Range(func(c LogSegment) bool {
        res = append(res, c)
        return true
    }, rangeFunc(from, to))
    
    if vp == "" {
        // Return all segments at this level and all children
        for _, ci := range i.Children {
            if ci == nil {
                continue
            }
            cRes := ci.LookupRange("", from, to)
            for j := range cRes {
                seg := cRes[j]
                // Reconstruct full kpath: Join PathKey with relative KindedPath
                if seg.KindedPath == "" {
                    seg.KindedPath = ci.PathKey
                } else {
                    seg.KindedPath = ir.Join(ci.PathKey, seg.KindedPath)
                }
                res = append(res, seg)
            }
        }
        slices.SortFunc(res, LogSegCompare)
        return res
    }
    
    // Split kpath to navigate hierarchy
    firstSegment, restPath := ir.Split(vp)
    c := i.Children[firstSegment]
    if c == nil {
        return res
    }
    
    // Recursive call
    cRes := c.LookupRange(restPath, from, to)
    
    // Reconstruct path for results
    cResCopy := make([]LogSegment, len(cRes))
    // Reconstruct full kpath for results
    for j := range cRes {
        seg := cRes[j]
        if seg.KindedPath == "" {
            seg.KindedPath = firstSegment
        } else {
            seg.KindedPath = ir.Join(firstSegment, seg.KindedPath)
        }
        res = append(res, seg)
    }
    slices.SortFunc(res, LogSegCompare)
    return res
}
```

**Key Changes**:
- Use `ir.Split(kpath)` instead of `splitPath(vp)` ✅ **Available**
- Use `ir.Join(firstSegment, restPath)` to reconstruct full paths ✅ **Available**
- Using `KindedPath` field with kpath syntax

### Phase 5: Update LogSegCompare()

#### 5.1 Current Implementation
```go
func LogSegCompare(a, b LogSegment) int {
    n := cmp.Compare(a.StartCommit, b.StartCommit)
    if n != 0 {
        return n
    }
    n = cmp.Compare(a.StartTx, b.StartTx)
    if n != 0 {
        return n
    }
    n = cmp.Compare(a.EndCommit, b.EndCommit)
    if n != 0 {
        return n
    }
    n = cmp.Compare(a.EndTx, b.EndTx)
    if n != 0 {
        return n
    }
    return cmp.Compare(a.KindedPath, b.KindedPath) // String comparison fallback
}
```

#### 5.2 Proposed Implementation
```go
func LogSegCompare(a, b LogSegment) int {
    n := cmp.Compare(a.StartCommit, b.StartCommit)
    if n != 0 {
        return n
    }
    n = cmp.Compare(a.StartTx, b.StartTx)
    if n != 0 {
        return n
    }
    n = cmp.Compare(a.EndCommit, b.EndCommit)
    if n != 0 {
        return n
    }
    n = cmp.Compare(a.EndTx, b.EndTx)
    if n != 0 {
        return n
    }
    
    // Compare KindedPath using KPath.Compare()
    aKp, errA := ir.ParseKPath(a.KindedPath)
    bKp, errB := ir.ParseKPath(b.KindedPath)
    
    // Fallback to string comparison if parsing fails
    if errA != nil || errB != nil {
        return cmp.Compare(a.KindedPath, b.KindedPath)
    }
    
    // Handle nil cases (empty paths)
    if aKp == nil && bKp == nil {
        return 0
    }
    if aKp == nil {
        return -1 // Empty path < non-empty
    }
    if bKp == nil {
        return 1 // Non-empty > empty
    }
    
    return aKp.Compare(bKp)
}
```

**Key Changes**:
- Parse both `KindedPath` strings into `*ir.KPath` structures using `ir.ParseKPath()` ✅ **Available**
- Use `KPath.Compare()` for proper semantic comparison ✅ **Available**
- Fallback to string comparison if parsing fails (shouldn't happen in normal operation)
- Using `KindedPath` field with kpath syntax

### Phase 6: Update Index Tree Comparison Function

#### 6.1 Current Implementation
```go
func NewIndex(pathKey string) *Index {
    return &Index{
        PathKey: pathKey,
        Commits: NewTree(func(a, b LogSegment) bool {
            if a.StartCommit < b.StartCommit {
                return true
            }
            // ... more comparisons ...
            if a.KindedPath < b.KindedPath {
                return true
            }
            return false
        }),
        Children: map[string]*Index{},
    }
}
```

#### 6.2 Proposed Implementation
```go
func NewIndex(pathKey string) *Index {
    return &Index{
        PathKey: pathKey,
        Commits: NewTree(func(a, b LogSegment) bool {
            if a.StartCommit < b.StartCommit {
                return true
            }
            if a.StartCommit > b.StartCommit {
                return false
            }
            if a.StartTx < b.StartTx {
                return true
            }
            if a.StartTx > b.StartTx {
                return false
            }
            
            // Compare KindedPath using KPath.Compare()
            aKp, errA := ir.ParseKPath(a.KindedPath)
            bKp, errB := ir.ParseKPath(b.KindedPath)
            
            // Fallback to string comparison if parsing fails
            if errA != nil || errB != nil {
                return a.KindedPath < b.KindedPath
            }
            
            // Handle nil cases
            if aKp == nil && bKp == nil {
                return false
            }
            if aKp == nil {
                return true // Empty < non-empty
            }
            if bKp == nil {
                return false // Non-empty > empty
            }
            
            return aKp.Compare(bKp) < 0
        }),
        Children: map[string]*Index{},
    }
}
```

### Phase 7: Update ListRange()

#### 7.1 Current Implementation
```go
func (i *Index) ListRange(from, to *int64) []string {
    i.RLock()
    defer i.RUnlock()
    
    children := make([]string, 0, len(i.Children))
    for name, ci := range i.Children {
        ci.RLock()
        segs := ci.LookupRange("", from, to)
        ci.RUnlock()
        if len(segs) == 0 {
            continue
        }
        children = append(children, name)
    }
    return children
}
```

#### 7.2 Proposed Implementation
```go
func (i *Index) ListRange(from, to *int64) []string {
    i.RLock()
    defer i.RUnlock()
    
    children := make([]string, 0, len(i.Children))
    for name, ci := range i.Children {
        ci.RLock()
        segs := ci.LookupRange("", from, to)
        ci.RUnlock()
        if len(segs) == 0 {
            continue
        }
        children = append(children, name) // name is already a valid kpath segment
    }
    
    // Sort children by kpath comparison (not string comparison)
    slices.SortFunc(children, func(a, b string) int {
        aKp, errA := ir.ParseKPath(a)
        bKp, errB := ir.ParseKPath(b)
        if errA != nil || errB != nil {
            return cmp.Compare(a, b) // Fallback
        }
        if aKp == nil && bKp == nil {
            return 0
        }
        if aKp == nil {
            return -1
        }
        if bKp == nil {
            return 1
        }
        return aKp.Compare(bKp)
    })
    
    return children
}
```

**Key Changes**:
- Sort children using `KPath.Compare()` instead of string comparison
- Ensures proper ordering: fields < dense arrays < sparse arrays

### Phase 8: Update Iterator Usage

#### 9.1 IndexIterator
The `index_iterator.go` file uses `KindedPath` for seeking:

```go
// Current:
KindedPath: "\xff\xff\xff\xff", // Maximum string value

// Implementation:
KindedPath: "\xff\xff\xff\xff", // Maximum string value (for seeking)
```

**Note**: For proper kpath comparison in iterators, we may need to use a different approach for "maximum" kpath. Consider:
- Using a sentinel value that compares greater than all valid kpaths
- Or using `KPath.Compare()` in iterator seek operations

### Phase 9: Variable Naming Cleanup (After Testing Passes)

**Note**: This phase should be done **after** all tests pass, as a cleanup/refactoring step. It improves code clarity but doesn't change functionality.

#### 9.1 Rename Variables

Rename local variables to better reflect kpath usage:

**In `index.go`**:
- `NewIndex(name string)` → `NewIndex(pathKey string)` (parameter matches struct field name)
- `LookupRange(vp string, ...)` → `LookupRange(kpath string, ...)` (vp = "virtual path" is outdated)
- `splitPath(vp string)` → `splitPath(kpath string)` (or remove if replaced by `ir.Split()`)
- Loop variables: `for name, ci := range i.Children` → `for pathKey, ci := range i.Children`
- `hd` (head) → `firstSegment` (more descriptive)
- `rest` → `restPath` or `remainingPath` (more descriptive)

**In `index_iterator.go`**:
- `Down(childName string)` → `Down(pathKey string)` or `Down(segment string)`
- `IterAtPath(pathStr string)` → `IterAtPath(kpath string)`
- `ToPath(pathStr string)` → `ToPath(kpath string)`
- Loop variables: `for _, part := range parts` → `for _, segment := range segments`
- `parts` → `segments` (when referring to path segments)

**Examples**:

```go
// Before:
func (i *Index) LookupRange(vp string, from, to *int64) []LogSegment {
    // ...
    parts := splitPath(vp)
    hd, rest := parts[0], parts[1:]
    for name, ci := range i.Children {
        // ...
    }
    // ...
}

// After:
func (i *Index) LookupRange(kpath string, from, to *int64) []LogSegment {
    // ...
    firstSegment, restPath := ir.Split(kpath)
    for pathKey, ci := range i.Children {
        // ...
    }
    // ...
}
```

```go
// Before:
func NewIndex(name string) *Index {
    return &Index{PathKey: name, ...}
}

// After:
func NewIndex(pathKey string) *Index {
    return &Index{PathKey: pathKey, ...}
}
```

```go
// Before:
func (it *IndexIterator) Down(childName string) bool {
    child := it.current.Children[childName]
    // ...
}

// After:
func (it *IndexIterator) Down(pathKey string) bool {
    child := it.current.Children[pathKey]
    // ...
}
```

**Rationale**:
- `vp` (virtual path) is outdated terminology - use `kpath` to match kpath operations
- `name` is misleading for kpath segments (could be `"[0]"`, `"{42}"`, not just names)
- `pathKey` matches the struct field name `PathKey`
- More descriptive names (`firstSegment`, `restPath`) improve readability

## Implementation Status

**Current State**:
- ✅ `Index.PathKey` field renamed from `Name` (completed)
- ✅ KPath parsing/utilities implemented in `ir` package (`ir.ParseKPath()`, `ir.Split()`, `ir.Join()`, `KPath.Compare()`)
- ✅ `LogSegment` uses only `KindedPath` field (`RelPath` removed)
- ✅ Index operations use `ir.Split()` for path navigation
- ✅ Index operations use `KPath.Compare()` for semantic comparison

## Implementation Order

**Prerequisites** (completed):
- ✅ `ir.ParseKPath()` implemented in `ir/kpath.go`
- ✅ `ir.Split()` and `ir.Join()` functions implemented in `ir/kpath.go`
- ✅ `KPath.Compare()` method implemented in `ir/kpath.go`

**Note**: `RelPath` has been removed from `LogSegment`. The migration uses `KindedPath` with kpath operations (`ir.Split()`, `ir.Join()`, `KPath.Compare()`). All paths are stored as full paths from root in `KindedPath`.

**Next steps**:
1. **Phase 1**: Replace `splitPath` calls with `ir.Split`
2. **Phase 2**: Update `Index.Add()` 
3. **Phase 3**: Update `Index.Remove()`
4. **Phase 4**: Update `Index.LookupRange()`
5. **Phase 5**: Update `LogSegCompare()`
6. **Phase 6**: Update `NewIndex()` comparison function
7. **Phase 7**: Update `ListRange()` sorting
8. **Phase 8**: Update iterator usage
9. **Phase 9**: Variable naming cleanup (after tests pass) - rename `vp`, `name`, `hd`, `rest` to kpath-appropriate names

## Testing Considerations

### Test Cases to Add

1. **Path Splitting**:
   - `"a.b.c"` → segments: `["a", "b", "c"]`
   - `"[0].b"` → segments: `["[0]", "b"]`
   - `"a{42}.c"` → segments: `["a", "{42}", "c"]`
   - `"'field name'.b"` → segments: `["'field name'", "b"]`

2. **Path Joining**:
   - `ir.Join("a", "b.c")` → `"a.b.c"`
   - `ir.Join("[0]", "b")` → `"[0].b"`
   - `ir.Join("a", "{42}.c")` → `"a{42}.c"`

3. **Comparison**:
   - Field < Dense Array < Sparse Array
   - Numeric indices compare numerically
   - Quoted fields compare correctly

4. **Hierarchical Navigation**:
   - `Add()` with `"a.b.c"` creates hierarchy: `Index("a") -> Index("b") -> Index("c")`
   - `LookupRange("a.b", ...)` navigates correctly
   - `LookupRange("", ...)` returns all segments

5. **Edge Cases**:
   - Empty path `""` (root)
   - Single segment `"a"`
   - Quoted fields with special characters
   - Array indices at root `"[0]"`

## Migration Notes

### Backward Compatibility
- **Breaking Change**: `RelPath` field removed from `LogSegment` (use `KindedPath` instead)
- **No Breaking Changes**: `PointLogSegment()` signature unchanged (parameter now sets `KindedPath`)
- **No Breaking Changes**: `Index.LookupRange()` parameter renamed from `vp` to `kpath` for clarity

### Performance Considerations
- `ir.ParseKPath()` is called frequently (in comparison functions) ✅ **Implemented**
- `ir.Split()` and `ir.Join()` are efficient (parse once, reconstruct) ✅ **Implemented**

**Note on Recursive Splitting**: The current migration approach uses `ir.Split()` during recursion (e.g., in `Add()`, `Remove()`, `LookupRange()`). This means paths are re-parsed at each level:
  - For `"a.b.c.d.e"`: Level 1 parses full path, Level 2 parses `"b.c.d.e"`, Level 3 parses `"c.d.e"`, etc.
  - This is simple and matches the current code structure, but re-parses overlapping portions.
  - **Future optimization**: Once migration is complete, consider parsing once upfront and traversing the `*KPath` linked list structure instead of re-splitting strings at each level. This would eliminate redundant parsing for deep paths.

### Future Enhancements
- **Wildcard Support**: Index does not support wildcards initially
  - `[*]`, `.*`, `{*}` will not work in `LookupRange()`
  - Can be added later with pattern matching logic
- **KeyValue Paths**: `!key(path)` syntax not yet supported
  - Will need additional parsing logic when implemented

## Summary

The migration involves:
1. **Removing `RelPath`** from `LogSegment` (using only `KindedPath` with full paths)
2. **Using `ir.Split()` and `ir.Join()`** for path manipulation instead of `splitPath()`
3. **Using `KPath.Compare()`** for semantic path comparison instead of string comparison
4. **Maintaining hierarchical structure** with kpath segments as map keys

The hierarchical structure remains the same - only the path manipulation and comparison logic change. Each level of the hierarchy uses a kpath segment (field name, array index, or sparse index) as the key to child indices. The `KindedPath` field stores the full path from root, and the index navigates hierarchically by splitting this path.
