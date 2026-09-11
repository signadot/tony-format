# logd: after the format-5 index rebuild, commits spend seconds in apply while path snapshots are retaken in pairs

Observed on verse's staging store (docd-0, ~565MB of log) on the first boot of go-tony v0.0.212 over a data directory written by v0.0.211. Commits took 0.3–3s each for more than twelve minutes after boot, all of it in the apply phase, while the store regenerated path snapshots. Steady-state throughput at v0.0.212 is unchanged from v0.0.209, so this is a post-upgrade transient, not a slower commit path.

WHAT THE STORE LOGGED, in order:

    22:52:02 WARN rebuilding the index from the logs reason="manifest version 4, want 5"
    22:52:22 logd listening on [::]:7070                      (rebuild: ~20s)
    22:52:41 INFO path snapshot created path=verse.verse.gate.question commit=88643 bytes=67710
    22:52:41 WARN slow commit path="verse.verse.gate.question.\"verse.trigger.greet-pr.greet~ba147954\"" took=421ms apply=420ms append=0s index=1ms other=0s commits=183
    22:52:44 INFO path snapshot created path=verse commit=88641 bytes=3605642
    22:54:51 WARN slow commit path="verse.github.pr.\"signadot/signadot#7420\"" took=1.426s apply=1.425s append=0s index=1ms other=0s commits=373
    22:56:48 WARN slow commit path=verse.github.repo.signadot/signadot took=1.484s apply=1.483s append=0s index=1ms other=0s commits=392
    22:59:07 WARN slow commit path="verse.github.issue.\"signadot/signadot#7008\"" took=1.341s apply=1.34s append=0s index=0s other=0s commits=440
    23:02:05 INFO triggering snapshot commitsSinceSnapshot=1000
    23:02:22 INFO snapshot created commit=89461 logFile=B     (took 17.5s)
    23:02:22 INFO reads since start narrow=13285 wideRoot=0 wideOperator=0
    23:03:31 WARN slow commit path="verse.github.comment.\"signadot/signadot#7384/3862671531\"" took=3.0s ...   (still going at 23:04)

IndexFormatVersion is 4 at v0.0.209, v0.0.210 and v0.0.211 and 5 at v0.0.212 (storage/index/regions_file.go:41, "the manifest carries the schema history"), so the rebuild is expected and happened once. What follows it is the question.

THE PHASE. 58 of the first 66 slow lines are apply ≈ took, append=0, index≈0: the verify at the write's path -- "a bounded read of the value there and the fold of the write's node onto it" (write_stats.go) -- is what takes the time, on ordinary one-entity writes at leaf paths. The counter line says every read was narrow.

THE SNAPSHOTS. Path snapshots are created for the same path, at the same size, in pairs 0.3s apart, then again ~65 commits later, for as long as writes land under that path:

    22:59:37.38 path snapshot created path=verse.github.issue commit=88981 bytes=548339
    22:59:37.65 path snapshot created path=verse.github.issue commit=88983 bytes=548339
    23:00:02.63 path snapshot created path=verse.github.issue commit=89048 bytes=548339
    23:00:02.92 path snapshot created path=verse.github.issue commit=89049 bytes=548339
    23:00:26.31 ... commit=89119   23:00:26.72 ... commit=89120   23:00:48.49 ... 89185   23:00:48.73 ... 89186
    23:02:39.28 path snapshot created path=verse.github.comment commit=89530 bytes=1041086
    23:02:39.58 path snapshot created path=verse.github.comment commit=89531 bytes=1041086
    (then 89597/89598, 89678/89679, 89757/89758 ...)

My reading, offered as a reading: a rebuilt index does not carry the path snapshots the old one had, so after the format bump every apply under a large path reads cold until that path is snapshotted again -- and the snapshot is retaken every few dozen commits, twice each time. The writes are verse's sources reconciling after the restart (github issues, PRs, runs, comments; git refs; git-issue), i.e. the post-restart burst is exactly when this costs the most.

STEADY STATE IS UNCHANGED. verse's entity.TestALongCatchUpCompletes -- 1500 one-entity writes through docd, then a 1500-commit replay -- on one verse tree with only the pin varying: v0.0.209 13.2s, v0.0.210 13.4s, v0.0.211 15.1s, v0.0.212 13.4s. There is no per-commit regression in the release.

Two things look worth a look: the cold period after a format bump (twelve-plus minutes of 0.3–3s commits on this data), and the identical path snapshot rewritten in pairs every ~20s under write load. Full docd-0 log for the window is available from the verse side if wanted.