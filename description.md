# logd: the storage vocabulary cannot say 'the value here is exactly this'

The storage vocabulary has no primitive meaning "the value here is exactly this", and
the one it names as meaning that does not.

`storableTags` describes `insert` as

> adds a value; the value is what results

It does not, for a container. `insertOp.Patch` calls the patch function on the
document with its child, so the child MERGES with what was there:

```
doc    {a: [!t1 {x: 1}, 2]}
patch  {a: !insert [!t2 {y: 9}]}
gives  {a: [!t1.t2 {x: 1, y: 9}]}
```

The array was replaced -- element `2` is gone -- but the surviving element merged and
its tag composed. `!t1` and `!t2` became `!t1.t2`.

## where it bites

`DiffAbsolute` restricts a diff to primitives that state what a value is, so that a
store can keep the answer and re-apply it to a base that has moved. It works for
scalars, objects and keyed arrays. It cannot answer for a POSITIONAL array or a type
change, because the only absolute answer there is the whole new value, and there is no
way to say "this value, replacing what was there".

Measured over the diff/patch property generator, 500 seeds:

- vocabulary restriction: holds on all 500. Nothing relative is emitted.
- round trip: 6 of 500 fail, all the same shape -- a whole container emitted, its
  contents merged with the base, tags composed.

```
a     [true, !t2 [!t1 {rev: !t2 1}]]
b     ["",   !t2 [!t1 {rev: !t2 1}]]
diff  ["",   !t2 [!t1 {rev: !t2 1}]]
left  !arraydiff 1: !arraydiff.addtag(t2) 0: !retag(t1.t1,t1) null
```

## what would close it

Either

**(a) a primitive that replaces.** Absolute by construction: the result is the value
it carries, whatever was there. `!insert` is the natural name for it and already has
the docstring; what it lacks is the behaviour. Changing it is a format change and
would want its own look at what depends on the merge semantics.

**(b) state the tags separately.** `!addtag` and `!rmtag` are both storable and are
the unconditional halves of `!retag`, so a tag difference CAN be stated absolutely.
That does not by itself solve the container case: the contents still merge, so an
element the new array does not have is not removed.

(a) looks like the answer, and (b) is needed anyway wherever a tag changes.

Found while building the lowering, where a lowered write is exactly
`DiffAbsolute(base, next)` and has to reproduce `next` against the base it was taken
from before it can be trusted against one that has moved.