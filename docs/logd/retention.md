# Retention: ageing log-like records out

A store that holds log-like data — jobs, runs, events, anything written once and
then only read — grows without bound, and [compaction](compaction.md) does not help:
compaction removes the *memory* of how the state was reached, never the state. A
record that nobody deletes is state. Retention is what deletes it.

Retention is a **request**, and the request is a **write**. A client sends the rules
and the time; logd reads each rule's container, finds the items whose own timestamp
is older than the rule allows, and commits an ordinary delete for them. logd holds no
retention policy and no clock for it. The caller carries both, and the commit is the
record that it ran — so whoever drives it, a controller on its own schedule, an
operator by hand, a cron, sees the commit it caused, and a pass given an explicit
`now` is a function of the state and the request, reproducible from the log.

Everything that is true of a client's delete is true of these:

- watchers see them as deltas, carrying the request's author;
- a read at an older commit still shows the records;
- the schema still applies, so a delete it forbids fails visibly instead of leaving a
  broken snapshot;
- each batch carries a precondition on what the pass read, so a writer that touches a
  record between the read and the delete makes that batch fail rather than lose a
  write.

## The request

```tony
{retain: {
  now: "2026-09-12T08:00:00Z"        # optional; the server's clock when absent
  batch: 256                         # optional; the most items one delete commit removes
  author: verse/retention            # optional; the session's when absent
  what:
  - path: jobs.*
    match: {status: !or [done, canceled]}
    age: .updatedAt
    after: 1d
  - path: events(*)
    age: .at
    after: 1y
}}
```

```tony
{result: {retain: {
  now: "2026-09-12T08:00:00Z"
  commit: 4127
  deleted: 312
  rules:
  - {path: jobs.*, deleted: 300, unreadable: 2}
  - {path: events(*), deleted: 12}
}}}
```

| field | what it says |
|---|---|
| `now` | the time ages are measured against, RFC3339; the server's clock when absent, and the result says which was used |
| `batch` | the most items one delete commit removes; 256 when absent |
| `author` | what the deletes are recorded as written by, as a patch's `author`; the session's when absent |
| `what` | the rules, at least one |

And a rule:

| field | what it says |
|---|---|
| `path` | the items: a path whose **last** segment is the wildcard of the container's kind, and whose other segments are concrete |
| `match` | optional; an object pattern in the [match language](../matchpatch.md) the item must match to expire |
| `age` | the field path, inside the item, of the RFC3339 timestamp its age is read from |
| `after` | how old an item may be: it expires when `now - timestamp >= after` and `match` holds |

An item is deleted **whole**, never a field inside it. An item with no parseable
timestamp at `age` is kept, and counted as `unreadable` in the result. A batch whose
precondition no longer held is counted as `skipped`: the state moved under the pass,
and asking again reads it afresh.

The request runs on the loop, as a plain patch does, so a read pipelined behind it
sees what it deleted. In a scoped session it reads the scope's view and deletes in the
scope; baseline keeps the record.

### Through docd

[docd](../docd/index.md) routes each rule to the owner of its container — logd for a
base path, the controller whose mount holds it otherwise — in one request per owner,
with one `now` for all of them, and answers the rules in the order they were sent.
Each rule then also says:

| field | what it says |
|---|---|
| `owner` | who ran it: `logd`, or the mount path of the controller |
| `under` | the mounts beneath the rule's container, which the rule did **not** reach |
| `error` | what the owner said when it refused the rule |

Nothing is composed across the mounts beneath a container: a controller answers for
its subtree, and one that does not implement `retain` says `unsupported`, which the
result reports for that rule. An error is answered for the request only when nothing
ran anywhere.

```tony
{result: {retain: {now: "2026-09-12T08:00:00Z", commit: 4127, deleted: 300, rules: [
  {path: jobs.*, deleted: 300, owner: logd}
  {path: verse.ctl.runs.*, owner: verse.ctl, error: {code: unsupported, message: "unsupported operation: request type"}}
  {path: verse.*, deleted: 0, owner: logd, under: [verse.ctl]}
]}}}
```

### What the last segment may be

