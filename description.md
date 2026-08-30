# logd: a read answers `{}` at a container nobody wrote, which is the null bug unfinished

# logd: a read answers `{}` at a container nobody wrote, which is the null bug unfinished

`bymhrqz7h12ksas3jhn0` established the rule and v0.0.201 made it hold for null:

> A read answers `null` **iff** a null was written at that path. Absence answers `not_found`, at
> every depth.

The same sentence with `{}` in it is not true yet. Deleting the last member of a container leaves
the container standing as an empty one, so a read answers `{}` where nobody ever wrote `{}`.

```
null    — the rule, holding since 201
  written as null                  -> Null (0)
  never written                    -> not_found

object  — the same question, answered the old way
  written as {}                    -> Object (0)
  EMPTIED by deleting its member   -> Object (0)      <- nobody wrote {} here
  never written                    -> not_found
```

So `{}` means two things — *"somebody wrote an empty object"* and *"something used to be here"* —
and a client cannot tell them apart, which is exactly the shape the null fix removed one type over.

## How far it reaches

The container left behind is the immediate parent only; ancestors with other members are
unaffected, which is right:

```
  x.a.b.c  before                  -> Number
  x.a.b.c  after delete            -> not_found
  x.a.b    (its only member gone)  -> Object (0)      <- the leftover
  x.a                              -> Object (1)      <- still holds b, correctly
  x                                -> Object (1)

  y.a.b    (still holds a sibling) -> Object (1)      <- correct, untouched
```

So a delete removes its own key and stops, leaving one empty container per emptied path. Under
the rule, `x.a.b` should be `not_found` and the emptying should propagate up while each parent is
left holding nothing.

## Scope: this is about DELETION, not about empty values

An empty container that was *written* is a value and must read back as one. Checked, and both of
these are already right:

```
  written as {}                    -> Object (0)      correct — somebody wrote it
  written as [], or overwritten with []  -> Array (0) correct — somebody wrote it
```

An overwrite with `[]` is a write, so the array case has no defect. The defect is only where a
container is left behind by removing what it held.

## The part that needs a decision rather than a fix

An explicitly written `{}` and an emptied one are the same bytes. If `a.b` is written as `{}`,
then `a.b.c` is written and deleted, is `a.b` still the `{}` somebody wrote, or a leftover?

Answering that needs the store to know whether a container is **load-bearing** (written as a
value) or **incidental** (brought into being as the path to a child) — which is a distinction it
does not carry today, and is the reason this is filed rather than assumed to be a small fix. The
narrow version — a delete propagates upward through containers it empties, and an explicit write
of `{}` is not distinguished — closes the case that actually bites, and is stated here so the
choice is visible rather than made by accident.

## Why it matters downstream

Verse is making null-vs-absence coherent on the back of 201 and immediately meets this: a slice
holding nothing must answer not-found, and it cannot, because `{}` from an emptied slice is
indistinguishable from `{}` somebody wrote. Patching around it in verse would rebuild exactly the
parent-key tiebreak that 201 just made unnecessary — a second round trip, and a client's guess at
a rule the store should state.

Repro is `bymhrqz7`'s, with the object cases above added; it needs only a logd, a docd and a
session.