# Element identity

Prerequisite 3 of wk5w1ddkh12krj1tkxn0, settled on paper before any of it is built. It
answers the three questions that issue says have real unknowns: what a composite key and a
multi-key array serialize to as a PATH SEGMENT, since that segment is what the index keys on
and what a client addresses; and what happens to an element whose key changes.

It does not settle the read interface (prerequisite 1) or presence (prerequisite 2). It is
written first because the cursor in prerequisite 1 seeks to a NAME, and a name it cannot
spell is a name it cannot seek to (thqtmm2th12kr051jhn0).

## The invariant everything else follows from

    One element has one name. The name is a path segment. The index is a trie of names.

The index today is already a trie: `Index.Children map[string]*Index`, one node per path
segment, built by splitting a kpath in `Add` (index.go). That shape is kept. Every question
below is a question about what strings may appear as a key in that map, and the answer is
always the same shape of answer: a NAME may, a QUERY may not.

## The stored form of a keyed array is an object

Once identity no longer rests on position, nothing is left that makes a keyed array an array
in the store. So it is not stored as one:

    LOWERED         !logd-keyed(sku) {
                      (sku=A): {sku: A, qty: 3}
                      (sku=B): {sku: B, qty: 9}
                    }

    RAISED          !key(sku) [ {sku: A, qty: 3}, {sku: B, qty: 9} ]

An object whose field names are the element names, carrying a tag that says to present it as
an array and by which fields it is identified. Array-ness is presentation; identity is the
field name. The format already has this construction: a sparse array is an object whose field
keys are numbers, written `{n}` in a path (ir/kpath.go). A keyed array is the same
construction with names instead of numbers.

WHAT THIS BUYS, and it is the reason to prefer it to any ordering rule:

  - Key fields come first by construction, which was the thing that had to be arranged. The
    name precedes the content because it IS the field name, so a stream knows an element's
    identity before it sees a byte of the element. No lowering rule, no buffering, no pending
    subtree per open element, and nothing to enforce or check later.
  - The substrate needs no schema. `stream.State.CurrentPath()` answers a field path because
    the thing it is walking is an object; snap indexes it because it indexes fields; the
    index keys the trie on it because it keys the trie on segments. Two of the three defects
    in thqtmm2th12kr051jhn0 -- the snapshot indexing by position, and the projection refusing
    to descend `!key` -- do not need a fix so much as they stop having a subject.
  - There is no merge at the container, keyed or otherwise. A delta names an element, so it
    is recorded at that name and folded there, and the substrate never builds the container
    in order to put a child in it. Inserting `jane` into `{bob, joe, susan, ...}` is one
    record at `items.'(name=jane)'` and, where the index has not seen the name before, one
    child on a trie node. The other N are not read, not rewritten, and not resident.

    This is the part that is NOT residency and must not be deferred to it. `mergeop`'s
    `!key` is O(N) because it maps the document's elements to find the one it names, and
    `tony.Patch` rebuilds a container of N children whatever kind of container it is. Both
    are properties of merging two documents held IN MEMORY, which is what a client does with
    the format and precisely what the store is to stop doing. Below the line a write is a
    record at a path and a fold is a stream, so N appears only when the answer is the whole
    container -- where N is the answer's size, which is what the bound is stated against.
    A store that pays O(N) to insert one element is not out of memory by construction, and
    no later optimisation makes it so.

    What is O(N) and stays so is the index's SPACE, and it is worth being exact about it
    because a trie node is not a map entry: every node carries a full B-tree, and that tree
    preallocates a 32-slot leaf. Measured on this tree:

        LogSegment                  120 B
        Index                        56 B    plus its children map
        Tree                         16 B
        node                         64 B
        the leaf it starts with   3,840 B    32 x 120, allocated empty

    So an EMPTY node is about four kilobytes, and the forensics' ~266 B of leaf per segment
    is the same structure seen from the other end -- 120-byte segments in leaves under half
    full. Element-granular naming multiplies nodes by the number of elements, so the rebuilt
    index owes a node whose cost is proportionate to what it holds. A fixed 32-slot leaf was
    affordable when a node was a PATH and there were 46,836 of them; it is not affordable
    when a node is an ELEMENT. That is qvn7ptxch12krxzt9hmg, and this document is what makes
    it urgent rather than merely true.

    Two things bound it from inside this design. The index is O(elements WRITTEN), not
    O(array size): an element nothing has ever written has no node, exactly as an unwritten
    path has none. And `LogSegment` today carries `ArrayKey *ir.Node` and `ArrayKeyField
    string` -- 24 of its 120 bytes, plus an allocated `ir.Node` for every keyed segment --
    which exist only because identity could not live in the path. With identity in the name
    they go, across 4,519,816 segments in the measured store.

    Neither is a bound on the index itself, and there is not one. The bound that is wanted
    is on RESIDENCY, not on content: an index truncated to live state and recent commits
    would be small and would make history unusably slow by construction, which is the
    opposite defect and not an improvement. What the index DESCRIBES is all of history,
    durably. What it HOLDS is chosen by a policy over (commit, path) under a ceiling that is
    configured rather than emergent -- frecency being the obvious shape, since both axes are
    skewed. The path axis is measured: ten shallow paths carry 30.1% of 4.5M segments. The
    commit axis is assumed and should be measured before anything is tuned to it.

    Compaction is complementary rather than a substitute: property 5 shrinks what there is
    to describe, 4,519,816 -> 47,648 in the forensics, and residency bounds what is held of
    it. Together they give a read at the head no I/O and a read deep in history some, which
    is the right gradient and is worth stating as a policy instead of arriving at it.

    Element identity is what makes this pressing rather than merely true: it multiplies
    names, and a keyed array of 100,000 elements with 200 hot ones is exactly the case a
    residency policy answers and a size bound cannot. It belongs to the proposal
    (qvn7ptxch12krxzt9hmg), which is where it is raised.
  - An element is a field, so everything the substrate already does per path applies to it:
    a snapshot at an element, a patch at an element, a compaction that collapses an
    element's history. Property 5's headroom stops being an array special case.

