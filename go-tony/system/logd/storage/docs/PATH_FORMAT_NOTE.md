# Path Format Note

## Important: All Paths Use Kinded Format

All design documents have been updated to use **kinded paths** throughout (e.g., `a.b`, `a.b.c`) instead of the old "/a/b" format.

## Kinded Path Syntax

- `a.b` → Object accessed via `.b` (a is ObjectType)
- `a[0]` → Dense Array accessed via `[0]` (a is ArrayType)
- `a{0}` → Sparse Array accessed via `{0}` (a is SparseArrayType)
- `""` (empty string) → Root path

## Conversion

**Old format** (deprecated): `/a/b/c`
**New format** (current): `a.b.c`

**Root**:
- Old: `/`
- New: `""` (empty string)

## Documents Updated

All primary design documents have been updated:
- ✅ `DESIGN.md` - Uses kinded paths throughout
- ✅ `implementation_plan.md` - Uses kinded paths throughout
- ✅ `kindedpath_package.md` - Uses kinded paths throughout
- ✅ `current_state_summary.md` - Uses kinded paths throughout

## Archived Documents

Archived documents may still contain old path format references. These are kept for historical reference only and should not be used as design references.
