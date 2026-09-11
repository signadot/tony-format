# logd: a transaction writing two elements of one array fails, because each participant's array site is lowered separately and the two conflict

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

Two participants of one transaction writing different elements of one plain array --
`d.items[0].v` and `d.items[1].v` -- both get "storage_error: ... lowering !arraydiff:
patch at ditems conflicts with ditems". Plain absolute values fail as !replace does.

Merging two positional writes gives an !arraydiff, which needs lowering. Each
participant's write site is the array itself, and storage/commit_ops.go:80-90 appends
every participant's sites without removing duplicates; lowerWrite then emits two patches
at one path, and MergePatches refuses them as a prefix conflict (tx/merge.go:36-41),
whose message joins segments with "" -- hence "ditems".

Consequence: a transaction cannot update two elements of one array.