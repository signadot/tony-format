# index: TestAWalkDoesNotStopWrites flakes under full-suite load

Under `go test ./... -count=1` on 2026-10-31, the test failed:

    walk_lock_test.go:66: walk 67ms, worst write during it 62.922ms
    walk_lock_test.go:70: a write took 63ms while the index was walked (67ms): it waited for the walk

It passed 5 times in a row run alone (`-run TestAWalkDoesNotStopWrites -count=5`). It decides "waited for the walk" from a wall-clock ratio. Under load, the scheduler can stall a write about as long as the walk takes, with no lock involved.

Seen while testing branch issue-v5dn9vfbh12krj, which changes only the parser and wire decoder, not the index.