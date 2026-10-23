# proposal: scratch — a persistent document with no history

Issue r482sz5rh12ksj0cvdn0: a record overwritten often keeps its whole history for the
store-wide cutoff, and neither retention nor compaction can say otherwise.

This plan replaces `path_history_plan.md` (three drafts, see the branch's history),
which taught compaction a per-path cutoff from a schema tag. That design touched the
compaction walk, the index footprint, the proof of absence, past reads and the schema
parser, and left three forks open. Scott's direction (2026-10-23): make it as simple as
possible. One scratch area per store, no history, no compaction, no retention, reads
and writes only, never inside a docd mount, reached by a session operation of its own,
persistent, with writes coalesced behind a flush and their order kept.

Second draft, after a review against the code (findings folded in below: write
serialization, the cost of `Patch`, nil documents, unsafe mergeops, how config reaches
storage, zero-means-default, flush retry, comments, absence on the wire).

Read first: `docs/logd/session.md` (Requests, Writing, Liveness), `server/session.go`
`dispatch`, `server/server.go` `New` (how config is applied by setters),
`api/state.go` (presence and comments doctrine), `docd/server/client_session.go`
`routeClientRequests` and `requestPath`, `docd/server/doc.go`.

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

### The operation

```
{scratch: {write: {path: "verse.iods.x.out", data: {...}}}}  ->  {scratch: {write: {data: {...}}}}
{scratch: {read:  {path: "verse.iods.x.out"}}}               ->  {scratch: {read: {data: <node>}}}
```

One operation, `scratch`, holding exactly one of `write` or `read`. (This is stricter
than `schema`, which takes `get` when both are given: a request that says two things
is a bug, and is answered `invalid_message`.)

**write** applies `data` at `path` as a merge patch, as the document applies one:
mergeops `!delete`, `!replace`, `!insert` and the rest mean what they mean anywhere in
Tony, comments are kept, and the unsafe mergeops the document refuses (`!pipe` and its
kin, `tx/unsafe_write.go`) are refused here too, with `invalid_diff`: a session
operation does not run commands on the daemon. The path is rooted as a participant's
patch is rooted (`tx.RootPatchAt`): a write at an absent object field or sparse index
creates the containers above it; a dense index `[i]` needs the array to exist, as it
does in the document. The result echoes the data written, as a patch's does; there is
no commit to name.

**read** answers the node at `path` as it stands now, the whole subtree. Absent is an
omitted `data`, as the store's presence rule has it (`api/state.go`: a nil node is
absent, and nothing maps absence onto null). No `commit`, no `depth`, no paging, no
sets: a wildcard path is refused. A reader who wants a part asks for the part.

**Who may ask.** Any session, scoped or not. Scratch has no scopes: a scoped session's
scratch operations reach the one scratch document. A write takes no author and the
session's is not recorded: author in logd lives on the log entry and is seen on a
watch event, and scratch has neither. There is no audit log to put one in, and a
field that looks like one would invite the misuse.

### Persistence

The scratch document lives in `<root>/scratch.tony`, as Tony text with comments. A
write applies the patch in memory and is acknowledged. A flusher ticks every
`scratchFlush` (default 1s): if the document has changed since the last flush, it
copies it, encodes the copy to `scratch.tony.tmp`, and renames it over `scratch.tony`.
Under `durability: sync` the temp file is fsynced before the rename and the directory
after it. A failed flush is logged and the next tick tries again; the document stays
in memory meanwhile. `Close` flushes once more, synchronously, before the log closes.
A scratch deleted to nothing flushes as an empty file, which reads back as empty.

**Order is kept.** One mutex guards the document. A write holds it while it applies
the patch; a read holds it while it copies the subtree; the flusher holds it while it
copies the whole document, and encodes the copy outside. So writes apply one at a
time in arrival order, and what the flusher encodes is the document as it stood after
some write k: every write up to k is in it, no write after k is, and no write is ever
in the file without the writes before it. A crash loses a suffix of the write
sequence, never a hole in it. The suffix is one tick long while flushes succeed;
while they fail it grows, and the log says so each tick.

