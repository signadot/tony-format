# Design Decisions Archive

This document archives the decision-making process that led to the final design. It is kept for historical reference but should not be used as the primary design reference.

## Main Design Document

**Use `DESIGN.md` for the final design reference.**

## Decision History

### Path Representation

**Initial**: Path stored as string in `LogEntry`
**Considered**: Recursive `PathSegment` struct in `LogEntry`
**Final**: Path removed from `LogEntry` (always root), use `kindedpath` package for operations

**Documents**: `log_entry_critique.md`, `remove_path_field.md`

### Write Timing

**Initial**: Pre-write to temp file, append on commit
**Considered**: Direct append, in-memory buffer + temp file
**Final**: In-memory buffer only, atomic append on commit

**Documents**: `write_timing_critique.md`

### Index Persistence

**Initial**: In-memory only, rebuild on startup
**Considered**: Persisted index, no persistence
**Final**: Hybrid approach - persist with max commit for fast startup

**Documents**: `index_critique.md`

### Parent Diff Storage

**Initial**: Separate entries for each path
**Considered**: Subtree-inclusive parent diffs at immediate parent
**Final**: Always write at root, merge all paths into single root diff

**Documents**: `parent_diff_indexing.md`, `single_entry_per_path.md`, `merge_to_root.md`

### Path Extraction

**Initial**: ExtractKey (single key)
**Considered**: ExtractPath (full path)
**Final**: KindedPath (unified kinded path representation)

**Documents**: `extract_key_vs_path.md`

### Log File Format

**Initial**: Tony wire format, self-describing (no delimiters)
**Final**: Length-prefixed entries (`[4 bytes: length][Tony wire format]`)

**Documents**: `length_prefix_analysis.md`

### Terminology

**Initial**: Mixed terminology (ExtractPath, path syntax, extract path)
**Final**: Unified to "kinded path" throughout

**Documents**: Various design docs

### RelPath Removal

**Initial**: `LogSegment` had both `RelPath` and `KindedPath` fields
**Final**: `RelPath` removed, use `KindedPath` only

**Rationale**: 
- All entries written at root (no "relative" concept)
- `RelPath` and `KindedPath` were always the same
- `KindedPath` is more descriptive

**Documents**: `relpath_analysis.md`

## Archived Documents

The following documents contain the decision-making process but are archived:

- `log_entry_critique.md` - Critique of LogEntry schema
- `write_timing_critique.md` - Analysis of write timing approaches
- `index_critique.md` - Analysis of index persistence
- `parent_diff_indexing.md` - Analysis of parent diff storage
- `single_entry_per_path.md` - Why merge to root
- `merge_to_root.md` - Analysis of merging to root
- `extract_key_vs_path.md` - ExtractKey vs ExtractPath analysis
- `length_prefix_analysis.md` - Length-prefixed format analysis
- `remove_path_field.md` - Why remove Path field
- `parent_diff_write_contention.md` - Write contention analysis
- `read_path_sanity_check.md` - Read operation sanity check
- `bottom_up_critique.md` - Bottom-up component analysis

## Current State Documents

These documents reflect the current state and are kept for reference:

- `DESIGN.md` - **Primary design reference**
- `implementation_plan.md` - Step-by-step implementation guide
- `kindedpath_package.md` - Kinded path package documentation
- `current_state_summary.md` - Current implementation state

## Notes

- All design decisions are documented in `DESIGN.md`
- Historical decision-making process is archived here
- Use `DESIGN.md` as the source of truth for implementation
