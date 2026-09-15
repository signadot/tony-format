# docd: a clock watch event carries the clock's value as its commit, so every clock tick raises docd's pong mark and the client's KnownCommit to a nanosecond count

serveClockWatch stamped clk.value() -- epoch + N*frequency in nanoseconds -- into WatchEvent.Commit (clock.go, NewStateEvent on the initial state and on every tick). A clock is not in the commit sequence, and the commit field names a point in it.

What it cost: writeResponse feeds every event's commit to Server.noteCommit, so the first clock event pushed docd's seen mark (what a pong reports to every client) to the clock value, and it never comes back down -- it is monotonic. libctl's noteResponseCommit does the same to the client's KnownCommit. A client comparing that mark with a real commit got nonsense. It also put the value in lastSeen under the clock watch's key, where nothing ever deleted it.

Fix: clock watch events carry commit 0, as the clock's match result already did, and the value is only the state. Documented in docs/logd/session.md (Clocks) and on api.ClockSpec. TestServeClockWatch asserts commit 0 and that server.seen stays 0.