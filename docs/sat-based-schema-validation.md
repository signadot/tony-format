# SAT-Based Schema Satisfiability Checking

*Co-authored by a human and Claude (Anthropic). This post describes both the technical approach and how we arrived at it together.*

## The Problem

Schema languages let you define types with recursive references. For example, a linked list node:

```yaml
define:
  node:
    value: int
    next: !or [null, .node]
```

This is valid because the recursion has an **escape hatch** - `null` breaks the cycle. A finite value like `{value: 1, next: null}` satisfies the constraint.

But what about this?

```yaml
define:
  node:
    next: !and
    - !not null
    - .node
```

This says: `next` must be *not null* AND must be a `.node`. The only way to satisfy this is with an infinite chain of nodes - impossible to represent as a finite value. This is an **impossible cycle**.

## The Solution: SAT Solving

We treat the schema as a **boolean formula** and check satisfiability.

### Formula Construction

Each schema construct maps to boolean operations:

| Schema | Formula |
|--------|---------|
| `!or [X, Y]` | X ∨ Y |
| `!and [X, Y]` | X ∧ Y |
| `!not X` | ¬X |
| `{a: X, b: Y}` | X ∧ Y (fields are AND-ed) |
| `[X, Y]` (untagged) | X ∧ Y (piecewise matching) |

### Variable Allocation

Here's the subtle part: when are two type references the *same* variable?

Consider `{a: string, b: !not string}`:
- The `string` at field `a` and field `b` refer to *different positions*
- A value `{a: "hi", b: 42}` makes `string_a = true, string_b = false`
- These need **separate variables**

But `!and [string, int]` at the same position:
- Both constrain the *same* value
- Nothing can be both string AND int
- Need **mutex constraint**: ¬(string ∧ int)

The rule: allocate variables per **(position, type)** pair, where:
- Object fields and array indices create new positions
- Boolean operators (`!and`, `!or`, `!not`) stay at the same position

### Self-Reference Handling

For cycle detection, we substitute self-references with `false`:

```
Checking: node: !or [null, .node]
Expand .node → false (self-reference)
Formula: null ∨ false
Result: SAT (null=true works) → Escape exists ✓
```

```
Checking: node: !and [!not null, .node]
Expand .node → false
Formula: (¬null) ∧ false = false
Result: UNSAT → Impossible cycle ✓
```

This unified approach handles both:
- **Non-recursive contradictions**: `!and [string, int]` → UNSAT
- **Impossible cycles**: `!and [!not null, .node]` → UNSAT

### Cross-References

For mutually recursive definitions like `a: .b`, `b: .a`:

```
Checking a:
  Expand .b → .a (self-ref) → false
  Formula: false → UNSAT → Impossible cycle ✓
```

## How We Built This

This was a genuine collaboration between human expertise and AI capabilities. Here's how it unfolded:

**Human contribution**: Domain knowledge, problem framing, design guidance. The human identified the bug, asked probing questions ("what is a negated context?", "when are arrays boolean operands?"), caught errors in proposed solutions, and steered toward the right abstraction.

**Claude's contribution**: Formalizing ideas, exploring implications, writing code. I helped translate intuitions into precise rules, worked through examples, and implemented the formula builder.

**The key insight** came from a question the human posed: "If we tag all leaves as true or false - node is false inside node, null is true, arrays are true - and the formula evaluated under these constraints is true, then what?"

This reframing - from "detect cycles" to "check satisfiability with self-ref = false" - unified the approach. We then worked through variable allocation together, with the human pointing out that `{a: string, b: !not string}` needs separate variables while `!and [string, int]` needs mutex constraints.

The human reviewed my code attempts critically, catching issues like inconsistent `!` prefix handling and the need to use existing patterns (SplitChild) for tag parsing. Several back-and-forths refined the implementation before it was ready.

## Implementation

The formula builder uses the [Gini SAT solver](https://github.com/go-air/gini):

```go
type formulaBuilder struct {
    c           *logic.C           // Boolean circuit builder
    path        string             // Current position (kinded path)
    vars        map[varDef]z.Lit   // (position, type) → SAT variable
    mutexes     map[string][]z.Lit // position → types (for mutex)
    checkingDef string             // Self-reference target
    definitions map[string]*ir.Node
}
```

Key methods:
- `build(node)` - Recursively builds formula, dispatching by node type
- `buildTagged(node, tag)` - Handles `!not`, `!or`, `!and`, type tags
- `buildRef(name)` - Self-ref → false, otherwise inline expand
- `getVar(type)` - Allocate variable, add mutex with other types at same position

## Future Work

The current implementation hardcodes `!or` and `!and` as boolean operators. A tag like `!parity` (matches if even number of elements match) would need code changes. A registry or tag metadata system could make this extensible.
