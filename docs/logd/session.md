# The session protocol

A session is one connection carrying a stream of **newline-delimited Tony documents**
in both directions. Every message a client sends names exactly one operation; every
message a server sends is a result, a watch event, or an error.

```tony
{hello: {clientId: verse, protocol: 3, author: verse}}
{patch: {path: verse.entities.e1, data: {status: ready}, author: alice}}
{match: {path: verse.entities.e1}}
{watch: {path: verse.entities}}
```

A client says which session protocol it speaks, and a server that speaks another refuses
the session at the handshake, naming both numbers -- a request field a server does not
know is ignored, so a mismatch that got past the handshake would be answered rather than
refused, and wrongly. This page describes **protocol 3**.

**docd speaks this protocol verbatim**, so a client written against logd talks to docd
unchanged — and the operations it composes across mounts (reads, watches, transactions)
answer in the same shapes. See [docd: Composition](../docd/composition.md).

You can speak it by hand:

```console
$ o system session localhost:7070
{hello: {clientId: probe}}
{result: {hello: {protocol: 3 schemaCommit: 0 serverId: tcp-1}}}
```

## Requests

Every request is an object with one operation field, and the operation's own fields sit
directly inside it:

| operation | shape |
|---|---|
| `hello` | `{hello: {clientId: <id>, protocol: 3, scope: <scope>, author: <principal>}}` |
| `match` | `{match: {path: <kpath>, data: <pattern>, commit: <n>, limit: <n>, cursor: <s>, return: <retspec>}}` — a wildcard path answers a [set](#reading-a-set) |
| `patch` | `{patch: {path: <kpath>, data: <value>, match: {path, data}, txId: <n>, timeout: "5s", author: <principal>}}` |
| `newtx` | `{newtx: {participants: <n>, timeout: "5m", author: <principal>}}` |
| `watch` | `{watch: {path: <kpath>, fromCommit: <n>, noInit: <bool>, waitIfAbsent: <bool>}}` |
| `unwatch` | `{unwatch: {path: <kpath>, watchId: <id>}}` |
| `schema` | `{schema: {get: {at: <n>}}}` reads the schema in force (at a commit); `{schema: {set: {schema: <doc>, force: <bool>}}}` sets it, as one commit |
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

A read at a commit reads the document **under the schema in force at that commit**, not
today's: which arrays are [keyed](keyed.md), and by what, is that commit's, so the shape of
an array and the path that names one of its elements are both as they were. If `runs` was
keyed by `id` then and is not now, `{match: {path: "runs(r1)", commit: N}}` reads element
`r1`, and the same path at the head is `invalid_path`. Since a path is judged against the
commit it reads, an out-of-range commit is refused first.

Every answer carries the `commit` it was read at — which is also the store's head, and
therefore a revision a client can compare without asking for anything extra.

### Reading a set

A path holding a wildcard — `.*`, `[*]`, `{*}`, `(*)`, at **any** segment — names a set
of nodes, and the answer is the set, **one node at a time**:

```tony
{id: "7", match: {path: "jobs.*"}}
{id: "7", result: {match: {path: jobs.a1, body: {status: done} commit: 91}}}
{id: "7", result: {match: {path: jobs.a2, body: {status: ready} commit: 91}}}
{id: "7", result: {match: {commit: 91 done: true}}}
```

Each member carries its own `path`, and the same `commit`: the set is one snapshot. The
`done` marker ends it. A member's path is the store's own spelling, so it is a path to
read, patch or watch on its own — a keyed array's elements come back as
`runs."(id=r1)"`, since `(*)` names them by identity and `[*]` names nothing there.

Nothing is gathered into one body: a container of ten thousand is not a document anyone
wants built at either end, and the paths are half the answer.

- `data` is matched and trimmed against **each member separately**, and a member it
  rejects is not sent. "Every job that is done, just its status" is one request.
- a wildcard that meets a container of another kind reaches nothing, and a branch where
  the rest of the path finds nothing contributes nothing. That is a non-match, not an
  error — the same rule `o list` follows walking a document.
- an **empty set is the marker alone**. A query for a set answers with a set, and empty
  is one; `not_found` keeps its meaning for a path that names one place.
- `..` is **not** a read: it names nodes at any depth, and is refused here as everywhere
  a path must name a place.

#### `return`: what an answer carries

`return` is a comma-separated retspec naming the result's own fields — `path`, `id`,
`iterType`, `body`. A spec lists what comes back, so there is nothing to translate, and
the names that follow (an author, the commit a node last changed at) join it the same
way. A set answers `"path,body"` unless asked otherwise.

`path`, `id` and `body` say a node three ways:

```tony
{id: "3", match: {path: "jobs.*", return: path}}
{id: "3" result: {match: {path: jobs.a1 commit: 91}}}
{id: "3" result: {match: {commit: 91 done: true}}}

{id: "4", match: {path: "jobs.*", return: "id,body"}}
{id: "4" result: {match: {id: a1 body: {status: done} commit: 91}}}
{id: "4" result: {match: {commit: 91 done: true}}}
```

- **`path`** is where the node is, whole — what the next read or write is addressed by.
- **`id`** is the name it lives under in its parent, as a **value**: `a1` for a field,
  `0` for a position, `7` for a sparse key, and `r1` for an element of a keyed array —
  what it is addressed *by*, not the `"(id=r1)"` the store spells its field with. A
  caller that asked `jobs.*` knows the rest, so `"id,body"` is the listing without the
  prefix repeated on every member. What *addresses* a node is its `path`: an id is not a
  path segment, and a position is not an identity.
- **`body`** is what is there. **`return: body` alone answers nodes nobody can tell
  apart**, which is what a cumulative read wants — summing, counting, measuring — and
  what nothing else should ask for.

**`iterType`** says what kind of node it is, in the terms a client lists by — so what is
under it, and which wildcard reaches it:

| `iterType` | children listed by |
|---|---|
| `Object` | `.*` |
| `SparseArray` | `{*}` |
| `Array` | `[*]` |
| `KeyedArray` | `(*)` |
| `String`, `Number`, `Bool`, `Null` | — |

```tony
{id: "5", match: {path: "*", return: "path,iterType"}}
{id: "5" result: {match: {path: jobs iterType: Object commit: 91}}}
{id: "5" result: {match: {path: runs iterType: KeyedArray commit: 91}}}
{id: "5" result: {match: {commit: 91 done: true}}}

{id: "6", match: {path: "runs(*)", return: "path,iterType"}}
```

The member's path with its wildcard appended is the next set to ask for, so
`"path,iterType"` is the walk a client browsing the store makes, a level at a time.

It is **not the IR's type**. A sparse array is an `Object` in the IR, tagged
`!sparsearray`; a keyed array is an `Array` to a client and an object of names in the
store. Neither type says how to list one, and `[*]` over a keyed array names nothing — so
the four containers are four names. Which arrays are keyed is the schema's word **at the
commit read**, and an element of a keyed array is whatever the element is, not a
`KeyedArray`.

A spec with no `body` **reads no node at all** when there is no pattern: the walk that
finds the members already knows their names, so "which jobs are there?" over ten
thousand costs the walk rather than ten thousand reads. `iterType` reads the first event
of each member — how a node begins is what it is — and builds nothing. A path the walk
*named* rather than found — anything after the wildcard, as in `jobs.*.status` — is
settled by a presence check, so what is not there is not answered. With a pattern the nodes are still
read, because the pattern has to see them; what is saved then is the bodies on the wire.

A name this server does not know — `return: "path,author"` — is `unsupported`, said
rather than ignored, so a client asking a later server for more is never answered with
less and told nothing.

For a path that names **one** node the default is `body`, since the caller already has
the path; `return: path` there is an existence question, answered by the path alone or
by `not_found`, and `return: iterType` is the same question answered with the kind.

**Across docd**, a read docd passes through to logd — no mount at or beneath its path —
answers its retspec as logd does. Past a mount it cannot: a controller answers a body,
and a read composed across mounts is a body docd assembles. There a spec asking for more
than `body` is `unsupported`, said rather than answered with the body alone.

A wildcard anywhere else — a `patch`, the `match` precondition a patch carries, a
`watch` — is `invalid_path`. Those need one node, and a set is not one.

### Paging a set

`limit` bounds a page, and the marker carries a `cursor` when the set goes on:

```tony
{id: "8", match: {path: "jobs.*", limit: 2}}
{id: "8", result: {match: {path: jobs.a1, body: {…} commit: 91}}}
{id: "8", result: {match: {path: jobs.a2, body: {…} commit: 91}}}
{id: "8", result: {match: {commit: 91 done: true cursor: "…"}}}

{id: "9", match: {path: "jobs.*", cursor: "…"}}
```

**The marker is the authority, not the count**: a page shorter than `limit` does not
mean the set ended, because the server caps a page at its own size. A marker with no
cursor is the end.

A cursor names the commit the set is being read at and how far the read got, so a
continuation reads *that* commit: a write between two pages does not change what the
second page answers, and no page straddles two states. A cursor whose commit has aged
out of range is `commit_not_found`, and one sent with a different `path` than the read
it came from is `invalid_path`. It is **opaque** — read it back to the server rather
than reading it.

**Across docd**, a set no mount is near passes through to logd and is answered as logd
answers it. A set that crosses a mount — a member at, under or above one — is
`unsupported`: its members live in more than one place, and docd does not compose one.
A path naming one node is unaffected.

**A read answers `null` only where a null was written.** A path holding nothing is
`not_found`, at every depth, whether or not an ancestor of it resolves — and on a store
where nothing has been written that is true of every path, the empty one included:

```tony
{match: {path: verse.a}}
{error: {code: not_found message: 'no value at "verse.a": no field "verse" at the document root'}}

{match: {path: ""}}
{error: {code: not_found message: 'no value at "": the store is empty'}}
```

So `null` means one thing. It used to mean two — a written null, and a path nobody had
written to — and which one a caller got depended on whether some *ancestor* happened to
resolve, so no client could recover the distinction from the answer.

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

A precondition that does not hold answers `match_failed`, and nothing is written. It
reads what its pattern names, not the whole value at its path (see
[Conditions on writes](index.md#conditions-on-writes)). What a write must satisfy to be
storable at all is [What a write must be](writes.md).

### Who wrote it

A commit records its **author**: a string the caller chooses, its principal, which logd
stores beside the commit's timestamp and never interprets or checks. A write's author is
the `author` on the patch; without one it is the `author` on the session's `hello`;
without that the write has none.

```tony
{hello: {clientId: verse, protocol: 3, author: verse}}
{patch: {path: verse.entities.e1, data: {status: done}}}                 # written by verse
{patch: {path: verse.entities.e2, data: {status: done}, author: alice}}  # written by alice
```

A client with one principal says it once, in `hello`. A server multiplexing many
principals onto one session says each on the patch, per write. Every delta event a watch
delivers for the commit carries it (see [Watching](#watching)), live and replayed alike,
so a reader learns who wrote what without a second lookup.

**A transaction has one author, and it is `newtx`'s.** The author on `newtx`, else the
one on the session's `hello`, is the transaction's, and every participant inherits it --
whatever session the participant arrives on. A participant does not name an author: a
joining patch that carries one is refused with `invalid_tx` rather than having the one
field that exists to be kept quietly dropped. So there is no such thing as a transaction
of mixed principals, by construction rather than by a check at each join.

```tony
{id: t, newtx: {participants: 2, author: alice}}
{id: p1, patch: {txId: 1, path: verse.a, data: {n: 1}}}   # written by alice
{id: p2, patch: {txId: 1, path: verse.b, data: {n: 2}}}   # written by alice
```

The author is what the caller says it is: logd stores it and does not authenticate it.
Whoever stands in front of logd and stamps principals is trusted for the stamp.

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

A transaction waits for its participants for its timeout: the one `newtx` names, or the
server's (`tx.timeout` in logd's config, 5m unless configured). The server's is also
the most a `newtx` may ask for; a longer one is refused with `invalid_tx`. Past it the
transaction fails and every participant still waiting is answered. There is no
transaction without a timeout — a `tx.timeout` of 0 is the 5m default. A patch's
`timeout` bounds that one participant's wait; a participant which names none waits
the transaction's. A participant answered `timeout` has **withdrawn**: its patch is
not part of the transaction, which goes on waiting for the participant it is short,
and a retry rejoins it. Once every participant has arrived the commit is under way and
a participant's `timeout` no longer applies: it is answered with the commit.

Across mounts, docd decomposes a patch spanning several controllers into exactly this —
see [Multi-mount transactions](../docd/transactions.md).

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
{event: {author: alice commit: 2 patch: {e2: {id: e2}} path: verse.entities} id: w1}
```

The first event is the **state** at the path; every event after it is the **delta of
one commit**, in commit order, with no gaps. A consumer that applies them in order
holds what the store holds. A delta event carries the commit's `author` when the write
named one ([Who wrote it](#who-wrote-it)); the state event has none, being the fold of
many commits.

!!! note "Both event kinds are rooted at the watched path"

    `state` carries the value **at the watched path**, and every `patch` after it is a
    delta **of that value**, rooted at the same place: apply the patches in order to the
    state, with the fold the store uses (`api.NextState`, so comments count the same on
    both sides), and you hold what the store holds at the path. Nothing has to be
    navigated or re-rooted, and a consumer *applies* a delta rather than reading its
    surface: the store may lower an operation to the result it produced (see [What a
    write must be](writes.md)), so the shape that arrives is what happened, not what was
    written.

    A null in `state` or `patch` is a null the path holds. A path that holds nothing is
    said by **`absent: true`** on the event -- the first event of a watch that asked to
    wait, or a delta that removed the path, which is still delivered so that applying it
    is how a client's own copy comes to hold nothing.

- `fromCommit` replays the exact delta history from that commit before streaming live,
  so a client that knows where it left off reconnects with no gap. The watch result then
  carries `replayingFrom` and `replayingTo` — the range being replayed — and a
  `replayComplete` event marks the end of it. Below the retained history an **absolute**
  `fromCommit` is `replay_compacted`: a client naming a commit is claiming to know where
  it was, and deserves to be told the history is gone.

    The replay is **streamed**, not collected: deltas go out as the range is read, so the
    server holds one entry rather than the whole range however wide the catch-up. A
    consumer that cannot keep up is failed at the watch's own buffer, which is the
    existing contract — the server does not hold the range on its behalf.
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
- `waitIfAbsent` asks to watch a path that **holds nothing yet**. Without it such a watch
  is refused with `not_found`, for the same reason a read of that path is: a watch that
  delivered null would say what a read says, and then "watch this, it will appear" and
  "watch this, I have the path wrong" would be one request with one outcome. With it, the
  watch is established, the first event says `absent: true`, and the value is reported
  when it arrives.

    Waiting is the ordinary way to start watching something a peer has not created yet, so
    a client doing that says so:

    ```tony
    {id: w1, watch: {path: verse.entities.e9, waitIfAbsent: true}}
    ```

    It has to reach whoever serves the path. docd carries it to the controller owning that
    subtree, and a controller serving from its own logd session passes it on — a hop that
    drops it refuses a watch the client asked to wait for. A composed watch reports
    `not_found` only when *every* source is absent.

    The refusal arrives **before the `watch` confirmation**, in place of it. That is what
    lets a caller decide something on the answer: an HTTP endpoint bridging a watch to an
    event stream has already committed its status code by the time a later failure could
    arrive, so a refusal after the confirmation cannot become a 404. Waiting a moment for
    one does not work either — with `noInit` a path that exists and is quiet sends nothing,
    so there is no signal to wait for at any duration.

    It asks whether the path holds anything **now**, not at the commit a `fromCommit`
    replay starts from. A client replaying history is asking about the path it is
    resuming; absence back at the cursor is history, which the replay then plays forward.

**Across docd mounts.** Mounts share the commit sequence for their lifetime — docd
allocates a transaction id from logd, every participant commits through that one logd under
it, all-or-nothing — so a commit means the same thing to every mount and a cursor works on
a composed path too. docd resolves it once (a relative `-N` against the watermark, clamped
to the retained floor), reads the composed initial state at that commit, replays every
mount from it, and delivers the replayed deltas **in commit order** followed by a single
`replayComplete`.

A watch that has been confirmed always ends with a terminal **event**, never an error
response — the request it came from finished when the watch opened, so an error routed by
that id matches nothing in flight. The event carries `endReason`, a code from the ErrCode
vocabulary, and `endMessage`, which is where the numbers live: `replay_compacted` says the
cursor is gone, and only the message says from which commit the store can still serve.

What a composed watcher must account for is **membership**: a mount arriving or leaving
mid-watch ends the watch with `session_mounted` or `session_unmounted`, and the re-watch
composes the new membership — the composition changed, so deltas from before it describe a
different document.

**A change of keying ends the watches over the array.** A schema commit that gives an array
an identity, takes it away, or keys it by other fields ends every watch overlapping that
array — at it, under it, or above it, the root included — with `keying_changed`, before
anything of the commit is delivered. The alternative was to hand the watcher the rewrite,
and it would read wrong: an element that was renamed arrives as a delete, a path stops
naming anything, and the array's elements are addressed another way from then on. A watch
replaying across such a commit delivers what comes before it and then ends the same way;
one whose path did not name the same place before the change ends without sending anything.

The ending's `commit` is the **schema commit**, not the last one delivered, and it is where
to watch again from: its state is the first under the new keying, and a watch resumed from
earlier would cross the change again. Take the state (not `noInit`) — what you hold is keyed
the old way — and spell the path as the schema now does:

```tony
{id: "w", watch: {path: "runs(r1)"}}
…
{id: "w" event: {path: 'runs."(id=r1)"' commit: 88 ended: true endReason: keying_changed
  endMessage: 'the keying of "runs" changed at commit 88 (keyed by sku, was by id): …'}}

{id: "w2", watch: {path: "runs(A)", fromCommit: 88}}
```

A schema commit that changes no array's keying ends nothing.

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
client — reads it answered, writes it reported, watch events it forwarded. Mounts share
the commit sequence, so the number names a real point in it; what it is not is the
**head**, since docd only learns of a commit by handling it. It is monotonic, it chases
the head, and it is a lower bound on it.

## Scopes

`hello` fixes a copy-on-write scope for everything sent on that connection:

```tony
{hello: {clientId: verse, scope: sandbox-7}}
```

Reads then see baseline with the scope's own writes on top, and writes land in the
scope. Baseline keeps moving underneath — a scope is a live overlay, not a frozen
branch.

A session says `hello` once. A second `hello` is refused, `hello_repeated`: the scope
and author a session's watches and transactions answer for are fixed by the one it said,
and another scope or author is another connection. A `hello` refused for its protocol
was not said, and the client says it again with one the server speaks.

## Retention

A `retain` request ages log-like records out of the state. It is a write: for each
rule in `what`, the items of the rule's container whose own timestamp at `age` is older
than `after`, and whose `match` holds, are deleted in commits of at most `batch` items,
each under a precondition on what was read.

```tony
{retain: {now: "2026-09-12T08:00:00Z", what: [{path: jobs.*, match: {status: done}, age: .updatedAt, after: 1d}]}}
{result: {retain: {now: "2026-09-12T08:00:00Z", commit: 4127, deleted: 300, rules: [{path: jobs.*, deleted: 300}]}}}
```

logd holds no policy and no clock for it: the caller carries the rules and the time, and
the commit is the record that it ran. `now` is optional, and the result says what was
used. See [Retention](retention.md) for what a rule may name, why age is read from
the record, and what is refused.

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
| `invalid_path` | **not a well-formed question** — `..` names nodes at any depth, a wildcard where a path must name a place (a write, a watch; a read answers a [set](#reading-a-set)), and an element named by a key the array does not have — for a read, under the schema of the commit it reads |
| `match_failed` | a precondition did not hold; the write did not happen |
| `invalid_diff` | the delta would not apply to the state it would be stored against, or the schema's keying refuses it — an element without a name, a position on a keyed array, a name where there is no identity |
| `commit_not_found` | a historical read outside `[0, current]` |
| `replay_compacted` | `fromCommit` is below retained delta history |
| `slow_consumer` | a watch was dropped because the client did not keep up |
| `keying_changed` | a watch ended because a schema commit changed the keying of an array at, under or above its path; watch again from the commit it names |
| `tx_full`, `tx_not_found`, `tx_scope_mismatch` | transaction membership |
| `invalid_tx` | a transaction asked for more than the server allows, or a participant named an `author` (a participant inherits the transaction's) |
| `invalid_retain` | a [retain](retention.md) request that cannot mean what it says — a rule naming one node or a dense array, an age that is not a field path, a duration that is not one — or whose rule disagrees with the schema's keying |
| `controller_unavailable` | (docd) the controller owning that subtree is gone |
| `unsupported` | the responder does not implement that operation, or cannot answer what the request asks: a `return` name it does not know, or more than a body past a mount |

`timeout`, `session_closed` and `invalid_message` mean what they say.

**Across a hop.** A code describes either the *document* or the *connection*, and only the
first kind survives being passed on. When a controller answers for its subtree, the codes
above about the document — `not_found`, `path_conflict`, `invalid_path`, `invalid_diff`,
`match_failed`, `commit_not_found` — reach the client as the controller reported them,
because they are as true for the client as they were for the controller. So does
`invalid_tx`, which is about the request the client wrote and the controller relayed.

The ones about a connection do not travel: the controller's session closing is not the
client's session closing, and a downstream calling the controller's message invalid is the
controller's bug rather than the client's. A controller failing for a reason it did not
classify reads as `storage_error` — the responder could not do it — and never as
`invalid_message`, which would tell a client to rewrite a request that was fine.

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
- **Scope rides the request, and so does the author.** A client's COW scope and default
  author are fixed by its `hello`, but docd multiplexes many client sessions onto one
  controller connection, so per-connection state cannot tell them apart. docd sets
  `scope` on each routed request instead, and resolves the client's author onto each
  routed stand-alone `patch`; a scope-aware controller honours the scope, and a
  controller that writes to logd carries the author to that write. A routed participant
  (`txId` set) carries none: it inherits the transaction's, which the client's `newtx`
  fixed on logd.

```tony
{id: "7", scope: "sandbox-3", match: {path: "verse.sources.git.repos"}}
{id: "7", result: {match: {body: {…} commit: 91}}}
{id: "8", scope: "sandbox-3", patch: {path: "verse.sources.git.repos.r1", data: {…}, author: alice}}
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
is docd's own, so logd knows nothing about it. A clock is not in the commit sequence:
its match results and watch events carry `commit: 0`, and the value is only ever the
state. The clock lives as long as the mount connection that asked for it; when that
closes, docd removes it and ends every watch on it with `session_unmounted`.

See [Mounts & routing](../docd/mounts.md) for the registry, tombstones and the `.meta`
namespace, and [Composition](../docd/composition.md) for what happens to a read or a
watch that spans several mounts.
