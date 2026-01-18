## Tony Eval nodes

With Tony, anything in a match or a patch or a tool node has expr-lang support
with respect to the environment and a suite of special operations either for
matching, patching, or evaluation.  Tony uses [YAML
tags](https://yaml.org/spec/1.2.2/#24-tags) to denote these operations and
supplies a library for easily adding tags.

### Eval Nodes

Eval nodes can be placed in an object and then evaluated

```tony
name: Sam # this is just plain json-izable YAML
position: "somewhere $[x]" # this is the literal string `somewhere $[x]`
what: !eval
  # in the below, `x` is any expr-lang expression
  - .[x] # this evaluates the the value of x in the environment
  - $[x] # this evaluates to a string containing the value of x from the environment
  # Use \] to include literal ] in expressions: $[map["key\]"]]
  - | # below expands the expression x in the multiline string
    well hey $[x]
```

Running `o -e x=7 -c` on the above gives us

```tony
name: Sam # this is just plain object literal.
position: "somewhere $[x]" # this is the literal string `somewhere $[x]`
what:
  # in the below, `x` is any expr-lang expression
  - 7
  - "7" # this evaluates to a string containing the value of x from the environment
  - | # below expands the expression x in the multiline string
      well hey 7
```
