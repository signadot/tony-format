# parse: an unquoted scalar that starts with a digit and is not a number is a hard error — 100m, 1Gi, 30s

tony rejects any unquoted scalar that begins with a digit and does not finish as a number.
The lexer takes the leading digits as a number and the rest as a separate literal, and the
document then fails as two adjacent values.

```
args:
- -timeout
- 30s
```

```
imbalanced document: unseparated array elements TLiteral "s" `...\n- 30s\n\n...` at offset 15 (line=2, col=4)
```

The same for `2m`, `1h30m`, `3d`, `10x`. In a mapping the message is different and further
from the cause:

```
resources:
  limits:
    cpu: 100m
    memory: 1Gi
```

```
imbalanced document: key not followed by : got TIndent "    " `...100m\n    m...` at offset 35 (line=3, col=0)
```

Quoting fixes both, and the YAML reader accepts all of them unquoted (`o -I yaml view`),
so a manifest that round-trips through YAML stops parsing the moment it is written as tony.

Whether the lexer should ACCEPT these is a real question and this issue does not assume the
answer — `1e9` and `0x1f` are reasons to be careful about "digits then letters". But two
things are true whichever way that goes:

- **The diagnostics are wrong.** Neither message mentions the scalar, quoting, or numbers.
  "unseparated array elements" and "key not followed by :" both describe what the parser
  found after it had already mis-lexed, and in the mapping case it points at the line after
  the offending one.
- **The k8s cost is high**, which is the audience `o build` was written for: `cpu: 100m`,
  `memory: 1Gi`, `- 30s` and every duration and quantity in a manifest are exactly this
  shape. Kubernetes' own quantity type is digits-then-suffix by definition, so a manifest
  author hits this immediately and the error does not tell them what to do.

If the answer is "quote it", the fix is a lexer error naming the scalar and saying so. If
the answer is that a digit-leading token with a non-numeric suffix should lex as a string,
that is a language change and wants writing down in the spec next to numbers.

Checked at a9b63d9 and at go-tony v0.0.113.

Context: found writing verse's `deploy/build.tony`, where a container arg list carried
`- 30s` and `- 2m`.