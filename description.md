# go-tony: small defects the docs sweep found -- latent library bugs, dead surface, wrong comments and help text

Small defects the go-tony docs sweep (3d7t1khxh12krsddmdn0) found in the code and left
alone: latent (no production caller), cosmetic, dead, or a comment that is wrong. Each
was confirmed at 546bd53 unless marked SUSPECTED. The larger findings have their own
issues.

## Library bugs with no production caller

- [ ] stream: EncodeNode and DecodeNode are TODO stubs that report success
      (conversion.go:303-320); nothing calls them.
- [ ] stream: KPathState panics on a wildcard path (a.*, a[*], a{*}: kpath_state.go:41,
      :51, :71) and silently drops (key) and `..` segments. Its one production caller,
      snap.NewPathFinder, gets its path from a snapshot index entry, so a client cannot
      reach it.
- [ ] kpath: the package-level Compare(a, b string) answers 0 for every pair of valid
      paths -- the check at kpath.go:929 is inverted; no callers.
- [ ] kpath: (*KPath).Compare treats any two keyed segments as equal (a(x) vs a(y)), and
      `..` against a key. Storage paths never hold keys (ident.CanonicalPath), so nothing
      in production collapses.
- [ ] kpath: SplitAll("a..b") gives ["a", "", "b"]; RSplit("a..b.c") gives ("a.b", "c"),
      because copyKPathSegment (kpath.go:486) does not copy Descend. (docd's fieldPrefix
      reads the "" as an index; logd refuses `..` first.)
- [ ] kpath: SegmentString writes a key unquoted (segment.go:196): key x)y gives (x)y),
      which does not parse back.
- [ ] ir: Comment(n, c) on a node that is already a comment appends c to its lines without
      "# " and wraps it again -- a comment inside a comment, text duplicated
      (ir/node.go:281-284). The only caller (o build) passes a fresh object.
- [ ] gomap: FromString[T](s, v) decodes into a new value and ignores v (from.go:13-20).
- [ ] schema: without MergeBaseDefinitions a reference to a base type passes ParseSchema
      (formula_builder.go:301, :315 treat the names as known) and then constrains nothing:
      against .[number], `age: x` validates; .[array(string)] fails in Validate with an
      expr-lang reflect error. (o schema check merges them.)
- [ ] schema: SchemaRegistry never records a schema by URI -- schemasByURI is never
      written (schema_registry.go:19, :29, :68) -- so a URI reference never resolves.
- [ ] schema: Validate reparents its accept pattern's leaves into each expansion it
      discards (ExpandIRWithOptions without a clone). No observable effect found; it is the
      parent-tree invariant (see qzmkhfjqh12ksydsmdn0 for the Patch face).
- [ ] eval: once EvalOptions names ParameterizedDefs, the script functions are gone:
      .[whereami()] errors "reflect: call of reflect.Value.Call on zero Value".
- [ ] dirbuild: Run(nil) with no destDir panics -- a typed-nil *bufio.Writer (run.go:61)
      wrapped at write.go:99. o build passes nil only with destDir set.
