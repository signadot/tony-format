# o: filtering a list by a match -- a !filter operator, and o m -each over it

`x | o m <what> -`, where x produces a LIST, cannot answer "which elements match". The unit of
matching is the DOCUMENT, and a list is one document, so the pattern is asked about the whole
array and the elements are never considered separately.

Measured against `- {name: a, state: open}` / `- {name: b, state: closed}` / `- {name: c, state: open}`:

    o m '{state: open}' list.tony        nothing, exit 0
    o m '!all {state: ...}' list.tony    the WHOLE list, or nothing -- a boolean about the array
    o m '!subtree {state: open}' ...     likewise
    o m -trim '!subtree {name: c}' ...   the whole list: -trim trims to the PATTERN, not to hits
    o list '$[*]' list.tony              the whole list back; objpath has no predicates

The same `o m` already does exactly what is wanted when the input is a multi-document stream:

    printf '{name: a, state: open}\n---\n{name: b, state: closed}\n' | o m '{state: open}' -
    => {name: a, state: open}

So the semantics exist and are one granularity off from the thing people have in their hands,
which is a list.

## What is missing in the language, not just the CLI

`!all` maps a patch over every element (mergeop/all.go, Patch). Nothing DROPS one. There is no
composition of the current operators that filters: the natural attempt

    o patch -s '!all !if {if: {state: open}, then: !pass null, else: !delete null}' list.tony

panics rather than filtering -- see the separate report on `o patch` and a delete at the root.

## Proposal, in two parts

### 1. `!filter <match>`, a patch operator

    o patch -s '!filter {state: open}' list.tony
    o patch -s '{items: !filter {state: open}}' doc.tony      # at any depth

Keep the elements of a list (or the values of an object, keyed as they were) for which the child
MATCHES; drop the rest. It is the sibling `!all` never had: `!all` says what happens to every
element, `!filter` says which ones remain.

This belongs in the operator set rather than only in the CLI:

  - it reaches any depth. A flag can only ever filter the top level, and `{items: [...]}` is the
    common shape
  - it is a pure function of the document it meets, so unlike !pipe it can be stored in a logd
    delta and replayed, and it can live in a build file or a docd patch
  - a match inside a patch has precedent: !if takes an `if:` (mergeop/if.go)

Decisions it needs: the name (!filter / !select / !keep); whether the child is a bare match
(`!filter {state: open}`) or a mapping; whether an object's values filter by value with keys kept
(proposed) or something else; and its place in the schema context vocabulary, which after
cv90ehkvh12krm4sfxn0 is checked to name only operators that exist.

### 2. `o m -each`, the one-liner over it

    x | o m -each '{state: open}' -           # the matching elements, as a list
    x | o m -each -trim '{name: ""}' -        # ... projected to what the pattern names

`-each` changes the unit of matching from the document to the elements of the top-level array,
and emits an array of the matches. Implemented over `!filter` rather than beside it, so there is
one implementation of what "matches" means.

Semantics to pin down:

  - an object input filters its values and keeps their keys
  - a scalar input is an error; there are no elements to match
  - a multi-document stream applies -each per document
  - nothing matching yields an empty list rather than no output -- the silence is half of what
    makes the current behaviour confusing, and the other half is the exit code (filed separately)

## Why both

Either alone is worse. The flag alone leaves a nested list unreachable and has to be reimplemented
the moment somebody wants the same thing inside a document. The operator alone leaves the shortest
path -- a person with a list in a pipe -- spelled `o patch -s '!filter ...'`, which is a patch
command doing a query, and nobody will guess it.