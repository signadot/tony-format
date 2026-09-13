# sweep tier 2: wrong answers, hangs, and client-crashable requests (28 findings)

Codebase sweep (2026-09-13) tier 2: wrong answers, hangs, and client-crashable requests. All reproduced by throwaway test unless marked. None matches an open or closed issue. Tier 1 is 05d8w3cj.

logd server
1. newtx with a large participants panics the request loop and kills logd. system/logd/server/session_write.go:240 -> storage/storage.go:454 make([]*tx.PatcherData, 0, participantCount); only < 1 is checked (:222). {newtx: {participants: 1125899906842624}} -> makeslice: cap out of range, unrecovered in handleConnection. 10^9 allocates 8 GB per request. Cap it.
2. A second hello that drops the scope while a scoped watch is open nil-derefs on the next baseline commit. session_watch.go:446 reads w.s.scopeID() live while w.scoped was fixed at start; :456 BaselineDeltaInScope(*mine, ..) with mine == nil. `hello scope: s1` -> `watch a` -> `hello` -> any baseline write to a -> SIGSEGV. The reverse (gaining a scope) leaves the hub filtering by the old scope. Snapshot the scope into the watchStream, or refuse a hello that changes scope while watches are open.
3. A participant told timeout still commits. session_write.go:148-150 answers ErrCodeTimeout and returns, but the patcher registered by NewPatcher (:103) stays joined. newtx 2; p1 with timeout 100ms -> timeout; p2 joins -> commit=1; match "" shows both. A retry double-applies non-idempotent ops. Withdraw the patch, or document and answer the later commit.

stream (every logd read goes through it: session_read.go:237)
4. stream/encoder.go:397 WriteFloat uses 'g': 1.0 goes out as 1 and decodes as an int; a stored ratio: 1.0 reaches every client as an integer. Same defect encode fixed in 6fnd2hxe.
5. stream/decoder.go:265,277,296 parse ints with ParseInt(..,10): {a: 0x1f}, 0o644, 0b101 fail, and a ReadDocument error ends the logd/docd session (session.go:177, client_session.go:189).
6. stream/conversion.go:204 EventsToNode never adds !sparsearray to an int-keyed object, so a client {1: b} patched onto a stored sparse array yields {"1": b} and drops 0: a.

docd / libctl
7. A split write with a participant refused before joining is answered only when logd's tx times out (5m default). client_session.go:488-505 waits for all count results; no abort op exists. Read-only controller at org.ro; Patch("org", {ro: {..}, x: 2}) with a 4s ctx -> deadline exceeded; without a deadline the client waits 5 minutes for a refusal docd holds in milliseconds; goroutine, logd connection and tx held per attempt. Atomicity holds.
8. An empty-object part at a mount becomes a nil-data participant, which logd refuses (invalid_diff), and via 7 the whole write hangs. split.go:213 cloneOrNil keeps a part with nil data while emitBase drops empties at :166; counted at client_session.go:428. Patch("org", {users: {}, audit: {e: 2}}) over two logd-backed controllers -> 5s ctx exceeded. Direct to logd the same patch is a no-op merge.
9. An unwatch overtakes its watch while a mount is pending, leaving a zombie watch holding a reader token. client_session.go:275-277 releases a token by key and finds nothing; watch.go:52-71 the watch goroutine is still blocked in beginRead under writer priority and registers afterwards. Any client with a watch deadline under forceAfter can hit it; the next mount under that path stalls the full forceAfter.
10. libctl: a slow-consumer-failed Watch is never unwatched on the server. libctl/watch.go:304-317 deliver closes the watch locally only; :269-273 Close returns early once closed. Hub.WatcherCount() stays 1 for the session's life; through docd the reader token is kept and a mount at a.b after Close() stalled forceAfter (forever at "0").
11. A NoInit watch routed to a controller is not confirmed until its first event, so the client's Watch() hangs on a quiet path. libctl/controller.go:325-339, :366-368 send the confirmation lazily on first emit or handler return.

core library
12. !all and !dive rebuild a container without the document's tag, so a !key(name) list stops being keyed (and merge keys are dropped). mergeop/all.go:59,73; mergeop/dive.go:91,105. Patch({items: !key(name)[{name:a,v:1},{name:b,v:1}]}, {items: !all {v: 2}}) -> items tagged !bracket. Contrast keyed_list.go:140-143.
13. !key(name) written over an absent or scalar base loses its keying and inherits the scalar's tag. mergeop/keyed_list.go:143 WithTag(doc.Tag). Patch({}, {items: !key(name) [{name: a}]}) -> untagged; over items: !foo 5 -> !bracket.foo.
14. Diff re-parents its inputs' nodes into the diff and shares one of them. libdiff/make.go:30-33 FromMap over the original nodes; object.go:24-26 returns to uncloned; array_by_key.go:49-50; array_by_index.go:76. After Diff(a, b): a.a.KPath() = "a.from", b.a.KPath() = "a.to", and Match(b, {a: !get-path(root) b.x}) errors. Violates the parent-tree invariant and patch.go:23's contract.

