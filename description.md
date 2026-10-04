# mergeop: a regular-expression match beside !glob

go-tony's match operations have `!glob`, which is `filepath.Match`: `*` spans any run of characters but the separator, and a character class matches one character. There is no way to anchor a pattern or repeat a class, so a match over free text cannot say where a word must stand.

The ask: a regular-expression match, for example `!regex "<pattern>"` (Go's `regexp` syntax, RE2, so no backtracking cost), matching a string node when the pattern matches it. Anchoring is the pattern's own (`^…$`), as Go's `MatchString` does, so `!regex "proceed"` is a substring match and `!regex "^proceed$"` an exact one.

Why it comes up: verse's repo-charters reconciler (signadot/verse, git-issue dzxn44gw) reads the answer to its question as text, `<principal> chose <option>` with `: <words>` when the person said more. A principal has no space in it, so `^\S+ chose proceed(: |$)` says exactly "the option chosen was proceed". With `!glob "* chose proceed*"` the leading `*` also spans the person's words, so an answer like `user:a chose proceed: last time they chose deny` matches the deny rule as well as the proceed one.

Filed from the verse repository (signadot/verse), 2026-10-04.