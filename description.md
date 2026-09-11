# libdiff: a keyed-list diff panics when an element lacks its key

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

`o diff` on `l: !key(name) [{name: a, v: 1}, {v: 2}]` against the same with v: 3 panics
with a nil dereference at libdiff/array_by_key.go:207: yKeyNodeOf does not check
GetPath's nil result (:203).

logd does not reach it: it refuses !key in a patch without a schema, lowering diffs keyed
arrays as objects of names, and raised states are untagged, so deltaAt takes the
positional branch.

Related, closed: bzf782sph12krsmwe5n0 (the patch-side twin).