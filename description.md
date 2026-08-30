# logd: retire the lowering escape hatch, and the in-scope refusal that is its only behaviour

Lowering has been ON by default since it landed, and `EnableLowering(false)` exists
so a store can be put back the way it was. Nothing in the tree turns it off outside
tests. While it stays, a whole second behaviour of the write path stays with it --
including a client-facing refusal that can no longer happen.

This is cleanup, not a fix. Nothing here is a defect; it is code that is dead under
the defaults and cannot be reasoned about without knowing that.

## What goes

The switch itself:

- `Storage.lowering` and `EnableLowering` (lower.go)
- `lowerWrite`'s `if !s.lowering` early return (lower.go)

And, reachable ONLY when it is off:

- `commitOps.LowersScopeWrites` (commit_ops.go) -- returns `c.s.lowering`
- `CommitOps.LowersScopeWrites` on the interface (tx/tx.go) and its mock
  (tx/tx_test.go)
- the gate at tx/coord.go:177, `if co.commitOps == nil || !co.commitOps.LowersScopeWrites()`
- `checkStorableInScope` and `NotStorableInScopeError` (tx/scope_storable.go, 72
  lines -- the whole file)
- the `NotStorableInScopeError` arm of session_write.go's error mapping

`checkStorableInScope` is the pre-lowering rule: it refused a relative operation in
a scope outright, because a scope's base moves and the operation would stop applying.
Lowering keeps that property and allows the write, by storing the claim instead --
so the refusal is not merely unreachable, it is the thing lowering replaced.

## What STAYS, and must not be swept up with it

`LowerEverything` / `Storage.lowerAll` / `LOGD_LOWERING=all` is NOT the escape hatch.
It is the amplifier that forces every write through the lowering path, and it is how
lowering is actually tested: with the ordinary rule the suite reaches that path a
handful of times in twenty thousand writes, because almost nothing anyone writes is
relative. It has found real defects that the default mode does not reach
(2w62pyyah12ksqh0jdn0). It sets `lowering = true` today, so it needs a small edit
rather than deletion.

## Tests

Three files run both arms and want thinking about rather than mechanical edits:

- `lower_test.go` -- parameterised on `EnableLowering(lowering)`; its point is the
  before/after, so some of it becomes a statement about the present tense and some
  of it goes.
- `lower_matrix_test.go` -- the three-row matrix (plain / engine-lowered /
  client-lowered). The plain row is the unlowered store.
- `lower_scope_test.go` -- `TestLoweringScopeDifferential` is a lowered store against
  an UNLOWERED one over the same stream. That differential cannot survive this; what
  it was checking -- that lowering does not change what the log says -- has to be
  re-expressed against something else, or retired with an argument for why the
  property is covered elsewhere (the claim differential and the read differentials
  both bear on it).

`patch_root_marker_test.go`'s `TestARelativeRootWriteDoesNotDestroyTheDocument` also
runs a `lowering off` arm, but only as a control; it can simply lose that arm.

## Why not now

The property `TestLoweringScopeDifferential` checks is worth keeping until it is
clear what replaces it, and that is a judgement about test coverage rather than a
deletion. Doing the deletion first and the argument afterwards is how a differential
quietly stops covering anything.

## Related

- `4wpqh7t2h12ks1fvj5n0` -- the tracking issue for the lowering work.
- `3xn08cb6h12kr4psg5n0` -- why the in-scope refusal existed.