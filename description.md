# logd: a watch's ending message is logged and dropped, so the client gets a reason code alone

Split out of 89my9f0kh12ksqknjhn0, where verse reported it as the smaller half: when a
replay is refused for being below the floor, the client learns `replay_compacted` and
nothing else, so it cannot tell a person what the store still holds -- only that this
cursor is gone.

It is wider than the compacted case. `Session.failWatch` takes a `message` and only LOGS it:

    func (s *Session) failWatch(watcher *Watcher, reason, message string, commit int64) {
        s.log.Warn("watch ended", ..., "detail", message, ...)
        s.send(api.NewEndedEvent(watcher.ID, watcher.Path, reason, commit))

`NewEndedEvent` carries id, path, reason and commit. There is nowhere for the message to
go, so EVERY `w.fail(...)` string in the watch path is written to the server's log and
discarded as far as the client is concerned. Several are carefully phrased and carry the
one fact the client needs:

  - replay_compacted: "cannot replay from commit N: ...; exact from F" -- the floor,
    which is exactly "what the store still holds".
  - replay_failed: which commit range failed.
  - invalid_path: why the path cannot be extracted. That one never resolves, so it wants
    saying once, clearly.

The client-visible vocabulary is the reason code, and a code cannot carry a number.

WHAT IT NEEDS: a message field on the ended event. That is a wire change -- api.WatchEvent,
its codec, docd's forwarding of terminal events, and libctl's WatchEndedError -- which is
why it was not folded into the streaming fix in 25a9e17.

The relative cursor (-N, clamped) is verse's workaround, and it answers a different
question than "what is the oldest commit you have".