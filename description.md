# logd: race condition in tx/coord.go Commit() - multiple goroutines can execute commit path

Multiple patchers can pass the done check simultaneously since we release p.mu before actually performing the commit. While setResult does handle the race via co.resultMu, this creates unnecessary contention and redundant work - multiple goroutines may execute the full commit path (GetCurrentCommit, evaluateMatches, etc.) before one wins the result lock.

Fix: Use a single sync.Once per coordinator to ensure only one goroutine executes the commit logic.