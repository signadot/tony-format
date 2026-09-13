# sweep tier 3: lower-impact defects across parser, logd, docd, cli, gomap (27 findings)

Codebase sweep (2026-09-13) tier 3: lower-impact defects, all reproduced by throwaway test unless marked "by reading". None matches an open or closed issue. Tier 1 is 05d8w3cj; tier 2 is the sibling sweep issue.

core library
1. An array patch over a non-array base is returned verbatim, operator tags included. patch.go:214-217 `if doc.Type != ir.ArrayType { return patch.Clone() }`. Patch({}, {b: [!insert 5]}) = {b: [!insert 5]}; Patch({a: 1}, {a: [!delete null, 2]}) = {a: [!delete null, 2]}. Through logd this is an unstorable shape written as a value.
2. A namespaced (colon) data tag does not survive Diff -> Patch. libdiff/make.go:64 emits !insert(acme:thing); mergeop/insert.go:32 -> ir.CheckTag (ir/tags.go:299-312) rejects ':' while the tokenizer and RegisterNamespaced admit it. Patch(a, Diff(a, b)) == b is false for any document carrying !ns:name as data.
3. The parser accepts duplicate keys, and Match/Patch then misbehave silently. match.go:187-219 counts both; patch.go:559-577 applies to the first and then overwrites with the raw second. `a: 1\na: 2` matches neither {a: 1} nor {a: 2}; Patch(doc, {a: 3}) answers a: 2. ir.Get answers the first, ToMap the last, encoder writes both; {1: 1, 1: 2} keeps the last. Per "make bad shapes illegal", fix in the parser.
4. !tag reads only the head label, so a flow-written value never matches. mergeop/tag.go:52 TagArgs(doc.Tag) head, presentation not stripped. `c: !mytag {x: 1}` parses as !bracket.mytag and `c: !tag {name: mytag}` answers false; block style answers true.
5. Explaining a !ir {comment: ..} match on a line-commented node panics. explain.go:280 n.KPath() on the Node.Comment that mergeop/ir.go:197 hands back; its Parent is the scalar it annotates, so ir/kpath.go:57-58 panics "parent but not in container". Plain Match is fine.
6. arraydiff/strdiff keys are truncated to uint32. ir/node.go:219 uint32(*field.Int64). Patch([1,2,3], !arraydiff {4294967296: !insert 99}) = [99, 1, 2, 3]. Same in the parser: parse/parse.go:873 (:690-695 accepts any uint64 first); {4294967296: a, 0: b} -> !sparsearray {0: b}, an entry vanishes by collision. The stream decoder errors here; the parser does not.
7. Dangling `- ` accepted: `-\n` alone -> [] and `a: -` -> a: [] (token/balance.go:133-147) while `-\n-\n` is an error; YAML mode drops a bare - item: `- a\n-\n- c` -> [a, c] (YAML: [a, null, c]).
8. Normalization slips: formatFloat (encode/encode.go:1049-1061) writes 1e21 as 1e+21, which reparses with !exp and is rewritten 1e21, so `o v` is not idempotent on large/small floats; bracket-mode arrays under a key indent elements 4 deeper than the key.
9. tony.md:678-690 shows unbracketed `a b c` as a key set and the parser (per the prose at :556) refuses it; doc inconsistency.

logd server
10. unwatch does not canonicalize its path, so a watch opened with element sugar cannot be closed by path. session_watch.go:613 vs :34-39. With items keyed by sku: `watch {path: "items(A)"}` confirms as items."(sku=A)"; `unwatch {path: "items(A)"}` -> not_watching. Feeds g02yc3r4.
11. A watch accepts an absolute fromCommit above the head where a match refuses it. session_watch.go:117-122, 236-240 (contrast session_read.go:51-57). Head 2, `watch a fromCommit: 100` -> state stamped commit: 100, replayComplete, next patch commit: 3; delivered becomes 100 so an Ended event hands back a fictitious resume point and docd's high-water mark would jump. Should be commit_not_found.

