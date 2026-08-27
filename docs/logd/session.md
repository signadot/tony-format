# The session protocol

A session is one connection carrying a stream of **newline-delimited Tony documents**
in both directions. Every message a client sends names exactly one operation; every
message a server sends is a result, a watch event, or an error.

```tony
{hello: {clientId: verse}}
{patch: {path: verse.entities.e1, data: {status: ready}}}
{match: {path: verse.entities.e1}}
{watch: {path: verse.entities}}
```

**docd speaks this protocol verbatim**, so a client written against logd talks to docd
unchanged — and the operations it composes across mounts (reads, watches, transactions)
answer in the same shapes. See [docd: Composition](../docd/composition.md).

You can speak it by hand:

```console
$ o system logd session localhost:7070
{hello: {clientId: probe}}
{result: {hello: {schemaCommit: 0 serverId: tcp-1 usingPending: false}}}
```

## Requests

Every request is an object with one operation field, and the operation's own fields sit
directly inside it:

| operation | shape |
|---|---|
| `hello` | `{hello: {clientId: <id>, scope: <scope>}}` |
| `match` | `{match: {path: <kpath>, data: <pattern>, commit: <n>}}` |
| `patch` | `{patch: {path: <kpath>, data: <value>, match: {path, data}, txId: <n>, timeout: "5s"}}` |
| `newtx` | `{newtx: {participants: <n>}}` |
| `watch` | `{watch: {path: <kpath>, fromCommit: <n>, noInit: <bool>}}` |
| `unwatch` | `{unwatch: {path: <kpath>, watchId: <id>}}` |
| `ping` | `{ping: {}}` |

`path` is in the same place in all of them, and **a request never has a `body`** — a
body is what a *response* carries, and it is the answer.

!!! warning "A request in the wrong shape is answered, not refused"

    A field the protocol does not recognise is ignored, and an unread `path` defaults
    to `""` — which is the whole document for a read and the document **root** for a
    write. So `{match: {body: {path: "a.b"}}}` reads everything and reports success,
    and a patch whose `path` is misspelled merges the client's data into the top of the
    document and answers with a commit.

    Strict rejection of unknown fields is not implemented; until it is, the shape
    above is the contract.

### id: synchronous or pipelined

An `id` on a request comes back on its response, so a client may keep several in
flight:

```tony
{id: r1, match: {path: verse.meta}}
{id: r2, patch: {path: verse.meta.rev, data: {n: 2}}}
```

**Responses may arrive out of order.** A server answers reads concurrently, so a read
issued before a write can be answered after it — pipelining without ids is how a client
loses track of which answer is which. The `id` is also the routing key for watch events,
which is what keeps several watches on one path apart.

Ordering that IS guaranteed: a request sent after its predecessor's response was
received happens after it, and a read dispatched after a write is dispatched after that
write committed — so read-your-writes holds for the usual write-then-read.

## Reading

```tony
{match: {path: verse.entities.e1}}
{result: {match: {body: {id: e1 status: ready} commit: 1}}}
```

`path` **restricts the read to that subdocument** — the answer is what lives at the
path, not the document with the path highlighted. `data`, when given, is a pattern the
state is matched and trimmed against *within* that path, so a caller can ask for the
shape it wants:

```tony
{match: {path: verse.entities.e1, data: {status: !irtype ""}}}
{result: {match: {body: {status: ready} commit: 1}}}
```

`commit` reads the state **as of a past commit** rather than the current one. It must
be in `[0, current]`; out of range is `commit_not_found`. Across docd this addresses
logd's single commit sequence, so a composed read at a commit is one consistent
snapshot.

Every answer carries the `commit` it was read at — which is also the store's head, and
therefore a revision a client can compare without asking for anything extra.

## Writing

```tony
{patch: {path: verse.entities.e1, data: {status: done}}}
{result: {patch: {commit: 2 data: {status: done}}}}
```

The result's `data` is the patch **as stored**, which is where a client learns a value
the server generated for it (see [Keyed arrays](keyed.md)).

A patch may carry a **compare-and-swap precondition** — it commits only if the state at
that path still matches:

```tony
{patch: {path: verse.entities.e1, data: {status: done},
         match: {path: verse.entities.e1, data: {status: ready}}}}
```

A precondition that does not hold answers `match_failed`, and nothing is written. What
a write must satisfy to be storable at all is [What a write must be](writes.md).

### Transactions

Several paths commit together by naming one transaction:

```tony
{hello: {clientId: probe}}
{id: t, newtx: {participants: 2}}
{id: t result: {newtx: {txId: 1}}}
{id: p1, patch: {txId: 1, path: verse.a, data: {n: 1}}}
{id: p2, patch: {txId: 1, path: verse.b, data: {n: 2}}}
{id: p1 result: {patch: {commit: 1 data: {n: 1}}}}
{id: p2 result: {patch: {commit: 1 data: {n: 2}}}}
```

