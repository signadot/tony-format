# kpath: Matches and MatchesPrefix answer false for every pattern holding `..`

`KPath.Matches` and `KPath.MatchesPrefix` say, and do: "A `..` segment, on either side, matches nothing" (go-tony/ir/kpath/kpath.go). So a pattern can find a node through `ListKPath` and then fail to say it denotes that node's path:

```
doc:      {verse-dev: {drift: {finding: {abc: {}}}}}
pattern:  verse-dev..finding
ListKPath(doc, "verse-dev..finding")                 → [{abc: {}}]
Parse("verse-dev..finding").Matches(Parse("verse-dev.drift.finding"))  → false
```

The two halves of one grammar disagree: listing understands `..` ("a..x finds an x directly under a as well as one further down", listKPath), matching refuses it. A caller that asks both questions of one pattern — list what a pattern reaches in a document, and decide whether a path handed to it (a change event, a write, a permission's coverage) is one the pattern reaches — cannot use `..` for either, because the answers would contradict.

Wanted, for a pattern receiver and a concrete target, the same meaning listKPath gives:

- `..` in the pattern matches zero or more target segments, of any kind, the position itself included;
- `Matches` holds when the whole target is consumed; `MatchesPrefix` when the pattern is consumed (a `..` still pending at the end of the target consumes nothing and holds);
- a `..` in the TARGET still matches nothing: a target with a query segment is not a path.

The asymmetry p7gd2y87h12kswh0g9n0 named — a descent is a query segment, never a stored path — stays: this only asks that a pattern holding one can be asked about stored paths.

Found in verse, planning containment (confinement, watch filters, permission coverage) on kpath as the one grammar (docs/working/paths-and-nodes-plan.md in signadot/verse).