# index: the persister writes the residency list while a commit reads it through touched(), a data race under -race

`go test -race` fails on main (8ec4840) in
TestSnapshotDoesNotRideOnTheWriteThatTripsIt (system/logd/server), with one data race.
Found while race-testing qzmkhfjqh12ksydsmdn0's branch, where it is the only race; it
does not involve that change.

    Read  by the committing goroutine:
      index.(*Index).touched            region.go:293
      index.(*Index).withResident       region.go:322
      index.(*Index).addSegment         region.go:422
      index.(*Index).Add                index.go:144
      index.IndexPatch                  log_segment.go:184
      storage.(*commitOps).WriteAndIndex   commit_ops.go:135

    Previous write by the persister goroutine (started at commit_ops.go:148):
      index.(*Residency).pushLocked     region.go:212
      index.(*Residency).list           region.go:191
      index.(*Index).persistRegions     regions_file.go:374
      index.(*Index).Persist            regions_file.go:314
      storage.(*IndexPersister).persistAsync   index_persist.go:65

Repro: `go test -race -short -count=1 -run TestSnapshotDoesNotRideOnTheWriteThatTripsIt
./system/logd/server/`.

The residency list is written under the persister's hold while a commit reads a node's
residency state through touched() without the lock the write holds. What the two sides
lock is not yet read; the consequence of the race (a lost LRU update, a torn list, or
worse under eviction) is not measured.

Neighbour, closed: sqn7ptxch12ksxp19smg (logd race in index persist).