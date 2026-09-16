# logd: match cannot ask for a set -- a wildcard path is refused, so a fan-out read is N round trips or one oversized body

A wildcard path names a set of values, and `match` answers one, so today it is refused: `invalid_path`, "segment \"*\" names a set of values, not one" (server/session_read.go:284-292, server/match_data.go:227-234). A caller that wants every job under `jobs` has to know the names first -- a read per name -- or read the whole container and throw away what it did not ask for. Neither is what it meant.

Ask: `match` accepts a wildcard path, and **answers one node at a time**.

## The answer is a sequence, not a list

Each node is its own response, stamped with the request id, carrying the node's own concrete path:

```tony
{id: "7", match: {path: "jobs.*"}}
{id: "7", result: {match: {path: jobs.a1, body: {status: done}, commit: 91}}}
{id: "7", result: {match: {path: jobs.a2, body: {status: ready}, commit: 91}}}
{id: "7", result: {match: {commit: 91, done: true}}}
```

Why a sequence rather than one body holding a list:

- a container with 10k items is not a document anyone wants assembled in memory at either end, and the caller usually stops early;
- the caller learns WHERE each node is. `MatchResult` carries only `commit` and `body` today (api/session.go:346-349), so a set delivered as one body would lose the paths, and the paths are the point -- they are the input to the next read or write (`o list -paths` makes the same case, docs/objpath.md:50-62);
- the protocol already demuxes several responses on one id -- that is what a watch is -- so nothing new is needed on the client's routing.

New on the wire: `MatchResult.Path` and a terminal marker. The marker follows `WatchEvent.ReplayComplete` (api/session.go:471, NewReplayCompleteEvent:728-736): a flag on the last result rather than a separate message kind.

## One commit, one snapshot

Every node in the sequence is read at the same commit, and each result says which. A set assembled from several commits is not a state anything ever held. `commit:` on the request still means a historical read, of the whole set.

## The pattern applies per node

`data:` is matched and trimmed against each node separately, and a node that does not match is not sent. That is what makes the wildcard worth having -- "every job that is done, just its status" is one request -- and it is what `o match -each` already means for a stream of documents.

An empty set is the marker alone, with a `commit`, not `not_found`: a query for a set answers with a set, and empty is an honest one. `not_found` stays what it is for a path that names one place and finds nothing.

## A wildcard at any level

`a.*.b`, `runs(*).status`, `a.*.b.*` -- a wildcard is a segment like any other, and the walk is per level. A retain rule allows one only in its last segment (server/retention.go:145-147) because a rule is about the items of one container and "delete the item whole" has to mean one thing; a read has no such reason.

Every kind of wildcard is a read here, `[*]` included. A retain rule refuses `[*]` because an expiry shifts every later position (docs/logd/retention.md:114-120); a read at a position is a read of what is there now, which logd already answers for `a.b[0]`.

`..` is not in this issue. It is a query segment whose walk is a whole subtree of unknown size rather than one node's children, and the index is keyed by literal segments -- a different problem behind the same protocol shape. It stays refused wherever a path must name a place (docs/objpath.md:80-88). Follow-up: th7sdhvyh12ksjtfn9n0.

## docd answers unsupported

A wildcard match whose set spans mounts has no single owner. docd routes by field prefix and `MountsUnder` answers nil for a path it classifies as indexed (docd/server/registry.go:175-184, paths.go:63-78), so today it would forward a wildcard to one owner as though the set were that owner's -- answering a different question, silently, with no way for the caller to tell a complete answer from a partial one.

So docd answers `unsupported` (logd/api/session.go:592) for a wildcard match until it composes one. Composing it -- fan out, interleave, one commit for the whole set -- is the follow-up: 5f6vrzw0h12ksrtfn9n0.

## What else this touches

**libctl** needs an each-node API (a callback or an iterator) beside `Match`, which returns one body (libctl/logd.go:464-679).

## What already exists to build on

- The index enumerates a node's children by name (storage/index/index.go:115-119, storage/index/cursor.go:35-60) -- a wildcard level is that enumeration rather than a new lookup.
- Retention already walks a container's children, selects with a pattern and acts per item (server/retention.go:256-280). That is the same walk this read needs, in the one operation where a wildcard is already legal.
- `ir.ListKPath` is the in-process answer to exactly this question (ir/kpath.go:279-291), wildcards and `..` included, and kpath's `Matches` now agrees with it (17hj5ygkh12ks7j7n5n0). The wire operation is what is missing.

## Related

- pvre1n2fh12ksmptn5n0 and t55dmsthh12kssetn5n0: a wildcard is not refused on a PATCH or a WATCH, where a path must name a place. This issue is the other side -- the one operation where a set is a legitimate answer -- so the two should land together: the write and watch boundaries refuse a wildcard, and the read accepts it.
- Docs to follow: the request table and the `invalid_path` row (docs/logd/session.md:36-45, :417), and the query-vs-place statement (docs/objpath.md:80-88).