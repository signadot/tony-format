# proposal: scratch — a persistent document with no history

Issue r482sz5rh12ksj0cvdn0: a record overwritten often keeps its whole history for the
store-wide cutoff, and neither retention nor compaction can say otherwise.

This plan replaces `path_history_plan.md` (three drafts, see the branch's history),
which taught compaction a per-path cutoff from a schema tag. That design touched the
compaction walk, the index footprint, the proof of absence, past reads and the schema
parser, and left three forks open. Scott's direction (2026-10-23): make it as simple as
possible. One scratch area per store, no history, no compaction, no retention, reads
and writes only, never inside a docd mount, reached by session operations of its own,
and persistent.

Read first: `docs/logd/session.md` (Requests, Writing, Liveness), `server/session.go`
`dispatch`, `docd/server/client_session.go` `routeClientRequests`, `docd/server/doc.go`.

---

## Part 1: what scratch is

### Terms

**The document.** What logd stores today: the log, its index, its snapshots, commits,
scopes, schema, watches, retention, compaction. Nothing in this plan changes it.

**Scratch.** A second document a store keeps beside the first. It is one Tony node,
whole in memory and whole in one file. It has no commits, no history, no index, no
scopes, no schema, no watches, no transactions, no preconditions. It has two
operations: write a patch at a path, read the value at a path.

**A scratch path.** A kinded path into the scratch document, from its root. Scratch
has its own namespace: a scratch path and a document path with the same spelling name
different things, and nothing is reserved in the document for scratch.

### The two operations

```
{scratchWrite: {path: "verse.iods.x.out", data: {...}}}     ->  {scratchWrite: {}}
{scratchRead:  {path: "verse.iods.x.out"}}                   ->  {scratchRead: {data: <node>}}
```

**scratchWrite** applies `data` at `path` as a merge patch, with the same `Patch` the
document uses (mergeops `!delete`, `!replace`, `!insert` and the rest mean what they
mean anywhere in Tony). The path is rooted the way a participant's patch is rooted
(`tx.RootPatchAt`), so a write at an absent path creates the containers above it. The
result carries nothing: there is no commit to name.

**scratchRead** answers the node at `path`, or `data: null` when nothing is there. The
whole subtree, as it stands now. No `commit`, no `depth`, no paging, no sets: a
wildcard path is refused. A reader who wants a part asks for the part.

**Who may ask.** Any session, scoped or not. Scratch has no scopes: a scoped session's
scratch operations reach the one scratch document. The session's author is not
recorded; scratch records nothing about who wrote it.

### Persistence

The scratch document lives in `<root>/scratch.tony`, as Tony text. A write applies the
patch in memory, encodes the whole document to `scratch.tony.tmp`, and renames it over
`scratch.tony`, as the floors are written (`replay_floor.go` `writeFloor`). Under
`durability: sync` the temp file is fsynced before the rename. A write is acknowledged
after the rename, so an acknowledged write survives a crash, and the file on disk is
always a whole document.

On `Open`, a missing file is an empty scratch. A file that does not parse refuses the
open, naming the file, so the operator can move it aside: a store that silently
started empty would have lost data without saying so.

### Why it is correct, and what it costs

**C1. The document is untouched.** Scratch is a field on `Storage` and a file beside
the log; no code path of the document reads or writes it. Compaction, retention,
replay floors, provenAbsent, past reads, watches, docd composition: none sees scratch,
so none needs a rule for it.

**C2. A write is atomic and ordered.** One mutex serializes writes and reads; a read
sees a whole write or none of it. Both operations run on the session's request loop,
so a client's own write-then-read reads its write, as it does for the document.

**C3. A write is durable when acknowledged.** The rename is the commit point; the
file is never partial.

**C4. Scratch is never in a mount.** docd routes by document path. A scratch
operation names no document path, so docd routes it where it routes session-level
operations: its own logd link. A controller never sees one, and a mount, which claims
a document subtree, cannot claim scratch.

**Cost.** Every write rewrites the whole scratch file: O(size of scratch) per write.
Scratch is for small, often-overwritten state; a scratch of a few hundred kilobytes and
a few writes per second is well within one disk. A read copies the subtree under the
mutex. If a consumer grows scratch to megabytes with tens of writes per second, the
remedy is write coalescing (flush at most every N milliseconds), which trades C3 for
"an acknowledged write may be lost on crash within N"; that is not built here.

### What it does not do

- No history: a scratch read is always now. A past read, a watch, a delta are not
  offered. A consumer who needs to be told when scratch changes polls.
- No schema: keyed arrays do not exist in scratch; arrays merge positionally, as plain
  `Patch` merges them. A consumer who needs identity uses objects keyed by name.
- No preconditions, no transactions: two writers to one scratch path race, last write
  wins.
- No scopes, no author.
- Scratch is not in the document: a root read of the document does not show it, and
  a backup of the log is not a backup of scratch; `scratch.tony` is its own file.

### Alternative not taken

Reserving a top-level document name (as docd reserves `.meta`) and refusing match,
patch, watch, retain and mount at or under it. That puts scratch in the document's
namespace at the cost of five refusal sites and a collision with any existing store
that has a field by that name. Separate operations make the namespace separate for
free. If a single namespace is wanted later, the refusals can be added without
changing what scratch is.

---

## Part 2: how to get there from here

### Phase 1: protocol (`system/logd/api/session.go`)

- `ScratchWriteRequest{Path string; Data *ir.Node}` and `ScratchReadRequest{Path string}`,
  with `//tony:schemagen=session-scratch-write-request,notag` and the read likewise.
- `SessionRequest` gains `ScratchWrite *ScratchWriteRequest` (`tony:"field=scratchWrite"`)
  and `ScratchRead *ScratchReadRequest` (`field=scratchRead`). Wire names are camelCase
  as `deleteScope` and `newtx` are.
- `ScratchWriteResult struct{}` and `ScratchReadResult{Data *ir.Node}`; `SessionResult`
  gains `ScratchWrite` and `ScratchRead`.
- `make generate` regenerates `api_gen.go` and `schema_gen.tony`.
- No protocol version bump. An older server ignores the unknown field and answers
  `invalid_message: no operation specified`, which is the right answer from a server
  without scratch.

### Phase 2: storage (`system/logd/storage/scratch.go`, new)

```go
type scratch struct {
    mu   sync.Mutex
    doc  *ir.Node   // nil = empty
    path string     // <root>/scratch.tony
    sync bool       // DurabilitySync
}
func openScratch(root string, d Durability) (*scratch, error)   // parse or empty; parse error refuses Open
func (s *Storage) ScratchWrite(path string, data *ir.Node) error
func (s *Storage) ScratchRead(path string) (*ir.Node, error)
```

- `ScratchWrite`: `tx.RootPatchAt(path, data)` → `tony.Patch(doc, rooted)` → encode
  with `encode.Encode` to `scratch.tony.tmp` → fsync if sync → `os.Rename`. The in-memory
  document is replaced only after the rename succeeds, so a failed write leaves memory
  and disk agreeing.
- `ScratchRead`: `doc.GetKPath(path)` under the mutex; absent is `(nil, nil)`, the
  package's idiom, and `GetKPath` answers a copy, so a later write cannot change an
  answered read. It also refuses a wildcard or `..` itself.
- `Open` calls `openScratch` with the store's durability (`Storage.durability`, set in
  `Open`); `Close` has nothing to flush.