The transaction commits when every participant has arrived; every precondition is
checked at that moment, and either all of them hold and the whole transaction commits,
or one fails and none of it is written. Both participants report the **same commit**.
`timeout` bounds one participant's wait. Across mounts, docd decomposes a patch spanning
several controllers into exactly this — see
[Multi-mount transactions](../docd/transactions.md).

!!! warning "Give the participants ids"

    A joining patch does not return until the whole transaction commits, so the
    participants must be **in flight together**. With `id`s they are: the client sends
    them all and matches the answers as they arrive. Without ids, a client that waits for
    the first answer before sending the second is waiting for a transaction that is
    waiting for it, and it fails on the transaction timeout.

    The participants may share one session (as above) or sit on separate ones — a
    multi-mount write through docd is the latter, one participant per controller.

**Across docd mounts**, a client's own transaction works as it does anywhere: mounts share
the commit sequence, so each participant is routed to its owning controller, which joins
that transaction on the one logd, and all of them report the same commit.

!!! warning "A participant may not span mounts"

    What a participant patch may *not* do is span mount boundaries itself. docd decomposes
    such a patch into one participant per mount — and a transaction's participant count was
    fixed when the client created it, counting its own patches rather than docd's
    decomposition of one of them. It is refused:

    ```tony
    {error: {code: invalid_tx message: "a patch inside a transaction may not span mounts:
      \"verse\" covers [verse.a verse.b] and the base; send one patch per mount as its own
      participant, and count them in newtx"}}
    ```

    A *stand-alone* patch spanning mounts is a different thing and needs no `newtx`: docd
    decomposes it into its own transaction, which is what
    [Multi-mount transactions](../docd/transactions.md) describes.

## Watching

```tony
{id: w1, watch: {path: verse.entities}}
{id: w1 result: {watch: {watching: verse.entities}}}
{event: {commit: 1 path: verse.entities state: {e1: {id: e1 status: ready}}} id: w1}
{event: {commit: 2 patch: {verse: {entities: {e2: {id: e2}}}} path: verse.entities} id: w1}
```

The first event is the **state** at the path; every event after it is the **delta of
one commit**, in commit order, with no gaps. A consumer that applies them in order
holds what the store holds.

!!! warning "The two event kinds are rooted differently"

    `state` carries the subtree **at the watched path**. `patch` carries a delta rooted
    at the **document**, as stored — above it is the untouched spine, `{verse:
    {entities: …}}` for a watch of `verse.entities`.

    So a delta cannot be applied to the state as given; a consumer either navigates the
    patch down to the watched path, or keeps a document-rooted copy. The store keeps the
    raw committed delta deliberately, because rewrapping it would lose operator fidelity
    for `!key` and friends — but the asymmetry is a trap, and it is a known one.

- `fromCommit` replays the exact delta history from that commit before streaming live,
  so a client that knows where it left off reconnects with no gap. The watch result then
  carries `replayingFrom` and `replayingTo` — the range being replayed — and a
  `replayComplete` event marks the end of it. Below the retained history an **absolute**
  `fromCommit` is `replay_compacted`: a client naming a commit is claiming to know where
  it was, and deserves to be told the history is gone.
- **A negative `fromCommit` is relative**: `-N` asks for *the last N commits*, resolved
  against the store's watermark at the moment the watch is established.

    ```tony
    {id: w1, watch: {path: verse.entities, fromCommit: -100}}
    {id: w1 result: {watch: {replayingFrom: 41 replayingTo: 141 watching: verse.entities}}}
    ```

    It is how a client asks for a window of history **without knowing where the store
    is** — no read, no ping, no arithmetic on a number it had to fetch first. Unlike an
    absolute cursor it is **clamped, not refused**: below the retained history it starts
    at the floor, and below zero at zero, because a request for a window is a request for
    what there is. `replayingFrom` says what it resolved to, so a client that was clamped
    can see that it was.

- `noInit` skips the initial state for a client that already has one.

**Across docd mounts.** Mounts share the commit sequence for their lifetime — docd
allocates a transaction id from logd, every participant commits through that one logd under
it, all-or-nothing — so a commit means the same thing to every mount and a cursor works on
a composed path too. docd resolves it once (a relative `-N` against the watermark, clamped
to the retained floor), reads the composed initial state at that commit, replays every
mount from it, and delivers the replayed deltas **in commit order** followed by a single
`replayComplete`.

What a composed watcher must account for is **membership**: a mount arriving or leaving
mid-watch ends the watch with `session_mounted` or `session_unmounted`, and the re-watch
composes the new membership — the composition changed, so deltas from before it describe a
different document.

## Liveness, and where the store is

```tony
{ping: {}}
{result: {pong: {commit: 52795}}}
```

A ping is answered by whichever server owns the connection — logd, or docd itself for a
client session — so a pong means **that server's request loop is alive**, which is what
a liveness probe is asking. It carries the head commit with it, so a client tracks the
store's revision from the heartbeat it already sends: no watch held open, no polling
read, nothing extra on the wire.

