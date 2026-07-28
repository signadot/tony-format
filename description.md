# encode: scalars that are bare '-' or start with '<', '>' or '|' are emitted unquoted and the parser rejects them (NeedsQuote misses structural indicators)

The tony encoder emits some scalar strings unquoted that the parser then refuses, so a
document the library produced cannot be read back by the same library.

```
scalar    encoded as        reparse
"-"       a_block: -        imbalanced document: extraneous indent
"<"       a_block: <        unexpected <
"<x"      a_block: <x       unexpected <
">"       a_block: >        unexpected >
">x"      a_block: >x       unexpected >
"|"       a_block: |        (read as a block literal, swallows the next key)
"|x"      a_block: |x       (same)
```

`token.NeedsQuote` returns **false** for every one of them.

Complete for single characters: I swept every printable ASCII byte as a whole scalar, as a
prefix (`Cx`) and as a suffix (`xC`), and these seven are the entire set. The pattern is
structural indicators — a value that is bare `-`, or begins `<`, `>` or `|`, is read as a
list marker or a block-scalar header rather than as text.

## Reproduction

```go
doc := ir.FromMap(map[string]*ir.Node{
    "lvl1": ir.FromMap(map[string]*ir.Node{
        "a_block": ir.FromString(">"),      // any of the seven
        "z_after": ir.FromString("sibling"), // must sort AFTER, so it follows the scalar
    }),
})
var b bytes.Buffer
encode.Encode(doc, &b, encode.EncodeFormat(format.TonyFormat))
_, err := parse.Parse(b.Bytes())   // err != nil
```

The sibling has to sort after the offending key — with only the bad scalar present, the
document ends and nothing trips.

## Found by

A fuzz target over encode → parse, which hit the first case in **51 executions** and the
second within a second. There is no such target in the tree; adding one is probably worth
more than this fix. `FuzzEncodeParseRoundTrip` as written generates a nested document with
a fuzzed string leaf and a following sibling, and requires the parser to accept whatever
the encoder produced.

## Scope: no longer a storage risk, still a real one

Log entries are binary events as of v0.0.106 (commit 7733b6b), so a patch carrying one of
these values no longer passes through the text encoder on its way to disk. Anything
depending on indentation or quoting is out of the storage path by construction.

Two caveats on that:

- **The deployed binary has not caught up.** `~/.verse/v0/bin/o` is built from v0.0.102 and
  is what is running now, so records are still being written as block-style text today.
  The exposure closes when it is rebuilt, not when the tag was cut.
- **I have not checked the session protocol.** If logd/docd carry tony text over the wire,
  this is back on a live path. Worth confirming before treating storage-only as the bound.

It remains real for documents on disk, the `o` CLI, and any tony text the library emits.

## The other corpus

`~/.verse/v0/archive/data6-imbalanced` is an unreduced instance from the same family —
verse wrote a record logd refuses at init with `imbalanced document: key indented too much
(10/0) ... (line=60)`. One bad record, `logB` at offset 1914063, 27244 bytes; `logA` is
clean. The break is immediately after a `body: |+` block literal at indent 12 whose content
ends in blank lines: the following key is emitted at column 0 instead of its indent.

I could not reduce it — a `|+` block with trailing blanks and a following sibling
round-trips fine at that nesting, so the trigger is narrower still. It is a text-era
artifact and cannot recur in a v0.0.106 log, which is why it is recorded here rather than
filed on its own. The corpus is preserved if anyone wants to finish the reduction.