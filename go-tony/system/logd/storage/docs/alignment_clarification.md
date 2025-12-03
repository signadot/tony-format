# Alignment Checking: Clarification

## The Confusion

You're right to be confused. Let me clarify the difference between:

1. **Commit-driven (fragile)** - what I rejected
2. **Alignment checking (simple)** - what I'm now proposing
3. **Intent-based** - what you suggested

## Commit-Driven (Fragile) - REJECTED

**How it worked:**
- `OnCommit(N)` callback called after commit N allocated
- Callback checks if N is alignment point, triggers compaction
- **Problem:** Callbacks can arrive out of order (after `indexMu` released)
- Creates race conditions and timing dependencies

**Why fragile:**
- Relies on "segments arrive before OnCommit()" timing assumption
- Out-of-order callbacks create complex synchronization requirements
- Hard to reason about, easy to introduce bugs

## Alignment Checking (Simple) - PROPOSED

**How it works:**
- When processing segment with `EndCommit = N`, check if `N % align == 0`
- If yes and `Inputs >= Divisor`, compact
- Segments processed in order (from channel), so no out-of-order issues

**Why simpler:**
- No separate callback mechanism
- No out-of-order issues (segments are in order)
- Deterministic check: `commit % align == 0`

**BUT - There's a problem:**

Different paths receive segments at different rates:
- Path `/a` receives segments at commits 1, 2, 3, 4
- Path `/a/b` receives segments at commits 1, 3, 5, 7
- Both check alignment when processing segments
- `/a` compacts at commits 2, 4 (alignment points) ✅
- `/a/b` never hits alignment points (1, 3, 5, 7 are odd, alignment is 2, 4, 6...) ❌

**Result:** Paths don't compact together. Alignment checking alone doesn't guarantee coordination.

## Intent-Based - YOUR SUGGESTION

**How it works:**
- When commit N allocated, if `N % align == 0`, write intent atomically (seq lock held)
- Intent notifies all DirCompactors at that level
- DirCompactors check alignment when processing segments (as above)
- **Key:** All paths see the same alignment points (from intents)

**Why this works:**
- Intents written atomically (no out-of-order issues)
- All paths see same alignment points (coordination)
- Alignment check is still simple (`commit % align == 0`)
- No timing assumptions needed

## The Real Difference

**Commit-driven (fragile):**
- Callback triggers compaction directly
- Callbacks can arrive out of order
- Creates race conditions

**Alignment checking (simple but incomplete):**
- Check alignment when processing segments
- Segments are in order (no race conditions)
- BUT: Doesn't coordinate across paths

**Intent-based (robust):**
- Intents written atomically (no out-of-order)
- Intents coordinate all paths
- Alignment check is still simple
- Best of both worlds

## Conclusion

You're right - alignment checking is "commit-based" in that it uses commit numbers. But the key difference is:

1. **Fragile version:** Separate callback mechanism with out-of-order issues
2. **Simple version:** Check alignment when processing segments (in order, but no coordination)
3. **Intent-based:** Atomic intents coordinate all paths, then simple alignment check

**Recommendation:** Intent-based approach gives us:
- No out-of-order issues (intents written atomically)
- Coordination across paths (all see same alignment points)
- Simple alignment check (deterministic)

The intent ensures coordination, the alignment check ensures correctness.
