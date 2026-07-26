# token streaming: tags truncated at a buffer refill, block-style strings and end-of-document multiline literals lost

`token.TokenSource` grows its buffer when a scan reports "I ran to the end, give me
more" — that is how integers, literals, quoted strings and multiline literals survive
a refill landing in the middle of them. Three constructs do not participate in that
mechanism correctly. All three reproduce in **pure ASCII** at the **default 4096-byte
buffer**, so they are distinct from the multi-byte-rune straddle (hb8c72fph12k) —
that one is fixed; these are not.

Reproduction is attached (`streaming_boundary_bugs_test.go`): drop it into
`go-tony/token/` and run `go test ./token/ -run TestStreamingBoundaryBug -v`.
Everything reaches production through `stream.Decoder` (docd/logd sessions) and
`parse.ParseNodeFromSource`.

---

## Bug 1 — a tag straddling a refill is silently truncated

`TokenizeOne`'s `'!'` case (`token/tokenizer.go:368`) scans forward to whitespace and,
if it reaches the end of the buffer, **emits the tag anyway**. There is no
"ran to the buffer end, need more data" exit, so the tag is cut at the boundary and
the remainder is re-tokenized as a separate literal.

```
doc = "a: " + strings.Repeat("x", pad) + "\nb: !mytag 1\n"   // buffer 4096
pad=4084   streaming tag "!myta"   whole-buffer "!mytag"
pad=4085   streaming tag "!myt"    whole-buffer "!mytag"
pad=4086   streaming tag "!my"     whole-buffer "!mytag"
pad=4087   streaming tag "!m"      whole-buffer "!mytag"
pad=4088   streaming: unexpected end at offset 8191          // '!' is the last byte
```

This is the most severe of the three: for four of five boundary positions it is
**silent corruption** — a valid document decodes to a different document, with no
error anywhere. The fifth fails hard. Tags carry type information in the wire
protocol, so a corrupted tag is a wrong value, not a parse failure.

## Bug 2 — a block-style quoted string fails in any document larger than the buffer

A quoted string at line start goes through `mString` (the multiline-string path).
`mString` (`token/mstring.go:14`) rewrites every error from `mStringOne`:

```go
toks, off, err := mStringOne(d[i:], start+i, indent, posDoc)
if err != nil {
    return nil, 0, NewTokenizeErr(ErrMultilineString, posDoc.Pos(i))
}
```

That discards the `ErrUnterminated` that `tokenizer.go:342` tests for
(`errors.Is(err, ErrUnterminated)` → `io.EOF` → refill). The refill is never
requested, so the document fails:

```
doc = "a: " + strings.Repeat("x", pad) + "\n\"a string value\"\n"   // buffer 4096
every pad in 4080..4094: streaming: multiline string at offset 0   (whole-buffer: ok)
```

Note it fails at *every* pad once the document exceeds the buffer, not only at the
boundary — the string does not have to straddle anything. `"a string value"` alone,
or at the head of a large document, is fine.

Two smaller defects in the same three lines: the position is `posDoc.Pos(i)` where `i`
is an offset *relative to the mString slice* used as an absolute document offset
(hence "offset 0" above), and the underlying cause is unrecoverable from the error.

## Bug 3 — a multiline literal at the end of the document is dropped entirely

Not a boundary bug. `a: |\n  one\n  two\n` loses its `TMLit` — and everything after
it — at **every** buffer size, including one large enough to hold the whole document:

```
buf=4        got 0 TMLit, want 1   (2 tokens vs 4)
buf=4096     got 0 TMLit, want 1
buf=1048576  got 0 TMLit, want 1
```

The same literal with any content after it (`a: |\n  one\nb: 1\n`) is fine at every
size. Traced:

```
read 0: eof=false bufPos=1  len(buf)=11  TLiteral(a)
read 1: eof=false bufPos=2  len(buf)=11  TColon(:)
read 2: eof=true  bufPos=3  len(buf)=11  <no tokens>  err=EOF
```

At read 2 the whole document is already in the buffer and `bufPos` sits on the `|`.
`scanLinesStreaming` correctly reports "the literal runs to the end of my input, give
me more" (`mlit_streaming.go:57`); `handleTokenizeEOF` → `ensureTrailingNewline`
(`source.go:207`) finds the buffer already ends with `\n` and returns
`(false, io.EOF)`, so `Read` gives up and the token is lost silently.

### Root cause behind bug 3, and why it will keep recurring

