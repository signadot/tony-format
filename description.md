# encode: a head comment on a key whose value is TAGGED renders the value detached from its key

go-tony v0.0.146, encode with EncodeComments(true).

## Repro — three lines

    a: 1
    # note
    b: !and [x, y]

Parse it with ParseComments(true) and encode it with EncodeComments(true):

    a: 1
    b:
    # note
    !and
    [
      x
      y
    ]

The key is emitted with nothing after it, and the comment, the tag and the list all land at
column 0. That is no longer `b`'s value: re-parsing says

    imbalanced document: extraneous indent (no comment) TTag "!and" `...note\n!and\n...`
    at offset 15 (line=3, col=0)

The parse error is the symptom that made it visible. The defect is that the RENDERING says
something different from the tree — a reader (or another parser) has lost which key the value
belongs to, and it would have been silent if the result happened to parse.

## It is the encoder, not the parser

The tree the parser builds is correct:

    Object
      a: Number
      b: Comment lines=[# note]
           Array tag="!bracket.and"
             String "x"
             String "y"

Building that same shape BY HAND — ir.FromKeyVals with a CommentType node wrapping
ir.FromSlice(...).WithTag("!and"), no parser anywhere — and encoding it gives the identical
broken output. So the parser is exonerated and this is entirely in encode.

## What does and does not trigger it

| value under a head-commented key | round trips |
|---|---|
| untagged block list (`b:` / `- x` / `- y`) | YES — and correctly, with the comment ABOVE the key |
| tagged scalar (`b: !glob "x*"`) | YES |
| tagged flow list (`b: !and [x, y]`) | no |
| tagged block list (`b: !and` / `- x` / `- y`) | no |

The untagged case shows the shape the tagged one should have: the comment goes above `b:`, and
the value stays where it was.

## Why it matters downstream

verse posts documents to itself — the CLI parses a charter file, re-renders it, and the server
parses what arrives — so this makes a correct charter uninstallable. The rule that found it has

    # ONLY WHILE IT CAN STILL LAND. A branch fast-forwards onto main exactly while …
    value: !and
    - !not.has-path state
    - !let …

which is an ordinary thing to write: an operator with a paragraph above it explaining why. The
workaround is to move the comment somewhere its value is not tagged, which is exactly the
placement a person would not choose.