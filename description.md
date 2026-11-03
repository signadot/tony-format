# eval: no exported way to list a string's expressions as ExpandString finds them, so an embedder writes its own scanner

Context: verse expands a spawn's strings and a gate's pose with `eval.ExpandStringWithOptions` (verse branch `verse/spawn-eval`, issue y1n12a3gh12ksh4mx5n0). Before a firing, and without the values, verse has to know which expressions a string holds:

- **at install**, a ceiling check stands each expression in for `**` so `repo: acme/$[subject.value.name]` is checked as "some repo under acme" (`systems/ceiling.go`, the `bound` regex);
- **before binding**, verse reads the subject's value at a revision only when an expression reads `subject` other than as `subject.ref` / `subject.name` (`launch.Spec.Reads`, over `expressions()` in `launch/launch.go`).

go-tony exports `GetRaw` for a whole `.[…]`, but nothing for the `$[…]` spans inside a string. `scanExpression` is unexported, so verse has two scanners of its own, and neither matches go-tony's:

- the ceiling's regex takes one level of nested brackets and no quotes: `acme/app-$[subject.value.repos[subject.value.idx[0]]]` is not stood in, and the literal text is held to the ceiling;
- verse's `expressions()` skips `"` and `'` strings but not backticks, and does not undo `\]`: ``$[x ?? `a]` + subject.value.round]`` ends early for verse and not for the evaluator.

Where an embedder's scanner and the evaluator disagree, install checks and pre-reads see different expressions from the ones that are evaluated.

## Ask

An exported function that answers the expressions of a string exactly as `ExpandString` / `ExpandStringWithOptions` would evaluate them, for example:

```go
// Expressions is each expression ExpandString would evaluate in v, in order, as the
// evaluator gets it (escapes undone): each `$[…]`, or v's one expression where v is a
// whole `.[…]` (GetRaw). `$$[` and an unclosed `$[` contribute nothing.
func Expressions(v string) []string
```

With the spans too (start, end), a caller can stand each one in for a placeholder, as verse's ceiling does with `**`; that could be a second function or a struct result.

Verse would then use it in both places and delete both scanners.