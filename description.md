# docs: the match-op lists outside the reference omit !lt, !le, !gt, !ge

nqe7v0j0 and ec3dnqpk added `!lt`, `!le`, `!gt` and `!ge`. They updated the tables a test keeps equal to the registry (`docs/matchpatch.md`, `docs/generated/index.md`, `docs/generated/mergeop.md`), but missed the prose lists of match ops elsewhere:

- `docs/tonyschema/contexts.md`: the match context's tags. This list is otherwise complete, so the omission reads as "not in the match context".
- `docs/tonyschema/tags.md`: Match Context Tags.
- `docs/tonyschema/validation.md`: the operations an `accept` commonly uses. A numeric range is a typical schema constraint.
- `go-tony/README.md`: the tags `o match` supports.

Left alone: `mergeop/doc.go` and `go-tony/docs/mpd-right.md`, an article, both name examples ending in "etc." or "round out", not a list a reader takes as complete.