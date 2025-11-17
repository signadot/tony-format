# Schema Tags Reference

This page documents tags that are commonly used in [Tony Schema](../tonyschema.md) definitions.

For complete operation documentation, see the [Operations Reference](./generated/index.md).

## Tags Used in Schemas

| Tag | Category | Summary | Schema Usage |
|-----|----------|---------|-------------|
| [`!all`](./generated/mergeop.md#all) | mergeop | Apply match/patch to all array or object elements | Used in schema `define:` sections to constrain array/object elements. Example... |
| [`!and`](./generated/mergeop.md#and) | mergeop | Match all conditions (logical AND) | Used in schema `define:` sections to combine multiple constraints. Example: `... |
| [`!not`](./generated/mergeop.md#not) | mergeop | Negate a match condition | Used in schema `accept:` sections to exclude certain types. Example: `accept:... |
| [`!or`](./generated/mergeop.md#or) | mergeop | Match any condition (logical OR) | Used in schema `accept:` and `define:` sections to allow multiple valid types... |
| [`!type`](./generated/mergeop.md#type) | mergeop | Match by node type | Fundamental schema operation for type checking. Used in `define:` sections: `... |

## Schema Context

Tags in schemas are used in two main contexts:

1. **`define:` section** - Define reusable type constraints
2. **`accept:` section** - Define what the schema accepts

Example:

```tony
!schema
define:
  bool: !type true
  array(t): !and
    - .array
    - !all.type t
accept:
  !or
    - .bool
    - .array(string)
```

## Related Documentation

- [Tony Schema](../tonyschema.md) - Complete schema documentation
- [Operations Reference](./generated/index.md) - All operations
- [Mergeop Operations](./generated/mergeop.md) - Match and patch operations