WHAT IT COSTS:

  - A keyed array reads back in name order, not in the order a client wrote it. Storage
    already sorts object keys on write and read, and order control is post-v0.1; this puts
    keyed arrays under the rule the rest of the document is already under, rather than
    inventing a second one.
  - The key fields appear twice, in the name and in the element. The NAME IS AUTHORITATIVE:
    an element whose key fields disagree with its name is invalid, refused where it is
    written. The redundancy is kept rather than stripped because an element read on its own
    should carry its own key, and a scalar per element is not what the measured store is
    short of.

## Names

Three spellings, one name. A key segment binds each field of the declared identity to a
literal value.

    items.'(sku=A)'              the stored form: an ordinary field
    items(sku=A)                 sugar, self-describing
    items(A)                     sugar, resolved against the schema

The first is what the trie holds and what a stored `LogSegment.KindedPath` carries, so no
part of the substrate needs a new segment kind and no stored path needs a schema to be read
back. The other two are client-facing and canonicalize to it at the boundary. The last one
carries the key VALUE where building a structure needs the key FIELD, which is exactly why
`RootPatchAt` cannot express an element path today; resolving it at the boundary is what
removes that.

THE FIELD FORM IS THE SEGMENT AT THE STORAGE LEVEL, and the sugar lives above it. The
alternative -- a distinct kpath kind, with the substrate rendering `items(sku=A)` from the
container's tag -- would preserve the kind for matching at the cost of teaching `stream` the
tag, which is a change in the format library and outside what this rebuild is costed for.
Nothing is lost by putting it higher up: the layer that resolves a client's `items(A)`
against the schema is the same layer that would want the kind, and it has both.

CANONICAL FORM, which is the field name:

  - Bindings sorted by field name. One binding renders `(f=v)`, several render `<{...}>`:

        (sku=A)                  one field
        <{n: 2, name: jane}>     a composite identity

  - Values render with the inline encoder, tag and comment stripped, so `(n=42)` and
    `(n='42')` are different names, as they must be -- the name is the only place the key's
    type survives.
  - Inside `(...)`, an unquoted `=` at depth 0 separates field from value. A key value that
    contains one is quoted: `items('a=b')` is the schema-resolved sugar, `items(a=b)` is a
    binding.
  - A key field whose value is null is not a name. An element carrying it is unkeyed and
    cannot be addressed -- the same reading `mergeop`'s keyed merge already takes of a
    document element that does not carry the key: it is not one of the ones being merged,
    and that is not an error.

RESTRICTIONS, so that a name stays a name:

  - Key fields are top-level fields of an object element, and their values are scalars. A
    key that is a subtree is not renderable as a name. `mergeop`'s `!key(p)` takes a
    yamlpath and will keep taking one; storage declares the narrower thing.
  - A name binds every field of the declared identity. A segment that binds some of them is
    a query.

## Queries

A segment that may name more than one element is WILD. It is not an identity, it never
appears in the trie, and it is not stored.

    items(status=open)          binds a field that is not the identity
    items<{name: jane}>         binds part of a composite identity
    items.*                     the wildcard that already exists

A wild segment RESOLVES TO A CURSOR OVER NAMES, and a read at one is the union of narrow
reads taken in that order. Not to a set: a set of names is a slice whose length grows with
the match, which is the same signature mistake wk5w1ddkh12krj1tkxn0 names in
`LookupSubtree(...) []LogSegment` -- 166 MB returned before a single log entry is read. A
wild read at a keyed array of 100,000 with 40,000 matches must hold one name at a time and
the answer it has emitted so far, and nothing else.

A WRITE AT A WILD PATH IS REFUSED. A write names the element it writes. This is not a
residency argument -- a write could stream its matches as readily as a read -- but a
transactional one: the set a wild write applies to would be decided at some instant during
the write and there is no reading of that instant that is worth defending. It also costs
nothing to allow later, and everything to remove later.

