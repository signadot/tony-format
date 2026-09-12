# Retention: ageing log-like records out

A store that holds log-like data — jobs, runs, events, anything written once and
then only read — grows without bound, and [compaction](compaction.md) does not help:
compaction removes the *memory* of how the state was reached, never the state. A
record that nobody deletes is state. Retention is what deletes it.

Retention is a **writer**. On a timer, the server reads each rule's container, finds
the items whose own timestamp is older than the rule allows, and commits an ordinary
delete for them. Nothing in the storage layer knows it exists, and everything that is
true of a client's delete is true of these:

- watchers see them as deltas, carrying the retention author;
- a read at an older commit still shows the records;
- the schema still applies, so a delete it forbids fails visibly instead of leaving a
  broken snapshot;
- each delete carries a precondition on what the pass read, so a writer that touches a
  record between the read and the delete makes that batch fail rather than lose a
  write.

## A rule

```tony
retention:
  every: 1h                  # how often the rules run; default 1h
  batch: 256                 # the most items one delete commit removes; default 256
  author: logd/retention     # what the deletes are recorded as written by; default
  rules:
  - path: jobs.*
    match: {status: !or [done, canceled]}
    age: .updatedAt
    after: 1d
  - path: events(*)
    age: .at
    after: 1y
```

| field | what it says |
|---|---|
| `path` | the items: a path whose **last** segment is the wildcard of the container's kind, and whose other segments are concrete |
| `match` | optional; an object pattern in the [match language](../matchpatch.md) the item must match to expire |
| `age` | the field path, inside the item, of the RFC3339 timestamp its age is read from |
| `after` | how old an item may be: it expires when `now - timestamp >= after` and `match` holds |

An item is deleted **whole**, never a field inside it. An item with no parseable
timestamp at `age` is kept, and the pass says so once in the log.

### What the last segment may be

| segment | items are | example |
|---|---|---|
| `.*` | the fields of an object | `jobs.*` |
| `{*}` | the entries of a sparse array | `events{*}` |
| `(*)` | the elements of a [keyed array](keyed.md) | `runs(*)` |

A dense array, `[*]`, is **refused** at load. An index names a position, not an
element: a concurrent insert or delete before that index lands the expiry on a
neighbour, and every expiry shifts every later positional watch, on every tick for as
long as the rule exists. This is the case the keyed-array page already makes for any
durable array two writers touch. Declare the array keyed — `!logd-key` or
`!logd-auto-id` in the schema — and write the rule with `(*)`. Items must be objects
anyway, since they carry a timestamp, and `!logd-auto-id` generates the key for a
producer that has none.

The rule and the schema have to agree: `runs.*` over an array the schema keys is
refused when the rule runs, naming `runs(*)`, and `list(*)` over an array the schema
gives no identity is refused the same way.

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

### The precondition

A batch's delete asserts, for each item, the timestamp the pass read and the fields
the rule's `match` names, combined into one object pattern. That is why `match` has
to be an object — `{status: !or [done, canceled]}` rather than `!or [...]` at the item
— and why it may not name the `age` field itself. A batch whose precondition no longer
holds is skipped, logged, and re-read on the next pass.

### Durations

`every` and `after` are written the way [compaction's durations](compaction.md#durations)
are, plus the units a retention limit is asked for in: `1d` is 24h, `2w` is 14d and
`1y` is 365d, calendar-blind, and they mix with the rest (`1y6w`, `1d12h`).

## What the pass does

1. Reads the container as of the current commit, under the configured read budget.
   A container larger than the budget is a rule error, logged; raise `storage.readBudget`
   or split the collection.
2. For each item: reads the timestamp at `age`; skips the item if it is missing or will
   not parse, or if `now - timestamp < after`, or if `match` does not hold.
3. Deletes the rest in commits of at most `batch` items, each under its precondition.

The first pass runs when the server starts serving, so a backlog is not left waiting
an interval to be noticed. The timer then fires every `every`. Retention runs on the
**baseline** only; a scope is deleted whole with `deleteScope`.

## What it does not do

- **It does not free disk on its own.** A delete removes the record from the current
  state, but the record survives in every older root snapshot the tier policy keeps,
  and some snapshot from every era survives indefinitely. Set
  [`compaction.horizon`](compaction.md#the-knobs) to bound how far back any history
  survives; past it the deleted record is gone from the store.
- **It does not read a timestamp that is not RFC3339.** A numeric epoch is kept, not
  expired.
- **It does not look inside `!raw`.** A raw subtree is a document the store carries
  rather than one it owns.

## Validation

The section is checked when the file loads, and a rule that cannot mean what it says
refuses the whole config: a path that names one node, or a dense array, or a wildcard
in the middle; an `age` that is not a field path; an `after` that is not positive; a
`match` that is not an object or that names the age field; a section with no rules.
