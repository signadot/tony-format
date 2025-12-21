# Migration to `kpath` Package Proposal

## Overview

This proposal outlines the migration to a unified `kpath` package (renamed from `kindedpath`) for **logd-specific functionality**. The `kpath` package will be forward-looking and designed to support future features like `!key(path)` objects. **All JSONPath references will be removed** from TokenSource/TokenSink and incremental parsing - these will use kpaths exclusively.

**Note**: At higher levels (e.g., `go-tony/ir`), the existing `ir.Path` with `[*]` wildcards can coexist with `kpath`. Under logd and new functionality, we use `kpath` forward-looking.

## Goals

1. **Rename package**: Change `kindedpath` package name to `kpath` for brevity
2. **Complete implementation**: Implement all TODO functions in the `kpath` package
3. **Design for future**: Support future `!key(path)` syntax (e.g., `a.b(<!key path value>)[2]{13}.fred`)
4. **Remove JSONPath**: Remove all JSONPath tracking from TokenSource/TokenSink, use kpaths only
5. **Update Index**: ✅ Completed - `RelPath` removed, using `KindedPath` throughout (core to logd)
6. **Focus on new functionality**: TokenSource/TokenSink, incremental parsing, and other logd-specific new code

## Current State

### Path Formats in Use

1. **RelPath** (`index.LogSegment`, `index.Index`):
   - Format: `""`, `"a/b/c"`, `"0/key"` (slash-separated)
   - **Status**: ✅ REMOVED - Replaced with KindedPath

2. **KindedPath** (`index.LogSegment.KindedPath`):
   - Format: `""`, `"a.b.c"`, `"a[0].b"` (kinded syntax)
   - **Status**: ✅ COMPLETED - Now the only path field in LogSegment, used throughout

3. **JSONPath** (`token.TokenSource`, `token.TokenSink`):
   - Format: `"$"`, `"$.a.b.c"`, `"$[0].key"`
   - **Status**: **REMOVED** - will be replaced with kpaths

4. **ir.Path** (`go-tony/ir`):
   - Format: `"$.a.b[*].c"` (with `[*]` wildcards, `..` subtree)
   - **Status**: **Coexists** - used at higher level, not replaced by kpath

### Future Syntax: !key(path) Support

Kpaths will need to support `!key(path)` objects in the future:
- Example: `a.b(<!key path value>)[2]{13}.fred`
- Syntax: `key(<value>)` for !key objects
- Mixed with arrays `[n]` and sparse arrays `{n}`

**Design consideration**: `kpath.Parse()` and `PathSegment` should be designed to accommodate this future syntax without breaking changes.

### Current Package Structure

```
go-tony/system/logd/storage/kindedpath/
  - doc.go          (package documentation)
  - path.go         (PathSegment, Parse, String, Parent, etc. - TODOs)
  - extract.go      (Get, GetAll, ToKindedPath, FromKindedPath - TODOs)
```

**Target Location**: Move to `go-tony/ir/kpath.go` (inside `ir` package, similar to `ir.Path` in `ir/path.go`)

## Proposed Changes

### Phase 1: Rename and Complete `kpath` Package

#### 1.1 Move into `ir` Package

**Action**: Move `kindedpath` package code into `ir` package (similar to how `ir.Path` is implemented)

**Rationale**: Having `kpath` inside `ir` allows:
- Adding methods to `ir.Node`: `node.GetKPath()`, `node.ListKPath()` (consistent with `node.GetPath()`, `node.ListPath()`)
- API consistency: Both path types live in `ir` package
- Access to `ir.Node` internals for future `!key(path)` support
- No extra imports needed - just `ir` package

**Changes**:
- Move code: `go-tony/system/logd/storage/kindedpath/` → `go-tony/ir/kpath.go`
- Update package: All code becomes part of `package ir`
- Update imports: Remove `kindedpath` imports, use `ir` package directly

**Files affected**:
- `system/logd/storage/kindedpath/path.go` → `go-tony/ir/kpath.go` (merge path.go and extract.go)
- Delete `system/logd/storage/kindedpath/doc.go` (documentation goes in `ir/kpath.go`)
- Delete `system/logd/storage/kindedpath/extract.go` (merge into `ir/kpath.go`)

#### 1.2 Implement Core Functions (Similar to `ir.Path` Pattern)

**Action**: Implement all TODO functions in `kpath` package, following the `ir.Path` implementation pattern

