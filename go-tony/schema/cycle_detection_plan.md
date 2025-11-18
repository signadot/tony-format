# Schema Cycle Detection Implementation Plan

## Problem Statement

Schema definitions in the `define:` field can create cycles through `.name` references. Some cycles are valid (can be instantiated), while others are impossible to realize (similar to `struct S { F S }` in Go).

## Valid vs Impossible Cycles

### Valid Cycles (have escape hatches)
- **Array-based cycles**: `children: .array(.node)` - arrays can be empty `[]`
- **Nullable cycles**: `parent: !or [null, .node]` - can be null
- **Optional fields**: Fields not in match pattern are optional/ignored

### Impossible Cycles (no escape hatches)
- **Required direct self-reference**: `node: { parent: .node }` where `parent` is required and non-nullable
- **Mutual required cycles**: `a: { b: .b }` and `b: { a: .a }` where both are required
- **Chain cycles**: `a -> b -> c -> a` where all fields are required

## Detection Algorithm

### Phase 1: Build Dependency Graph
1. Parse all definitions in `define:` field
2. For each definition, extract all `.name` references (including parameterized like `.array(t)`)
3. Build directed graph: `definition -> [referenced definitions]`
4. Track field context for each edge (field name, whether nullable, whether array)

### Phase 2: Detect Cycles
1. Find strongly connected components (SCCs) in the dependency graph
2. For each SCC that forms a cycle:
   - Trace all paths in the cycle
   - Check if each edge has an escape hatch

### Phase 3: Validate Escape Hatches
For each edge in a cycle, check if it has an escape hatch:
- **Array type**: Edge references `.array(...)` or similar - safe (can be empty)
- **Nullable**: Edge uses `!or` that includes `null` - safe (can be null)
- **Optional field**: Field is not in the match pattern - safe (can be absent)
- **Required non-nullable**: Edge is required and non-nullable - problematic

### Phase 4: Report Impossible Cycles
If a cycle has no escape hatches on any path, report it as an error:
- List the cycle path
- Indicate which fields are required
- Suggest fixes (make nullable, use array, make optional)

## Implementation Steps

### Step 1: Graph Building
- Create `cycle_detector.go` in `schema/` package
- Implement `buildDependencyGraph(schema *ir.Node) (*DependencyGraph, error)`
- Parse `define:` field and extract `.name` references
- Handle parameterized references (`.array(t)`, etc.)

### Step 2: Cycle Detection
- Implement SCC detection (Tarjan's algorithm or similar)
- Return list of cycles found

### Step 3: Escape Hatch Analysis
- For each cycle, analyze each edge:
  - Check if field is in match pattern (required vs optional)
  - Check if field type is array
  - Check if field allows null (`!or` with null)
- Mark cycles as valid or impossible

### Step 4: Integration
- Add validation function: `ValidateCycles(schema *ir.Node) error`
- Call during schema parsing/validation
- Return descriptive errors for impossible cycles

### Step 5: Testing
- Test cases for:
  - Valid cycles (array-based, nullable)
  - Impossible cycles (required mutual, required self-ref)
  - Complex cycles (chain cycles, nested cycles)
  - Edge cases (parameterized types, nested references)

## Key Considerations

1. **Matching semantics**: Fields in match pattern are required; fields not in pattern are optional
2. **Array types**: `.array(t)` and similar can always be empty, providing escape hatch
3. **Null handling**: `!or [null, .node]` allows null, providing escape hatch
4. **Parameterized types**: Need to handle `.array(t)` where `t` might be part of cycle
5. **Nested references**: References might be nested in objects/arrays, need to traverse

## Example Cases

### Valid Example (from docs)
```tony
node:
  parent: .node  # required, but...
  children: .array(node)  # array can be empty - escape hatch!
```
**Analysis**: Cycle exists, but `children` array provides escape hatch.

### Impossible Example
```tony
a:
  b: .b  # required
b:
  a: .a  # required
```
**Analysis**: Mutual cycle with no escape hatches - impossible.

### Impossible Example 2
```tony
node:
  parent: .node  # required, non-nullable
  # no array or nullable fields
```
**Analysis**: Self-reference with no escape hatch - impossible.