Through docd the number is docd's own high-water mark over everything it has told any
client. It is monotonic and it chases the head — a revision to **compare**, not a commit
to read at, since docd composes mounts with independent commit sequences.

## Scopes

`hello` fixes a copy-on-write scope for everything sent on that connection:

```tony
{hello: {clientId: verse, scope: sandbox-7}}
```

Reads then see baseline with the scope's own writes on top, and writes land in the
scope. Baseline keeps moving underneath — a scope is a live overlay, not a frozen
branch.

## Errors

```tony
{error: {code: not_found message: 'no value at "verse.nope": resolved through "verse", no field "nope"'}}
```

Branch on `code`, not on `message`. The first three are three facts about the PRESENT, and
none of them says anything about the future: in a mutable document `a.b[0]` resolves the
moment someone writes an array at `a.b`, exactly as `a.b.c` resolves the moment someone
writes an object at `a.b`. What separates them is what is there now.

| code | means |
|---|---|
| `not_found` | **nothing is there.** Nothing in the document contradicts the path, so creating what is missing is a reasonable next move |
| `path_conflict` | **something is there, of a shape that cannot hold what you asked for** — an index into an object, a field under a string. Creating here means clobbering what is already there, so the move is to re-examine the shape you assumed |
| `invalid_path` | **not a well-formed question** — a wildcard names a set of values and a read answers one |
| `match_failed` | a precondition did not hold; the write did not happen |
| `invalid_diff` | the delta would not apply to the state it would be stored against |
| `commit_not_found` | a historical read outside `[0, current]` |
| `replay_compacted` | `fromCommit` is below retained delta history |
| `slow_consumer` | a watch was dropped because the client did not keep up |
| `tx_full`, `tx_not_found`, `tx_scope_mismatch` | transaction membership |
| `controller_unavailable` | (docd) the controller owning that subtree is gone |
| `unsupported` | the responder does not implement that operation |

`timeout`, `session_closed` and `invalid_message` mean what they say.

## The mount protocol

Everything above is what a **client** sends. A **controller** — a process that owns a
subtree of the document — connects to docd's *mount* listener instead, and the shape of
the conversation inverts: after a short handshake, docd sends it session requests and it
answers them.

### Handshake

Two steps, both Tony documents:

```tony
{hello: {controller: "git-source"}}
{result: {hello: {docdId: "docd-1"}}}

{mount: {path: "verse.sources.git", schema: <schema>, forceAfter: "5s"}}
{result: {mount: {path: "verse.sources.git" accepted: true}}}
```

- `controller` names the process; docd answers with its own identity.
- `path` is the subtree this controller owns. Mounts are single-owner and may nest;
  routing resolves to the **deepest** mount covering a path. `.meta` is reserved.
- `schema` is the controller's contribution — chiefly which of its arrays are
  [keyed](keyed.md), which changes what a write to them *means*.
- `forceAfter` bounds how long the mount waits for overlapping watches to drain before
  force-ending them. A controller releases its subtree with
  `{unmount: {forceAfter: "5s"}}`.

Errors on this listener are `{error: {code: …, message: …}}`, same vocabulary.

### Then the session protocol, inverted

Once the mount is accepted, docd forwards every client `match`, `patch`, `watch` and
`unwatch` whose path falls at or under the mounted path — **as the same session
requests documented above** — and relays the controller's responses back to the client
that asked. A controller is therefore a *server* of this protocol, not a client of it:
it answers `{result: {match: …}}`, it emits `{event: …}` for a watch it is serving, and
it declines what it does not implement with `unsupported`.

Two differences from a client connection are worth knowing:

- **`id` is docd's, not the client's.** docd rewrites the id on the way out and maps the
  answer back, because many clients share one controller connection. Answer with the id
  you were given.
- **Scope rides the request.** A client's COW scope is fixed by its `hello`, but docd
  multiplexes many client sessions onto one controller connection, so per-connection
  scope cannot tell them apart. docd sets `scope` on each routed request instead; a
  scope-aware controller honours that field.

```tony
{id: "7", scope: "sandbox-3", match: {path: "verse.sources.git.repos"}}
{id: "7", result: {match: {body: {…} commit: 91}}}
```

### Clocks

A mount connection can also ask docd to serve a **virtual clock** rather than (or
alongside) a controller-backed subtree:

```tony
{hello: {controller: ticker, clock: {path: sys.clock, frequency: 1s, epoch: 0}}}
```

docd then serves `sys.clock` itself as a single monotonic int64 — `epoch + N ×
frequency` in nanoseconds at tick N, computed on demand, with no tick history kept.
Reads and watches of that path are answered by docd directly; it is read-only, and it
is docd's own, so logd knows nothing about it.

See [Mounts & routing](../docd/mounts.md) for the registry, tombstones and the `.meta`
namespace, and [Composition](../docd/composition.md) for what happens to a read or a
watch that spans several mounts.
