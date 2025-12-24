# logd: commitsSinceSnapshot not thread-safe in server.go

commitsSinceSnapshot is accessed from onCommit() (called from session goroutines) and maybeSnapshot() without synchronization:

func (s *Server) onCommit() {
    s.commitsSinceSnapshot++  // Not atomic
    s.maybeSnapshot()
}

Fix: Use atomic.Int64 or add mutex protection.