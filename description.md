# encode: a multi-line string gains a trailing newline through the |+ block literal, so it does not survive encode -> parse

A multi-line string does not survive encode → parse: trailing newlines are not preserved.

```go
in  := "one\ntwo\n\n"
out := "one\ntwo\n\n\n"   // one newline too many
```

Encoded, that value takes the `|+` (keep) block literal, and the extra newline comes back
on the way in.

## Reproduction

```go
doc := ir.FromMap(map[string]*ir.Node{
    "lvl1": ir.FromMap(map[string]*ir.Node{
        "a_scalar": ir.FromString("one\ntwo\n\n"),
        "z_after":  ir.FromString("sibling"),
    }),
})
var b bytes.Buffer
encode.Encode(doc, &b, encode.EncodeFormat(format.TonyFormat))
n, _ := parse.Parse(b.Bytes())
// ir.Get(...,"a_scalar").String == "one\ntwo\n\n\n"
```

## Where to look

`encodeBLit` (`encode/encode.go`) picks the chomping indicator: `|-` when the value does not
end in a newline, `|+` when there is more than one trailing newline or space, plain `|`
otherwise. It then strips exactly one trailing newline from the value before writing it, and
for the `|+` case writes an extra bare newline afterwards — that last write goes straight to
the writer rather than through the indent-tracking path.

So there are two things to check together: whether the count written matches what `|+` means
on the way back in, and whether the raw newline write is correct at all. The same raw write
is a suspect in the unreduced `data6-imbalanced` corpus, where the key following a `body: |+`
block is emitted at column 0 instead of its indent.

## Found by

`FuzzEncodeParseRoundTrip` in `parse/encode_roundtrip_test.go`, on its own seed corpus, once
that target was strengthened from "the output parses" to "the value comes back unchanged".
The weaker property had been passing.

That target currently compares multi-line values with trailing newlines trimmed, so this bug
does not mask new ones. Tightening it back to exact comparison is the test for this fix.

Related: `dss3tkggh12kr4w2d1n0` (scalar quoting — same round-trip property, different part of
the encoder).