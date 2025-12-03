# Code That Can Be Deleted for Redesign

## 1. State Cache (Entire File)
**File**: `cache.go`
**Reason**: Entirely tied to the broken `ReadStateAt` implementation. Caches incorrect hierarchy construction.

**Dependencies**:
- Used by `storage.go` (initialization, updates)
- Tests disable it (`storage.stateCache = nil`)

**Delete**:
- `cache.go` (entire file)
- All `stateCache` fields and methods in `storage.go`
- `updateCompactedState()`, `clearCompactedState()` methods

## 2. Broken Read Functions
**Functions**: `ReadStateAt()`, `ReadCurrentState()`
**Location**: `storage.go` lines 192-301
**Reason**: Fundamentally broken - doesn't construct hierarchy correctly, just applies diffs blindly.

**Dependencies**:
- Used by transaction system (`ReadCurrentState` for match evaluation)
- Used by many tests
- **Note**: Transaction system will need new read function anyway

**Delete**:
- `ReadStateAt()` function
- `ReadCurrentState()` function
- Related helper code in `ReadStateAt` (lines 192-290)

## 3. Compaction Alignment Logic (Broken)
**Function**: `shouldCompactNow()` in `compact/dir_compact.go`
**Location**: Lines 288-314
**Reason**: Has TODO comment, alignment logic is fundamentally broken

**Dependencies**:
- Used by `addSegment()` to decide when to compact
- **Note**: Will need new alignment logic based on correctness criteria

**Keep for now**:
- The correctness criteria understanding
- The compaction structure itself

**Delete**:
- Current `shouldCompactNow()` implementation
- Can keep the function signature, rewrite the logic

## 4. Scattered Path Mapping (Partial)
**Functions**: `FS.PathToFilesystem()`, `FS.FilesystemToPath()`
**Location**: `fs.go`, `paths/map.go`
**Reason**: Assumes scattered layout by virtual path, doesn't match hierarchy

**Dependencies**:
- Used extensively throughout codebase
- **Note**: Will need new path mapping for hierarchical layout

**Keep for now**:
- The concept of path mapping
- Will need to rewrite for hierarchical layout

**Delete later**:
- Current implementation once new hierarchical layout is designed

## Summary

### Immediate Deletions (Safe Now):
1. ✅ `cache.go` - entire file
2. ✅ `ReadStateAt()` / `ReadCurrentState()` functions
3. ✅ `stateCache` field and all related methods in `storage.go`
4. ✅ `shouldCompactNow()` implementation (keep signature, rewrite logic)

### Keep But Will Rewrite:
- Path mapping functions (need new hierarchical version)
- Compaction structure (keep, fix alignment logic)
- Index structure (keep as-is)
- **Read function**: One correct read function will replace `ReadStateAt()` - used by everything including transactions

### Tests to Update:
- All tests using `ReadStateAt` / `ReadCurrentState`
- Tests that disable `stateCache` (can remove those lines)
- Compaction alignment tests (will need new tests)
