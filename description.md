# logd: a patch request-timeout abandons (does not cancel) the commit → phantom write + client/server divergence

Severity: MEDIUM (correctness: client told timeout but the write can still land). Confidence: traced.

handlePatch with a request Timeout spawns go func(){ resultCh <- patcher.Commit() } and on time.After returns ErrCodeTimeout to the client, abandoning the goroutine (session.go:489-500). The commit is NOT cancelled: for a multi-participant tx the participant's data was already submitted, so the tx can still commit durably after the client was told it timed out (onCommit/snapshot tracking is also skipped for that commit). If Commit() never returns (e.g. Tx.Timeout=0) the goroutine leaks. resultCh is buffered(1) so the send itself does not block.

Fix: cancel/withdraw the participant on request timeout (or make the timeout a soft signal and reconcile), so a timed-out patch cannot commit behind the client's back. Files: server/session.go:489.