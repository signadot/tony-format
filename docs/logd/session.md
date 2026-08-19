# The session protocol

A session is one connection carrying a stream of **newline-delimited Tony documents**
in both directions. Every message a client sends names exactly one operation; every
message a server sends is a result, a watch event, or an error.

```tony
{hello: {clientId: verse}}
{patch: {path: "verse.entities.e1", data: {status: ready}}}
{match: {path: "verse.entities.e1"}}
{watch: {path: "verse.entities"}}
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
{id: "r1", match: {path: "verse.meta"}}
{id: "r2", patch: {path: "verse.meta.rev", data: {n: 2}}}
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
{match: {path: "verse.entities.e1"}}
{result: {match: {body: {id: e1 status: ready} commit: 1}}}
```

`path` **restricts the read to that subdocument** — the answer is what lives at the
path, not the document with the path highlighted. `data`, when given, is a pattern the
state is matched and trimmed against *within* that path, so a caller can ask for the
shape it wants:

```tony
{match: {path: "verse.entities.e1", data: {status: !irtype ""}}}
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
{patch: {path: "verse.entities.e1", data: {status: done}}}
{result: {patch: {commit: 2 data: {status: done}}}}
```

The result's `data` is the patch **as stored**, which is where a client learns a value
the server generated for it (see [Keyed arrays](keyed.md)).

A patch may carry a **compare-and-swap precondition** — it commits only if the state at
that path still matches:

```tony
{patch: {path: "verse.entities.e1", data: {status: done},
         match: {path: "verse.entities.e1", data: {status: ready}}}}
```

A precondition that does not hold answers `match_failed`, and nothing is written. What
a write must satisfy to be storable at all is [What a write must be](writes.md).

### Transactions

Several paths commit together by naming one transaction:

```tony
{newtx: {participants: 2}}
{result: {newtx: {txId: 4}}}
{patch: {txId: 4, path: "verse.a", data: {n: 1}}}
{patch: {txId: 4, path: "verse.b", data: {n: 2}}}
```

The transaction commits when every participant has arrived; every precondition is
checked at that moment, and either all of them hold and the whole transaction commits,
or one fails and none of it is written. `timeout` bounds one participant's wait. Across
mounts, docd decomposes a patch spanning several controllers into exactly this — see
[Multi-mount transactions](../docd/transactions.md).

## Watching

```tony
{id: "w1", watch: {path: "verse.entities"}}
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
    {id: "w1", watch: {path: "verse.entities", fromCommit: -100}}
    {id: w1 result: {watch: {replayingFrom: 41 replayingTo: 141 watching: verse.entities}}}
    ```

    It is how a client asks for a window of history **without knowing where the store
    is** — no read, no ping, no arithmetic on a number it had to fetch first. Unlike an
    absolute cursor it is **clamped, not refused**: below the retained history it starts
    at the floor, and below zero at zero, because a request for a window is a request for
    what there is. `replayingFrom` says what it resolved to, so a client that was clamped
    can see that it was.

- `noInit` skips the initial state for a client that already has one.

!!! note "A composed watch cannot replay"

    A watch on a path spanning several docd mounts multiplexes backends with independent
    commit sequences, so no single commit can resume them: `fromCommit` — absolute or
    relative — is not honoured, and the watch re-initializes at current state instead.
    The confirmation then carries no replay range at all, which is how a client tells,
    and docd logs it.

### A watch ends rather than skipping

If a watch cannot keep that promise it **ends**, and says so, instead of quietly
missing an event:

```tony
{event: {commit: 57 ended: true endReason: slow_consumer path: verse.entities} id: w1}
```

The `commit` on a terminal event is the last one delivered, so the client re-watches
with `fromCommit` set to it. `endReason` comes from the error vocabulary below —
`slow_consumer` when the client fell behind, `session_mounted` / `session_unmounted`
when docd's mount set moved under a composed watch, `controller_unavailable` when the
controller owning a mounted subtree went away.

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
{hello: {clientId: verse, scope: "sandbox-7"}}
```

Reads then see baseline with the scope's own writes on top, and writes land in the
scope. Baseline keeps moving underneath — a scope is a live overlay, not a frozen
branch.

## Errors

```tony
{error: {code: not_found message: 'no value at "verse.nope": resolved through "verse", no field "nope"'}}
```

Branch on `code`, not on `message`. The distinctions worth knowing:

| code | means |
|---|---|
| `not_found` | the path holds nothing — an answer, not a failure |
| `invalid_path` | the path cannot address anything (a segment names a non-field, or it does not parse) |
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
{hello: {controller: "ticker", clock: {path: "sys.clock", frequency: "1s", epoch: 0}}}
```

docd then serves `sys.clock` itself as a single monotonic int64 — `epoch + N ×
frequency` in nanoseconds at tick N, computed on demand, with no tick history kept.
Reads and watches of that path are answered by docd directly; it is read-only, and it
is docd's own, so logd knows nothing about it.

See [Mounts & routing](../docd/mounts.md) for the registry, tombstones and the `.meta`
namespace, and [Composition](../docd/composition.md) for what happens to a read or a
watch that spans several mounts.
