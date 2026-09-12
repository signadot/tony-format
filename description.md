# logd: a commit records no writer -- a server multiplexing principals onto one session cannot say who wrote what

Context: found while designing verse's users and ownership layer (verse git-issue
yezdk01bh12kshx9m9n0), whose base layer puts a principal on every store write so that
the store can later check it and a reader can later ask who wrote a commit.

## What the log records today

A commit carries what changed and when, and nothing about who. Against v0.0.212:

- `PatchRequest` (system/logd/api/session.go:95) is `txId`, `timeout`, `match`, and the
  patch. There is no field for a writer.
- `Hello.ClientID` (api/session.go:26) is per session, and the server only logs it at the
  handshake (server/session.go:343-357). It reaches no entry.
- `dlog.Entry` (storage/internal/dlog/entry.go:34) is commit, timestamp, patch, tx state and
  snapshot position. `CommitNotification` (storage/storage.go:24) and `WatchEvent`
  (api/session.go:352) carry commit, path, state and patch.

So a client multiplexing many principals onto one session, which is what a server in front
of logd is, cannot record which of them made a given commit, and a client that opened one
session per principal would gain nothing, since the session's id is not stored either.

## What is asked

A per-request writer on the patch, carried through to the record and to every reader of
it:

1. `PatchRequest` takes an optional `author` (a string; the caller's principal, opaque to
   logd, the way `ClientID` is).
2. The entry stores it beside the timestamp, and a transaction's participants each keep
   their own.
3. `CommitNotification` and `WatchEvent` carry it, so a watcher and a `since` replay see
   who wrote each commit without a second lookup.
4. docd relays it where it builds a logd `PatchRequest` on a client's behalf
   (docd/server/logdwrite.go:49, docd/server/client_session.go:442).

## The hazard worth stating

A request field a server does not know is IGNORED (the `ProtocolVersion` comment,
api/session.go:31-40). For most fields that is a wrong answer that looks like success; for
an author it is an audit record that looks kept and is not. So either the protocol version
moves with this, or a request carrying `author` to a server that does not store it is
refused rather than accepted. Silently dropping the one field whose whole purpose is to be
kept is the failure to avoid.

## Meanwhile

Verse carries the principal on its own `entity.Write` and checks it before issuing the
patch, which needs nothing from logd. Its durable record until this lands is an
owner-at-creation mirror plus the attribution fields its acts already carry; every write
after creation by a non-owner is unrecorded. When `author` exists, the same field on
verse's write populates it and nothing on verse's side moves.