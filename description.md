# parse: a root comment after a dedent of several levels, followed by a nested list, is refused as "trailing material"

A comment at the root, placed after a value that ends several levels deep and before a key whose value holds a nested list, makes the document unparseable. Without the comment, the document parses.

Reproduced at `go-tony/v0.0.246`, which `origin/main` is at (2026-10-28), with `o view`:

```tony
a:
  b:
    c:
      d:
      - 1
    e: 2
# comment
f:
  g:
    h:
    - 3
  i: 4
```

```
$ o view min.tony
error processing min.tony: error decoding document 0: imbalanced document: trailing material "  "
```

If the `# comment` line is deleted, the document parses. The indentation reported as trailing material (`"  "` here, `"    "` in the original file) is that of the first line after the nested list under `f`.

## Where it was found

verse's runner configuration (signadot/verse, branch `verse/runner-config`, `deploy/verse-runner/verse-runner.yaml`): the comment introducing a `script:` section at the root followed `agents: {verse: {systems: {...}}}` and preceded `script: {systems: {git: {ceiling: [{push: [...]}]}}}`. `runnerd` refused to start with the error above. The workaround there is to move the comment inside the `script:` block, as its first line.

## Not the same as

- 4ynqp7wqh12krg32msn0 item 18: a line comment on a nested closing bracket, written where the parser refuses it (also "trailing material", but a bracketed document and an encoder issue).
- z3zdhbtph12kr3nzmdn0: whether a whitespace-only line is extraneous indentation.