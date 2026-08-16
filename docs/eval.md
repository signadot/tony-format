## Tony Eval nodes

With Tony, anything in a match or a patch or a tool node has expr-lang support
with respect to the environment and a suite of special operations either for
matching, patching, or evaluation.  Tony uses [YAML
tags](https://yaml.org/spec/1.2.2/#24-tags) to denote these operations and
supplies a library for easily adding tags.

### Eval Nodes

Eval nodes can be placed in an object and then evaluated.  A value is only
evaluated where an `!eval` says so: everywhere else `$[x]` is the text `$[x]`.

```tony
name: Sam                     # plain object notation, evaluated by nothing
position: "somewhere $[x]"    # the literal string `somewhere $[x]`
what: !eval
# in the below, `x` is any expr-lang expression
- .[x]                        # the VALUE of x in the environment
- $[x]                        # a string containing the value of x
# the expression expands inside a block string too -- the comment goes here,
# because everything after the | on its own line belongs to the block
- |
  well hey $[x]
```

Running `o eval -e x=7` on the above gives

```tony
name: Sam
position: "somewhere $[x]"
what:
- 7
- "7"
- |
    well hey 7
```

`.[x]` answers with the value the environment holds, keeping its type -- `7`, a
number -- while `$[x]` builds a string, so the same binding comes back as `"7"`.

Use `\]` for a literal `]` inside an expression: `$[map["key\]"]]`.

### Two things the format does not allow here

The list under `!eval` is written at the same indentation as the key, not
indented beneath it: in block mode the `- ` prefix IS the indentation (see
[White Space](./tony.md#white-space)), and an indented list under a key is
refused as an extraneous indent.

A block scalar cannot carry a line comment: `- | # what follows` does not parse,
because everything after the `|` on that line belongs to the block.  Put the
comment on the line before, or after the block.

### Comments and eval

`o eval` reads a document and writes the result, and it has no `-c` flag, so
comments in the input do not appear in the output.  `o view -c`, `o diff -c`,
`o patch -c`, `o get -c` and `o list -c` do keep them.
