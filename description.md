# logd: !logd-patch-root is not inert in an operator's tag chain -- a root !rename nulls the whole document

Severity: total document loss, on default settings, from one accepted write. No
snapshot needed, no scope, no keyed array.

    store holds   {k1: 5, k2: 16, a: {z: 1}}
    client writes ""  <-  !rename [{from: "k1", to: "k1"}]      (a no-op rename)
    store reads   null

With EnableLowering(false) the same write leaves the document untouched. Lowering
is ON by default, so this is the shipped path.

## Mechanism

The marker says WHERE an entry is applied from. It is carried in the node's TAG
CHAIN, which is also where the operation lives, and a merge dispatches on that
chain -- mergeop hands back whatever trails the operation as the value's own tags.
On a plain container the two never meet. On an OPERATOR node they share one
namespace, and the marker stops being metadata.

The no-op rename lowers to a presentation-only delta whose ROOT is the tag op:

    !addtag(bracket) null

`null` there means "no value change". markDeltaRoots cannot descend past it -- the
descent wants a single-field object -- so it marks that node, giving
`!addtag(bracket).logd-patch-root null`. The trailing marker becomes the value's
own tag, the null becomes a real tagged null, and the merge answers null.

Applied WITHOUT the marker the same delta is correct. The delta is right; the
marker destroys it.

## The family, not the case

Marked vs unmarked application of the same patch to `{k1: 5, k2: 16}`:

    !delete                            <nil>            <nil>            same
    !delete(bracket)                   <nil>            <nil>            same
    !insert {z: 1}                     {z: 1}           {z: 1}           same
    !replace {from: ..., to: {z: 9}}   {z: 9}           {z: 9}           same
    !raw {z: 1}                        {z: 1}           {z: 1}           same
    !addtag(bracket) null              {k1: 5 k2: 16}   null             DIFFERS
    !rmtag(bracket) null               {k1: 5 k2: 16}   null             DIFFERS
    !retag(bracket,flow) null          !flow {k1:5...}  !flow null       DIFFERS

The three that differ are the tag ops, and they differ for one reason: their child
is `null` meaning "keep the value", which only survives while nothing appends a tag
to it. Any future operator with that shape joins them.

## Scope of reachability

It needs the delta's ROOT to be the operator node, which is a write at the DOCUMENT
ROOT. The same write one level down is safe, because the delta is then
`{a: !addtag(bracket) null}` and markDeltaRoots marks the plain object above it:

    write ""   <- !rename [{from: "k1", to: "k1"}]   ->  read null
    write "a"  <- !rename [{from: "z",  to: "z"}]    ->  read correct

A rename that actually renames is also safe: its delta's child is an object, not a
null, so materializing it costs nothing. Only the null-child ops lose.

## Suggested fix

Strip the marker from the patch before merging, not only from the result. The
marker's job ends once the streaming processor has used it to decide where to
apply; from that point it has no business in WHAT is applied. buildPatchValueIndex
locates the roots first, so applyPatchesToNode and the empty-base fold can each
merge a marker-free copy. That retires the whole family rather than the three ops
that show it today.

Choosing a different node to mark does not work at the document root -- there is no
other node -- and putting the marker at the head of the chain is what MarkPatchRoot
exists not to do (jjbapb1ah12kranxg5n0): there it masks the operation instead.

The deeper statement, and the reason this took three sittings to see: a marker in
the tag chain of an operator node is not a patch root at all. It is a label inside
the patch's own grammar, and the merge reads it as such.

## Related

- 2w62pyyah12ksqh0jdn0 (closed) -- the same collision seen from the other side: the
  marker riding OUT of a fold into the document, where the next operation's tag
  check refused it.
- 1xnezrpkh12ksavvjdn0 (closed) -- re-rooting a marker onto a projection had to
  decline for operator nodes for exactly this reason. That decline can go away if
  this is fixed.

## Reproducing

Read-only, outside any checkout: a module with a `replace` to the tree under test,
public API only (storage.Open, EnableLowering, NewTx/NewPatcher/Commit,
ReadPatchesInRange, ReadStateAt). Every table above is that program's output.