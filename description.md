# parse: a dangling ':' is accepted as the last pair of a document and rejected everywhere else — a form the grammar does not define

`docs/tony.md` defines omitted values one way, and only one:

> ### Key Sets
> In bracketed mode only, a set of keys may be denoted by dropping the ':' and value after
> any key. This is syntactic sugar for associating a null value with the key.

So `{a b c}` is the form, and it works. A dangling `:` is a different thing and is in the
language nowhere: `{p:}` and `{a: 1, p:, b: 2}` are both rejected, and so is a bare key in
indentation mode.

Except in one position. As the LAST pair of a document, a dangling `:` is accepted and
yields null:

```
a: 1
p:
```
```
a: 1
p: null
```

Anywhere else it fails, and the message describes what the parser found after it had
already gone wrong rather than the pair it could not close:

```
list:
- a: 1
  p:
- b: 2
```
```
imbalanced document: extraneous '- ' indent `...  p:
- b: ...` at offset 18 (line=3, col=0)
```

A tag makes no difference — `p: !delete` behaves exactly as `p:` does, accepted last and
rejected before a sibling.

THE ASK IS TO REJECT IT CONSISTENTLY, not to accept it. Accepting a dangling ':' in
indentation mode would mean deciding value-versus-sibling by lookahead on indentation, and
the YAML rule it would be imitating — a sequence may sit at its parent key's column — makes
`p:` followed by `- x` genuinely two-way readable. That is a grammar this format seems to
have deliberately not taken, and the accidental acceptance at end-of-document is not a
reason to take it. Two things instead:

- **Close the pair, or refuse it, in one place** — so end-of-document stops being special.
  A document whose last pair is `p:` is currently accepted with a value the author did not
  write, which is the same class of surprise as a silently dropped field.
- **Say what to write.** The error names neither the pair, nor the requirement, nor either
  spelling that works (`p: null`, or the bracketed key-set form). In the list case above it
  points a line past the cause, so the natural reading is that the sibling is at fault.

If the end-of-document acceptance is deliberate rather than accidental, then it is the spec
that needs the sentence, and the other positions still need the message.

Checked at a9b63d9 and at go-tony v0.0.113.

Context: found writing verse's `deploy/build.tony`, where `patch: !delete` is the natural
spelling for "drop this object" and a build file's patches are a list — so it parsed while
it was the last patch and stopped the moment another was added below it. The fix there was
`patch: !delete null`, which is explicit and conformant; `!delete {}` also builds, but it
passes an empty object where a null was meant, and I reached for it first precisely because
the error did not mention null.