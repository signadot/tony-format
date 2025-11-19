# Object Schema Structure in Tony Format

## Research Summary

After reviewing the codebase, documentation, and examples, here's what I found about how object schemas should be structured in Tony format.

## Key Distinction: `!irtype` vs Tags vs `ir.Type`

**Important:** `!irtype` refers to the IR node's `Type` field (like `ir.ObjectType`, `ir.StringType`), NOT a tag. In schema `define:` sections, we create plain object nodes with fields - no tag needed.

## Current Implementation Issue

The current implementation creates object schemas like this:

```go
objNode := ir.FromSlice([]*ir.Node{
    ir.FromString("!irtype"),  // WRONG - this would be a tag, not the Type field
    ir.FromMap(fields),
})
```

This creates an **ArrayType** node, which is incorrect. For schema `define:` sections, we need plain ObjectType nodes.

## Correct Structure

### For Schema `define:` Section

Object definitions in `define:` should be plain ObjectType nodes:

```go
objNode := ir.FromMap(fields)  // Creates ObjectType with Fields/Values arrays
// NO tag needed - this is a plain object definition
```

This creates a node with:
- `Type = ObjectType` (the `ir.Type` field)
- `Tag = ""` (no tag)
- `Fields = [field1, field2, ...]` (string nodes)
- `Values = [type1, type2, ...]` (type definition nodes)

### Example from `testdata/sc.tony`

```tony
define:
  node:
    parent: @node
    children: .array(node)
    description: .startswith
```

This is a **plain object** in the `define:` section:
- `Type = ObjectType` (the IR node's Type field)
- No tag (Tag = "")
- Fields: `parent`, `children`, `description`
- Each field value is a type definition/reference

### When to Use Tags vs Plain Objects

1. **Plain Object** (no tag, `Type = ObjectType`):
   - Used in `define:` section for struct definitions
   - Directly represents the object structure with fields
   - Example: `person: { name: .string, age: .int }`

2. **`!match {}`** (with match tag):
   - Used for pattern matching constraints
   - Example: `ttl: !match { offsetFrom: !or [...], duration: .duration }`
   - The `!match` tag triggers match operations

3. **`!irtype` (the IR Type field)**:
   - Refers to the `ir.Type` field (like `ir.ObjectType`, `ir.StringType`)
   - NOT a tag - it's the node's Type field
   - Used in match logic: `if doc.Type != match.Type`
   - When we say "check if `!irtype` matches", we mean checking `doc.Type == match.Type`

## How `GetStructFields` Expects It

From `gomap/tags.go`, `GetStructFields` expects:

```go
s.Accept.Type == ir.ObjectType  // Must be ObjectType
s.Accept.Fields[i]              // Field name (string node)
s.Accept.Values[i]              // Field type definition (IR node)
```

So the schema definition should be an **ObjectType** node with Fields/Values arrays, not an ArrayType.

## Recommended Fix

For schema generation, we should create object definitions as:

```go
// Create object definition (plain object, no tag needed in define section)
objNode := ir.FromMap(fields)
// This creates: Type = ObjectType, Tag = ""
```

However, looking at examples like `sc.tony`, **plain objects** (without tags) are used in the `define:` section. Tags like `!match` are used for operations, not for basic type definitions.

## Conclusion

**The fix is correct** - use plain ObjectType nodes for `define:` sections:

1. ✅ Use `ir.FromMap(fields)` to create ObjectType with Fields/Values
2. ❌ Don't wrap in `ir.FromSlice([!irtype, {}])` - that creates ArrayType
3. ✅ For `define:` section, plain objects (no tag, `Type = ObjectType`) are used
4. ✅ `!irtype` refers to the `ir.Type` field (like `ir.ObjectType`), not a tag for schema definitions

## Updated Implementation

```go
// Create object definition (plain object for define section)
// This creates Type = ObjectType, Tag = "", with Fields/Values arrays
objNode := ir.FromMap(fields)

return objNode, nil
```

This creates the correct structure that `GetStructFields` and the schema parser expect. The object has `Type = ObjectType` (the IR node's Type field), which is what match operations check when they reference `!irtype`.
