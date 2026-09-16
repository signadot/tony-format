# A wildcard match, one node at a time — dev plan

Issue: `y2agz9dyh12kse24n9n0`. Follow-ups it defers to: `th7sdhvyh12ksjtfn9n0` (`..`
over the wire), `5f6vrzw0h12ksrtfn9n0` (docd composing a set across mounts).

`match` today answers one node, and a wildcard path is refused as `invalid_path`
(`server/session_read.go:284-292`, `server/match_data.go:227-234`). This gives it a
wildcard path at any level and answers the set as a sequence of one-node results.

Everything below is against `go-tony/system/logd` unless it says otherwise.

## Order of work

`pvre1n2fh12ksmptn5n0` (a wildcard patch is not refused) lands **first**, or with
step 1. It is small, it is a defect, and it moves `validateDataPath` to the
role-aware shape this plan needs anyway: a wildcard is legal in a read and illegal
in a write, so the one validator both call has to know which it is answering.
`t55dmsthh12kssetn5n0` (wildcard watch) rides along with it.

## 1. The wire

`MatchResult` (`api/session.go:346-349`) carries `commit` and `body`. Add:

- `Path string` — the node's own concrete kpath. Absent (empty) on a single-node
  match, so today's answers are unchanged on the wire.
- `Done bool` — the terminal marker, `omitzero`, following
  `WatchEvent.ReplayComplete` (`api/session.go:471`, `NewReplayCompleteEvent:728`);
- `Cursor string` — on the marker, when more of the set remains (step 5).

`MatchRequest` (`api/session.go:95-98`) gains `limit` and `cursor`, which are the
protocol's paging interface rather than match's own (step 5).

```tony
{id: "7", match: {path: "jobs.*"}}
{id: "7", result: {match: {path: jobs.a1, body: {status: done}, commit: 91}}}
{id: "7", result: {match: {path: jobs.a2, body: {status: ready}, commit: 91}}}
{id: "7", result: {match: {commit: 91, done: true}}}
```

Constructors beside `NewMatchResponse`: one for a member of a set, one for the
marker. `api_gen.go` is generated — regenerate with `tony-codegen` / `go generate
./...` (docs/gomap.md:157-165), never by hand.

**No protocol bump.** A wildcard path is an error today, so no conforming client
can be relying on the old answer, and an unknown field is ignored by construction.

## 2. Validation becomes role-aware

`validateDataPath` (`server/path.go:20-32`) refuses `..` and asks nothing else. Split
the question by what the path is for — the three call sites are already one per role:

- read (`session_read.go:28`): `..` refused, a wildcard allowed;
- write (`session_write.go:27`) and the CAS path (`:39-47`): both refused;
- watch (`session_watch.go:31`): both refused, until a watch over a set is designed.

Keep the refusal messages that exist: the wildcard one reads well already
(`match_data.go:78-81`, "segment \"*\" names a set of values, not one") and the
`..` one is pinned by `TestSession_DescendPathIsRefused`
(`server/session_test.go:1289-1348`).

## 3. Expanding the path

A wildcard at any level, so the walk is left to right: resolve the concrete prefix,
enumerate the children at the wildcard, recur on each with the rest of the path.
`a.*.b.*` is two levels of that, and falls out of the recursion.

Enumeration reads the container at the prefix and iterates it — the same walk
retention already does for a rule's items (`server/retention.go:236-280`: read the
container, iterate `Fields`/`Values`, select, act). Reuse the shape; do not invent a
second one. The index's `Children` map (`storage/index/index.go:17`,
`storage/index/cursor.go:35-60`) is a faster enumeration for object levels, but it
knows written paths rather than the value at a commit, and `provenAbsent`
(`storage/cursor.go:110-119`) already documents why a keyed or indexed segment is the
document's question, not the index's. Correctness first; the index is an
optimization with its own issue if enumeration shows up in a profile.

