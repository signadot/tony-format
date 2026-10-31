# stream: the wire decoder reads a key set as values under empty keys, and accepts an empty slot the parser refuses; the parser accepts `{a: 1,,b: 2}`

Measured at 2d6db94a, parse.Parse against stream.ReadDocument on the same text:

```
{a b c}       parse: {a: null, b: null, c: null}    wire: {"": a, "": b, "": c}
{a !t}        parse: {a: !t null}                   wire: {"": a, "": !t null}
{a !t, b: 1}  parse: {a: !t null, b: 1}             wire: {"": a, "": !t null, b: 1}
[1, , 2]      parse: refused, non separating ','    wire: [1, 2]
```

**Key sets.** docs/tony.md (Key Sets) makes a bracketed key with no `:` a key with a null value, and a tag after it a tag on that null. The decoder only makes a token a key when a `:` follows it. Otherwise it is a string value, and the state takes a value with no key under the empty key. Nothing refuses, so a key set sent to logd or docd is stored wrong without a word.

**Empty slot.** The decoder drops every comma, so it never sees two in a row. The parser refuses `[,1]`, `[1,,]` and `[1, , 2]`, and the spec's Commas section shows `[,]` as invalid.

The fix is for the decoder to read both the way the parser does.
