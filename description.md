# logd session: a progress request names a watch by id, and the watch answers in its own stream once it is current through the head

Option (1) from 7v4azhtjh12krv76q9n0: a client asks one watch whether it is current, and the watch answers in its own stream, so a consumer applying the watch's events in order meets the answer where it falls.

## Shape

```tony
{id: p1, progress: w1}
{id: p1 result: {progress: {commit: 52795}}}
{event: {commit: 52795 path: verse.perms progress: true} id: w1}
```

- The request names the watch by the id of its watch request. An id-less watch (the legacy path-routed form) cannot be asked.
- It is acknowledged at once with C, the head when it was handled. A write the client sent before it is at or below C.
- The watch then sends a progress event, after every event it sends for a commit at or below the one the event carries. That is C, or a later commit the watch has already taken, so the stream's commits stay in order. Any progress event at or above R answers a client waiting on R. The event is a resume point.
- A watch that ends first sends no progress event, and its ended event is the answer. One failed as a slow consumer has missed commits and ends. A watch the session does not hold is `not_watching`.

## logd

1. The tick records how far the dispatcher has delivered, and `Storage.WaitDispatched(C)` blocks until every notification ≤ C has been handed to the hub, and so is in its watcher's `Events`.
2. The handler reads C on the loop and acknowledges. Off the loop it waits for the dispatcher to pass C, then hands C to the watch's stream.
3. The stream (`live`) takes what is in `Events` up to C, holding back one above C until after the event, and sends the progress event from its own goroutine.

## docd

It finds the watch by id and routes by the watch's path: to logd on the client's own link, or to the controller, re-targeted at docd's id for the watch there. A watch composed across mounts is `unsupported`. libctl controllers answer `unsupported` to any request type they do not serve.

## libctl

`Watch.Progress(ctx)` sends the request and answers C. The event arrives in `Events()`.

## Docs

docs/logd/session.md, "Is a watch current?", plus a line at the ping.