`scratchFlush` is a storage config field (`storage: {scratchFlush: 1s}`). Zero means
the default, as every config field here does. There is no write-through mode: a test
that wants the file now calls `Close`, or the flusher's `flushNow` from inside the
package.

On `Open`, a missing or empty file is an empty scratch. A file that does not parse
refuses the open, naming the file, so the operator can move it aside. The store's logs
step over an unreadable region because refusing to open recovers nothing (storage.go,
t96b5ejqh12krprjghn0); scratch is one whole file, so stepping over it means starting
empty, which loses everything without a word. Refusing loses nothing.

### Why it is correct, and what it costs

**C1. The document is untouched.** Scratch is a field on `Storage` and a file beside
the log; no code path of the document reads or writes it. Compaction, retention,
replay floors, provenAbsent, past reads, watches, docd composition: none sees scratch,
so none needs a rule for it.

**C2. A write is atomic and ordered.** The mutex is held across apply, so two
sessions writing at once apply one after the other and neither is lost; a read sees a
whole write or none of it. Reads come from memory, and both operations run on the
session's request loop, so a client's own write-then-read reads its write, as it does
for the document.

**C3. The file is whole and ordered.** The rename is the commit point, so the file is
never partial, and the flusher's copy under the mutex makes it a prefix of the write
sequence (above). What scratch does not promise is that an acknowledged write survives
a crash: the newest writes may be lost, as a suffix. A consumer that needs a write to
be durable before it acts on it does not keep that state in scratch.

**C4. Scratch is never in a mount.** docd routes by document path. A scratch
operation names no document path (`requestPath` answers ""), so docd forwards it on
the client's own logd link, as it forwards session-level operations. A controller
never sees one, and a mount, which claims a document subtree, cannot claim scratch.

**Cost.** A write is one in-place patch, O(spine of the path plus the data): the
patch is applied with `PatchOwned`, which consumes the old document rather than
cloning it, and the mutex is what makes that safe (nothing else holds the document).
A read copies the subtree it answers. A flush copies the whole document under the
mutex, O(size of scratch), once per tick when anything changed, and encodes the copy
outside it. Memory holds the document plus one copy while a flush runs. The mutex is
held for the write's patch, a read's copy, or a flush's copy, never for an encode or
a disk write.

### What it does not do

- No history: a scratch read is always now. A past read, a watch, a delta are not
  offered, and will not be: a consumer designs so that nothing waits on scratch
  changing. What a consumer must be told about goes in the document, where a watch
  can see it; scratch holds what is read when something else says to read it.
- No schema: keyed arrays do not exist in scratch; arrays merge positionally, as plain
  `Patch` merges them. A consumer who needs identity uses objects keyed by name.
- No preconditions, no transactions: two writers to one scratch path race, last write
  wins.
- No durability promise for the newest writes (C3).
- No scopes, no author.
- Scratch is not in the document: a root read of the document does not show it, and
  a backup of the log is not a backup of scratch; `scratch.tony` is its own file.

### Alternative not taken

Reserving a top-level document name (as docd reserves `.meta`) and refusing match,
patch, watch, retain and mount at or under it. That puts scratch in the document's
namespace at the cost of five refusal sites and a collision with any existing store
that has a field by that name. A separate operation makes the namespace separate for
free. If a single namespace is wanted later, the refusals can be added without
changing what scratch is.

---

## Part 2: how to get there from here

### Phase 1: protocol (`system/logd/api/session.go`)

- `ScratchRequest{Write *ScratchWriteRequest; Read *ScratchReadRequest}`, shaped as
  `SchemaRequest{Get, Set}`; `ScratchWriteRequest{Path string; Data *ir.Node}`,
  `ScratchReadRequest{Path string}`, each with a `//tony:schemagen=...,notag` marker.
