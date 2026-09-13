# logd: a precondition costs the whole value at its path, even when the pattern names one child or none

Measured 2026-09-13 against go-tony v0.0.217, on a local logd + docd (verse's entitytest harness: storage.Open on a temp dir, logd server, docd client face), over one raw libctl session. A path `big.kind` holds N children, each written with an unconditional PatchWith of `{owner: someone, grants: [{read: ["*"]}]}`. Each row is 20 operations.

A write's precondition costs the size of the value at the precondition's path, whatever the pattern asks about it. The patch goes to a sibling path (`big.side.w<i>`) and returns no body; only the `Match` in PatchOpts differs:

| precondition pattern | at | 10 children | 3,000 children |
|---|---|---|---|
| `!or [{}, !irtype null]` (the path is an object or null) | `big.kind` | 1.28ms | 79.7ms |
| `!not {nope: !not.or [{}, !irtype null]}` (one named child is absent) | `big.kind` | 1.22ms | 82.0ms |
| `{e5: {}}` (one named child is present) | `big.kind` | 1.25ms | 81.7ms |
| `!or [{}, !irtype null]` | `big.kind.e5` | 0.93ms | 2.12ms |
| no precondition | | 0.88ms | 1.30ms |

The first three ask nothing about 2,999 of the children — the first asks only about the path's own shape, the next two about one named child — and each costs what reading all 3,000 would. Asked at one child it is flat. The store's index already knows which paths were written beneath `big.kind` (it answers a never-written path as `narrow-absent` without a read), so the shape of `big.kind` and the presence of `big.kind.nope` are both knowable without building the value.

For reference, the same patterns as a plain MatchPattern, which returns a body: shape-only and absent-child returned all 3,000 children (207ms and 130ms), named-present returned one (82ms). Those times include shipping the result, so the precondition column above is the cost of deciding the pattern.

Why it matters to verse. Every write verse makes to a whole entity carries a precondition at the entity's parent: the parent must be an object or nothing (so a write never silently merges over a scalar or an array standing there), and `Created()` is asked as a named child's absence at the same parent. The named-child form is deliberate — it is the one that keeps null and absence distinct. With the parent holding thousands of children, a single-entity write costs O(children):

- a verse write of a whole entity into a kind of 10 took 1.7ms, into a kind of 3,000 took 90ms (118ms with `Created()`); a write one field inside an entity stayed at 1.0–1.6ms;
- on staging, re-installing a charter of 12 rules took 37s, with docd at ~1.6 cores throughout: about 48 such writes, half of them into verse's perms mirror, one kind of ~2,900 records;
- a source's first pass after a wipe writes each reflected entity into a kind like `git.ref` (838 entries), which is the same cost per entity.

What would make both of verse's questions cheap without changing what they mean: evaluate a precondition against only what its pattern names — a shape-only pattern needs the path's own node, a named child needs that child — rather than against the whole value at the path.

The harness is attached (matchcost_test.go); run it from any module that has verse's entitytest, or adapt the store setup.