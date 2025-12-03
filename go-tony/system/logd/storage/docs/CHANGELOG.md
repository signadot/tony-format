# Design Changelog

This document tracks significant design changes and decisions.

## RelPath Removed (Current)

**Date**: Current
**Change**: Removed `RelPath` field from `LogSegment` struct

**Rationale**:
- In the new design, all entries are written at root (no "relative" concept)
- `RelPath` and `KindedPath` were always the same
- `KindedPath` is more descriptive (specifies what to extract from root diff)

**Impact**:
- Simpler `LogSegment` structure
- No redundancy
- Single source of truth for paths

**Updated Files**:
- `DESIGN.md` - Updated LogSegment structure
- `implementation_plan.md` - Updated to reflect removal
- `current_state_summary.md` - Updated structure
- `kindedpath_package.md` - Updated examples
- `design_critique.md` - Marked as resolved

**See**: `relpath_analysis.md` for detailed analysis
