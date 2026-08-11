# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This is the **storage** package within the **logd** (log daemon) system, part of the larger **tony-format/go-tony** project. The storage layer provides transactional, path-indexed document storage using the Tony format (an enhanced YAML/JSON alternative with rich metadata support).

## Bible

Your bible is in ../../../../docs/tony.md

Your rock is in ../../../../docs/ir.md

You believe that ../../../../docs/tonyapi.md will provide enormous value to users and are proud to be a part of making that happen.

You conclude that
- hiding bugs is bad
- you can't get there from here if the ground collapses between
- the immediate sub-goals are imbued in this context, and that implies deviating from them regularly.





## Tony Format Quick Reference

**Tony** is a self-describing data format that combines structure with metadata:
- Supports both object `{key: value}` and array `[item1, item2]` syntax
- Uses **kinded paths** like `foo.bar[0]` (object field → array index) or `data{3002}.settings` (sparse array/map → object)
- **Kinded path syntax**: `.field` (object), `[index]` (dense array), `{index}` (sparse array/map), `.*` / `[*]` / `{*}` (wildcards)
- Rich semantic IR (Intermediate Representation) with tagged unions, comments, and type information

## Common Commands

### Testing
```bash
# Run all tests in storage package
go test -v .

# Run tests in a specific subpackage
go test -v ./index
go test -v ./tx
go test -v ./internal/snap

# Run specific test by pattern
go test -v ./index -run TestIndex_LookupRange

# Run all tests in project (from go-tony root)
cd /Users/scott/Dev/github.com/signadot/tony-format/go-tony
go test ./...
```

### Building
```bash
# Build from go-tony root
cd /Users/scott/Dev/github.com/signadot/tony-format/go-tony

# Build the 'o' tool (primary CLI for Tony format)
go build ./cmd/o

# Build tony-codegen (code generator)
go build ./cmd/tony-codegen

# Build tony-lsp (language server)
go build ./cmd/tony-lsp
```

### Working with Storage
```bash
# Storage is a library package, not a standalone binary
# Import path: github.com/signadot/tony-format/go-tony/system/logd/storage
```

## Architecture

### Core Concepts

1. **Multi-Participant Transactions**: Multiple clients can patch different paths in the same transaction. Patches are merged atomically before commit.

2. **Path-Based Indexing**: Hierarchical index mirrors kinded path structure, enabling efficient lookups by path and commit range.

3. **Event-Based Snapshots**: Snapshots store `stream.Event` sequences with size-bound indices, not full IR nodes. This enables scalable storage for large documents.

4. **Double-Buffered Logging**: Two alternating log files (logA/logB) provide atomic switching for consistent snapshots.

### Package Structure

```
storage/
├── storage.go              # Main Storage API (Open, NewTx, GetTx)
├── snapshot_interface.go   # SnapshotReader interface
├── read_patches.go         # Read patches from dlog
├── tx/                     # Transaction coordination
│   ├── tx.go              # Tx interface and Patcher handles
│   ├── coord.go           # Transaction coordination logic
│   ├── store.go           # In-memory transaction storage
│   ├── merge.go           # MergePatches - combines multi-path patches
│   └── match.go           # Match evaluation for conditional updates
├── index/                  # Path-based segment index
│   ├── index.go           # Hierarchical index structure
│   └── index_iterator.go  # Range iteration over segments
├── internal/dlog/          # Double-buffered write-ahead log
│   ├── dlog.go            # DLog management (logA/logB switching)
│   ├── entry.go           # Log entry format
│   └── snapshot.go        # Snapshot coordination
├── internal/snap/          # Event-based snapshot storage
│   ├── snap.go            # Snapshot reader/writer
│   ├── builder.go         # Build snapshots from events
│   ├── index.go           # Size-bound path→offset index
│   ├── io.go              # Snapshot file I/O
│   └── archive/           # Legacy IR-node-based snapshots
└── internal/seq/           # Atomic sequence counters
    └── sequence.go        # Commit and transaction ID generation
```

### Key Dependencies (Same Go Module)

These packages are critical to understanding storage behavior:

- **`ir/`**: Intermediate representation of Tony documents. Core type: `ir.Node` (tree with fields, values, tags, comments)
- **`ir/kpath/`**: Kinded path parsing and navigation. Syntax: `.field`, `[index]`, `{sparse-index}`, wildcards `.*`/`[*]`/`{*}`
- **`stream/`**: Event-based encoding/decoding. Core types: `stream.Event`, `stream.Encoder`, `stream.Decoder`, `stream.State`
- **`schema/`**: Tony schema definitions (type validation, constraints)
- **`token/`**: Low-level tokenization (lexer)
- **`parse/`**: Parse Tony text into IR

### Data Flow: Writing a Patch

```
Client Patch Request
    ↓
Storage.NewTx(participantCount) → creates Tx
    ↓
tx.NewPatcher(patch) → returns Patcher handle
    ↓
[Multiple participants add patches to different paths]
    ↓
patcher.Commit() → triggers merge
    ↓
tx/merge.go:MergePatches() → combines patches into single root-level patch
    ↓
DLog.WriteEntry() → appends to active log (logA or logB)
    ↓
Index.Add(segment) → records path→offset mapping
    ↓
Seq.NextCommit() → generates monotonic commit ID
    ↓
Result returned to all participants
```

### Data Flow: Reading at a Path

```
ReadStateAt(kpath, commit)  [NOT YET IMPLEMENTED]
    ↓
Index.LookupRange(kpath, commit) → finds LogSegments affecting path
    ↓
For each segment:
    DLog.ReadEntryAt(logFile, position) → loads patch
    Extract relevant portion for kpath
    ↓
Merge all patches → reconstruct state at path
```

