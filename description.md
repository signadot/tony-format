# logd: the scope overlay merges an owned value into an operator's operand

`BuildScopeOverlay` finishes by re-stating every path the scope owns, merging each into
the diff it just computed (plan R3):

	rooted, err = tx.RootPatchAt(p, val)
	overlay, err = api.NextState(overlay, rooted)

That treats the overlay as a document and the owned value as a patch, which is right
while the overlay holds only data. It no longer does: since 4c37d3e a node whose
comment and value both changed is stated as `!comment {head: ..., value: ...}`, and
merging an owned value into THAT merges it into the operand:

```
d: e: !comment  head: []  k1: 10  value: {k1: !insert 10}
                          ^^^^^^ the owned value, merged into the operand
```

`k1: 10` is the scope's value at `d.e`. It landed beside `head:` and `value:` because
`api.NextState` was asked to merge two objects and the operand is an object.

Every read of that scope then fails:

	failed to apply patches: comment op operand names "head", "line" or "value", got "k1"

Found by the scoped differential (`TestLoweringScopeDifferential`), on the PLAIN store
-- lowering off. The overlay always runs, so this is not about lowering; it is about
the diff shape the overlay is built from.

## the shape of the fix

The union has to know that an operand is not data. Three ways:

1. **Merge into the operator's value:.** `!comment` now carries the value it is about
   (f854bb1), so the combined statement at that path is `!comment {head: ..., value:
   <the owned value>}` -- the owned value replaces the comment's value rather than
   joining its operand. Uses the machinery that already exists; needs the union to root
   at `p` + the operand's value field when the overlay has an operator at `p`.

2. **Ask mergeop where the values are.** `mergeop.OperandPaths` (81deb91) answers
   exactly "which parts of an operand are document values, and at what path", which is
   the same question this union is getting wrong. It is the general fix and the union
   is one more caller.

3. **Build the overlay's diff without comments and state them separately.** Smallest
   change, and it gives up what `api.SameState` counts: a scope whose only difference
   from baseline is a comment would stop being represented.

(2) looks right and (1) is the smaller step toward it.

## note

This is the second walker to get an operand wrong in the same way -- the index was the
first (81deb91), and the note there predicted the rest: "The other walkers ... ask the
same question and can move over one at a time." This is one of them, found the hard way
rather than by moving it over.