# Kinded Path Package

## Overview

The `kindedpath` package provides operations for kinded paths - a unified path representation that encodes node kinds in the path syntax itself.

## Location

**Package**: `go-tony/system/logd/storage/kindedpath`

**Rationale**: 
- Path operations are fundamental and may replace `go-tony/ir/path` in the future
- Separate package allows for future promotion to `go-tony/kindedpath` or `go-tony/ir/kindedpath`
- Keeps path operations separate from storage-specific code

## Terminology

**Unified Terminology**: Everything uses "kinded path" consistently:
- ✅ **Kinded path**: Path string that encodes node kinds (e.g., "a.b[0]")
- ❌ ~~Extract path~~: Replaced with "kinded path"
- ❌ ~~Path syntax~~: Replaced with "kinded path"

## Kinded Path Syntax

Kinded paths encode node kinds in the syntax:
- `a.b` → Object accessed via `.b` (a is ObjectType)
- `a[0]` → Dense Array accessed via `[0]` (a is ArrayType)
- `a{0}` → Sparse Array accessed via `{0}` (a is SparseArrayType)

See `go-tony/system/logd/api/kinded_paths.md` for full syntax specification.

## Package Structure

### `path.go`
- `PathSegment` struct (recursive path structure)
- `Parse(kindedPath string) (*PathSegment, error)` - parse kinded path string
- `PathSegment.String()` - convert to kinded path string
- `PathSegment.Parent()` - get parent path
- `PathSegment.IsChildOf()` - check if child of parent
- `PathSegment.Compare()` - compare paths

### `extract.go`
- `Get(diff *ir.Node, kindedPath string) (*ir.Node, error)` - extract nested path from diff
- `ExtractAll(diff *ir.Node) (map[string]*ir.Node, error)` - extract all paths from diff

## Usage in Storage

### Index Structure
```go
type LogSegment struct {
    StartCommit int64
    StartTx     int64
    EndCommit   int64
    EndTx       int64
    KindedPath  string  // "a.b.c" (kinded path - used for querying and extraction; "" for root)
    LogPosition int64   // Byte offset in log file
}
```

### Reading
```go
// Query index
segment := index.Query("a.b.c", commit=2)
// Returns: {LogPosition: 200, KindedPath: "a.b.c"}

// Read entry
entry := ReadEntryAt(logFile, 200)
// Entry.Diff = {a: {b: {c: {z: 3}}}}

// Extract using kinded path
childDiff := kindedpath.Get(entry.Diff, segment.KindedPath)
// Result: {z: 3}
```

### Indexing
```go
// Extract all paths from diff
paths := kindedpath.ExtractAll(entry.Diff)
// Returns: {"a": {...}, "a.b": {...}, "a.b.c": {...}}

// Index each path
for kindedPath, value := range paths {
    index.Add(kindedPath, logPosition, kindedPath, commit, tx)
}
```

## Future Promotion

This package may be promoted to:
- `go-tony/kindedpath` (top-level package)
- `go-tony/ir/kindedpath` (under ir package)

The package is designed to be independent of storage-specific code to facilitate this promotion.
