# parse: a comment on the opening line of a nested brace object is refused

`x: {# c\n a: 1}` fails with `parseObj: unexpected token "# c"`, while `{# c\n a: 1}` at the root, `x: {\n# c\n a: 1}`, and the array form `x: [# c\n a]` all parse.

The tokenizer marks a comment after `{` on a line that already holds `x:` as a TLineComment, where after a root `{` it is a TComment. `parseObj` collects only TComment before a key and its default arm refuses anything else when no pair has been read yet; `parseArr` reaches the same token through `parseBalanced` and takes it.

Found while probing comment shapes for q47aszzwh12kr7x6xxn0.