| segment | items are | example |
|---|---|---|
| `.*` | the fields of an object | `jobs.*` |
| `{*}` | the entries of a sparse array | `events{*}` |
| `(*)` | the elements of a [keyed array](keyed.md) | `runs(*)` |

A dense array, `[*]`, is **refused**. An index names a position, not an element: a
concurrent insert or delete before that index lands the expiry on a neighbour, and
every expiry shifts every later positional watch. This is the case the keyed-array page
already makes for any durable array two writers touch. Declare the array keyed —
`!logd-key` or `!logd-auto-id` in the schema — and write the rule with `(*)`. Items
must be objects anyway, since they carry a timestamp, and `!logd-auto-id` generates the
key for a producer that has none.

The rule and the schema have to agree: `runs.*` over an array the schema keys is
refused, naming `runs(*)`, and `list(*)` over an array the schema gives no identity is
refused the same way.

`..` is refused, as it is [everywhere a path must name a place](../objpath.md#where--may-not-go).
A wildcard anywhere but the last segment is refused too: a rule is about the children
of **one** container, which is what makes "delete the item whole" mean one thing.

### Why age comes from the record

The rule requires the timestamp field rather than asking the store how old a record
is, and that is the point rather than a limitation. Items are assumed to be log-like,
so they carry their own timestamp; read from the record, age is exact, depends on no
compaction setting, and needs no new metadata in the store. The store cannot answer
the question itself: once compaction drops a record's delta past the cutoff, the index
no longer knows when it was last written, and with a one-hour cutoff and a one-year
limit that is exactly the records a rule is for.

Age is not written as a match operator. Match tests structure and equality and has no
ordering, and it is also the language of preconditions, which have to be repeatable;
a clock-dependent predicate would be neither.

### Clocks

The timestamp is the writer's and `now` is the caller's, or the server's. A record
from a clock that runs fast expires early by the skew, and a record dated in the
future never expires until `now` passes it. For limits in days and years that is
noise; for a one-hour rule it is real, and the remedy is to send `now` from the same
clock the records are stamped with. logd's own commit timestamps are never read by a
retain, so the two cannot disagree.

### The precondition

A batch's delete asserts, for each item, the timestamp the pass read and the fields
the rule's `match` names, combined into one object pattern. That is why `match` has
to be an object — `{status: !or [done, canceled]}` rather than `!or [...]` at the item
— and why it may not name the `age` field itself.

### Durations

`after` is written the way a duration is written — `1h`, `90m`, `30s` — plus the
units a retention limit is asked for in: `1d` is 24h, `2w` is 14d and `1y` is 365d,
calendar-blind, and they mix with the rest (`1y6w`, `1d12h`).

## What the pass does

1. Reads the container as of the current commit, in the session's view, under the
   configured read budget. A container larger than the budget is a `storage_error`;
   raise `storage.readBudget` or split the collection.
2. For each item: reads the timestamp at `age`; keeps the item if it is missing or will
   not parse, or if `now - timestamp < after`, or if `match` does not hold.
3. Deletes the rest in commits of at most `batch` items, each under its precondition.

Rules run in order, and an error in one answers for the request: the rules before it
have run and their deletes are committed, which the error says.

## What it does not do

- **It does not run on its own.** There is no timer in logd. A store nobody asks
  retains everything, and the ask is the caller's clock, not logd's.
- **It does not free disk on its own.** A delete removes the record from the current
  state, but the record survives in every older root snapshot the tier policy keeps,
  and some snapshot from every era survives indefinitely. Set
  [`compaction.horizon`](compaction.md#the-knobs) to bound how far back any history
  survives; past it the deleted record is gone from the store.
- **It does not read a timestamp that is not RFC3339.** A numeric epoch is kept, not
  expired.
- **It does not look inside `!raw`.** A raw subtree is a document the store carries
  rather than one it owns.

## Errors

A request that cannot mean what it says is refused whole, `invalid_retain`, before any
rule runs: a `now` that is not a time; no rules; a path that names one node, or a
dense array, or a wildcard in the middle; an `age` that is not a field path; an
`after` that is not a positive duration; a `match` that is not an object or that names
the age field; a negative `batch`. A rule that disagrees with the store it meets — the
schema's keying, a dense array where the rule said elements — is `invalid_retain` too,
when it runs.