**Design Pattern** (matching `ir.Path`):
- Linked list structure with `Next *PathSegment`
- Fields similar to `ir.Path`: `Field *string`, `Index *int`, `SparseIndex *int` (for `{n}`), `KeyValue *ir.Node` (for future `!key(path)`)
- `Parse()` function (not method) - similar to `ir.ParsePath()`
- Helper functions: `parseFrag()`, `parseField()`, `parseIndex()`, `parseSparseIndex()` - similar to `ir.parseFrag()`, `ir.parseField()`, `ir.parseIndex()`
- `String()` method - similar to `ir.Path.String()`
- Methods on `ir.Node`: `GetKPath()`, `ListKPath()` - similar to `ir.Node.GetPath()`, `ir.Node.ListPath()`

**Functions to implement** (all in `ir` package):

1. **`ParseKPath(kpath string) (*KPath, error)`**
   - Parse kinded path string (e.g., `"a.b.c"`, `"a[0].b"`, `"a{0}.c"`)
   - **Future**: Support `"a.b(<value>)[2].fred"` for !key objects
   - Similar to `ParsePath()` but for kinded syntax (no `$` prefix, uses `.`, `[n]`, `{n}`)
   - Return structured `KPath` tree (linked list)
   - Use helper `parseKFrag()` function similar to `parseFrag()`

2. **`(p *KPath) String() string`**
   - Convert `KPath` back to kinded path string
   - Similar to `Path.String()` but outputs kinded syntax
   - Preserve kind information: `.` for objects, `[n]` for dense arrays, `{n}` for sparse arrays
   - **Future**: Output `key(<value>)` syntax for !key objects when `KeyValue` is set

3. **`(p *KPath) Parent() *KPath`**
   - Return parent path (all segments except last)
   - Return `nil` for root
   - Traverse `Next` chain, return new `KPath` with all but last segment

4. **`(p *KPath) IsChildOf(parent *KPath) bool`**
   - Check if this path is a child of parent
   - Compare segments recursively

5. **`(p *KPath) Compare(other *KPath) int`**
   - Lexicographic comparison (-1, 0, 1)
   - Compare segments in order

6. **`(y *Node) GetKPath(kpath string) (*Node, error)`**
   - Navigate an `ir.Node` tree using a kinded path (method on `Node`, similar to `GetPath()`)
   - Parse kinded path, traverse node structure to find nested node
   - Similar to `Node.GetPath()` but uses kinded path syntax
   - Example: `rootNode.GetKPath("a.b.c")` navigates to `rootNode.Values["a"].Values["b"].Values["c"]`
   - **Future**: Handle `!key(path)` objects when traversing

7. **`(y *Node) ListKPath(dst []*Node, kpath string) ([]*Node, error)`**
   - Traverse `Node` tree, collect all nodes matching kinded path (method on `Node`, similar to `ListPath()`)
   - Similar to `Node.ListPath()` but uses kinded path syntax
   - Return slice of matching nodes
   - Used for indexing: when writing a diff, index all paths within it
   - **Future**: Generate correct kpaths for `!key(path)` objects

8. **`GetAllKPaths(node *Node) (map[string]*Node, error)`**
   - Traverse entire `Node` tree, collect all nodes with their kinded paths
   - Return map of kinded path strings to nodes
   - Helper function (not method) for convenience
   - **Future**: Generate correct kpaths for `!key(path)` objects

9. **`ToKindedPath(fullPath string) string`**
   - Convert `/a/b/c` → `a.b.c`
   - Handle root `/` → `""`

10. **`FromKindedPath(kindedPath string) string`**
   - Convert `a.b.c` → `/a/b/c`
   - Handle root `""` → `"/"`

**PathSegment Structure** (similar to `ir.Path`):
```go
type PathSegment struct {
    Field       *string  // Object field name (e.g., "a", "b") - similar to Path.Field
    Index       *int     // Dense array index (e.g., 0, 1) - similar to Path.Index
    SparseIndex *int     // Sparse array index (e.g., 0, 42) - for {n} syntax
    KeyValue    *Node    // Optional: for !key(path) objects, the path value (future)
    Next        *KPath   // Next segment in path (nil for leaf) - similar to Path.Next
}
```

**Parsing Helper Functions** (similar to `Path`, in `ir` package):
- `parseKFrag(frag string, parent *KPath) error` - parse fragment, similar to `parseFrag()`
- `parseKField(frag string) (field, rest string, err error)` - parse object field, similar to `parseField()`
- `parseKIndex(is string) (index int, err error)` - parse dense array index `[n]`
- `parseKSparseIndex(is string) (index int, err error)` - parse sparse array index `{n}`
- **Future**: `parseKKeyValue(frag string) (value *Node, rest string, err error)` - parse `!key(path)` value

