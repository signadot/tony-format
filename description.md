# o: filtering a list by a match -- `!filter`, the transformation half

The CLI half of this is done in 864f8c1: `o get -if` and `o list -if` put the two halves of the
question on one command, the path saying WHERE and the match saying WHICH.

    x | o list -if '{state: open}' '$[*]' -            # the matching elements
    o list -if '{state: open}' '$.items[*]' doc.tony   # wherever the path reaches
    o get  -if '{state: open}' '$.status' doc && deploy

The `o m -each` flag this issue originally proposed is RETIRED, unbuilt. It would have meant "the
elements of the top-level array", which is one path spelled as a mode; as a path it costs nothing
and `$.items[*]` comes free, which -each could never have reached. Filtering a nested list is the
common case rather than the exotic one.

`o m -at path` was considered and rejected for the same reason: `!at(path) PAT` already filters
DOCUMENTS by a nested condition and has done all along. Its problem is that nobody can find it --
see the separate report on !at being absent from the docs.

## What is left: the transformation

`-if` is a QUERY. It answers with the nodes the path named and the match kept, and throws away
the document they came from. That is what a pipe wants and it is not what a document wants:

    o patch -s '{items: !filter {state: open}}' doc.tony

should answer with the document, its items filtered in place, everything else untouched. There is
no way to say that today, and no composition of the existing operators does it -- the natural
attempt, `!all !if {..., else: !delete null}`, deletes the whole document (it used to segfault;
see a7bwkxwah12kr0n0fxn0).

`!filter <match>` is the sibling `!all` never had: `!all` says what happens to every element,
`!filter` says which ones remain. Keep the elements of a list, or the values of an object keyed as
they were, for which the child MATCHES; drop the rest.

It belongs in the operator set and not only in the CLI:

  - it reaches any depth, in place, without a query-and-regraft
  - it is a pure function of the document it meets, so unlike !pipe it can be stored in a logd
    delta and replayed, and it can live in a build file or a docd patch
  - a match inside a patch has precedent: !if takes an `if:` (mergeop/if.go)

## Decisions it needs

  - the name: !filter / !select / !keep
  - whether the child is a bare match (`!filter {state: open}`) or a mapping
  - an object's values filter by value with keys kept (proposed), or something else
  - its entry in the schema context vocabulary, which since cv90ehkvh12krm4sfxn0 is checked to
    name only operators that exist
  - whether it is storable: it should be, and logd's StorageContext (system/logd/api) has to say
    so explicitly rather than inherit it

## Not needed for this

`o list -if` covers the pipe. This issue is now only about saying the same thing inside a
document, which is the part a CLI flag cannot reach.
