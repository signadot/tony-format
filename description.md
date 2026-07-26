# stream.Decoder: a multi-byte UTF-8 rune straddling the 8KiB buffer refill fails the document (kills logd/docd sessions)

`stream.Decoder` corrupts a multi-byte UTF-8 rune that straddles its 8 KiB buffer refill,
failing the document with `bad utf8`. Any document over ~8 KiB containing non-ASCII text is
affected; pure-ASCII documents of any size are fine.

Because the session protocols (logd, docd mount/controller) read through `stream.Decoder`, the
consequence is not a bad parse — it is a **killed session**. docd logs `session error` and drops
the connection, the controller's mount is tombstoned, and everything served through that mount
starts failing.

## Minimal reproduction

No verse, no sockets, no concurrency — a `bytes.Reader` is enough:

```go
func roundTrip(body string) error {
    doc := ir.FromKeyVals([]ir.KeyVal{{Key: ir.FromString("content"), Val: ir.FromString(body)}})
    var buf bytes.Buffer
    if err := encode.Encode(doc, &buf, encode.EncodeFormat(format.TonyFormat),
        encode.EncodeWire(true), encode.EncodeBrackets(true)); err != nil {
        return err
    }
    dec, err := stream.NewDecoder(bytes.NewReader(buf.Bytes()), stream.WithWire())
    if err != nil {
        return err
    }
    for {
        _, err := dec.ReadEvent()
        if err == io.EOF { return nil }
        if err != nil { return err }
        if dec.Depth() == 0 { return nil }
    }
}

body := strings.Repeat("em-dash — arrow → check ✓\n", reps)
```

| body bytes | result |
|---|---|
| 6400 | ok |
| 7040 | ok |
| 7680 | ok |
| **8000** | **`bad utf8 at `...?...` at offset 10 (line=0, col=10)`** |
| 8192 | fail |
| 9600 | fail |

The same sweep with the same text in pure ASCII (`em-dash - arrow > check x`) passes at **104,000
bytes**. So it is not size, and not the encoder — it is size *interacting with multi-byte runes*,
with the boundary landing on 8192 once the `{content: "…"}` framing is counted.

Note the reported offset is **10** — the position where the string token *started*, not where
the corruption is. That is worth fixing alongside: the error points thousands of bytes away from
the actual bad byte, which is most of why this took a while to find.

The tokenizer raises it at `token/tokenizer.go:376` and `:626`, both of which compute the
position as `bufferStartOffset + start` — the refill boundary is right there in the error path.

## How it surfaces

`verse connect <dir>` federates a source tree and serves file contents on demand through a
mounted controller. Any source file over 8 KiB containing an em-dash, an arrow, a check mark, an
accented name in a comment — ordinary things in real code — kills the mount session:

```
verse: connect: content controller: read from docd: read tcp ...: read: connection reset by peer
docd:  ERROR "session error" session=docd-1 error="read error: bad utf8 at `...?...` at offset 131"
docd:  INFO  "controller disconnected (mount tombstoned)" controller=content-xxx
```

After that every read through the mount fails, and (in verse's case) surfaces to an agent as
"file not found" — so the visible symptom is an agent being told a file does not exist, several
layers away from a UTF-8 bug in a stream buffer.

Isolated by A/B on two generated trees of 37 files each, identical in shape:

| tree | reads | `bad utf8` in docd | controller reset |
|---|---|---|---|
| ASCII only, 444 KB | 8/8 ok | 0 | 0 |
| same text with `—`/`→`, 1.0 MB | 0/8 ok (all 404) | 1 | 1 |
| multi-byte but small (34 B/file) | 8/8 ok | 0 | 0 |

Present in v0.0.87 and v0.0.88 alike — not a recent regression, just one that needs a document
big enough and a non-ASCII byte in the right place.

## Suggested fix

Preserve the partial rune across a refill: when the buffer ends mid-sequence, carry the trailing
bytes into the next fill rather than decoding them in place. `utf8.DecodeRune` returning
`RuneError` with `size == 1` at the buffer tail is the signal — it needs to mean "refill and
retry", not "invalid input", unless the reader is genuinely at EOF.

And separately: report the offset of the offending byte rather than the token start.