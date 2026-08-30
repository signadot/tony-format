# What a write must be

**logd** is a log: state is reconstructed by replaying stored deltas over snapshots.
A delta the store cannot apply is therefore not a failed write — it is a permanent
one. Every later read meets it again, including reads of documents the write never
touched, since they all replay through the same log; and no later patch repairs it,
because the read dies on the way past the bad one. The store cannot snapshot past it
either, so it cannot compact.

So a write is checked *before* it is stored, and one that would do that is refused while
the client is still holding the call. Each refusal below comes back as an error code
rather than as a surprise at read time.

One section is not a refusal: what a write may *say* is wider than what the log *keeps*,
and the difference is resolved by converting the write rather than rejecting it.

Because docd speaks the logd protocol verbatim, clients get these through docd too.

## A write must apply

Every patch is applied to the state it would be applied to before it is stored. If it
does not apply, nothing is written.

This is not a rule about which operations are allowed. A field write states what
results and cannot fail to apply. An operation that asserts something about the base
can, and its assertion can be false:

```tony
v: !arraydiff {0: 99}                 # on [1, 2] — applies, and always will
v: !arraydiff {5: 99}                 # on [1, 2] — refused
s: !replace {from: bob, to: rob}      # on "bob"  — applies
s: !replace {from: nope, to: rob}     # on "bob"  — refused
```

The question is whether *this* delta applies to *this* state, so the same operation is
accepted or refused depending on what it meets.

Refused with `invalid_diff`.

## An array index must name an element

An index is positional: `votes[2]` names the element that is there. A field is not —
writing `a.b.c` creates whatever is missing on the way — and an index cannot be, because
there is no third element of a two-element array to create.

What each index has to be true of depends on what the write does at the end of the path:

| the write at `votes[i]` | means | requires |
|---|---|---|
| plain data | patch element `i` | element `i` exists |
| `!insert v` | insert before `i`; `i == len` appends | `0 ≤ i ≤ len` |
| `!delete v` | remove element `i` | element `i` exists |

An index that is not the last segment always needs the element to exist: a write cannot
insert *through* a position on its way to something deeper, so `votes[3].choice` needs a
`votes[3]`.

The array's length is a fact when the write is submitted, which is where the client is
told; it is checked again at commit, because the array can lose the element in between.

Refused with `invalid_path`, and the message carries the array's length.

!!! note "Positional writes name a position, not an element"

    The commit-time check asks whether an element is still *there*, not whether it is
    still the *same* one. A concurrent insert or delete before the index leaves an
    element at that position and makes it a different one, so a positional write can
    land on a neighbour. For anything durable, name elements by identity instead — see
    [Keyed arrays](keyed.md).

## What is stored is the result a write produced

A patch may use anything the format offers. What the log *keeps* is narrower — `!insert`,
`!delete`, `!key`, `!raw`, `!addtag`, `!rmtag`, `!comment` — and what those have in common
is that each states what the value **is**, so that re-applying one to a base that has
moved gives what it gave at the write.

An operation whose meaning depends on what it lands on — `!replace`, `!rename`,
`!strdiff`, `!arraydiff`, `!retag`, `!jsonpatch`, `!if`, `!let` — is therefore not stored
as written. It is applied, and its **result** is stored in its place:

```tony
# baseline holds {s: bob}
s: !replace {from: bob, to: rob}    # accepted
                                    # stored as  s: !insert rob
```

This costs the client nothing at the write and one thing at the read: a client reading
back its own write sees what the operation produced, not the operation it sent. Nearly
every write is unaffected, because a plain field write already states its result and is
kept exactly as it arrived.

### Why a scope needs it

Baseline and a scope are safe from different things, because their bases behave
differently. A baseline delta replays against the same base forever, so one that applied
once applies always. A **scope's** base moves: baseline advances underneath it, and an
operation whose meaning depends on what was there can stop applying long after it was
written, with nothing wrong at the time of the write.

So the two layers are converted to different things. Baseline stores the **difference**
the write made; a scope stores the **claim** it made — what the scope holds at each path
its patch stated, whatever baseline does next:

```tony
# baseline holds {s: bob}
s: !replace {from: bob, to: rob}    # in a scope: stored as  s: !raw rob
# baseline then writes s: someone-else
#   -> baseline reads someone-else, and the scope still reads rob
```

A claim is not the same as a difference, and the distinction is load-bearing. A scope
that deletes a field baseline has not created yet has made no *difference* to state — but
it has made a claim, and without storing it the scope would stop shadowing that path the
moment baseline created the field.

| | replay | what is stored |
|---|---|---|
| baseline | deterministic | the difference the write made |
| scope | base moves | the claim the write made |

Writing at an array index is unaffected: the positional form is logd's own routing, not
an operation the client wrote, so a scoped `votes[1]` write works normally.

## Nothing that calls out to the system

`!pipe` runs a program. A stored one runs it *again* — on every read, every replay and
every snapshot build — so it states no value: the same commit reads two ways, and the
store's three appliers (a full read, the stepped head, a watch's deltas) stop agreeing
with each other.

It is refused at the write, and never applied by a read.

Escaped data is unaffected, and this is what makes storing a document *about* patches
possible at all: under `!raw` nothing beneath is interpreted, so a charter, a stored rule
or a stored patch is ordinary data. The escape composes onto the node's own tag when what
it escapes is a leaf — `!irtype` escaped is `!raw.irtype` — and both forms are data.

Refused with `invalid_diff`.

## Preconditions are a different thing

None of the above is a precondition. A patch may also carry a compare-and-swap
precondition, which is a claim about the document that the client chooses to make and
that fails with `match_failed`; see [Conditions on writes](index.md#conditions-on-writes).
The rules on this page are not optional and are not about what the client expects — they
are what the store must be able to store.
