# logd race in index persist

WARNING: DATA RACE
Write at 0x00c0001ba6e8 by goroutine 90:
  github.com/signadot/tony-format/go-tony/system/logd/storage.(*IndexPersister).persistAsync()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/index_persist.go:69 +0x238
  github.com/signadot/tony-format/go-tony/system/logd/storage.(*IndexPersister).MaybePersist.gowrap1()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/index_persist.go:46 +0x3c

Previous read at 0x00c0001ba6e8 by goroutine 82:
  github.com/signadot/tony-format/go-tony/system/logd/storage.(*IndexPersister).MaybePersist()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/index_persist.go:41 +0x50
  github.com/signadot/tony-format/go-tony/system/logd/storage.(*commitOps).WriteAndIndex()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/commit_ops.go:56 +0x25c
  github.com/signadot/tony-format/go-tony/system/logd/storage/tx.(*txPatcher).doCommit()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/tx/coord.go:373 +0x648
  github.com/signadot/tony-format/go-tony/system/logd/storage/tx.(*txPatcher).Commit.func2()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/tx/coord.go:279 +0x84
  sync.(*Once).doSlow()
      /usr/local/go/src/sync/once.go:78 +0x94
  sync.(*Once).Do()
      /usr/local/go/src/sync/once.go:69 +0x40
  github.com/signadot/tony-format/go-tony/system/logd/storage/tx.(*txPatcher).Commit()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/tx/coord.go:264 +0x2e0
  github.com/signadot/tony-format/go-tony/system/logd/server.doPatchErr()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/server/snapshot_stress_test.go:45 +0x214
  github.com/signadot/tony-format/go-tony/system/logd/server.TestSnapshotStress.func1()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/server/snapshot_stress_test.go:149 +0x1d4
  github.com/signadot/tony-format/go-tony/system/logd/server.(*Server).maybeSnapshot()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/server/server.go:98 +0x23c
  github.com/signadot/tony-format/go-tony/system/logd/server.(*Server).onCommit()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/server/server.go:151 +0x3c
  github.com/signadot/tony-format/go-tony/system/logd/server.doPatchErr()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/server/snapshot_stress_test.go:50 +0x2c0
  github.com/signadot/tony-format/go-tony/system/logd/server.TestSnapshotStress.func1()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/server/snapshot_stress_test.go:149 +0x1d4

Goroutine 90 (running) created at:
  github.com/signadot/tony-format/go-tony/system/logd/storage.(*IndexPersister).MaybePersist()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/index_persist.go:46 +0x100
  github.com/signadot/tony-format/go-tony/system/logd/storage.(*commitOps).WriteAndIndex()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/commit_ops.go:56 +0x25c
  github.com/signadot/tony-format/go-tony/system/logd/storage/tx.(*txPatcher).doCommit()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/tx/coord.go:373 +0x648
  github.com/signadot/tony-format/go-tony/system/logd/storage/tx.(*txPatcher).Commit.func2()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/tx/coord.go:279 +0x84
  sync.(*Once).doSlow()
      /usr/local/go/src/sync/once.go:78 +0x94
  sync.(*Once).Do()
      /usr/local/go/src/sync/once.go:69 +0x40
  github.com/signadot/tony-format/go-tony/system/logd/storage/tx.(*txPatcher).Commit()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/tx/coord.go:264 +0x2e0
  github.com/signadot/tony-format/go-tony/system/logd/server.doPatchErr()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/server/snapshot_stress_test.go:45 +0x214
  github.com/signadot/tony-format/go-tony/system/logd/