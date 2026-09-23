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
- | # the expression expands inside a block string too
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

A document with no `!eval` in it is answered with itself, expanding nothing.  That
is what makes `o eval` safe over a document someone else wrote, and it is what the
author of a file written FOR eval trips over, the tag reading as noise in a file
that is for nothing else.  `o eval -a` (or `-all`) evaluates the whole input, as if
its root carried `!eval`:

```bash
o eval -a -e x=7 mine.tony    # every $[x] in mine.tony, tagged or not
```

Use `\]` for a literal `]` inside an expression: `$[map["key\]"]]`.

### When the data is not there

`fail(msg)` is how an expression says it has no answer: it raises `msg` as the
expression's own error, so `.[cond ? value : fail("no sha")]` refuses rather than
defaulting to something.  A ternary short-circuits, so the branch not taken raises
nothing, and `o eval` prints the message and exits non-zero.

Whether a path that is not there is a null or an error is the fetch's to say:

```tony
a.b        # a must be there.  A missing b on an object that IS there is null;
           # fetching b from nothing is an error, and so is a.b.c when a.b is missing
a?.b       # a need not be there: absent anywhere along the path is null
x ?? y     # y when x is null -- it cannot rescue a fetch on nothing
"b" in a   # whether a HOLDS b, which tells an absent b from one written null,
           # and answers false rather than erroring when a is nothing
```

So a default is `?.` with `??` -- `.[a?.b?.c ?? "unset"]` holds however far the path
got -- a demand is `.` with `fail()`, and `in` is how an expression asks which of the
two it has.

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
