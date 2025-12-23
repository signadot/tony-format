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

With the Tony format, it's easy to have arbitrary Boolean combinations describing conditions in
schema, and so one can imagine that changing an `!or` to an `!and` may have such effects.

So, we implemented a checker for this using a [SAT solver](https://github.com/go-air/gini).

## Modelling Schema with SAT

We model the schema as a **Boolean formula** and check satisfiability.

### Formula Construction

Each schema Boolean operation and container type  maps to Boolean operations:

| Schema | Formula |
|--------|---------|
| `!or [X, Y]` | X ∨ Y |
| `!and [X, Y]` | X ∧ Y |
| `!not X` | ¬X |
| `{a: X, b: Y}` | X ∧ Y (fields are AND-ed) |
| `[X, Y]` (untagged) | X ∧ Y (piecewise matching) |

The untagged array case is conjunctive because when a schema accepts a document,
it matches the document structurally piece by piece, and an untagged array
in the matching structure is matched piece by piece, meaning that every
element of the array must have an escape hatch.

The case is similar for objects.


### Variable Allocation

With the structure of the formula in place, all that remains is defining
the variables and constants (true or false).  From the schema
format, and (again) the structure in place, we only need to look at the
leaves in the schema. And leaves are type references. 

Consider `{a: string, b: !not string}`:
- The `string` at field `a` and field `b` refer to *different positions*
- A value `{a: "hi", b: 42}` makes `string_a = true, string_b = false`
- These need **separate Boolean variables**

But `!and [string, int]` at the same position:
- Both constrain the *same* value
- Nothing can be both string AND int
- Need **mutual exclusion constraint**: ¬(string ∧ int)

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

For mutually recursive definitions like `a: .[b]`, `b: .[a]`:

```
Checking a:
  Expand .[b] → .[a] (self-ref) → false
  Formula: false → UNSAT → Impossible cycle ✓
```

## How We Built This

### Claude's Perspective
This was a genuine collaboration between human expertise and AI capabilities. Here's how it unfolded:

**Human contribution**: Domain knowledge, problem framing, design guidance. The human identified the bug, asked probing questions ("what is a negated context?", "when are arrays boolean operands?"), caught errors in proposed solutions, and steered toward the right abstraction.

**Claude's contribution**: Formalizing ideas, exploring implications, writing code. I helped translate intuitions into precise rules, worked through examples, and implemented the formula builder.

**The key insight** came from a question the human posed: "If we tag all leaves as true or false - node is false inside node, null is true, arrays are true - and the formula evaluated under these constraints is true, then what?"

This reframing - from "detect cycles" to "check satisfiability with self-ref = false" - unified the approach. We then worked through variable allocation together, with the human pointing out that `{a: string, b: !not string}` needs separate variables while `!and [string, int]` needs mutex constraints.

The human reviewed my code attempts critically, catching issues like inconsistent `!` prefix handling and the need to use existing patterns (SplitChild) for tag parsing. Several back-and-forths refined the implementation before it was ready.

### My Perspective

This was a 2 shot effort, both long sessions.

The first session was awful and frustrating.  Claude kept trying to apply
shallow Demorgan's law patterns to solve the problem, but there was always the
exception of a bigger, equivalent formula which didn't match the pattern.  Then
He started complaining about the SAT solver "not working with negation", as if he
could reason more thoroughly than the solver.

After entirely too much cajoling, he accepted to use the solver but kept trying
to fix bugs by doing the solving himself.  Again, questioning whether the SAT solver
understood negation.

That first session ended up using a SAT solver, but the modeling was all wrong
and yet presented in an apparently reasonable way, and tests were passing.  So I left it.

The second session came after I had gained some experience using agents.  Realizing that
1: they forget; 2: the vast majority of their "reasoning" skills are better described as
"fancy pattern matching based on numeric modeling of text and code", and certainly not
"true" reasoning.  So I adapt.

I now guide the discourse to a level where pattern matching
is likely to reflect the real reasoning I am looking for: I ask "why" a lot, I
point out indiscrepencies and contradictions at an abstract, logical level
in natural language.  This seems to induce Claude to communicate, and perhaps "think"
using this abstract, logical level, where the pattern matching is more likely to
work.

I also use a by-default offline [distributed issue tracker](https://pkg.go.dev/github.com/signadot/tony-format/go-tony@v0.0.20/cmd/git-issue) to keep bigger picture context.  Being offline by default,
it provides goals to Claude: 1 per issue, and structured discourse associated with each
goal.

Ironically, it is Claude that identified the shortcomings of the first version, which
had a lot of fundamental problems.  This is a danger of working with these agents, we
are distanced from the product and they are incredibly endowed with an ability to make
unreasonable things appear reasonable.

With the issue tracker, and similarly several rounds of modelling design being summarised
persitantly in a code comment, Claude proposed a complete algorithm that, when viewed top-down,
was correct.   But it also had several coherency problems with the design we had just
put together.  After some fixes: simply cutting and pasting problematic chunks of code
and stating briefly the problem, Claude readily fixed these issues and then we ran tests.
A touch of surface-level fixes and boom, it all worked.


## Implementation

The formula builder uses the Gini SAT solver's [logic package](https://pkg.go.dev/github.com/go-air/gini@v1.0.4/logic) to construct the formula described above.

It
- Recursively builds formula, dispatching by node type
- Handles `!not`, `!or`, `!and`, type tags
- Maps Self-ref → false, otherwise inline expand
- Allocates variables following target document position, adding mutual exclusion constraints
with other types at same position.

This process is linear in the size of the input.

### SAT Solving Overhead

One may ask aren't we risking having an NP complete solver checking whether schema are valid?

Some quick results:

| Schema           | Total Parse | Formula Build | SAT Solve | %Solve | Calls |
|------------------|-------------|-----------|-----------|--------|-------|
| simple recursive | 125µs       | 108µs     | 1.5µs     | 1.4%   | 2     |
| tree structure   | 41µs        | 39µs      | 0.9µs     | 2.2%   | 2     |
| deep chain       | 184µs       | 183µs     | 2.6µs     | 1.4%   | 5     |
| many definitions | 143µs       | 142µs     | 4.1µs     | 2.9%   | 6     |

It's clear the solving time is minimal.

With today's SAT solvers, there is
no reason to believe any reasonable input would take time, and with any
quadratic (or worse) complexity algorithm, there are unreasonable inputs which
take too long, so there's no realistic reason we can see to worry about this,
even for unreasonable inputs.

And empirically, it's much faster than traversing the schema to begin with!

## Future Work

The current implementation hardcodes `!or` and `!and` as boolean operators. A
tag like `!parity` (matches if even number of elements match) would need code
changes. A registry or tag metadata system could make this extensible.
