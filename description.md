# sweep tier 1: data loss, server crashes, store corruption in ordinary use (13 findings)

Codebase sweep (2026-09-13) tier 1: defects that lose data, crash a server, or corrupt a store in ordinary use. All reproduced by throwaway test. None matches an open or closed issue.

1. logd: a watch on a path that holds nothing crashes the process; recurs on resume.
   system/logd/server/session_watch.go:383 runs deltaAt(prev, next) before the SameState gate; :418-423 deltaAt(nil, nil) -> libdiff.MakeDiff(nil, nil) -> libdiff/make.go:50 escaped(nil) nil deref, unrecovered on the forwardEvents goroutine.
   Repro: (a) `watch a.b waitIfAbsent`, then `patch a: 5` or `patch "" {a: !insert {x: 1}}` -> SIGSEGV. (b) `watch a.b` on an existing value, `patch "" {a: !insert {x: 1}}` (client gets !delete), then `patch "" {a: !insert {y: 1}}` -> SIGSEGV. (c) `watch a.b fromCommit: 2` over history {a:{b:..}}, {a: !insert{x}}, {a: !insert{y}} -> panics in replay before the state event.
   Docd forwards such watches unchanged, so a controller waiting on a peer's subtree crash-loops the server. Guard: prev == nil && next == nil is SameState; return before diffing.

2. encode: bracketed arrays lose elements on a view/rewrite -- adjacent quoted strings fold into one.
   encode/encode.go:187-197 writeCommaSeparator writes no comma in text tony unless the element is an mstring; token/mstring.go folding then joins quoted strings at line start inside brackets (tony.md, multiline folding).
   Repro: `["a b", "c d"]` | o v -> `[\n  "a b"\n  "c d"\n]` -> reparse -> one element "a bc d". `["hello world", "foo bar", "baz"]` -> `["hello worldfoo bar", "baz"]`. The parser puts !bracket on every bracketed input, so `o v -w` on any bracketed file with two adjacent quote-needing strings destroys data.

3. storage: DeleteScope is undone by compaction's re-index.
   storage.go:663-670 DeleteScope; compaction.go:120-129, 301-311 reindexEntry; index/index.go:140-163 Add. reindexEntry calls index.Add for every survivor with no check that the scope still exists; Add at the root re-states the footprint.
   Repro: baseline {a:{x:1}}; scope s1 writes a.x=2; DeleteScope("s1") (scoped read -> 1); SwitchDLog + Compact with the scope's entry inside the cutoff -> scope entries in index: 1; scoped read a.x = 2. The resurrected statement is live again so dominatedScopeEntries never drops it; permanent until a second DeleteScope. Hits any store with compaction on where a scope is deleted within cutoff (1h) of its last write; a new scope reusing the id inherits the old claims.

4. storage: DeleteScope is not recorded in the log, so a rebuild or unclean restart brings the scope back.
   storage.go:663-670, index/build.go:103-113, index/regions_file.go:204-268. Build indexes every scope entry in the log unconditionally; manifests persist every 1000 commits.
   Repro: same writes, DeleteScope, then (a) reopen without Close -> scoped a.x = 2, entries 1; (b) reopen with manifest removed (as the format-4->5 bump did on every store) -> same. Same root cause as 3 (no tombstone in the log); 3 is the trigger that needs no failure.