### Key Types

**Storage** (`storage.go`):
- `Storage.Open()`: Initialize or open existing storage directory
- `Storage.NewTx(participantCount, meta)`: Create multi-participant transaction
- `Storage.GetTx(txID)`: Retrieve active transaction
- `Storage.Close()`: Persist index and close cleanly

**Transaction** (`tx/tx.go`):
- `Tx` interface: Represents coordinated transaction
- `Patcher`: Participant's handle for adding patches and waiting for commit
- `Result`: Outcome with commit ID, match evaluation, errors
- `PatcherData`: One participant's patch contribution

**Index** (`index/index.go`):
- `Index`: Hierarchical tree mirroring kinded paths
- `LogSegment`: Path, commit range, log file, offset
- `Index.Add(segment)`: Insert segment, creating child indices as needed
- `Index.LookupRange(commit)`: Find segments affecting this index's path
- `Index.LookupWithin(commit)`: Find segments active at specific commit

**DLog** (`internal/dlog/dlog.go`):
- `DLog`: Manages logA and logB
- `DLogFile`: Single log file with append/read operations
- `Entry`: Patch with metadata (commit ID, transaction ID, timestamp)

**Snapshot** (`internal/snap/snap.go`):
- Event-based format: `[event-stream-size][index-size][events][index]`
- `Builder`: Constructs snapshot from event stream
- `Index`: Maps kinded paths to byte offsets in event stream

## Important Patterns

### Kinded Paths Are Fundamental

Almost every operation uses kinded paths. Understanding `ir/kpath/` is essential:
- Paths encode both navigation and structure type
- `.field` vs `[index]` vs `{sparse}` determines interpretation
- Wildcards enable bulk queries: `resources[*].status`

### Streaming Over Full Documents

The architecture favors event streaming over full document reconstruction:
- `stream.Encoder` / `stream.Decoder` process events incrementally
- `stream.State` tracks structural position (current path, nesting, array indices)
- Snapshots store events directly, not materialized IR nodes
- containers MUST be treated out-of-memory for subpaths

### Multi-Participant Coordination

Transaction model enables atomic merging of disjoint path updates:
- Multiple HTTP handlers can patch `foo.a` and `foo.b` simultaneously
- `tx/merge.go:MergePatches()` combines into `{foo: {a: patch1, b: patch2}}`
- Single atomic commit with one merged patch

### Hierarchical Index Design

The index mirrors document structure for efficiency:
- Top-level index for root paths
- Child indices for nested paths
- Ancestor fallback: if exact path not indexed, find nearest ancestor and scan from there

## Current Implementation Status

**Completed**:
- Core storage API (`storage.go`)
- Transaction coordination (`tx/`)
- Path-based index (`index/`)
- Double-buffered logging (`internal/dlog/`)
- Sequence tracking (`internal/seq/`)
- Event-based snapshot design and partial implementation (`internal/snap/`)

**In Progress**:
- Event-based snapshot builder and reader
- Integration with `SnapshotReader` interface

**Not Implemented**:
- `Storage.ReadStateAt()` - full state reconstruction at path/commit
- Snapshot compaction
- Comment support in event streaming (Phase 2 feature)

## Key Files by Purpose

| Purpose | File |
|---------|------|
| Storage entry point | `storage.go` |
| Transaction interface | `tx/tx.go` |
| Patch merging logic | `tx/merge.go` |
| Index structure | `index/index.go` |
| Index iteration | `index/index_iterator.go` |
| DLog management | `internal/dlog/dlog.go` |
| Log entries | `internal/dlog/entry.go` |
| Sequence counters | `internal/seq/sequence.go` |
| Event-based snapshot | `internal/snap/snap.go` |
| Snapshot builder | `internal/snap/builder.go` |
| Snapshot index | `internal/snap/index.go` |
| Snapshot I/O | `internal/snap/io.go` |
| Legacy IR snapshots | `internal/snap/archive/` |

## Documentation

Extensive design documentation exists in:
- `docs/` directory (within storage package)
- Root `go-tony/` directory (project-wide design docs)

Key documents:
- `docs/EVENT_BASED_SNAPSHOTS.md` - Current snapshot design
- `docs/CLEANUP_SUMMARY.md` - Recent package cleanup
- Multiple streaming design docs in `go-tony/` root

## Important Notes

1. **Path Encoding**: When working with paths, always use kinded path syntax. JSONPath `$.foo.bar[0]` can be converted to kinded path `foo.bar[0]`.

2. **Event Stream State**: The `stream.State` type is critical for understanding position while decoding events. It maintains current kinded path, bracket stack, and array indices.

3. **Merge Semantics**: Patches use merge-patch semantics. There is a !delete mergeop for deletion. See `tx/merge.go` for multi-path merge logic.

4. **Index Granularity**: The index doesn't record every path, only significant checkpoints (size-bound). Reads may require scanning events from an ancestor path.

5. **File Extensions**: Tony files typically use `.tony` extension. The format also supports reading YAML and JSON.

6. **Tony Code Generation**: The project uses `tony-codegen` to generate Go structs from Tony schemas. Generated Go files have `_gen.go` suffix and generated schema are placed in schema_gen.tony.

7. **Test Patterns**: Tests often use testdata files (`.tony` format) and compare expected vs actual IR structures. See `*_test.go` files for patterns.

8. **Concurrent Access**: The current implementation is not fully concurrent-safe. Transaction coordination handles multi-participant coordination, but broader concurrent access patterns are limited.
