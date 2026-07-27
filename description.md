# encode: a value's preceding comment renders under its key, not above the pair, so formatting reflows every document once

A value's preceding comment is rendered *under* its key rather than above the pair, so
formatting relocates comments on the first pass:

```
in                     out
--                     ---
a: 1                   a: 1
b: 2                     # c
                         2
```

It is stable after that pass — formatting the output again is a no-op, pinned by
`TestCommentRoundTripIsStable` — but every existing document gets reflowed once, and the
result is not the shape anyone writes.


The spec attaches comments to values, not keys:

> Every object, list, and atomic value may have preceding comments and a "line comment".
> All subsequent comments are attributed to the preceding comments of the next value, which
> may be dedented or higher in the object notation.

So `# c` above is the preceding comment of the value `2`, and the IR records exactly that: a
`CommentType` node holding `2` as its only child, standing in the value's place (this is what
`parseBalanced` has always produced for values, and what `associateComments` walks).

The spec constrains *association*, not *layout*, and both of these express that same
association:

```
b: 2               # c
                   2
```

The IR records only the association, so the encoder has to pick a canonical layout — and
whichever it picks, the other input shape reflows. It currently picks the second, via
`encodeCommentUnderField` (`encode/encode.go`, the `ir.CommentType` arm of the object-value
switch), which is a deliberate function, not an accident.


Make the canonical form the first — hoist a value's preceding comment above its key — **when
the value renders inline**. That is what people write, and it makes `o f` idempotent on
existing documents instead of reflowing them.

Keep the under-key rendering when the value is a multi-line object or array, where the
comment sits at the head of a block that already starts on the next line and hoisting it
above the key would attach it visually to the wrong thing. `encodeCommentUnderField` already
special-cases `node.Values[0].Type == ir.ArrayType` for depth, which suggests the block case
was the one it was written for.

Worth confirming against the reference implementation / other tony tooling before changing
output, since this is user-visible formatting.


Before `911d959` these comments were dropped entirely, so there is no prior rendering to
preserve — this is choosing the layout for output that never existed. That also means the
change is safe to make now, before anyone's formatted files depend on the under-key shape.