# logd storage switchactive race

WARNING: DATA RACE
Read at 0x00c000146340 by goroutine 172:
  github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog.(*DLog).SwitchActive()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog/dlog.go:297 +0x1dc
  github.com/signadot/tony-format/go-tony/system/logd/storage.(*Storage).SwitchAndSnapshot()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/snap_storage.go:97 +0xc4
  github.com/signadot/tony-format/go-tony/system/logd/server.TestConcurrentSnapshots.func1()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/server/snapshot_stress_test.go:247 +0x74
  github.com/signadot/tony-format/go-tony/system/logd/server.TestConcurrentSnapshots.gowrap3()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/server/snapshot_stress_test.go:249 +0x40

Previous write at 0x00c000146340 by goroutine 166:
  github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog.(*DLog).SwitchActive()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog/dlog.go:286 +0x130
  github.com/signadot/tony-format/go-tony/system/logd/storage.(*Storage).SwitchAndSnapshot()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/storage/snap_storage.go:97 +0xc4
  github.com/signadot/tony-format/go-tony/system/logd/server.TestConcurrentSnapshots.func1()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/server/snapshot_stress_test.go:247 +0x74
  github.com/signadot/tony-format/go-tony/system/logd/server.TestConcurrentSnapshots.gowrap3()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/server/snapshot_stress_test.go:249 +0x40

Goroutine 172 (running) created at:
  github.com/signadot/tony-format/go-tony/system/logd/server.TestConcurrentSnapshots()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/server/snapshot_stress_test.go:244 +0x374
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:1934 +0x164
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:1997 +0x3c

Goroutine 166 (running) created at:
  github.com/signadot/tony-format/go-tony/system/logd/server.TestConcurrentSnapshots()
      /Users/scott/Dev/github.com/signadot/tony-format/go-tony/system/logd/server/snapshot_stress_test.go:244 +0x374
  testing.tRunner()
      /usr/local/go/src/testing/testing.go:1934 +0x164
  testing.(*T).Run.gowrap1()
      /usr/local/go/src/testing/testing.go:1997 +0x3c