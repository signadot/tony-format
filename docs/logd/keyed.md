# Keyed arrays

An array in a stored document is merged by **position** unless something says
otherwise: element 0 against element 0, replacing whatever happens to sit there. That
is the right answer for a list whose order is the point, and the wrong one for a
collection of things with identities — where a client wants to write *the vote by dee*
without knowing, or caring, where it currently sits.

[`!key(f)`](../generated/mergeop.md#key) says so for one patch. Keying is a property of
the write, though, so a patch that does not repeat the tag goes back to merging by
position — which means a store cannot rely on it. logd therefore takes the declaration
out of the write and puts it in **schema**, where it is a property of the array itself.

## Declaring one

Two tags declare a keyed array, in a schema's `define:` section. They mean the same
thing to a merge and to the index, and differ only in **who produces the key**:

```tony
define:
  items:
    sku: !logd-key null        # the CLIENT supplies the key
    qty: 0
```

```tony
define:
  users:
    id: !logd-auto-id.type ""  # the SERVER generates the key
    name: ""
    tasks:
      id: !logd-auto-id.type ""
      title: ""
```

The tag goes on the **field that identifies an element**, and the array it declares is
the one that holds it. Nesting works the way the schema reads: `users.tasks` above is
keyed by `id` in its own right.

`!logd-auto-id` is keying *plus* generation. When a write leaves the field null or
absent, logd fills it in with a monotonic value derived from the commit, so a client can
create an element without inventing an identity for it. `!logd-key` is the other half on
its own: the array is keyed, and the client says by what.

## What declaring one changes

A declaration changes what a write **means**, not just how it is recorded. logd injects
`!key(f)` into a write to a declared-keyed array, so it merges by identity:

```tony
# with items declared keyed by sku
items:
- sku: WIDGET
  qty: 5
```

updates the WIDGET element in place, however many others the array holds, and leaves
them alone. Without the declaration the same patch replaces element 0.

Two things are refused rather than resolved:

- a write that carries its own `!key` naming a **different** field than the schema
  declares — the write and the schema disagree about what an element *is*, and guessing
  which one is right is not logd's to do;
- a schema that declares **two identities** for one array, whether by two `!logd-key`s
  or by a `!logd-key` and a `!logd-auto-id` on different fields. This is checked where a
  schema is proposed, so a store never adopts one whose keying is ambiguous — a stored
  delta cannot be un-recorded afterwards.

## Changing one

A schema that gives an array an identity, takes it away, or keys it by other fields is
set as one commit, and the commit carries the rewrite of the data:

- **gaining** one names the elements from their key fields, generating an auto-id where
  one is missing; an element with no key, or two elements with one name, refuses the
  schema;
- **losing** one turns the elements back into an array, in name order, and is refused
  unless the set says `force`, since the names are lost with it;
- **changing** it names the elements again, under the new fields.

A change under a path where a scope has statements is refused, naming the scopes.

History keeps the keying it had. A read at an earlier commit answers under the schema in
force at that commit: with `runs` keyed by `id` then and by nothing now,
`{match: {path: "runs(r1)", commit: N}}` reads element `r1`, and the same path at the head
is `invalid_path`.

A watch over the array — at it, under it, or above it — **ends** at the schema commit with
`keying_changed` rather than being handed the rewrite, which would tell it a renamed
element had been deleted. Watch again from that commit, spelling the path as the schema now
does ([Watching](session.md#watching)).

## What may be a key

The index turns each element into a path segment, so a key must **render as one**: a
scalar — string, number or bool — and unique among its siblings once rendered.

This is narrower than a merge needs. `!key` alone keys elements by the whole element,
and `mergeop` will happily key an array by any node at all; the index cannot, because
there is no path segment for an object. Rendering also loses type, so `1` and `"1"` are
two elements sharing one path. Both are refused rather than collapsed.

## Why prefer it

A keyed path names the element it means. A positional one names a position, and a
concurrent insert or delete before that position silently makes it a different element
(see [What a write must be](writes.md#an-array-index-must-name-an-element)). For
anything durable — anything where two writers may touch the same array — the identity
belongs in the schema and the writes should name elements by key.
