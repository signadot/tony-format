# logd: session reader doesn't respect done signal during blocking read

The s.done check in session.go reader() only runs between reads. If the connection is idle, Close() won't take effect until a message arrives. This could cause slow session cleanup.

Fix: Set read deadline on connection before reads, or use a goroutine with context cancellation.