# logd: a relative operation composed behind an absolute one passes the storage vocabulary

`ValidateForStorage` reads only the HEAD of a composed tag, so a relative operation
written behind an absolute one passes the vocabulary check entirely.

```
{a: !retag(x,y) 5}                      refused: operation "!retag" may not be stored
{a: !insert.retag(x,y) 5}               STORABLE
{a: !insert.strdiff []}                 STORABLE
{a: !delete.replace {from: 1, to: 2}}   STORABLE
```

A tag composes with `.`, so `!insert.retag(x,y)` names two operations and the second
binds as much as the first. `validateForStorage` asked `mergeop.SplitChild`, which
answers the head only -- it said "insert, storable" and stopped.

## what it costs

The vocabulary exists so a scope cannot store an operation that re-evaluates against a
base that has moved. `3xn08cb6h12kr4psg5n0` records what happens when one gets through:
the scope becomes unreadable from the next baseline write onward and `DeleteScope` is
the only repair. `checkStorableInScope` is the guard on the client's write and it calls
this, so `{s: !insert.replace {from: bob, to: rob}}` is accepted today by a scoped
write, exactly as the plain `!replace` it wraps would be refused.

Also on the overlay path: `WriteScopeOverlay` validates what it built before appending,
which is what makes a `diffArray` fall-through safe (`diffarray_gate_test.go`). A
composed positional diff -- `!arraydiff` behind something absolute -- would pass.

## the walk that gets it right

`mergeop.FindUnsafe` has always read the whole chain, and answers correctly on the same
input: `{a: !insert.pipe {}}` gives `pipe`. Its loop over `ir.TagArgs` is the shape this
needs, asked about storability instead of safety.

One subtlety: a tag chain also carries data tags and presentation tags, which are not
operations at all. The test is `mergeop.Lookup(head) != nil && !IsStorableTag(head)`,
not `!IsStorableTag(head)` alone -- `!bracket`, `!oct` and a data tag like `!t1` are
none of the three classes.

Found while building `NeedsLowering` for the lowering, which asks the same question per
write and inherited the same walk.