- `SessionRequest` gains `Scratch *ScratchRequest` (`tony:"field=scratch"`).
- `ScratchResult{Write *ScratchWriteResult; Read *ScratchReadResult}`;
  `ScratchWriteResult{Data *ir.Node}` echoes the data written; `ScratchReadResult{Data
  *ir.Node}`, whose nil `Data` the codec omits, which is how absent is said;
  `SessionResult` gains `Scratch`.
- `make generate` regenerates `api/api_gen.go` and `api/schema_gen.tony`, and (Phase 3)
  `server/server_gen.go` and `server/schema_gen.tony` for the config field.
- No protocol version bump. An older logd ignores the unknown field and answers
  `invalid_message: no operation specified`; an older docd drops it on decode,
  forwards `{id}`, and logd gives the same answer. Both are the right answer from a
  server without scratch.

### Phase 2: storage (`system/logd/storage/scratch.go`, new)

```go
type scratch struct {
    mu      sync.Mutex
    doc     *ir.Node       // nil = empty; owned: patched in place under mu
    dirty   bool           // changed since the last successful flush
    path    string         // <root>/scratch.tony
    tick    time.Duration  // scratchFlush; 0 = default
    stop    chan struct{}
    stopped chan struct{}
    logger  *slog.Logger
}
func openScratch(root string, logger *slog.Logger) (*scratch, error) // parse (with comments) or empty
func (s *Storage) ScratchWrite(path string, data *ir.Node) error
func (s *Storage) ScratchRead(path string) (*ir.Node, error)
func (s *Storage) SetScratchFlush(d time.Duration)                   // 0 = default; starts the flusher
func (sc *scratch) flushNow(sync bool) error                         // the flusher's step, also Close's
```

- `ScratchWrite`: `mergeop.FindUnsafe(data)` refuses first, as `checkUnsafeWrite`
  does for the document, with a `tx.UnsafeOpError` the server maps to `invalid_diff`
  as `handlePatch` does. Then
  `tx.RootPatchAt(path, data)`; under `mu`: base `ir.Null()` when `doc` is nil, then
  `tony.PatchOwned(base, rooted, mergeop.Comments(true), mergeop.RejectUnsafe(true))`,
  `doc = result` (nil when the patch deleted everything), `dirty = true`. The rooted
  patch is built from the request's own decoded node, so `PatchOwned` may take it.
- `ScratchRead`: under `mu`, nil `doc` is `(nil, nil)`; else `doc.GetKPathWith(path,
  ir.WithComments(true))`, which answers a copy, `(nil, nil)` for an absent field or
  key, and refuses a wildcard or `..` itself.
- The flusher goroutine: a `time.Ticker` at `tick`; on each tick, under `mu`, if
  `dirty`, `copy := doc.Clone()` (nil stays nil) and `dirty = false`; outside `mu`,
  `encode.Encode(copy, tmp, encode.EncodeComments(true))`, or an empty file for nil;
  under `sync`, fsync the file, rename, fsync the directory; else rename. On error,
  log at error and under `mu` set `dirty = true` again, so the next tick retries.
  `sync` is read from `Storage.durability` (via `GetDurability`) at each flush.
- `SetScratchFlush` is applied in `server.New` beside `SetDurability` and the budgets,
  resolving zero to 1s, and starts the flusher; `Open` only parses the file. A store
  opened by a test that never calls it has no flusher, and `Close` still flushes.
- `Close`: stop the ticker, `flushNow`, then the rest of `Storage.Close`; the flush
  error joins Close's error chain before `dLog.Close`.
- `Storage.ScratchWrite` and `ScratchRead` refuse a path with a wildcard or `..`
  themselves (`kpath.Parse` plus the segment check the server's `validateDataPath`
  makes), so the store is correct when called directly, not only through the server.

### Phase 3: logd server (`session.go`, `session_scratch.go` new, `fileconfig.go`, `server.go`)

- `dispatch`: `case req.Scratch != nil` → `handleScratch`, on the loop (C2). Neither
  or both of `write`/`read` → `invalid_message`. Both paths are validated with
  `validateDataPath(path, roleWrite)`: a scratch read names a place, not a set, so it
  takes a write's rule, not a match's.
