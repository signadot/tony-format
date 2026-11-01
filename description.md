# kpath: `a.**` parses as `a.*.*`, a wildcard without the dot that spells one

kpath's rule, stated at `parseAfterSegment` in ir/kpath/kpath.go, is that a wildcard ends a segment and what follows needs its separator: `**` and `a[*]*` are refused with "expected '.', '[', '{' or '(' after a segment, got '*'". The `.*` branch of `parseKFrag` does not make that check. It recurses straight into `parseKFrag`, so a `*` after `.*` is read as the start of another segment:

```
**         refused: expected '.', '[', '{' or '(' after a segment, got '*'
a[*]*      refused: expected '.', '[', '{' or '(' after a segment, got '*'
a.**       = a.*.*
a.**.b     = a.*.*.b
a.*b       = a.*.b
a.*'x'     = a.*.x
```

(go-tony v0.0.248.)

A rule in verse written `reflex.**` for "everything under reflex" selected exactly two levels down and never fired (signadot/verse#62). The spelling for every depth is `..`; `a.**` should be refused as `**` is, in kpath's own words.

The fix: the `.*` branch continues with `parseAfterSegment`, as the leading-`*` branch does. `TestEveryRenderedPathParsesBack` keeps the renderer's side: String never emits a `*` directly after `.*`. The separator test can take `a.**`, `a.*b` and `a.*'x'` beside `**`.