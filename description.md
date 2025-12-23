# Schema diff computation

Parent: #55

## Problem

Given two schemas v1 and v2, compute path patterns describing where they differ. Needed for:
1. Sandbox isolation - determine which paths sandbox owns
2. Migration scope - identify data requiring transformation

## Challenge

Schemas define *types*, not concrete paths. Recursive definitions can nest arbitrarily deep. Cannot enumerate affected paths - need patterns.

## Path Pattern Language

**Single segment wildcards:**
- `*` - any single object field
- `(*)` - any key in keyed array  
- `[*]` - any index in indexed array

**Depth operators:**
- `**` - zero or more segments of any kind
- `(pattern)*` - zero or more repetitions of pattern (structured recursion)

## Examples

| Scenario | Pattern |
|----------|---------|
| Field added to root | `newfield` |
| Field added to recursive type | `(children(*))*newfield` |
| Array indexing changed | `users[*]`, `users(*)` |
| Nested field in recursive | `(children(*))*metadata.modified` |

## Tasks

1. Define pattern matching semantics
2. Detect self-referential paths in definitions (recursion edges)
3. Implement parallel schema walk with pattern generation
4. Pattern matching algorithm for concrete kpaths