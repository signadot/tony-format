# Compaction Filtering: Minimal Work Analysis

## Question

What's the minimal set of DirCompactors to notify for "correct" compaction? How does matching relate to filtering?

## Correctness Property

Query `/a/b @N` where `N % (divisor^level) == 0` should give precisely the view of `/a/b` at commit N.

## LookupRange Behavior

When `/a/b` compacts, `LookupRange("/a/b", ...)` returns:
- **Ancestors:** `/a`, `/` (if they exist)
- **Target:** `/a/b`
- **Descendants:** `/a/b/c`, `/a/b/c/d`, etc.

## Notification Scenarios

### Scenario 1: `/a/b/c` has commits
- Should `/a/b` be notified? **YES**
- Why: `/a/b` compacts and includes `/a/b/c` segments (descendant)
- Minimal: Notify `/a/b` (ancestor of path that had commits)

### Scenario 2: `/a` has commits
- Should `/a/b` be notified? **MAYBE**
- Why: `/a/b` compacts and includes `/a` segments (ancestor)
- But: `/a/b` will get `/a` segments via LookupRange anyway, even if `/a` didn't have commits in this window
- Question: Does `/a/b` need to know `/a` had commits to align compaction?

### Scenario 3: `/a/b` has commits
- Should `/a/b` be notified? **YES**
- Why: Exact match - `/a/b` itself had commits

### Scenario 4: `/a/b/c/d` has commits
- Should `/a/b` be notified? **YES**
- Why: `/a/b` includes `/a/b/c/d` (descendant)

## Minimal Notification Strategy

**Option A: Notify exact paths + ancestors**
- Notify paths that had commits (exact match)
- Notify ancestors of paths that had commits
- **Don't notify descendants** (they don't include ancestors in their view)
- **Rationale:** Ancestors need to know about descendant commits to align compaction

**Option B: Notify exact paths only**
- Only notify paths that had commits (exact match)
- **Rationale:** Each path compacts independently, LookupRange handles ancestors/descendants automatically

**Option C: Notify exact paths + ancestors + descendants**
- Notify paths that had commits
- Notify ancestors (they include descendants)
- Notify descendants (they include ancestors)
- **Rationale:** Full coordination ensures alignment

## Alignment Coordination

The key question: **Do we need coordination for alignment, or does LookupRange handle it?**

**If LookupRange handles it:**
- `/a/b` will get `/a` segments even if `/a` didn't have commits in this window
- So maybe we only need to notify exact paths that had commits
- Alignment happens naturally because all paths see the same segments

**If we need coordination:**
- All paths need to compact together at alignment points
- So if `/a` compacts at commit 2, `/a/b` should also compact at commit 2 (even if `/a/b` didn't have commits)
- This requires notifying ancestors/descendants

## Filtering Question

What are we filtering?
- **Reading:** Filter commits by range (last `divisor` commits)
- **Matching:** Filter DirCompactors by path relationship
- **Compaction:** Filter by `Inputs >= Divisor` and alignment check

**Minimal work:**
1. Read commits in alignment window
2. Extract paths that had commits
3. Notify DirCompactors for those exact paths
4. Let LookupRange handle ancestors/descendants automatically

**OR:**
1. Read commits in alignment window
2. Extract paths that had commits
3. Notify DirCompactors for paths AND their ancestors
4. Ensures ancestors compact when descendants have commits

## Recommendation

**Minimal:** Notify exact paths that had commits. LookupRange will include ancestors/descendants automatically, so alignment should work.

**But:** For alignment coordination, we might need to notify ancestors too, so they compact together at alignment points.

**Question for user:** Do we need to notify ancestors for alignment coordination, or is notifying exact paths sufficient?
