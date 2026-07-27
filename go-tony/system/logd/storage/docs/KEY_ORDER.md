# Key Order in Storage

Storage keeps object keys **sorted**: sorted on write, and reads are expected to come
back sorted. This is a contract, not an accident of implementation, and code in the read
path has to uphold it deliberately.

## Where the sort comes from

`tony.Patch` merges an object by accumulating fields into a `map[string]*ir.Node` and
rebuilding the result with `ir.FromMap`, which sorts (`ir/node.go`). Sparse arrays sort
the same way through `ir.FromIntKeysMapAt`. So anything that reaches storage through a
patch is sorted by construction.

## What does NOT sort, and why that is correct

`ir.FromKeyValsAt` preserves written order, and it is what `parse` uses for string-keyed
objects (`parse/parse.go`). Other contexts need that order — multi-key merge keys and
similar — and those contexts are not storage's. Preserving order has index implications
that do not make sense for the storage use case.

The two constructors are therefore deliberately different. Do not unify them, and do not
"fix" `parse` to sort or `FromMap` to preserve.

## What this obliges the read path to do

A read is not a pure pass-through of stored bytes: the streaming processor merges patches
into a base event stream. Anywhere it emits an object key, that key has to land where it
sorts.

The case that made this concrete is grafting — see `unreachedPatches` in
`internal/patches/processor.go`. A patch that creates a new key has no path in the base,
so the processor adds it. Emitting it at the container's close is the obvious
implementation and is wrong: a new `q` lands after `z` instead of between `m` and `z`.

It also compounds, because `createSnapshot` runs through the same processor. An unsorted
read result becomes an unsorted snapshot, which becomes the base for the next one.

The rule that holds: the base arrives sorted, so on each key, emit any pending graft that
sorts before it. Nothing later can collide, since later keys are larger still. A base key
equal to a pending graft's first segment is not a collision — that graft belongs deeper,
under it.

Two things to keep in mind if you touch this:

- A base written before this rule held may have keys out of order. Such a container is
  marked unsorted and falls back to grafting at the close: it does not sort, but it
  cannot duplicate a key either.
- Only a container's close proves a path absent. Mid-container, a path the base has not
  reached yet may still be reached by an element that has not streamed.

`TestReadEquivalence_SnapshotVsReference` compares object field order positionally, so a
regression here shows up as an ordering divergence against the no-snapshot reference
rather than as a content difference.
