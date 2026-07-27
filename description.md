# dlog: records have no checksum, so frame misalignment is undetectable and surfaces as a tokenizer error far from the cause

A dlog record is framed as a 4-byte big-endian length prefix followed by that many payload bytes
(or `BlobHeaderMagic` + 4-byte blob length + blob data). There is nothing that distinguishes a real
frame from arbitrary bytes that happen to parse as one, and nothing that detects a payload whose
contents were damaged after the length was written.

This matters in two places.


`scanFrames` (`system/logd/storage/internal/dlog/dlog.go`, added for `pb1aj0sqh12ksp38cxn0`) walks
the framing on open and truncates anything past the last complete frame. It accepts a frame if the
frame lies entirely within the file — that catches both tear modes (a partial prefix cannot be
read; a prefix whose payload was cut short runs past EOF), but it cannot reject a boundary that
lands on plausible-looking garbage. If misalignment ever occurs, the scan will happily walk the
wrong chain to the end of the file and report it clean.

A checksum makes the scan decisive: the first frame that fails is the truncation point, full stop.


The failure that motivated this was reported as:

```
unicode control at `...ctri\x00\x00-\xd9!e...` at offset 470 (line=16, col=82)
```

That is the tokenizer's complaint about a NUL, 470 bytes into what the reader believed was one
entry. The actual defect was a frame boundary at the wrong offset. Nothing between those two facts
was visible, which is most of why the diagnosis took as long as it did — the first hypothesis was
log corruption, the second was a tokenizer bug, and the truth was neither.

With a per-record checksum the reader fails at the frame, with the position and the expected vs.
actual digest, before the payload ever reaches the tokenizer.


Add a CRC32C (Castagnoli — hardware-accelerated on amd64 and arm64 via `hash/crc32`) over the
record, and verify it in `ReadEntryAt`, `singleFileIter.next`, and `scanFrames`.

The framing change needs a migration path, since existing logs have no checksum field and the
length prefix is positionally load-bearing. Options worth weighing:

- **A record-format version in the log header/state.** `dlog.state` already carries the active log
  and generation counters and is written durably (`dlog.go:180-204`), so it is a natural place for
  a format version. Readers honour v0 (no checksum) and v1 (checksum) per file; writers emit v1.
  Existing files stay v0 until compaction rewrites them — compaction already rebuilds the file, so
  it is the natural upgrade point.
- **A distinct magic for checksummed records**, alongside `BlobHeaderMagic`. Avoids a version
  field but spends more of the length space and makes the reader messier.

I'd lean to the first. Worth confirming whether any deployed log needs to survive the transition,
or whether compact-and-rewrite is acceptable.


This does not address durability — records are still acked before fsync
(`hyda9h82h12krp8mcdn0`). A checksum tells you the tail is *damaged*; it does not stop you from
losing an acked record. Separate concerns, both open.