### Phase 2: Remove JSONPath, Use Kpaths Only in TokenSource/TokenSink

#### 2.1 Remove JSONPath Tracking

**Action**: Remove all JSONPath tracking from `TokenSource` and `TokenSink`, replace with kinded paths

**Changes**:

**`token/source.go`**:
- Remove `currentPath string` field (was JSONPath)
- Remove `pathStack []string` field (was JSONPath stack)
- Add `currentKpath string` field
- Add `kpathStack []string` field
- Rewrite `updatePath()` to track kinded paths directly (no JSONPath)
- Replace `CurrentPath()` method to return kinded path:
  ```go
  func (ts *TokenSource) CurrentPath() string {
      return ts.currentKpath  // Returns kinded path, not JSONPath
  }
  ```

**`token/sink.go`**:
- Remove `currentPath string` field (was JSONPath)
- Remove `pathStack []string` field (was JSONPath stack)
- Add `currentKpath string` field
- Add `kpathStack []string` field
- Rewrite path tracking to use kinded paths directly
- Update `onNodeStart` callback signature:
  ```go
  onNodeStart(offset int, kpath string, tok Token)  // Only kinded path, no JSONPath
  ```

#### 2.2 Implement Kinded Path Tracking

**Action**: Implement kinded path tracking during tokenization

**Logic**:
- Object access: `"a.b"` → append `.b` to current kpath
- Dense array access: `"a[0]"` → append `[0]` to current kpath (or `".0"` if using dot notation)
- Sparse array access: `"a{0}"` → append `{0}` to current kpath
- Root: `""` (empty string)
- **Future**: `!key(path)` objects: `"a.b(<value>)"` → append `b(<value>)` to current kpath

**Implementation**:
- Track kinded path directly during tokenization
- No conversion needed - build kinded path as we tokenize
- **Design for future**: Path tracking should be extensible for `!key(path)` syntax

### Phase 3: Update Index to Use KindedPath

#### 3.1 Remove RelPath Field ✅ COMPLETED

**Action**: ✅ Removed `RelPath` field from `LogSegment` and replaced all usage with `KindedPath`

**Changes**:

**`index/log_segment.go`**:
```go
type LogSegment struct {
    StartCommit int64
    StartTx     int64
    EndCommit   int64
    EndTx       int64
    KindedPath  string // Full kinded path from root (e.g., "a.b.c", "" for root)
    LogPosition int64  // Byte offset in log file
}
```

**Updated functions**:
- ✅ `PointLogSegment()`: Now sets `KindedPath` instead of `RelPath`
- ✅ `AsPending()`: Copies `KindedPath` instead of `RelPath`
- ✅ `WithCommit()`: Preserves `KindedPath` (no change needed)

#### 3.2 Update Index Navigation ✅ COMPLETED

**Action**: ✅ Updated `Index.Add()` and `Index.LookupRange()` to use `KindedPath` instead of `RelPath`

**Approach**: Keep hierarchical index structure, parse `KindedPath` to navigate

**Changes**:

**`index/index.go`**:
```go
import "github.com/signadot/tony-format/go-tony/ir"

func (i *Index) Add(seg *LogSegment) {
    // Parse KindedPath to navigate
    pathSeg, err := ir.ParseKPath(seg.KindedPath)
    if err != nil {
        // Handle error
    }
    if pathSeg == nil {
        // Root segment
        i.Commits.Insert(*seg)
        return
    }
    // Navigate using PathSegment structure
    // Extract first segment (Field, Index, or SparseIndex), navigate to child index
    // ...
}

func (i *Index) LookupRange(kindedPath string, from, to *int64) []LogSegment {
    // Parse kindedPath, navigate tree, filter segments
    // ...
}
```

#### 3.3 Update Comparison Functions

**Action**: ✅ Updated `LogSegCompare()` to use `KindedPath` instead of `RelPath`

**Changes**:

**`index/index.go`**:
```go
func NewIndex(name string) *Index {
    return &Index{
        // ...
        Commits: NewTree(func(a, b LogSegment) bool {
            // ... compare commits, tx ...
            if a.KindedPath < b.KindedPath {  // Changed from RelPath
                return true
            }
            return false
        }),
        // ...
    }
}
```

#### 3.4 Update Iterator

