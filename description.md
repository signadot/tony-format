# Array diff optimization for object elements

## Observation

For arrays of objects, `DiffArrayByIndex` treats all objects as equal at the matching level (returns just "object" in `summaryStr`). This produces valid but non-minimal diffs.

Example:
```
a: [{id: 1} {id: 2}]
b: [{id: 0} {id: 1} {id: 2}]

Current diff: cascading replaces + insert at end
Minimal diff: insert {id: 0} at index 0
```

## Analysis

- Current behavior is **correct** - applying the diff produces the right result
- Minimality would require content-aware matching (hashing objects, more complex LCS)
- This is an **optimization**, not a bug
- Primitives work well because `summaryStr` includes value

## Design considerations

- Is the complexity worth it?
- Should logd or other consumers assume minimal diffs? **No**
- Keyed arrays (`!key(id)`) already solve identity matching for use cases that need it

## Location

`libdiff/array_by_index.go:91-94` - `summaryStr` for ObjectType