# kpath has no any-depth segment, so o lost objpath's $...x

`o get` and `o list` now take kpaths, which is the syntax the rest of the system uses. kpath
has no any-depth segment, and the objpath they took before had one, so the migration dropped a
capability rather than moving it.

## What it was

objpath's `..`, reachable only as three dots -- `..` for the descent and `.x` for the field:

```
o list '$...x' deep.tony      # every x at any depth
- 1
- 2
- 3
```

`$..x`, which is how JSONPath spells it and how anyone would write it, was a parse error. It
worked in list and not in get ("recurse .. in get"), and no page documented it.

## What replaces it

Nothing exactly. A wildcard names one level -- `a.*`, `a[*]`, `a{*}` -- so a path that reaches
an unknown depth cannot be written. `o match '!subtree ...'` matches at any depth but answers
with the whole document rather than the nodes.

## If it is worth having

kpath would need a segment for it, and the spelling is the first question: `..` is ambiguous
beside `.field`, and `**` is the glob convention (`a.**.x`).

The cost is not the parser. kpath is what logd indexes by, compares, and renders, so a new
segment kind reaches KPath.String, SegmentString, Compare, and the index's own walk -- and a
path with a descent in it has no meaning as a stored path, only as a query. That asymmetry is
the design question: either the type carries a segment that half its users must refuse, or
queries get a separate type that is a superset.

Filed rather than guessed at. It was reachable only by a spelling nobody would find.