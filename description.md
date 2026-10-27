# token: a streaming tokenizer keeps every newline offset it has seen — a long-lived stream decoder grows 8 bytes per line forever

In streaming mode, the tokenizer appends the offset of every newline it reads to its `PosDoc.n` (`token/pos.go:25`, called from `token/tokenizer.go:316` and `token/mstring.go`). Nothing ever trims it: `NewTokenSource` creates one empty `PosDoc` (`token/tokenizer.go:69`), and the tokenizer keeps it for the life of the source. A `stream.Decoder` owns one source per reader, so a decoder on a connection that lives for days holds 8 bytes for every line that ever crossed it.

Both ends of a logd session are such decoders:

- `system/libctl/logd.go:241` builds one decoder per connection, and `LogdSession.readPump` (`:902`) reads every document through it;
- `system/docd/server/client_session.go`, `pumpLogdToClient`, does the same on docd's side of each client session.

Wire documents are one line each (`client_session.go:651`, `:712`), so this is 8 bytes per message.

## Repro (go-tony v0.0.245)

Read one-line bracketed documents through a single decoder, and measure the heap after a GC every 2M documents:

```go
dec, _ := stream.NewDecoder(src, stream.WithBrackets()) // src yields "{a: 1}\n" 8M times
for {
	if _, err := stream.ReadDocument(dec); err == io.EOF {
		break
	}
	// every 2M: runtime.GC(); runtime.ReadMemStats(&m); print m.HeapAlloc - base
}
```

```
2000000 documents: heap +15 MB (8.3 B/document)
4000000 documents: heap +30 MB (8.1 B/document)
6000000 documents: heap +48 MB (8.4 B/document)
8000000 documents: heap +75 MB (9.9 B/document)
```

Nothing is retained but the decoder. The rise above 8 B/document is the slice's spare capacity after a growth.

## In production

On verse's staging, the logd session in versed held 875 MB in `token.(*PosDoc).nl` after 14h45m, 87% of its in-use heap, at about 2,000 lines/s. docd held 925 MB there on its side of the same traffic. The growth runs at about 1.4 GB/day. Each time the slice grows, `append` allocates the new array while the old one is still live and copies it, so at 3 GiB a single growth asks for about 3.75 GiB more within seconds. On staging's swapless 8 GiB nodes, that put the node into a reclaim livelock four times in four days (Linear ENG-1321). Parsed nodes keep no pointer to the table (`ir.Node` has no `Pos`), so a session reset frees it all at once: versed fell from 6.1 GiB to 345 MiB without restarting when its session reset.

## What a fix has to keep

A `Pos` holds `*PosDoc` and works out its line and column only when asked (`Pos.LineCol`, `Pos.String`). Error values keep their `Pos`. So trimming one shared table in place would make every older `Pos` that is still held report the wrong line.

One shape that keeps them right: in streaming mode, each document gets its own `PosDoc`, which carries the absolute offset at which its document starts. Line numbers then count from the document's start, which is the only line number that means anything in a stream of documents. A `Pos` still held keeps its own document's table, and the decoder holds at most the current one.

A regression test: a decoder that reads N one-line documents holds a line table no larger than one document's.

Context: found while looking at ENG-1321 (versed and docd memory on verse staging). The heap and goroutine profiles are attached there.