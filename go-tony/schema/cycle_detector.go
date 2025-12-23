package schema

// Schema Satisfiability and Cycle Detection
//
// # Problem
//
// Schemas can be impossible to satisfy in two ways:
//
//  1. Contradictory constraints (no recursion needed):
//     - !and [string, int] — nothing is both string and int
//     - !and [!not null, null] — must be null and not-null
//
//  2. Impossible cycles (recursive with no escape):
//     - node: {next: .[node]} — must recurse forever
//     - node: {next: !and [!not null, .[node]]} — must be non-null .[node] forever
//
// # SAT-based Satisfiability
//
// For each definition, build a boolean formula and check satisfiability.
// This single check catches both contradictory constraints AND impossible cycles.
//
// ## Inline Expansion
//
// Definition references (.[foo]) are expanded inline. When checking definition `node`,
// any self-reference (.[node]) is substituted with constant `false`.
//
// This unifies both cases:
//   - Non-recursive: no self-references, just check satisfiability
//   - Recursive: self-references become false, check if escape exists
//
// Cross-references are followed transitively:
//   - Checking `a` where a: .[b], b: .[a]
//   - Expand .[b] → .[a] (self-ref) → false
//   - Formula: false → UNSAT → impossible cycle
//
// ## Formula Structure
//
//   - !or [...]      → OR of elements (tagged array = boolean combinator)
//   - !and [...]     → AND of elements (tagged array = boolean combinator)
//   - !not X         → NOT X
//   - [...] (no tag) → AND of elements (implicit conjunction from piecewise matching)
//   - {a: X, b: Y}   → X AND Y (implicit conjunction of field constraints)
//   - Leaf types     → boolean variables per truth assignments above
//
// Note on untagged arrays: tony.Match matches array elements piecewise, so
// [string, int] means "matches string AND matches int" — implicit conjunction.
//
// ## Variable Allocation
//
// Variables are allocated per (position, primitive-type) pair.
//
// Position is determined by schema structure:
//   - Object fields create new positions: {a: X, b: Y} → X at position "a", Y at position "b"
//   - Array elements create new positions: [X, Y] → X at position [0], Y at position [1]
//   - Boolean operators stay at same position: !and[X, Y], !or[X, Y], !not X → all at current position
//
// Rules:
//   - Same (position, type) → same variable (idempotent)
//   - Different positions → independent variables (no mutex)
//   - Different primitive types at same position → mutex constraint ¬(t1 ∧ t2)
//
// Example: {a: string, b: !not string}
//   - string_a at position "a", string_b at position "b"
//   - Formula: string_a AND (NOT string_b)
//   - No mutex (different positions)
//   - SAT: string_a=true, string_b=false → value {a: "hi", b: 42} ✓
//
// Example: !and [string, int] at position p
//   - string_p and int_p with mutex ¬(string_p ∧ int_p)
//   - Formula: string_p AND int_p
//   - UNSAT: mutex forbids both true, formula requires both true
//
// Example: !and [string, !not int] at position p
//   - string_p and int_p with mutex
//   - Formula: string_p AND (NOT int_p)
//   - SAT: string_p=true, int_p=false (mutex satisfied, formula satisfied)
//
// ## Evaluation
//
// For each definition, build formula with inline expansion (self-ref → false).
// If satisfiable → definition is realizable.
// If unsatisfiable → impossible (contradictory or inescapable cycle).
//
// ## Examples
//
//   Checking node: !and [!not null, .[node]]
//     Expand .[node] → false (self-reference)
//     Formula: (NOT null) AND false = false
//     → UNSAT, impossible cycle
//
//   Checking node: !or [null, .[node]]
//     Expand .[node] → false
//     Formula: null OR false
//     → SAT (null=true), escape exists
//
//   Checking node: !not.or [null, .[node]]
//     Expand .[node] → false
//     Formula: NOT (null OR false) = NOT null
//     → SAT (can use string, int, etc.), escape exists
//
//   Checking node: !and [string, int]
//     No self-references
//     Formula: string AND int with mutex ¬(string ∧ int)
//     → UNSAT, impossible schema (no recursion involved)
//
//   Checking a: .[b] where b: .[a]
//     Expand .[b] → expand .[a] → false (self-ref to a)
//     Formula: false
//     → UNSAT, impossible cycle
//
// # Implementation
//
// The SAT-based satisfiability check is implemented in formula_builder.go.
// CheckAcceptSatisfiability is the main entry point, called from ParseSchema.
