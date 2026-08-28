# logd: one storable-delta pipeline, and scopes stop being a special case

# scopes after lowering: what it should look like, and how to get there

## what a scope is

	scoped@T  =  baseline@T  then the scope's own patches, applied last, from commit 0

`readScopedStateAtReplay` is that sentence. Three things follow from it, and the whole
of the scope code is those three things:

1. **A stored relative operation stops applying.** Baseline moves underneath, so a
   `!replace {from: bob}` verified at the write fails at the read, forever
   (3xn08cb6h12kr4psg5n0). `checkStorableInScope` refuses such a write at the door.
2. **A scope's patches cannot be snapshotted or compacted.** They have to re-apply over
   whatever baseline has become, so they replay in full on every read, forever.
3. **The overlay exists to bound (2).** It materializes the scope's ownership at T, so a
   read replays only what the scope wrote after T.

## what lowering changes

Nothing relative is stored, in either layer. The write is applied and its RESULT diffed,
and what is stored is absolute by construction and validated as such.

So (1)'s premise is gone. `checkStorableInScope` is not a rule any more, it is a leftover
-- and it is the reason lowering currently does nothing for scopes, since it refuses at
`NewPatcher` exactly the writes lowering exists to absorb.

## the actual bug, which is not any of the above

There are TWO implementations of "given a before and an after, produce a storable delta":

	                  lowerWrite (lower.go)        BuildScopeOverlay (scope_overlay.go)
	keys              keyedArrayPaths              keyedArrayPaths
	presentation      kept                         stripped
	annotate          annotateKeyed x2             annotateKeyed x2
	diff              DiffWith(comments, absolute) DiffWith(no comments)
	make absolute     DiffAbsolute                 unconditionalPatch
	mark              markDeltaRoots               MarkPatchRoot at the root
	validate          ValidateForStorage           ValidateForStorage

Same five steps. Different in four of them.

Every defect this session lived in a cell where the two columns disagree:

- the overlay's diff asking for comments while lowering's did too, and the overlay's
  owned-path union then merging a value into the operator that produced
  (nm5r3sxah12ks2zmj5n0)
- `markDeltaRoots` against `MarkPatchRoot`: the entire snapshot-boundary class, 24 cases
- `unconditionalPatch` against `DiffAbsolute`: the !strdiff and !arraydiff question,
  which the plan expected a post-pass to answer and which only the diff can

I added the second column this session rather than noticing there was already one. That
is the over-arching bug, and patching cells is what makes scopes worse.

## what it should look like

ONE function, in one place:

	storableDelta(base, next, keys) -> (*ir.Node, error)
	    annotate both sides from the schema
	    diff, absolutely, comments included
	    mark the patch root where the change lands
	    validate against the storage vocabulary

`lowerWrite` becomes: decide whether this write needs it (NeedsLowering), then call it.

`BuildScopeOverlay` becomes: choose the two states (baseline@T and scoped@T), call it,
then union the owned paths -- which is the one step that is genuinely the overlay's own,
and the one place R3 lives.

Deleted, not moved:

- `checkStorableInScope` and `NotStorableInScopeError`. The rule they enforce is now
  enforced by construction, on both layers, and the asymmetry their doc comment
  describes -- "baseline: verify once; scope: hold to the vocabulary" -- stops being
  true.
- `unconditionalPatch`. `DiffAbsolute` never emits a `!replace` for it to rewrite.

Kept:

- `ValidateForStorage`, as the assertion at the point of storing rather than a gate on
  what a client may send. It is what makes "the log holds only absolute operations" a
  checked claim instead of a hope.

What is left of the scope's own complexity after that is honest complexity: which two
states to diff, the owned-path union, and the keyed fallback.

## the keyed fallback, which is one fact and not three

Three places give up on a keyed array the schema does not declare: `scopeHasKeyedPaths`
(the overlay refuses to be written), `readScopedStateAtOverlay` (the read falls back to
replay), and `lowerWrite` (the write is not lowered). All three are the same missing
fact -- only the schema can say what keys an array, and a client's own `!key` rides in a
patch that lowering replaces.

They should say so once. Not in this change, but it is the next simplification and it is
the last thing standing between a scope and being ordinary.

## how to get there

1. **Unify.** Extract `storableDelta` from `lowerWrite`, which is the better-tested
   column, and make `BuildScopeOverlay` call it. Delete `unconditionalPatch`. No
   behaviour change is intended; the baseline differential and the scoped differential
   are what say whether that held.
2. **Remove the gate.** Delete `checkStorableInScope` and its error type, and let a
   scoped relative write be lowered like any other. The evidence it needs is one
   assertion, not a suite: a scope writes `!replace`, baseline then moves under it, and
   the scope still reads what it wrote -- which is 3xn08cb6 written as a test.
3. **Then the keyed fact**, once, in the schema, and the three fallbacks collapse into
   it.

Order matters. Removing the gate first drops scoped writes onto two pipelines that
disagree, which is how this session went.