- [ ] codegen: notag is ignored on an anonymous schema marker (parser.go:239 never sets
      NoTag; the //tony: directive parser does, :548). No production type combines them.
- [ ] index: IndexIterator.CommitsAt pages a cold region in under the read lock its caller
      must hold, and deadlocks (region.go:337). Tests only.

## Behaviour worth a look

- [ ] logd: lowering refuses a keyed diff whose keys differ only in type (ids 1 and "1"):
      validateKeyedArray (api/storage_context.go:187-210) renders keys with ir.ElemKey,
      for an index that no longer keys arrays by value. Reached by a relative write over an
      array carrying !key as data under !raw -- and that lowering also drops the !raw
      escape, storing a keyed-merge operation over data the client escaped (a question).
- [ ] index: SUSPECTED -- Build stopped by a read error names the last record read, which
      can be in the other log (build.go:81, :89; the walk interleaves A and B), so open
      drops readable segments of the wrong log (storage.go:281).
- [ ] libctl: SUSPECTED -- a controller Watch that fails after confirming is only logged
      (controller.go:340-343); nothing reaches docd, so the client's watch stays open and
      silent.
- [ ] git-issue: `blocks` ignores a failed reciprocal blocked_by write (relate.go:119),
      and a rerun answers "already has this relationship" (:93-96), so it cannot repair it.
- [ ] git-issue: ParseXID/ParseXIDR panic on a rune above U+00FF (xid.go:180, index 257 of
      256), and IsValidXIDRChar accepts control bytes 0x10-0x19 as digits (xid.go:42).
      No callers outside tests.
- [ ] o eval: `!toint 3.5` fails `strconv.ParseInt: parsing ""` (eval/to_int.go:54).

## Dead code and dead surface

- [ ] mergeop: OpContext's DefEnv, EvalOpts and SchemaRegistry are set (schema.go:95-96)
      and never read, and Expand/Unexpand/IsExpanding/EnsureExpanding have no callers,
      since 8d66f63: wire them or remove them. Let() is registered twice
      (register.go:136, :144).
- [ ] logd: server.CommitGetter; WatchHub.broadcastTimeout, NewWatchHubWithTimeout and
      DefaultBroadcastTimeout (set, never read -- they imply a delivery timeout that does
      not exist); api.Duration; api.Error/NewError (never built since 43f2eaf, so
      ErrorCode's *Error arm cannot match); ErrCodeInvalidWatch (never sent).
- [ ] storage: ReadWideBadPath, ReadWideAbsent and ReadWideNonFieldPath lost their only
      callers with read_subtree.go in 8dab04f; their reads.wide counters report 0 forever.
- [ ] codegen: HasToTonyMethod/HasFromTonyMethod answer false for a generated *T and are
      used only by a test that discards them; ResolveType is a stub with no callers;
      tony-codegen's determineSchemaPath has no callers.
- [ ] snap/patches: NewBuilder stores its patches argument and never reads it
      (builder.go:58); SubtreeCollector.Reset leaves preValue, took and released
      (collector.go:224-231).
- [ ] dirbuild: DirOutput.Filenames is never read (output.go:16).
- [ ] schema's built-in contexts name json-patch "jsonpatch" (builtin_contexts.go:37) and
      list the patch-only if, quote and unquote under match; nothing consults the context
      registry, so no effect. logd/api's whyNotStorable says "jsonpatch" too (only its
      refusal reason).

## Messages and help text

- [ ] tony-codegen's description says it generates ToTony/FromTony "from structs with
      schema tags" (it also writes ToTonyIR/FromTonyIR and reads //tony: directives), and
      -schema-registry says "cross-package references" (it is the last place
      ResolveSchemaPath looks for a schema= type's NAME.tony) -- main.go:37, :50.
- [ ] !script "1+1" says "script op expects no args, got []" when it requires one
      (eval/script.go:29); "ExpandAny is not for y.Y" (expand_env.go:141).
- [ ] buildinfo.shortRev's doc says it leaves a non-git revision alone; it cuts anything
      over seven characters (buildinfo.go:82-88). Fix the comment.
- [ ] docd's session.go:155 and :479 still spell forceAfter force_after.

## Comments that are wrong (inside functions, or not declaration docs)

- [ ] storage: storage.go's init body (a bad record stops the walk -- it is stepped over);
      snap_storage.go bodies (replayScopedAt, narrowSubtreeAt); the floating file comments
      in scope_compaction.go (a scoped read folds all of a scope's entries),
      path_snapshot.go (the byte budget as a refusal), lower.go (verifyApplies, "step the
      head"), raise.go ("the head is read through here"), replay_floor.go:35
      (ReadPatchesInRange); tx/unsafe_write.go ("the stepped head"); index build.go and
      ListRange (ReadPatchesInRange, LookupRange), region.go (admit); snap/snap.go:45 (a
      newline separator that is not there); patches/processor.go (InMemoryApplier ~l.161,
      nestUnder ~l.795, graftInto ~l.950); dlog/snapshot.go is an empty stub.
- [ ] logd server: server.go:154 (CheckHead, removed in 31d3e27); session_watch.go
      forwardEvents (ReadPatchesInRange), emitScopedDeltaFrom (storage/head.go, removed),
      handleWatch's dedup (applies only with fromCommit); match_data.go extractPathValue
      (valueAtPath, patchMayAffect); session_write.go "Strip internal tags" (nothing is
      stripped); watch.go Broadcast (the copy is in stepBaseline, not forwardEvents).
- [ ] token/tokenizer.go:795 (recentBuf, docPrefix).
- [ ] git-issue: show.go:135-138 and serve.go:370-372 say comment names do not sort by
      time (they do: <UTC ts>-<hash>); serve.go:242 says the index matches `git issue list`
      (it sorts by Updated, list by Created).

## Markdown, outside the Go docs

- [ ] docs/docd/composition.md: EndReason "membership_changed"; the code sends
      session_mounted / session_unmounted.
- [ ] docs/generated/eval.md: $USER syntax, !os_env, and !file said to parse its content
      (it answers a string).
- [ ] docs/gomap.md: gomap.ToIR / FromIR.
- [ ] cmd/tony-lsp/README.md: cites ytool/parse.
- [ ] system/logd/storage/CLAUDE.md is stale throughout.
- [ ] gomap/OPTIONS_IMPLEMENTATION_COMPLETE.md, options_implementation_plan.md and
      options_verification_summary.md are December planning artifacts; the plan lists as
      missing options that exist.