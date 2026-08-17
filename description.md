# logd: a quoted path segment in a snapshot panics every narrow read near it

Staging, v0.0.163, logd inside `o sys up`: a read of `verse.github.run.signadot`
brought the process down.

    panic: verse.github.pr."signadot/signadot#7349".checks
        -> verse.github.pr.signadot/signadot#7349.checks
        != verse.github.pr."signadot/signadot#7349".checks
      stream.KPathState  stream/kpath_state.go:73
      snap.NewPathFinder internal/snap/path_finder.go:37
      storage.findSubtreeBaseReader read_subtree.go:220

The path in the panic is not the path being read. A narrow read seeks to the snapshot
index entry at or before what it wants and builds its stream state from THAT entry's
path, so any store holding one path whose field name needs quoting killed reads which
never mentioned it. A github mirror writes `signadot/signadot#7349` as a field name;
one such entity poisons the whole store for narrow reads.

The bug is the check, not the path. `stream.KPathState` verifies that the state it
built lands where the kpath says, and it built the expected path by pasting bare field
names after dots while `State.CurrentPath()` renders segments properly -- so the two
disagreed for every field name that needs quoting, and it panicked on paths which were
correct. Same family as the dotted-id write corruption fixed earlier
(r05ms7nch12ksxttgdn), one layer down: a field name is not always its own segment.

Fixed by building the expected path with `kpath.ChildField`, and by returning an error
rather than panicking -- a state which lands in the wrong place means a seek would read
the wrong events, which is worth refusing a read over and is not worth taking the
process down with every other client on it.

Reachable only since narrow reads were wired (ap8ddvp2h12krd43gdn0); the panic
predates them, unreached.

Regressions: `stream.TestKPathStateKeepsQuotedFieldNames` over the names that need
quoting, and `storage.TestNarrowReadOverQuotedSnapshotPaths`, which builds a snapshot
of 200 `verse.github.pr."signadot/signadot#70NN"` entities and reads
`verse.github.run.signadot` across it -- it panics with the same message on the old
code.