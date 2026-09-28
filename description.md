# logd session: is a request a barrier for that session's watch events? (pong(C) ⇒ every watch on the session has delivered through C?)

## The question

On one logd session with watches open, a client sends a request, a ping for example, and gets back a result carrying commit C (`PongResult.Commit`).

**By the time that response arrives, has every event for every watch on this session, at or below C, already been sent ahead of it on the session's stream?** Put another way: is the response ordered after the session's watch output up to C, so that receiving pong(C) means the client's watches are current through C? That includes commits a watch accounted for without delivering anything (`accountFor`).

And does the same hold through docd, which answers the ping itself with `seen`?

## Why verse asks

verse's permission check answers from an in-process view of its perms mirror (`verse.perms.node`), kept current by a logd watch. A read served at commit R has to be checked against a view that has applied every perms change at or below R (verse 7q4azhtjh12ks7e0q9n0). The watch only delivers commits that touch the mirror, so the view can't tell "no perms change up to R" from "not caught up to R".

Today that gap is closed with a barrier that reads the mirror again, about 4ms under load. Under steady writes nearly every read pays it, because R is the head and the head moves with every write. If a request on the watching session is a barrier, the barrier becomes one round trip on that session: send a ping, and when pong(C ≥ R) arrives, the view is current through C.

## What the code and docs say, as read from verse's side

- `PongResult` (system/logd/api/session.go:443): "The number is monotonic and chases the head; it is not a promise that the client has seen everything below it. logd answers with its head. docd answers the ping itself, with the highest commit it has reported to any client."
- The logd ping handler (system/logd/server/session.go:347) reads `GetCurrentCommit()` and `s.send`s the pong on `s.outgoing`, the queue watch frames also go through. Whether a watch's frames for commits at or below that head are always enqueued before the pong isn't clear from here: watch streams look like they run on their own goroutines.
- The reading on the verse side is that "not a promise" is about the client reading asynchronously, not about server-side ordering, and that a new query on the session is a barrier. This issue asks that it be confirmed or corrected, and if it's true, that the promise be written into the contract (ping, and any other request), for logd and for docd.

## If it isn't a barrier today

Would making it one be acceptable: a response that waits until each of the session's watches has accounted for (or delivered) everything up to the commit it reports? Or is there another way for a client to learn "your watches are current through C"?