**Action**: Update `index.Iterator` and `index.IterAtPath()` to use `KindedPath`

**Changes**:

**`index/iterator.go`**, **`index/index_iterator.go`**: ✅ COMPLETED
- ✅ Replaced `RelPath` comparisons with `KindedPath` comparisons
- ✅ `IterAtPath()` accepts kinded path string
- Parse kinded path to navigate to correct position

### Phase 4: Update New/Incremental Functionality

**Focus**: Update new logd-specific functionality that uses paths. **Note**: `storage` package is mid-revamp and `compact` package may be deprecated, so focus on new/incremental code.

#### 4.1 Update Streaming Indexer (Prototype)

**Action**: Update `streaming_index_prototype.go` to use kinded paths

**Changes**:

**`streaming_index_prototype.go`**: ✅ COMPLETED
- ✅ Removed JSONPath conversion functions (now kpath-native)
- ✅ `onNodeStart` callback receives kinded path directly
  ```go
  sink := token.NewTokenSink(destFile, func(offset int, kpath string, tok token.Token) {
      // Use kpath directly
      seg := &index.LogSegment{
          // ...
          KindedPath: kpath,  // Use directly, no conversion
      }
      si.idx.Add(seg)
  })
  ```
- Update `ReadPath()` to accept kinded path:
  ```go
  import "github.com/signadot/tony-format/go-tony/ir"
  
  func (si *StreamingIndexer) ReadPath(kpath string) (*ir.Node, error) {
      // Use kpath directly for lookup
      segments := si.idx.LookupRange(kpath, &from, &to)
      // Use node.GetKPath() to navigate the node tree
      // ...
  }
  ```

#### 4.2 Update Incremental Parsing

**Action**: Update `parse.NodeParser` and related incremental parsing code to use kpaths

**Note**: This is new functionality, so should use kpaths forward-looking.

**Changes**:
- If `NodeParser` needs path tracking, use kpaths (not JSONPath)
- Any path-related APIs in incremental parsing should use kpaths

#### 4.3 Update Code Generation

**Action**: ✅ Updated code generation to use `KindedPath` instead of `RelPath`

**Files updated**:
- ✅ `index/index_gen.go`: Updated to serialize `KindedPath` and `LogPosition`
- ✅ Schema updated to reflect new structure

### Phase 5: Cleanup and Testing

#### 5.1 Verify JSONPath Removal

**Action**: Verify all JSONPath references are removed from TokenSource/TokenSink

**Note**: JSONPath should be completely removed - no backward compatibility needed since we're breaking things.

#### 5.2 Update Documentation

**Action**: Update all documentation to reflect kpath usage

**Files to update**:
- `docs/DESIGN.md`
- `docs/README.md`
- `docs/implementation_plan.md`
- All other docs that mention RelPath or JSONPath (remove JSONPath references)

#### 5.3 Add Tests

**Action**: Add comprehensive tests for `kpath` package and migration

**Test coverage**:
- `ir.ParseKPath()`: All syntax variations (current and future !key syntax)
- `KPath.String()`: Round-trip parsing and string conversion
- `Node.GetKPath()`: Navigate node trees using kinded paths (similar to `GetPath()` tests)
- `Node.ListKPath()`: Collect matching nodes using kinded paths (similar to `ListPath()` tests)
- `GetAllKPaths()`: Collect all paths from node trees
- Index operations with kinded paths
- TokenSource/TokenSink kinded path tracking (no JSONPath)

## Breaking Changes

This migration intentionally breaks things:

1. ✅ **`LogSegment.RelPath` field removed** - using `KindedPath` throughout
2. ✅ **`TokenSink` provides kinded path** - kpath-native, no JSONPath conversion
3. **`TokenSink` callback signature changes** - only receives kinded path, no JSONPath
4. **All JSONPath references removed** - no backward compatibility
5. **`kpath` functionality now in `ir` package** - use `ir.ParseKPath()`, `node.GetKPath()`, etc. (no separate `kpath` package)

**No transition period** - we're removing JSONPath entirely.

## Future: !key(path) Syntax Support

### Design Considerations

When implementing `ParseKPath()`, design `KPath` to accommodate future `!key(path)` syntax, following `Path` pattern (all in `ir` package):

```go
// KPath represents a kinded path (similar to Path but for kinded syntax)
type KPath struct {
    Field       *string  // Object field name (e.g., "a", "b") - similar to Path.Field
    Index       *int     // Dense array index (e.g., 0, 1) - similar to Path.Index
    SparseIndex *int     // Sparse array index (e.g., 0, 42) - for {n} syntax
    KeyValue    *Node    // Optional: for !key(path) objects, the path value (future)
    Next        *KPath   // Next segment in path (nil for leaf) - similar to Path.Next
}
```