storage (by reading)
12. compaction.go:306-309 reindexEntry does Remove then Add per segment with no lock spanning them; the root snapshot's own gap lets SnapshotAtOrAbove fall back to the previous root snapshot and fold patches already unindexed -> a stale head read or a CAS/lowering against stale state under commitMu. Microsecond window; a 20s stress run did not hit it.
13. compaction.go:157-160 with dlog/compaction.go:53-67: compactionRecords stops at a read error and says what follows "is not compacted this time", but CompactInactive(positions) rewrites the file to exactly the listed survivors; everything behind the error is deleted while still indexed. Only a transient I/O error reaches it.
14. snap_storage.go:91-97: commits that land between GetCurrentCommit() and SwitchActive() sit in the now-inactive log, newer than the snapshot, and are dropped by timestamp like any other patch when cutoff is shorter than snapshot+compaction duration. Not reachable with the 1h default.
15. dlog/snapshot_writer.go:170, 253: blob length written as uint32(blobLength) with no bound check (entries check > 0xFFFFFFFF, blobs do not); a snapshot >= 4 GiB wraps silently and the frame walk on open misreads the file.

docd
16. Addendum to jk3s11hx: a watch refused at establishment (not_found, unsupported, invalid_path) also keeps its reader token. watch.go:84-100, client_session.go:351-366. `Watch("nothing.here")` -> not_found; a mount at nothing.here.x then stalls forceAfter. Same root cause, far more common trigger; fold into jk3s11hx.
17. Test-only race: `go test -race ./system/libctl` fails in TestEveryDocdSessionSpeaksTheProtocol (docd_hello_test.go:70) reading its bytes.Buffer slog sink while a logd handleConnection goroutine still logs into it.

stream / eval
18. stream/decoder.go:61 a comment between a tag and its value drops the tag; conversion.go:289-296 a line comment after `key:` lands on the previous sibling; WriteFloat(±Inf/NaN) writes +Inf, read back as a string.
19. eval/expand_env.go:189-215 ExpandIR drops line comments on objects/arrays; :409-424 $[x] interpolates non-scalars as tony block text where build-eval.md says JSON.

o CLI / dirbuild
20. `o patch '' f` (cmd/o/patch.go:40,67, nil patch node dies at patch.go:80) and `o diff` with an empty side (cmd/o/diff.go:54-62, nil node into DiffWith, SIGSEGV at diff.go:397) panic instead of erroring. Realistic: `o patch "$UNSET" f.tony`; `cmd | o diff base.tony` when cmd printed nothing (loop mode guards this, two-operand mode does not). `-loopEvery 0s` panics in NewTicker (diff.go:89).
21. cmd/o/list.go:129-143: `o -j get .a *.json` over several files emits `# from <file>` lines into JSON that neither `o -j` nor jq can read back.
22. dirbuild/fetch.go:92,131: exec:/url: sources are killed at a hard-coded 10s and the error says only "signal: killed". dirbuild/filenamey.go:90: !filename(../x) writes outside destDir. `o view -w f.json` rewrites the file in tony syntax.

gomap / codegen
23. gomap/to.go:217 and codegen/generator.go:986,1284,619: uint64 above MaxInt64 encodes negative (MaxUint64 -> -1) and then refuses to decode. Adjacent to 29xhvsd1.
24. codegen/generator.go:2108-2111,2183: generated float decode refuses an integer literal (f: 1 -> "expected number, got Number"); reflection accepts it, so the two codecs disagree on one document.
25. codegen/generator.go:2129-2155: a named local int scalar field (type Level int) decodes to int, not Level; non-compiling. type_resolver.go:177-181: a named slice with codec=custom is inlined rather than dispatched (f69agjye item 2, slice form); non-compiling. parser.go:126-140 + generator.go:1804: import-path base != package name (lib-go -> lib, yaml.v3, /v2) -> unresolved or &lib-go.T{}. parser.go: a generic type with a directive is emitted as func (s *Box); should be refused.
26. gomap/from.go:50,58: var p *T; FromTonyIR(node, &p) where T has a generated codec calls it on nil and panics (reflection path allocates). from.go:584,605: head comment at depth >= 2 into any/map[string]any errors "unsupported IR type Comment"; from.go:126: head-commented *time.Time field fails (deferral rule ignores TextUnmarshaler).

housekeeping
27. data/logA, data/logB, data/meta/seq (an empty logd data directory) are tracked at the module root since 12df0878.