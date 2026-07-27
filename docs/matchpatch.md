# Matching and Patching

Our starting point is [rfc 7396](https://datatracker.ietf.org/doc/html/rfc7396),
which proposes a standard for using JSON documents as matching criteria of JSON
documents and as declarative criteria used to patch JSON documents.

Tony uses this as a basis for matching and patching object notation which can
be represented as JSON and extends it in various capacities.

## MergeOps

Tony operations are available in matches and patches when building from
directories and via `o match` and `o patch`.


| MergeOp    | Match | Patch | Arguments         | Description                                                                     |
|------------|-------|-------|-------------------|---------------------------------------------------------------------------------|
| key        |   +   |   +   | objectpath to key | associative lists as objects                                                    |
| and        |   +   |   -   |     -             | conjoin a list of matches to be applied to the corresponding doc                |
| or         |   +   |   -   |     -             | disjunction                                                                     |
| not        |   +   |   -   |     -             | negate a match (eg !not.or [1,2,3])
| all        |   +   |   +   |     -             | take the match (resp patch) apply it to all array or object elements of the doc |
| subtree    |   +   |   -   |     -             | match any subtree of the doc                                                    |
| dive       |   -   |   +   |     -             | dive into the doc and treat each subtree with a list of matches/patches         |
| quote      |   -   |   +   |     -             | quote a yaml as a string                                                        |
| unquote    |   -   |   +   |     -             | unquote a string as a yaml                                                      |
| nullify    |   -   |   +   |     -             | turn a yaml into a null without deleting it                                     |
| delete     |   -   |   +   |     -             | delete a top level document                                                     |
| type       |   +   |   -   |     -             | match by type                                                                   |
| field      |   +   |   +   |     -             | match the field (a string), not its value                                       |
| tag        |   +   |   -   |     -             | match the tag of a node, not its value                                          |
| glob       |   +   |   -   |     -             | glob match a string                                                             |
| pipe       |   -   |   +   |     -             | pipe the doc node to a program and replace it with the program's output         |
| json-patch | -     |   +   |     -             | apply a json patch to the corresponding doc node                                |
| pass       |   +   |   +   |     -             | match: always accept / patch: return the current doc                            |
| if         |   -   |   +   |     -             | evaluate a condition and patch either with `then` or `else`                     |
| raw        |   +   |   +   |     -             | the escape: treat the subtree as data, interpreting no operation at any depth   |

Operations are indicated by YAML tags within a match or a patch.

Most operations are either match operations or patch operations but not both.
Some operations, such as `key` and `field`, are both.

### The `raw` escape

The patch grammar and the data grammar share one tag namespace.  Without an
escape, a value whose tag happens to name a registered operation is always
_interpreted_, so a tony document which itself contains tony operators — a
match, a patch, a rule — cannot be written into a document at all:

```tony
# patch                                  # applied to {}
rule: {id: !glob "hot-*"}                # error: cannot patch with glob operation
rule: {tmp: !delete null, keep: 1}       # keep: 1 — the !delete executed
```

`!raw` says _this tag is data_.  Its subtree is stored as values, no operation
is interpreted anywhere beneath it, and the `!raw` tag itself is consumed so
the subtree lands with its own tags intact:

```tony
# patch
rule: !raw {id: !glob "hotfix-*", patch: {tmp: !delete null}}
# doc after
rule: {id: !glob "hotfix-*", patch: {tmp: !delete null}}
```

The escape belongs to the patch, not to the document, which is why the tag is
consumed: a stored patch keeps its `!raw`, so replaying it escapes again.  A
`!raw` _nested_ under a `!raw` is data like everything else beneath it.

In a match, `!raw` compares its subtree to the document as literal data: tags
are compared rather than evaluated, and the comparison is exact — same fields,
same length, `null` means `null` — rather than the partial object match of an
ordinary pattern.  Put `!raw` at the depth where literal comparison starts and
the enclosing pattern keeps ordinary match semantics:

```tony
# doc
rule: {id: !glob "hot-*", stage: open}

rule: !raw {id: !glob "hot-*"}    # no match: rule has a stage field too
rule: {id: !raw.glob "hot-*"}     # match: id compared literally, stage ignored
```

`Diff` emits `!raw` itself for a value which carries operator tags as data, so
that `Patch(a, Diff(a, b))` is `b` for documents which contain operations, and
`Reverse` stops at a `!raw` rather than reversing the operations named inside
it — those are the document's values, not the diff's instructions.

`!raw` executes nothing — it is the opposite of `!pipe` — so `RejectUnsafe`
has no quarrel with it, whatever the data it stores happens to name.

### Considerations

Contrary to evaluation tags, match and patch operations relate the match (or
patch) document to some _input_ document.  Evaluation tags just relate the node
in the document in which they reside to the environment.

This relating of match or patch doc leads to some interesting cases.

For example, let's consider the `and` and `all` matches.   The `and` match
consists of a list of matches, each of which must match the corresponding input
document.  The `all` match consists of a single match which must apply to all
array or object members of the document.

As a result `and` is not a patch operation.  However, `all` is both a match and
a patch operation:  as a patch it applies the child patch to all object or
array members of the corresponding input document.

## Custom Ops

Match and patch operations can be created by implementing a simple interface
and registering the operation in the `mergeop` package.
