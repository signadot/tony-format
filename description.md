# storage: an escaped value under a head comment is walked into, so a commented charter rule is refused

`validateForStorage` (go/system/logd/api/storage_context.go, v0.0.237) checks the node's tag for `!raw` **before** `ir.Uncomment`, and not after. When a head comment wraps a value tagged `!insert.raw`:

- the wrapper has no tag, so it isn't escaped;
- `Uncomment` then steps onto the escaped value;
- the walk goes into it without re-reading its tag.

So an operation inside correctly escaped data is refused.

Repro (go-tony v0.0.237):

```go
rule, _ := parse.Parse([]byte("a: !and [1, 2]\n"))
rule.Tag = "!insert.raw"
api.ValidateForStorage(ir.FromMap(map[string]*ir.Node{"spec": rule.Clone()}))
// <nil>
wrap := &ir.Node{Type: ir.CommentType, Lines: []string{"# why"}, Values: []*ir.Node{rule}}
rule.Parent = wrap
api.ValidateForStorage(ir.FromMap(map[string]*ir.Node{"spec": wrap}))
// at spec.a: operation "!and" may not be stored: it transforms whatever it finds rather than stating what results
```

This is reachable from verse. A charter rule with a comment above it, and a tagged condition (`value: !and [...]`), can't be installed. Verse writes the rule as `spec: # comment → !insert.raw {…}`, which is `entity.Raw` tagging the value inside the comment, as it should. The install answers 503 `the charter could not be written`.

Likely fix: after `n = ir.Uncomment(n)`, re-apply the checks that ran on the wrapper: `firstRelativeOp(n.Tag)`, and the `!raw` stop. The wrapper and what it wraps are one value (3cdjz00jh12krns4g1n0 made the walk see through the comment for containers, but not for the escape).

Verse is working around it for now by keeping per-rule comments out of charters with tagged conditions.