- write: nil `Data` → `invalid_diff` "scratch data is required", as `handlePatch`
  answers a patch with none; `tx.UnsafeOpError` → `invalid_diff`; then
  `s.storage.ScratchWrite`, answering `ScratchResult{Write: {Data: req.Data}}`.
- read → `s.storage.ScratchRead`, answering `ScratchResult{Read: {Data}}`.
- `invalid_path` for a bad or wildcard path; `internal` for anything else.
- `fileconfig.go` `StorageConfig` gains `ScratchFlush Duration`
  (`tony:"field=scratchFlush"`); `ToStorageConfig` carries it; `server.New` calls
  `SetScratchFlush`, zero meaning the default as the other fields do.

### Phase 4: docd (`system/docd/server/client_session.go`)

- `requestPath` gains an explicit arm for `Scratch` answering "", so a reader of the
  routing sees scratch named; `routeForPath("")` is `destLogd`, the client's own logd
  link. A test (`TestScratch_RoutesToLogd`) that a scratch op with a path spelled like
  a mounted subtree still goes to logd and never to the controller.
- `doc.go` "Two faces" and "Routing": add scratch to the client-facing list and the
  rule "scratch operations go to logd; a mount cannot hold scratch".

### Phase 5: client (`system/libctl/logd.go`)

- `ScratchWrite(ctx, path, data) (*ir.Node, error)` and `ScratchRead(ctx, path)
  (*ir.Node, error)`, shaped as `Retain` is; a read's nil result is absent.

### Phase 6: tests

- `storage/scratch_test.go`: write/read/overwrite/delete at nested paths; absent is
  nil; a comment written is read back and survives Close and reopen; N writes then
  Close reopen to the last; delete to nothing, Close, reopen to empty; a corrupt file
  refuses Open naming the file; `!pipe` is refused and the document is unchanged.
  Ordering (C3): with a long tick, write a, b, `flushNow`, write c, kill without
  Close, and the file reopens to exactly a+b. Retry: make the directory read-only,
  write, tick (flush fails, logged), restore, tick, reopen to the write. Concurrency:
  two goroutines each write their own leaf 1000 times; both leaves end at 1000.
- `server`: extend the session table test with a scratch write then read on one
  connection, a scoped session reaching the same scratch, and a `scratch` naming both
  operations refused.
- `docd`: `TestScratch_RoutesToLogd` as above.

### Phase 7: docs

- `docs/logd/session.md`: a row for `scratch` in the Requests table; a "Scratch"
  section after "Retention": the operation and its two forms, the no-history/no-schema/
  no-scope contract, what persistence does and does not promise (C3), `scratchFlush`,
  and that scratch is not in the document; the Errors table names scratch under
  `invalid_path`, `invalid_diff`, `invalid_message`.
- `docs/docd/mounts.md` Routing table: a row "logd (base) — a scratch operation".
- `docs/logd/index.md`: one paragraph naming scratch beside the document.
- `storage/CLAUDE.md` package structure: `scratch.go`.

### Phase 8: verse

Verse moves `verse.iods.*.out` to scratch: a scratch write at `verse.iods.<id>.out`
after each pass, a scratch read where it reads one back. Ticks, `~status` and
`lastError` stay in the document, where the consumer's own comments (10:04, 10:23 on
the issue) want them read at their causing commits. Nothing in verse watches `out`
today (Scott, 2026-10-23), and verse is to be designed so that nothing ever watches
scratch material: a reader of `out` is driven by the tick in the document.

---

## Decided (Scott, 2026-10-23)

- Nested shape, `scratch: {write | read}`, as `schema: {get | set}`.
- A write echoes its data, as a patch does.
- Writes are coalesced behind a flush tick; order is kept (C3). Scratch is assumed
  large and busy enough that a file rewrite per write would be too much, which is
  also why a write patches in place rather than cloning the document.
- Scratch is never watched. Verse does not watch `iods.*.out` today and is to be
  designed so that it never watches scratch material.
- No author. There is no audit log for it to feed, and a field that looked like one
  would invite misuse.
