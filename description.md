# token: a correctly encoded U+FFFD is rejected as bad utf8 — RuneError checked without sz==1, so any document containing the replacement character fails to parse

A quoted string containing a correctly encoded U+FFFD (the replacement character, `�`) is
rejected as `bad utf8`, so any document carrying one fails to parse.

```
in="�"    validUTF8=true  Quote="\"�\""   scan=(1,bad utf8)  Unquote=("",bad utf8)
in="a�b"  validUTF8=true  Quote="\"a�b\"" scan=(2,bad utf8)  Unquote=("",bad utf8)
in="ok"   validUTF8=true  Quote="\"ok\""  scan=(4,<nil>)     Unquote=("ok",<nil>)
```

The input is valid UTF-8 and `Quote` round-trips it correctly. The scanner is what rejects it.


`utf8.DecodeRune` returns `RuneError` for three different situations, distinguished only by
the size it returns alongside:

| input | returns |
|---|---|
| invalid byte | `(RuneError, 1)` |
| truncated sequence | `(RuneError, 1)` |
| **correctly encoded U+FFFD** | **`(RuneError, 3)`** |

The scanners switch on the rune alone:

```go
r, sz := utf8.DecodeRune(d[start:])
start += sz
switch r {
case utf8.RuneError:
    return start - sz, badRune(d[start-sz:])
```

so the third row is treated as the first two. The idiomatic guard is
`r == utf8.RuneError && sz == 1`.

`token/utf8.go` already reasons carefully about this decode — `partialRune` and `badRune`
separate "ran out mid-sequence" from "invalid bytes" — but neither considers the case where
the decode succeeded and simply produced U+FFFD. The helper is right about the distinction
it was written for and silent about this one.


All of these test `RuneError` without the size, and want auditing together:

- `token/quoted.go:154` (`bsEscQuoted` — the one confirmed above)
- `token/literal.go:52`, `token/literal.go:134`
- `token/mlit.go:215`
- `token/tokenizer.go:401`, `token/tokenizer.go:665`
- `token/yaml_quoted.go:74`
- `ir/kpath/kpath.go:901`

A shared helper — something like `badRuneAt(d) (error, bool)` returning "not a failure at
all" for a real U+FFFD — would be better than eight separate `&& sz == 1` edits, given that
this is the second bug in this family (see `hb8c72fph12krv2wcnn0`, where a multi-byte rune
straddling the buffer refill was misread as invalid).


U+FFFD is not exotic. It is exactly what every lossy transcode emits, so any text that has
passed through a decoder with unknown input encoding is likely to carry one — and storing
that text through logd/docd then fails the document rather than the field.


`FuzzQuoteRoundTrip` in `token/quoted_agree_test.go`, added while fixing
`pb1aj0sqh12ksp38cxn0`. That fuzz target currently **skips** strings containing U+FFFD, with
a comment pointing here — the exclusion is deliberate, so that this bug does not have to be
fixed inside a panic-handling change, and it should be removed when this is fixed.