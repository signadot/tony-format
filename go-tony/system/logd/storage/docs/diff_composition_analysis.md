# Diff Composition Analysis

## Question

Given a sequence of diffs D1, D2, D3, ... DN, can we compute the diff from state at D[i] to state at D[i+divisor] **without having the full document state**?

## The Problem

### Current Approach (Requires Full State)

To compute the diff from State[i] to State[i+divisor]:

1. **Apply all diffs from beginning**:
   ```
   State0 = empty
   State1 = apply(State0, D1)
   State2 = apply(State1, D2)
   ...
   State[i] = apply(State[i-1], D[i])
   ```

2. **Apply remaining diffs**:
   ```
   State[i+1] = apply(State[i], D[i+1])
   State[i+2] = apply(State[i], D[i+2])
   ...
   State[i+divisor] = apply(State[i+divisor-1], D[i+divisor])
   ```

3. **Compute diff**:
   ```
   D_composed = diff(State[i], State[i+divisor])
   ```

**Problem**: Requires computing State[i] by applying all diffs from the beginning.

## Can We Compose Diffs Directly?

### Diff Composition Algebra

**Question**: Can we compute `compose(D[i+1], D[i+2], ..., D[i+divisor])` without applying to state?

**The Challenge**:
- Diffs are **state-relative**: D2 is the diff from State1 to State2
- D3 is defined relative to State2, not relative to "State1 + D2"
- To compose D2 and D3, we need to know how D3 applies to the result of applying D2

### Example: Why Direct Composition Is Hard

**Scenario**: 
- D1: `{a: 1}` (set a=1)
- D2: `{a: 2}` (set a=2)
- D3: `{b: {c: 3}}` (set b.c=3)

**State progression**:
- State0: `{}`
- State1: `{a: 1}` (after D1)
- State2: `{a: 2}` (after D2)
- State3: `{a: 2, b: {c: 3}}` (after D3)

**Composing D2 and D3**:
- D2 sets `a: 2`
- D3 sets `b: {c: 3}`
- Composed: `{a: 2, b: {c: 3}}`

**This works!** But what about conflicts?

**Conflicting Example**:
- D1: `{a: {x: 1}}` (set a.x=1)
- D2: `{a: {y: 2}}` (set a.y=2)
- D3: `{a: {x: 3}}` (set a.x=3)

**State progression**:
- State0: `{}`
- State1: `{a: {x: 1}}`
- State2: `{a: {x: 1, y: 2}}` (merge: both x and y)
- State3: `{a: {x: 3, y: 2}}` (merge: x updated, y remains)

**Composing D2 and D3**:
- D2 sets `a: {y: 2}` (partial object)
- D3 sets `a: {x: 3}` (partial object)
- How do we merge? We need to know State2 to know that `a.x` exists

**The Problem**: Diffs are **partial** and **merge** with existing state. To compose them, we need to know what state they're merging with.

## Possible Solutions

### Option 1: Full State Required (Current Approach)

**Approach**: Always compute full state to compose diffs.

**Pros**:
- Simple, correct
- Works for any diff format

**Cons**:
- Requires applying all diffs from beginning
- Expensive for compaction

### Option 2: Snapshot-Based (User's Suggestion)

**Approach**: Store snapshots (full state) at compaction boundaries.

**How it works**:
- At commit i+divisor, store snapshot of State[i+divisor]
- To compute diff from State[i] to State[i+divisor]:
  - Read snapshot at State[i+divisor]
  - Read snapshot at State[i] (or compute from earlier snapshot)
  - Compute diff between snapshots

**Pros**:
- No need to apply all diffs
- Can compute diff between any two snapshots
- Snapshots can be stored efficiently (compressed)

**Cons**:
- Requires storing full state (larger than diffs)
- Still need to compute State[i] if no snapshot exists

### Option 3: Diff Composition Rules (If Possible)

**Approach**: Define algebraic rules for composing diffs directly.

**Requirements**:
- Diffs must be **complete** (not partial) - but this defeats the purpose of diffs
- Or diffs must track **all changes** including what was there before
- Or we need a way to "lift" diffs to be composable

**Example**: If diffs tracked "before" and "after":
- D2: `{a: {before: {x: 1}, after: {x: 1, y: 2}}}`
- D3: `{a: {before: {x: 1, y: 2}, after: {x: 3, y: 2}}}`
- Composed: `{a: {before: {x: 1}, after: {x: 3, y: 2}}}`

**Problem**: This makes diffs much larger (essentially storing state deltas).

### Option 4: Incremental Compaction (Hybrid)

**Approach**: Compaction computes and stores composed diffs incrementally.

**How it works**:
- When compacting D[i]..D[i+divisor]:
  1. Read snapshot at State[i] (or compute from earlier snapshot)
  2. Apply D[i+1]..D[i+divisor] to get State[i+divisor]
  3. Compute diff from State[i] to State[i+divisor]
  4. Store as single diff entry

**Result**: 
- Fewer diffs to apply (divisor diffs → 1 diff)
- But diff size may be larger (full state delta vs incremental)

**Pros**:
- Reduces number of diffs
- Can use snapshots to avoid recomputing from beginning

**Cons**:
- Still requires full state computation
- Composed diff may be larger than individual diffs

## The Algebraic Insight

**Key Question**: Are diffs **composable** without state?

**Answer**: Generally **no**, because:
1. Diffs are **partial** (only specify changes)
2. Diffs **merge** with existing state
3. To merge, we need to know what exists

**Exception**: If we store **complete state deltas** (before + after), then yes, but this defeats the purpose of diffs (they become as large as state).

## Recommendation: Snapshot-Based Compaction

Given the algebraic limitations, **snapshots are the right approach**:

1. **Snapshots at boundaries**: Store full state at compaction boundaries (commit % divisor^L == 0)
2. **Composed diffs between snapshots**: Optionally store diff from snapshot[i] to snapshot[i+divisor]
3. **Read algorithm**:
   - Read snapshot at boundary <= commit
   - Apply remaining diffs on top
   - Or read composed diff if available

**Benefits**:
- No need to apply all diffs from beginning
- Can compute any state efficiently
- Composed diffs can be computed from snapshots (not the other way around)

**Trade-off**:
- Snapshots are larger than diffs
- But they enable efficient reads and compaction

## Conclusion

**Direct diff composition without state is generally not possible** because diffs are partial and state-relative. 

**Solution**: Use **snapshots** as the foundation:
- Snapshots provide full state at boundaries
- Composed diffs can be computed from snapshots (if needed)
- Read algorithm uses snapshots + remaining diffs

This aligns with the user's insight: **snapshots are the mechanism, compaction is the optimization on top**.
