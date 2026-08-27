# encode: a block literal in an array gains two spaces of content on every o v pass

`o v` is meant to be the normal form -- `o v -w` writes its answer back over the file.
A block literal that is an ARRAY ELEMENT does not survive it: the encoder writes the
content one level deeper than the reader will look for it, so the difference becomes
part of the string, and it compounds.

```
$ printf -- '- |\n  ape\n' | o v -O json -wire
["ape\n"]

$ printf -- '- |\n  ape\n' | o v | o v -O json -wire
["  ape\n"]

$ printf -- '- |\n  ape\n' | o v | o v | o v -O json -wire
["    ape\n"]
```

| written | value | after one `o v` | |
| --- | --- | --- | --- |
| `k: \|` + `  ape` | `ape\n` | `ape\n` | same |
| `{k: \|` + `  ape` `}` | `ape\n` | `ape\n` | same |
| `- \|` + `  ape` | `ape\n` | `  ape\n` | **changed** |
| `a:` + `- \|` + `  ape` | `ape\n` | `  ape\n` | **changed** |
| `- - \|` + `    ape` | `  ape\n` | `      ape\n` | **changed** |

A field's value is fine. Only an element is wrong, and every element is wrong.

## which side is off

The reader computes the content indent from the line the `|` is on, one level in:
`mLitIndent`'s `TArrayElt` arm walks past the marker and adds 2, so `- |` at column 0
wants its content at column 2. That agrees with YAML, which reads

```
- |
  ape
```

as `ape\n`.

The encoder writes it at 4. `encodeArray` does `es.depth++` around each element --
correct for an object element, whose later fields sit at column 2 under the `- ` --
and `encodeBlockLit` then does `es.depth++` of its own. For a field's value the first
bump does not happen, which is why `k: |` is right. So the encoder counts the element
level twice and the reader counts it once.

The encoder is the side that is wrong: the reader agrees with YAML and with the spec's
own rule that the content is one level in from the opening line.

## why it matters more than it looks

`o v -w` was added so `o v` could be gofmt for tony. Every write of a file holding a
block literal in an array silently edits the value, and the next write edits it again.
Nothing errors and nothing warns; the document stays valid, so a diff looks like
whitespace.

Found while fixing the opening-line comment (e295310), which is unrelated and does not
cause it -- verified against the parent commit.