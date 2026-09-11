# libdiff: an object diffed against a sparse array answers the sparse array as a merge, so the other side's fields survive the patch

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

libdiff/object.go:20-21: DiffObject for a plain object against a !sparsearray answers
`to` itself, which a patch MERGES. Patch({a: 1}, Diff({a: 1}, !sparsearray {0: y}))
keeps a: 1.

It reaches logd lowering: state x: {a: 1}, then a block-style !replace to
!sparsearray {0: y} stores `x: !sparsearray 0: y` and the state becomes
x: !sparsearray {"": y, a: 1}. On a live server, replacing !sparsearray {0: y} with
{a: 1} reads back !sparsearray {"": y, a: 1}.

Consequence: Patch(a, Diff(a, b)) != b across the object/sparse-array boundary, and a
stored write keeps what it replaced. (The "" key is the read-side bug in the related
issue.)