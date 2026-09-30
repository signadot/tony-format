# match: an ordered comparison of numbers — against a literal, and against another field (scaled)

A match pattern can test a number for **equality** (`replicas: 3`), and `!get-path(root)` can test it for equality with another field in the same document (`{status: {replicas: !get-path(root) spec.replicas}}`). It cannot ask **less than or greater than**. Nothing in the match vocabulary (`!and`, `!or`, `!not`, `!glob`, `!irtype`, `!has-path`, `!at`, `!get-path`, …) orders two numbers.

## The case

verse wants to warn a Signadot customer when they near their quota. A source reflects each org's quota as

```tony
quota.<org>.<dimension>: {limit: 550, used: 471, month: "2026-09"}
```

and the rule has to select "`used` is at least 80% of `limit`". A verse condition is a match pattern, so today this cannot be written. The workaround is for the source to compute a band (`band: 80`) and the rule to match that. It works, but it moves the threshold out of the charter and into the source, where the person writing the rule cannot change it.

## The ask

Ordered comparison as match ops, in the same style as the rest:

```tony
used: !ge 400                              # a literal
used: !ge.get-path(root) limit             # another field in the document
used: !ge(0.8).get-path(root) limit        # another field, scaled  ← the one the case needs
```

Spelling and composition are yours to decide. Whatever the spelling, the needs are:
- `lt`, `le`, `gt` and `ge`, whose operand is either a literal or a path in the document.
- A **scale** on the path operand, since "within X% of a limit" is the common question and a charter cannot do arithmetic.
- Composition with `!at` and `!not` should read the way `!at(a.b).not 3` does now.
- **What doesn't match, and isn't an error:** a node that isn't a number, a path that names nothing (the reading `!at` gives a missing path), and a wildcard operand, since a set has no order.
- **Mixed int and float** compare by value (`3 < 3.5`). This touches 29xhvsd1, which covers numbers outside the int64/float64 bounds: a comparison should not fail on a value that parses.

## Not asked

General expressions (`$[…]` / expr-lang) in match position. The comparison is enough, and it keeps a pattern data and not a program.