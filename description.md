# logd session: a progress request answers, on one watch's stream, once that watch is current through the head

Option (1) from 7v4azhtjh12krv76q9n0: a client asks one watch whether it is current, and the answer comes on the session's stream after every event that watch sends for a commit at or below the commit the answer reports.

## Shape

- Request: `progress: {path, watchId?}`, naming a watch the way `unwatch` does (by the id of the watch request that established it; without one, the path's id-less watch).
- Answer: `result: {progress: {path, commit: C}}` on the request's id. C is the published head when the request was handled. Every event the watch sends for a commit ≤ C is on the stream ahead of the answer, including commits the watch accounted for without sending anything. A client checks `C ≥ R`.
- A watch that ends before answering: the progress request gets an error response (`not_watching`), and the watch's own `ended` event says why.

## logd

1. The tick records how far the dispatcher has delivered (`dispatchedThrough`), and a waiter can block until it passes C. Once it has, every notification ≤ C has been handed to its watcher's `Events` channel, or that watcher was failed.
2. The handler reads C, then waits off the loop for the dispatcher to pass it, and hands the ask to the watch's stream.
3. The stream (`live`) takes the ask, processes what is in `Events` until the channel is empty or it has processed a commit above C, and then sends the answer from the watch's own goroutine. The answer is ordered after the watch's events because the same goroutine sends both.

## docd

`progress` is routed by path like `unwatch`, to logd or to the controller owning the path. Through logd it rides the client's own logd link, so the order holds. A watch docd composes across mounts is refused with `unsupported` until composition is needed.

## Docs

The contract goes in docs/logd/session.md and on `PongResult`, which says what the ping does not promise.