### Phase 3: logd server (`system/logd/server/session.go`, `session_scratch.go` new)

- `dispatch`: `case req.ScratchWrite != nil` and `case req.ScratchRead != nil`, both on
  the loop (C2). Both paths are validated with `validateDataPath(path, roleWrite)`: a
  scratch read names a place, not a set, so it takes a write's rule (no wildcard, no
  `..`), not a match's.
- `handleScratchWrite` → `s.storage.ScratchWrite`, answers `ScratchWriteResult{}`.
- `handleScratchRead` → `s.storage.ScratchRead`, answers `ScratchReadResult{Data}`.
- Errors: `invalid_path` for a bad or wildcard path, `invalid_message` for a write with
  no data, `internal` for a persist failure (the write did not happen).

### Phase 4: docd (`system/docd/server/client_session.go`)

- `routeClientRequests`: a request with `ScratchWrite` or `ScratchRead` set has no
  document path, so it falls to `destLogd` with the session-level operations. One
  explicit arm so a reader of the routing sees scratch named, and a test
  (`TestScratch_RoutesToLogd`) that a scratch op with a path spelled like a mounted
  subtree still goes to logd and never to the controller.
- `doc.go` "Two faces" and "Routing": add scratch to the client-facing list and the
  rule "scratch operations go to logd; a mount cannot hold scratch".

### Phase 5: client (`system/libctl/logd.go`)

- `ScratchWrite(ctx, path, data) error` and `ScratchRead(ctx, path) (*ir.Node, error)`,
  shaped as `Retain` is.

### Phase 6: tests

- `storage/scratch_test.go`: write/read/overwrite/delete at nested paths; absent is
  nil; reopen reads what was written; a corrupt file refuses Open naming the file; a
  failed rename leaves memory and disk unchanged (inject by making the directory
  read-only).
- `server`: extend the session table test with a scratch write then read on one
  connection, and with a scoped session reaching the same scratch.
- `docd`: `TestScratch_RoutesToLogd` as above.

### Phase 7: docs

- `docs/logd/session.md`: a "Scratch" section after "Retention": the two operations,
  the no-history/no-schema/no-scope contract, the persistence promise, and that it is
  not in the document.
- `docs/docd/mounts.md` Routing table: a row "logd (base) — a scratch operation".
- `docs/logd/index.md`: one paragraph naming scratch beside the document.
- `storage/CLAUDE.md` package structure: `scratch.go`.

### Phase 8: verse

Verse moves `verse.iods.*.out` to scratch: `scratchWrite` at `verse.iods.<id>.out` after
each pass, `scratchRead` where it reads one back. Ticks, `~status` and `lastError`
stay in the document, where the consumer's own comments (10:04, 10:23 on the issue)
want them read at their causing commits. Anything in verse that watches `out` today
must poll or be driven by the tick it already watches; the consumer confirms which.

---

## Open questions

1. **Wire names.** `scratchWrite`/`scratchRead` as fields of the session request, or a
   nested `scratch: {write: ...}` as `schema: {get, set}` is. The plan takes flat.
2. **Write result.** Empty, or echo the data as `patch` does. The plan takes empty:
   there is no auto-id to report, and echoing doubles the bytes for nothing.
3. **Coalescing.** Not built; see Cost. If verse's scratch is larger or busier than the
   estimate, say so before Phase 2 and the flush interval becomes a config field.
