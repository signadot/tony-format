# mergeop: a nested !let is refused, because the outer one's expansion reaches the inner body

A nested `!let` is refused now rather than answered, because the outer one cannot know what the
inner binds and its expansion reaches the inner body:

```
!let {let: [{a: 1}], in: !let {let: [{b: zzz}], in: {sha: .[b]}}}
  -> let does not bind .[b]
```

Before it was worse than refused. The outer expansion blanked `.[b]` to null, a null pattern
matches anything, and the whole thing matched EVERY document -- including the one whose `sha`
is not `zzz`. An error is the improvement; scoping is the fix.

## What the scoping is

A binding list is evaluated in the ENCLOSING scope, and the body in the inner one:

    !let {let: [{outer: 1}],
          in: !let {let: [{inner: .[outer]}],   <- outer's scope: legitimate, and useful
                    in: {x: .[inner]}}}         <- inner's scope: outer must not touch it

So the outer's expansion should descend into a nested let's `let:` values and stop at its
`in:`. What is there now is one eval.ExpandIR over the whole body, which knows nothing about
scopes.

## Where it would go

mergeop/let.go, letOp.Match. Either a walk which expands structurally and hands leaves to
eval.ExpandIR, stopping at a nested let's `in:`; or an option on ExpandIR saying which subtrees
to leave alone. The first keeps the knowledge of what a let means inside the let op, which is
where the rest of it lives.

The check that produces the error above (letOp.unboundRefs) becomes the same walk: a name is
unbound only if no enclosing let binds it, which a scope-aware walk knows and a flat one cannot.