# logd: depth bounds a descent -- match: {path: x.., depth: n} takes at most n segments per ..

A `..` takes zero or more segments. `depth: n` on a match says it may take at most n, so `x..` with depth 1 is x and its direct children, depth 2 adds the grandchildren, and `..name` with depth 2 is name at the root, under any child, or under any grandchild. Each `..` in a path is bounded separately.

It is a listing a browsing client cannot ask for today: `x.*` names only fields, while `x..` at depth 1 names children of every kind without knowing the container's kind first.

The walk stops at the bound rather than walking deeper and filtering: a descent position in the position set (kpath.Positions) carries how many segments it has taken, and a step drops it at the bound. So `x..` with depth 1 costs one listing.

Refusals: a depth on a path with no `..` is invalid_path whatever its value, since nothing there is bounded; a negative depth is invalid_path; depth 0 is legal and means the descent takes nothing. The cursor carries the depth, and a continuation with a different depth is refused as a different path is.

ir's list walk takes the same bound, so `o list` and `match` keep agreeing; `o list` gets -depth. docd stays conservative: a descent that reaches a mount is unsupported whatever its depth.

Follows th7sdhvyh12ksjtfn9n0.