Secondary keys are what make `(f=v)` useful when f is not the identity. They are declared
with a tag that is not `!logd-key` -- proposed `!logd-index` -- precisely because they do not
confer identity: they are a lookup, they may repeat, and they may change. Resolution goes
through a structure held beside the trie rather than in it, whose representation is
deliberately left open here; its CONTRACT is fixed and small:

    (array-name, field, value, commit) -> a cursor over names

Additive, derivable, and droppable: losing it costs a scan, never an answer.

## Where identity is declared

The schema is the authority, and lowering is where it is applied -- it is the last place that
has both the schema and the whole element, and the only place that needs either.
`!logd-key` declares a client-supplied identity, `!logd-auto-id` a generated one, and the
migration path already refuses a schema that declares two identities for one array ("one
array has one identity", schema_keyfield_test.go). That rule survives verbatim, with one
clause added:

    several `!logd-key` fields on one array declare ONE identity, the tuple of them.

Which is why the existing "declared keyed by both" rejection is exactly the case being
opened: it was ambiguous only because a tuple had no meaning. `!logd-key` together with
`!logd-auto-id` stays refused -- an auto id is already a whole identity.

`mergeop`'s `!key(f)` in a raised document says how a merge is performed. It does not declare
an identity, and a patch that keys an array by a field the schema does not is an error rather
than a second name for the element. Two names for one element is the one thing the trie
cannot hold.

## Identity replaces position

`!logd-key` and `!logd-auto-id` do not add a second way to address an element. They REPLACE
the positional one for that array, totally, and the stored object form is what that
replacement looks like when it is taken seriously:

  - `items[2]` is not a name on a keyed array. There is nothing in the store for it to
    denote; the schema decides which regime an array is in and no array is in both.
  - Order carries no identity. A keyed array can be reordered, and a merge can place a new
    element wherever it lands, without renaming anything and without disturbing a single
    index node. That is the property the whole rebuild wants: it is what lets a subtree head
    sit at an element (rkb7p8v5h12ksdnmgsn0) instead of at the array.

An array that GAINS an identity is the boundary between the two regimes, and it needs no new
mechanism either. Before the migration commit the array is one indexed path and its elements
have no names; after it they do. A read carries a commit and so is already on one side or the
other. An array that LOSES one is refused, for the same reason a rename is: the elements it
would strand have names and no successor to hold them.

## An element's key does not change

A rename has no answer that is compatible with an immutable log. A read at commit C must see
the element under the name it had at C; if the name changed at C', then the trie node for the
old name holds this element's segments before C' and nothing after, the node for the new name
holds them after, and every read has to know which era it is in before it can seek. There is
no cheap version of that.

So: THE FIELDS OF AN ELEMENT'S IDENTITY ARE IMMUTABLE FOR THE LIFE OF THE ELEMENT. A write
that changes one is refused at the write, where it can still be a client error, rather than
at the read, where it is already a wrong answer. Changing the identity is a delete and an
insert, and it is a different element.

Secondary keys are mutable; they are a lookup and their structure is commit-ranged like
everything else.

This is enforcement that does not exist today -- `!logd-key` is client-supplied and nothing
checks it -- and it is the reason `!logd-auto-id` is the identity to prefer: nobody edits a
value they did not write.

## Arrays that declare no identity

They are stored as arrays, they are indexed AT the array and not below it, and reading or
writing one element materializes the array. `items[2]` still addresses an element in a
document a client holds. It is not a name in the store, because a position is not stable
under insertion and the store cannot rewrite history to agree with a shifted one.

This is deliberately not engineered around. An unkeyed array is a VALUE: asking for one of
its elements is asking for the array, and a client keeping a large one has explicitly asked
for a large resident document. The cost is real, it is charged where it is incurred, and the
remedy is entirely in the client's hands -- declare an identity and the array stops being a
value and becomes a set of names.

So there are no positional index nodes to close, no commit-ranged eras to resolve against,
and nothing an insert or delete must restate. "The one thing an insert or delete must
restate from the index up" turns out to be nothing at all once positional elements are not
indexed: the array is one path and a write to it is a patch at that path. The complexity
that would have made an unkeyed array cheap to address element-wise is the complexity of
giving it an identity it did not declare.

## What this gives back

Absence at a keyed element becomes statable. "Stating that an element is ABSENT from a keyed
list has no vocabulary" was the third reason the scope overlay could not be derived
(qth3kqe9h12ksxz9j9n0), and it was true because absence is a statement about a NAME and a
keyed element had none. It has one here, and in the stored form it is the ordinary absence of
a field, so prerequisite 2 can say it once rather than inventing a keyed special case.

## What is deliberately not decided

  - The representation of the secondary structure. Its contract is above; its shape waits
    until the read interface exists to be shaped against.
  - Whether a declared secondary key may also be declared UNIQUE, making `(f=v)` an alias
    that resolves to exactly one name. It costs nothing to add later and it is not a second
    identity: an alias resolves TO the name, it is never stored AS one.
  - Match syntax inside `<...>` beyond literal bindings. The angle-bracket form is where it
    would go, and every such segment is wild by the rule above, so it changes nothing in the
    trie whenever it arrives.
