# libctl: a read routed to a mount answers no commit, so a historical read under a mount cannot say what it answered

Found while working on cpqj2tf6 (a historical read reports the commit it actually answered).

## Today

A read whose path is at or under a mount is routed whole to that mount's controller. The controller runtime answers it with a body and nothing else (`handleMatch`, libctl/controller.go): `MatchResult{Body: body}`, so `commit` is 0 on every such answer, at the head or at a commit. An error goes back as a code and a message (`replyErr`), so a `SessionError.Commit` the controller got from logd is dropped.

`Handler.Match` returns `(*ir.Node, error)`. A logd-backed controller reads logd with `MatchAt` and has nowhere to put the commit logd answered at.

## What that leaves open

cpqj2tf6 makes logd answer a commit beyond compaction's cutoff at the earlier commit it holds exactly, and say which on the body and on a not_found. Through a mount neither reaches the client:

- a body answered at `S` arrives with `commit: 0`, the same as every other read under a mount;
- a not_found at `S` arrives with no commit.

A read composed across mounts is covered: docd takes the answered commit from logd's own part and reads every mount again at it. Only a read routed whole to one mount is affected.

A client that treats "answered commit differs from the commit asked" as an error (verse ctqj2tf6) would see every read under a mount differ, since 0 is never the commit asked.

## What would close it

The controller runtime would report the commit a read was answered at, on the result and on a state error. That needs `Handler.Match` to hand it back (a result type, or a field in what it returns), with a logd-backed controller passing through what `MatchAtCommit` gave it. A controller with no commit to report would leave it unset, as today.

This changes the `Handler` contract, so the shape is the tony-format owner's decision.