5. storage: an async index persist during compaction writes a manifest the next open trusts over stale segments; the store is dead until index.manifest is deleted by hand.
   compaction.go:109-129 (swap, then re-index loop), index_persist.go:65 Persist(p.generations()), index/regions_file.go:235-239 (manifest trusted when generations match). The persister can sample the generation after CompactInactive bumped it but before the re-index loop finishes.
   Repro (Compact's own order with the persist inserted): 20 writes, SwitchDLog, unindexEntry one patch, CompactInactive, index.Persist(logGenerations()), re-index, reopen without Close -> a = nil, err = failed to read snapshot entry: read interrupted by compaction. Every read fails, so every write's verify read fails, nothing commits, nothing repairs it. A clean Close masks it; a crash before the next persist exposes it. Window is the whole re-index loop under commit load (SwitchDLog runs on its own goroutine, server.go:213-228).

6. libdiff/mergeop: a commented element inserted into a positional array is applied as a replacement and the element after it is lost.
   libdiff/make.go:50-56 escaped puts !insert on the value inside the comment wrapper; mergeop/arraydiff.go:109-110 SplitChild(op) on the wrapper finds no op, so the default branch runs pf(docVals[fi], op) and consumes a document element.
   Repro: from=[a, b], to=[a, "# c\n c", b]; d := DiffWith(from, to, DiffComments(true)) -> `!arraydiff {1: <comment>!insert c}`; Patch(from, d, Comments(true)) = [a, c]. b is gone, no error. logd's watch delta path (session_watch.go:422, DiffComments(true) without DiffAbsolute) produces exactly this shape, so a client stepping by the delta drops an element. !delete of a commented element works only by accident through the same branch.

7. docd: an ancestor patch whose content lies in one mount is routed to logd base, bypassing the controller and tombstones.
   system/docd/server/client_session.go:384-386 -> :279 / :534; split.go:152. splitPatch finds one part (the mount's) and maybeCoordinatePatch routes "normally", but normal routing uses the client's path, which no mount is a prefix of.
   Repro: controller mounts org.users; client Patch("org", {users: {alice: {v:1}}}) -> commit 1 in logd base at org.users; controller store empty; Match("org.users") through docd answers null. Same write over a tombstoned org.users is accepted into base (split uses mountInfos liveOnly=true); a split {users: .., audit: ..} with users tombstoned and audit live commits the users half into base. Any client writing above a mount (whole-document writes, `o patch` of a doc) hits it; a non-logd controller loses the write silently.

8. logd: a root-level total write is never delivered to watchers of the paths it removed.
   storage/commit_ops.go:185-219 extractTopLevelKPaths names only fields the stored patch contains; server/watch.go:241 matchesPath finds no watcher; :263-265 the separator check defeats HasPrefix(watchPath, "").
   Repro: {a: 1, b: 2} stored, `watch a`: `patch "" !insert {}`, `patch "" !insert {c: 1}`, `patch "" !delete null`, `patch "" 7` all commit, a read of a is not_found afterwards, the watcher received nothing after its initial state, and resume replays nothing. Violates api/doc.go "every event after it is the delta of one commit ... with no gaps". Fix in notification: a patch whose root is an operator or non-object fans out as a root-touching commit.

9. o: `-o <file>` naming an input file truncates it before it is read.
   cmd/o/o.go:95 outOpt opens the output O_CREATE|O_TRUNC at option-parse time.
   Repro: `o -o in.tony view in.tony` -> exit 0, no message, in.tony is 0 bytes.

10. token: YAML double-quoted escapes panic the process.
   token/yaml_quoted.go:146-181: \u and \U assemble the rune from dst (the output buffer) instead of tmp, never advance i past the hex digits, and index past dst; \x calls hex.Encode (not Decode) into a 1-byte buffer; \N and \L are literal panic() calls.
   Repro: `a: "é"` with -I y -> panic: index out of range [1] with length 1; "\x41" and "\N" likewise; "xé\ty" -> "x≸00e9\ty".

11. token: a block literal that is the value of a key on a `- ` line swallows its sibling keys.
   token/tokenizer.go:537-545 mIndent = lnIndent + 2*bElt ignores the key, so `- k: |` expects content at column 2 while the encoder, YAML, and the spec put it at 4. yamlPlainRun (token/yaml_plain.go:32-35) already adds 2 for kvSep; the | arm needs the same.
   Repro: `- k: |\n    a\n  j: 1\n` -> [{k: "  a\nj: 1\n"}], j is gone. Encoder output for [{k:"a\nb", j:1}] is exactly that shape, so it does not roundtrip and compounds per pass. YAML: `steps:\n- run: |\n    make\n  env: x` -> run: "  make\nenv: x\n" (the GitHub Actions shape).

12. git-issue: concurrent edits of one issue are silently lost.
   cmd/git-issue/issuelib/git_store.go:309 (also :116, :377, :610): `git update-ref <ref> <new>` with no old-value guard after a read-modify-write of the tree.
   Repro in a scratch repo: 8 parallel `git issue comment <id> ...` -> all 8 print "Added comment", exit 0; show has 1 comment, the ref chain has 1 new commit. Fix: pass the old SHA (update-ref ref new old) and retry the read-modify-write on rejection.

13. docd: a composed watch over nested mounts delivers a commit once per mount stream.
   system/docd/server/watch.go:243-247 forwardFrom untrimmed vs :193-216 trimOwned applied only to the composed path's own stream. hs9fge9r fixed the base stream only.
   Repro: logd-backed watchable controllers at a.b and a.b.c; client watches a; Patch("a.b.c.x", ..) -> 2 deltas for commit 1. An operator delta (!arraydiff/!insert) applied twice corrupts the client's copy.