**Example future paths**:
- `"a.b(<path>)[2].fred"` → `KPath{Field: &"a", ...} → KPath{Field: &"b", KeyValue: <path node>, ...} → KPath{Index: &2, ...} → KPath{Field: &"fred", ...}`

**Implementation strategy** (similar to `Path`):
- Parse current syntax first (without `!key` support)
- Use helper functions `parseKFrag()`, `parseKField()`, `parseKIndex()`, `parseKSparseIndex()` similar to `parseFrag()`, `parseField()`, `parseIndex()`
- Add `KeyValue` field to `KPath` (nullable, unused initially)
- When `!key` support is added, add `parseKKeyValue()` helper and extend `parseKFrag()` to handle `key(<value>)` syntax
- `KPath.String()` should output `key(<value>)` when `KeyValue` is set (similar to how `Path.String()` outputs `[*]` when `IndexAll` is true)

## Scope: What We're NOT Updating

1. **`storage` package**: Mid-revamp, unstable - don't update for now
2. **`compact` package**: Possibly deprecated - don't update for now
3. **`go-tony/ir.Path`**: Higher level, can coexist with kpath - not replaced
4. **Old test files**: Only update if they test new functionality

**Focus**: New logd-specific functionality (TokenSource/TokenSink, incremental parsing, Index) uses kpaths forward-looking.

## Implementation Checklist

- [ ] Phase 1.1: Move `kindedpath` code into `ir` package as `ir/kpath.go`
- [ ] Phase 1.2: Implement `KPath` struct (similar to `Path`, in `ir` package)
- [ ] Phase 1.2: Implement `ParseKPath()` function (similar to `ParsePath()`, design for future !key syntax)
- [ ] Phase 1.2: Implement helper functions (`parseKFrag()`, `parseKField()`, `parseKIndex()`, `parseKSparseIndex()`)
- [ ] Phase 1.2: Implement `KPath.String()` method (similar to `Path.String()`)
- [ ] Phase 1.2: Implement `KPath.Parent()`
- [ ] Phase 1.2: Implement `KPath.IsChildOf()`
- [ ] Phase 1.2: Implement `KPath.Compare()`
- [ ] Phase 1.2: Implement `Node.GetKPath()` method (similar to `Node.GetPath()`)
- [ ] Phase 1.2: Implement `Node.ListKPath()` method (similar to `Node.ListPath()`)
- [ ] Phase 1.2: Implement `GetAllKPaths()` helper function
- [ ] Phase 1.2: Implement `kpath.ToKindedPath()`
- [ ] Phase 1.2: Implement `kpath.FromKindedPath()`
- [ ] Phase 2.1: Remove JSONPath tracking from TokenSource
- [ ] Phase 2.1: Remove JSONPath tracking from TokenSink
- [ ] Phase 2.2: Implement kinded path tracking in TokenSource
- [ ] Phase 2.2: Implement kinded path tracking in TokenSink
- [x] Phase 3.1: Remove RelPath from LogSegment ✅
- [x] Phase 3.2: Update Index.Add() to use KindedPath ✅
- [ ] Phase 3.2: Update Index.LookupRange() to use KindedPath
- [ ] Phase 3.3: Update LogSegCompare() to use KindedPath
- [ ] Phase 3.4: Update Iterator to use KindedPath
- [ ] Phase 4.1: Update streaming indexer
- [ ] Phase 4.2: Update incremental parsing (if needed)
- [ ] Phase 4.3: Update code generation (index_gen.go)
- [ ] Phase 5.1: Verify JSONPath removal complete
- [ ] Phase 5.2: Update documentation
- [ ] Phase 5.3: Add comprehensive tests

## Timeline Estimate

- **Phase 1**: 2-3 days (package rename + implementation, design for future)
- **Phase 2**: 2-3 days (Remove JSONPath, implement kpath tracking)
- **Phase 3**: 2-3 days (Index updates)
- **Phase 4**: 1-2 days (New functionality updates)
- **Phase 5**: 1-2 days (Cleanup and testing)

**Total**: ~8-13 days

## Next Steps

1. Review and approve this proposal
2. Start with Phase 1 (rename and implement `kpath` package, design for future !key syntax)
3. Implement incrementally, testing after each phase
4. Update this document as implementation progresses
