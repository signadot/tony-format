# token: QuotedToString panics on a quoted string the scanner accepted — unrecovered in docd's read pump, and the same string desynchronizes log frame alignment on restart

docd panicked on a quoted string containing `\n` escape sequences, backticks and em-dashes.
The failure was a **panic**, in the read pump, **unrecovered** — not a returned error — so it was
not confined to the offending session. The whole daemon went down.

On restart, docd cannot read the log back:

```
failed to read state for init  path=verse commit=3761
  failed to deserialize entry at position 19459758:
  unicode control at `...ctri\x00\x00-\xd9!e...` at offset 470 (line=16, col=82)
```


They read like a corrupt log. They aren't. `\x00\x00\x2d\xd9` is a 4-byte big-endian **record
length prefix** (0x2DD9 = 11737); the neighbouring frame's is `\x00\x00\x07\xb4` (0x07B4 = 1972).
That is exactly the framing `dlog` writes and reads — see `DLogFile.ReadEntryAt`
(`system/logd/storage/internal/dlog/dlog.go:585`), which reads 4 bytes of length at `position` and
then `length` bytes of entry, and the write-side position advance at `dlog.go:578`
(`currentPos + 4 + len(entryBytes)`).

So the decoder is parsing a **frame header as document text**. It has lost frame alignment;
`ErrUnicodeControl` is merely the first NUL it trips over, 470 bytes into what it believes is one
entry. The log is very likely **intact**. What is broken is the decision about where a record
starts and ends.


The live reader panicked on a specific quoted string. The restart reader loses alignment at what
is almost certainly the same string. A tokenizer that mis-consumes a quoted string produces both
faces: in-memory it walks off the end of the token and panics; on the storage path it reports a
consumed length that doesn't match the record, and every subsequent frame boundary is wrong.

Fix the tokenizer and commit 3761 should replay without any repair tooling. That is worth
confirming before anyone writes repair tooling.


Two **unrecovered panics** live in the string decoder:

- `token/quoted.go:207` — `panic("internal string: trailing %q")`, raised when the closing quote is
  found before the end of the byte range handed in. This is precisely the "I was given a token
  whose extent disagrees with mine" signature.
- `token/quoted.go:247` — `panic("internal string %q")` on an escape byte the decoder does not
  recognize.

Note that `:247`'s own message expression, `string(d[i-sz-4 : i+10])`, is unguarded: for a short
token `i-sz-4 < 0` or `i+10 > len(d)`, and it panics with *slice bounds out of range* before the
intended diagnostic is ever constructed. That is likely why the panic text was not more useful.

The structural defect underneath: `bsEscQuoted` (`token/quoted.go:126`) is a scanner that **returns
errors** for input it rejects, while `QuotedToString` (`token/quoted.go:190`) is a decoder that
**panics** on anything it does not expect. They are two hand-maintained implementations of the same
escape grammar, with no shared table and no test asserting they agree. Any divergence between them —
or any caller that hands `QuotedToString` a byte range the scanner did not produce — is a daemon
crash rather than a bad-document error.

`QuotedToString` is on live serving paths:

- `system/logd/server/session.go:984`
- `system/logd/storage/tx/match.go:91`
- `parse/parse.go:245`, `parse/parse.go:423`
- `token/types.go:83` (`TString`), `token/types.go:88` (`TMString`)

The `TMString` path deserves a hard look, because it is the one that reassembles a token out of
pieces. `Token.String()` splits the merged token bytes on `'\n'` and calls `QuotedToString` on each
part, so **every part must independently be a complete, well-formed quoted string**. Those bytes
come from `msMergeToks` (`token/mstring.go:135`), fed by segments that `nextMString`
(`token/mstring.go:100`) locates with an indentation scan. That scan understands `#` comments,
spaces and quote characters, and nothing else — notably not backticks (multiline literals). If it
ever picks up a quote that is not a continuation, or misses one that is, the parts stop lining up,
`:207` fires, and the reported consumed offset is wrong — which is exactly what would desynchronize
framing on the storage path.


1. `QuotedToString` must not panic. Return `(string, error)` and thread it through
   `Token.String()`'s callers. A malformed document is an error, never a crash.
2. Guard the `:247` slice expression regardless, so the diagnostic survives short tokens.
3. Fuzz the agreement between `bsEscQuoted` and `QuotedToString` — same accept/reject verdict and
   the **same consumed length** — plus a `Quote` → tokenize → `String()` round trip over content
   with escapes, backticks, and multi-byte runes.
4. Independently of the tokenizer: docd's read pump should `recover()` at the session boundary. A
   malformed document should kill a session at worst, never the daemon. (Possibly a separate issue.)


Not yet minimized. The trigger is a quoted string carrying `\n` escape sequences, backticks and
em-dashes. The panic text itself was not captured in full.


Same neighbourhood, all tokenizer/streaming boundary handling:

- `hb8c72fph12krv2wcnn0` — multi-byte UTF-8 rune straddling the 8KiB refill fails the document
- `m2y1cqc5h12ks4k3cnn0` — tags truncated at a buffer refill, block-style strings and end-of-document
  multiline literals lost