Kind-strictness is kpath's, not new here: `.*` takes an object's fields, `{*}` a
sparse array's entries, `(*)` a keyed array's elements, `[*]` a dense array's
positions. A wildcard meeting a container of another kind contributes nothing —
that is a non-match, not an error, exactly as `listKPath` treats it
(`ir/kpath.go:363-369`: "a segment which does not fit the node it meets has to be a
non-match rather than an error").

Order is the store's: object keys are sorted on write and on read, so the sequence
is deterministic without sorting anything here.

## 4. Answering each node

Each member is answered by the code that already answers one node — `readValueAt`
(`session_read.go:259-281`) for the pattern path, `encodedMatch`
(`session_read.go:132`) for the no-pattern, no-raising fast path. That is what keeps
raising (`storage.RaiseState`), filtering (`filterState`) and the read budget
identical for a member and for a single-node read, instead of a second code path
that drifts.

- one commit for the whole set: resolve it once, as `handleMatch` does
  (`session_read.go:42-58`), and read every member at it;
- `data:` is matched and trimmed per member, and a member that does not match is
  not sent;
- the budget stays per node. A set of 10k nodes is not one budgeted object, which
  is the point of the shape;
- the marker is sent last, carrying the commit. An empty set is the marker alone —
  not `not_found`, which keeps its meaning for a path that names one place;
- a member that fails to read fails the whole request (an error under the id,
  after which no marker is sent). A partial set a caller cannot tell from a
  complete one is the thing to avoid.

Reads run off the request loop (`session_read.go:20-21`) and every response goes
through `s.send` into the session's `outgoing` channel (`server/session.go:442-447`),
so a long sequence interleaves with other requests' answers and does not stall them.
Responses demux by id, which is what a watch already relies on.

## 5. Paging: the protocol's cursor, not a match feature

A caller cannot stop a sequence mid-flight — there is no `unmatch` — so a set of
unknown size needs a bound. A `limit` alone leaves a truncated set with no way to
ask for the rest, so the pair is `limit` and `cursor`, and it is a **protocol
interface**: every operation that answers a set uses the same one.

- request: `limit` (the most members in a page) and `cursor` (continue a paging
  read);
- the marker carries `cursor` when more remains and omits it when the set is
  finished. **The marker is the authority** — a page shorter than `limit` does not
  mean the set ended, since the server may cap a page below what was asked. Same
  choice as `replayComplete`: say it, do not have the client infer it;
- within a page, members still arrive one at a time. Paging bounds the request,
  streaming delivers a page.

The store is versioned, so this needs no server-side iterator and no session
affinity: a cursor is *(the commit the set was read at, the last path answered)*.
Continuing is a read at that same commit resuming after that path, so every page is
one snapshot and a boundary never straddles two states. A continuation whose commit
has fallen out of range answers `commit_not_found`, as a historical read already does
(`session_read.go:50-56`), rather than silently reading current.

This needs a total, stable order, which step 3 already has: sorted keys per level,
so a resume seeks to the cursor's position at each level instead of rescanning what
it has already answered.

The cursor is **opaque to the client**. That is what lets docd compose one later — a
composed cursor is a bundle of per-participant cursors, and a client that parsed it
would be parsing a shape docd is entitled to change (`5f6vrzw0h12ksrtfn9n0`).

Where it goes: the request fields on `MatchRequest`, the `cursor` on `MatchResult`
beside `done`, and the encode/decode of the cursor in one place that the `..` read
(`th7sdhvyh12ksjtfn9n0`) and docd both reuse rather than re-spell.

## 6. docd answers unsupported

docd must not forward a wildcard match to one owner as though the set were that
owner's (`docd/server/registry.go:175-184`, `paths.go:63-78` classify a wildcard
segment as "indexed", so `MountsUnder` answers nil and it routes by field prefix).

Refuse it in the client session's request loop, beside the `.meta` and clock
interceptions (`docd/server/client_session.go:224-238`): a match whose path holds a
wildcard answers `ErrCodeUnsupported` (`logd/api/session.go:592`). Composing it is
`5f6vrzw0h12ksrtfn9n0`.

## 7. libctl

`deliverResponse` deletes the pending entry on the first response
(`libctl/logd.go:860-867`), so today every member after the first is dropped with
"dropping response with no matching request". A streaming result needs the entry to
live until `done`.

Add an each-node call beside `Match` (`libctl/logd.go:464-679`) — a callback taking
`(path string, node *ir.Node)` and returning an error to stop, or an iterator — and
keep `Match` as it is for a path that names one node.

## Tests

- `server`: a wildcard at the last segment; at an interior segment (`a.*.b`); two
  wildcards; each kind (`.*`, `{*}`, `(*)`, `[*]`) against its own container kind and
  against a container of another kind (contributes nothing); every member carries its
  own path and the same commit; the marker ends the sequence; an empty set is the
  marker alone; `data:` filters members; a historical `commit:` reads the set at it; a
  keyed array raises per member; `..` still refused.
- `server`: a wildcard patch, CAS path and watch are refused (from
  `pvre1n2fh12ksmptn5n0` / `t55dmsthh12kssetn5n0`, and this plan's step 2 is what
  makes them one change).
- `server` paging: a `limit` bounds a page and the marker carries a cursor; the
  continuation answers the rest and its marker carries none; the pages together are
  the whole set, in order, with no member seen twice; a page shorter than `limit`
  still carries a cursor when more remains; a cursor whose commit has fallen out of
  range answers `commit_not_found`; a write between two pages does not change what
  the second page answers, because it reads the first page's commit.
- `docd`: a wildcard match answers `unsupported`, and nothing is forwarded.
- `libctl`: the each-node call collects every member end to end against a real logd,
  and a stop mid-sequence does not wedge the session.

## Docs

- `docs/logd/session.md`: the request table (`:36-45`), the reading section, and the
  `invalid_path` row (`:417`), which stops being the whole story for a wildcard —
  it stays true for a write and a watch.
- `docs/objpath.md:80-88`: the query-vs-place statement now has a read that accepts a
  wildcard; `..` is unchanged.
- `docs/logd/retention.md` is unaffected: a rule's grammar is its own, and its
  last-segment rule is about deleting an item whole.
