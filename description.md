# logd: a wildcard watch is confirmed and then ended, answered not_found on an empty store, or left alive delivering nothing

A watch whose path holds a wildcard (`.*`, `[*]`, `{*}`, `(*)`) is never refused as a request. `watchPath` validates only `Descend` (server/session_watch.go:31 -> path.go:20-32), so what the client is told depends on the store and the request flags -- three answers, none of them "this path cannot mean anything".

## Confirmed, then ended

On a store with content, the absence gate calls `absentAt`, which returns the `PathError` only when it is `PathAbsent`, and nil for anything else -- "let the watch establish and let the seed report it" (session_watch.go:745-758). A wildcard read raises `PathBadSegment`, so the gate passes.

The watch is registered and the confirmation is sent (session_watch.go:170). Then `sendInitialState` reads the path, gets `PathBadSegment`, and fails the watch with `invalid_path` (session_watch.go:322-327). The client is told the watch exists and, in the next message, that it ended.

docd already has the reasoning for why that is the wrong shape, in `refuseWatch` (docd/server/watch.go): before the confirmation the client is waiting for an answer to `watch`, and an error is that answer; ending a stream nobody has been told about leaves a caller deciding something before the news arrives. Here the news arrives in the wrong order instead.

## not_found on an empty store

At commit 0 `absentAt` returns `PathAbsent` for any path (session_watch.go:746-749), so unless `waitIfAbsent` is set, the watch is refused with `not_found` -- "nothing is there, creating what is missing is a reasonable next move" (docs/logd/session.md:417). No write can make `a.*` resolve. The path is invalid, and this says it is merely empty.

## Or alive forever, delivering nothing

Two ways the terminal event never comes:

- `noInit: true` skips `sendInitialState` entirely (session_watch.go:275-277), so nothing ever raises the bad segment.
- `waitIfAbsent: true` on an empty store passes the gate, and `sendInitialState` at commit 0 sends state without reading (session_watch.go:311-313).

The watch then lives and is silent: `ProjectDelta` answers `ok=false` for a non-field segment (logd/api/delta.go:37-40), the re-read swallows the `PathError` (session_read.go:401-411), `prev` and `next` are both nil and `SameState` suppresses every event. A client sees a healthy watch on a path that can never resolve.

## Shape of a fix

Refuse a wildcard watch where the watch is requested, as `invalid_path`, with the message reads use (match_data.go:78-81) -- one answer to one request, before there is a stream to end. That also removes the `noInit` and `waitIfAbsent` holes, since they exist only because the refusal happens in the seed.

Nothing pins any of this: `TestSession_DescendPathIsRefused` (server/session_test.go:1289-1348) covers `..` only. Reads are pinned (server/path_error_test.go:80-86, libctl/response_error_test.go:63,88-89), which is why the read behaviour is the one to match.