`TokenSource` has a "need more data" signal but **no "no more data is coming" signal**.
`Tokenizer` chooses the terminal or the streaming scanner by `t.reader != nil`
(`tokenizer.go:411` for `mLit`/`mLitStreaming`, and similarly in
`getSingleLiteralStreaming`, `bsEscQuoted`'s `ErrUnterminated` handling, and now the
partial-rune paths). That predicate is fixed for the life of the tokenizer, so after
the reader is drained the tokenizer keeps using the "it might continue" scanners and
keeps asking for bytes that will never arrive. Any construct whose terminator is
"end of input" rather than an explicit delimiter is exposed.

---

## Options

All three were prototyped on top of the current tree and the **full test suite passes
with all three applied** (`go test -count=1 ./...`), including the attached
reproduction. Nothing else in the suite depends on the current behaviour.

### A. Give `Tokenizer` a terminal state (fixes bug 3) — recommended

Add `atEOF bool` to `Tokenizer`, set by `TokenSource` once the reader is drained, and
make the trailing-newline pass always re-enter tokenization instead of bailing out:

```go
// source.go ensureTrailingNewline
if ts.trailingNL {
    return false, io.EOF        // final pass already made
}
ts.trailingNL = true
ts.tokenizer.atEOF = true       // no more data is coming
if len(ts.buf) == 0 || ts.buf[len(ts.buf)-1] != '\n' {
    ts.buf = append(ts.buf, '\n')
}
return true, nil                // retry in terminal mode (was: (false, io.EOF))
```

and select the terminal scanner on `t.reader != nil && !t.atEOF`. Termination is still
guaranteed: if a scan returns `io.EOF` again, `trailingNL` is already set and `Read`
returns `io.EOF` as before.

Verified: fixes `mlit-eod` at every buffer size; whole suite green. ~6 lines.

This is the piece the design is missing, and it is a prerequisite for doing bug 1
properly (see B) — without it, a tag that legitimately ends at the end of the document
would ask for data forever.

### B. Give the tag scan the "need more data" exit (fixes bug 1)

In the `'!'` case, before deciding the tag is complete:

```go
if t.reader != nil && !t.atEOF && start == n {
    return nil, 0, io.EOF   // tag runs to the buffer end; it may continue
}
```

~4 lines, depends on A. Verified against the attached reproduction.

### C. Stop `mString` swallowing the inner error (fixes bug 2)

```go
return nil, 0, NewTokenizeErr(fmt.Errorf("%w: %w", ErrMultilineString, err), posDoc.Pos(start+i))
```

The existing `errors.Is(err, ErrUnterminated)` check at `tokenizer.go:342` then fires
and the refill happens. One line (plus the `start+i` position fix). Verified: all
block-string cases pass, whole suite green. Independent of A and B.

### D. Structural: one scanner set with an explicit need-more-data result

The duplication that produced all of this — `mLit`/`mLitStreaming`,
`scanLines`/`scanLinesStreaming`, `getSingleLiteral`/`getSingleLiteralStreaming`, plus
`t.reader != nil` tests scattered through `TokenizeOne`, plus `ErrUnterminated`
doubling as "refill" — could collapse into a single scanner set that returns
`ErrNeedMore` whenever it reaches the end of its input, with exactly one place
(`TokenSource`, which knows whether the reader is drained) deciding whether that means
"grow the buffer" or "terminate here". That removes the bug class rather than three
instances of it, and would have prevented hb8c72fph12k as well.

Bigger diff and it touches the non-streaming path, so it is not a same-day change.
Suggested order: **A + B + C now** (≈12 lines, all verified), **D** when the
duplication next causes a bug.

### Rejected: hold back the incomplete tail in the reader

Never handing the tokenizer a buffer that ends mid-construct requires knowing where
constructs end, which is the tokenizer's job. It works for the rune case only (that is
effectively what the `utf8.FullRune` check in hb8c72fph12k does) and does not
generalize to tags, strings or literals.

---

## Also noticed, not filed separately

* `TokenizeOne`'s `'!'` case reports the empty-tag error at
  `t.posDoc.Pos(int(absOffset)+start)` — `absOffset` already includes `pos`, so `pos`
  is counted twice. Should be `bufferStartOffset+start` like the two `bad utf8` sites
  a few lines above.
* An unterminated quoted string at true EOF is reported as `unicode control` (the
  synthetic trailing newline is what the scanner trips over) rather than
  `unterminated`. It does fail, so this is a message-quality issue only.
* YAML streaming does not participate in the refill mechanism at all:
  `tokenizer.go:322` never converts `ErrUnterminated` to `io.EOF` for
  `YAMLQuotedString`, and `yamlPlain` (`yaml_plain.go`) has no need-more-data path.
  Long YAML documents read through `TokenSource` are presumably broken in the same
  ways; not investigated.