text layer
15. UTF-16 surrogate pairs in quoted strings decode as two U+FFFD. token/quoted.go:312-325 writes each \uXXXX with WriteRune. "😀" -> "��". Python's default json.dumps writes every emoji this way; tony.md says valid JSON is valid tony.
16. A |+ block literal that ends the document gains a newline per roundtrip. encode/encode.go:80-84, 93-97 write a final "\n" after encodeBlockLit (:940-953) already wrote the keep-newline. "x\n\n" -> "x\n\n\n"; also "x \n", " \n", "a\r\n". Residual of closed pyhfz6h6.
17. A string beginning --- is written bare and reads as a document separator. token/quoted.go:27-63 NeedsQuote has no --- rule; tokenizer.go:610-620. FromString("---") -> Parse -> (nil, nil); "----" -> []; key ---: 1 -> parse error. Streaming: the pos == 0 check is buffer-relative, so a literal --- at a buffer-compaction point emits TDocSep and the stream decoder errors (session death).
18. A line comment on a nested closing bracket is written where the parser refuses it. encode/encode.go:99-125 writes trailing comment lines verbatim; TLineComment keeps leading whitespace (tokenizer.go:828-841) while head comments are TrimSpace'd (parse.go:935). `[\n  [\n    1\n  ] # c\n]` -c roundtrip -> "imbalanced document: trailing material". `o v -w` makes such a file unreadable.

schema / eval
19. A recursive definition passes ParseSchema but Validate expands eagerly -> fatal stack overflow; `o schema check` dies. schema/schema.go:93 via eval/expand_env.go:227. node: {next: .[nullable(node)]}.
20. schema/formula_builder.go:803-805: any match operator not enumerated is "unknown tag", so schemas using !glob, !regexp (in tonyschema.md's own example), !raw, !pass, !tag(x), !subtree, !let cannot load.
21. eval/to_value.go:57, eval/script.go:90: !tovalue "" or comment-only text returns Parse's (nil, nil) as a value -> nil-pointer panic at tool.go:81. eval/json_any.go:18: .[getpath("$.absent")] clones a typed-nil *ir.Node -> panic in ExpandEnv/ExpandIR.

gomap / codegen
22. Decoding into a map with a named key type panics. gomap/from.go:775 and :744 SetMapIndex(reflect.ValueOf(key), ..) with a bare string/uint32. type Key string; map[Key]int encodes fine, FromTonyIR panics "value of type string is not assignable to type Key". Fix: .Convert(keyType).
23. An embedded pointer struct is silently dropped on both encode and decode. gomap/to.go:476, from.go:879 (also tags.go:239, promoted.go:102) require Kind() == Struct. type Outer struct{ *Inner; X int } encodes X only; decoding A: 7 leaves Inner nil. encoding/json promotes through embedded pointers.
24. codegen: a field whose type is a locally declared named slice or map (no directive) resolves to int and generates non-compiling code. type_resolver.go:618 (int fallback), :397-408 (only *types.Basic underlying recovered), :317. type Labels map[string]string -> cannot use int(..) as Labels value.
25. codegen: any non-struct type carrying //tony:schemagen= fails generation, and the schema file is written before the failure. generator.go:772 (missing closing brace), :1347-1370 (references ir.IntType/ir.FloatType, which do not exist). type Count int -> "failed to format generated code"; schema_gen.tony already updated beside the stale _gen.go.
26. codegen: []float64 / map[string]float64 fields emit ir.FromFloat64, which does not exist. generator.go:1286.
27. codegen: embedded structs. flatten.go:73-78. (a) type S struct{ *Base; Name string } with Base == nil: generated ToTony() and FromTony("name: b") nil-pointer panic. (b) type T struct{ Base; ID string } where Base also has ID: duplicate case "id" in expression switch.

tony-lsp
28. Incremental sync corrupts the server's copy of the document, and formatting writes the corruption back. cmd/tony-lsp/diagnostics.go:154 (a 0:0-0:0 range is taken as full replacement), :164 (an insert at EOF is dropped), :180-194 with :161-165 (lineColToOffset returns a byte offset used as an index into []rune). Insert "# hdr\n" at 0:0 into "a: 1\nb: 2\n" -> server holds "# hdr\n"; on "é: 1\n" replace col 3-4 with 2 -> server holds "é: 12", and format returns that as a whole-line replacement. Columns are bytes, not UTF-16 units, throughout hover/semantic tokens too.