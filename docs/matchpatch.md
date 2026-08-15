# Matching and Patching

Our starting point is [rfc 7396](https://datatracker.ietf.org/doc/html/rfc7396),
which proposes a standard for using JSON documents as matching criteria of JSON
documents and as declarative criteria used to patch JSON documents.

Tony uses this as a basis for matching and patching object notation which can
be represented as JSON and extends it in various capacities.

## MergeOps

Tony operations are available in matches and patches when building from
directories and via `o match` and `o patch`.


| MergeOp    | Match | Patch | Arguments      | Description                                                                      |
|------------|-------|-------|----------------|----------------------------------------------------------------------------------|
| all        |   +   |   +   |     -          | apply the match (resp. patch) to every element of an array or object             |
| and        |   +   |   -   |     -          | conjoin a list of matches, each applied to the corresponding doc                 |
| or         |   +   |   -   |     -          | disjunction                                                                      |
| not        |   +   |   -   |     -          | negate a match (eg `!not.or [1,2,3]`)                                            |
| at         |   +   |   -   | kpath          | walk to the path and apply the match there; see below                            |
| has-path   |   +   |   -   |     -          | the document has the path the operand names                                      |
| subtree    |   +   |   -   |     -          | match any subtree of the doc                                                     |
| glob       |   +   |   -   |     -          | glob match a string                                                              |
| irtype     |   +   |   -   |     -          | the node's kind equals the operand's: `!irtype ""` a string, `!irtype 0` a number|
| tag        |   +   |   -   |     -          | match the tag of a node, not its value                                           |
| field      |   +   |   +   | -, or from,to  | match the field (a string), not its value                                        |
| key        |   +   |   +   | field to key by| associative lists as objects                                                     |
| let        |   +   |   -   |     -          | bind names in `let:`, then match with `in:`, referring to them as `.[name]`      |
| pass       |   +   |   +   |     -          | match: accept anything / patch: leave the document as it is                      |
| raw        |   +   |   +   |     -          | the escape: treat the subtree as data, interpreting no operation at any depth    |
| if         |   -   |   +   |     -          | evaluate `if:` and patch with `then:` or `else:`                                 |
| dive       |   -   |   +   |     -          | dive into the doc and treat each subtree with a list of matches/patches          |
| embed      |   -   |   +   | key            | the operand is the result, with each occurrence of the key replaced by the doc   |
| quote      |   -   |   +   |     -          | quote a document as a string                                                     |
| unquote    |   -   |   +   |     -          | unquote a string as a document                                                   |
| nullify    |   -   |   +   |     -          | turn a node into a null without deleting it                                      |
| json-patch |   -   |   +   |     -          | apply a json patch to the corresponding doc node                                 |
| pipe       |   -   |   +   |     -          | pipe the doc node to a program and replace it with the program's output          |
| insert     |   -   |   +   |     -          | add a value; the value is what results                                           |
| delete     |   -   |   +   |     -          | remove a value; absence is what results                                          |
| replace    |   -   |   +   |     -          | CHECKED: verify the node still equals `from:`, then install `to:`                |
| addtag     |   -   |   +   | tag            | add a tag; the tag is what results                                               |
| rmtag      |   -   |   +   | tag            | remove a tag; its absence is what results                                        |
| retag      |   -   |   +   | from,to        | CHECKED: verify the tag is `from`, then make it `to`                             |
| strdiff    |   -   |   +   |     -          | a string edit, relative to the string that is there                              |
| arraydiff  |   -   |   +   |     -          | an array edit, relative and positional                                           |
| rename     |   -   |   +   |     -          | rename fields, relative to the keys that are there                               |

`o match -tags` and `o patch -tags` print this list from the binary, which is the
authority; a test keeps the table above equal to it.

The last nine are what a diff produces, and they divide on two lines worth
knowing: CHECKED operations assert something about what they meet and fail if it
does not hold, while insert, delete, addtag and rmtag simply state a result; and
strdiff, arraydiff and rename are RELATIVE, re-evaluating against whatever is
there. Both distinctions matter to anything that stores a patch and applies it
later -- see `system/logd/api/storage_context.go`, which declares which of them
may be stored and why.

### Reaching into a document with `!at`

`!at(kpath)` walks down the path and applies the match it holds to the node it
lands on:

```
o match '!at(spec.replicas).irtype 0' deploy.tony
```

matches a document whose `spec.replicas` is a number, whatever else it holds.
This is how a document is filtered by a condition somewhere inside it, which is
otherwise the thing people reach for `o list -if` to do -- the difference being
that `!at` answers about the whole document, and `-if` answers with the nodes.

A path which names nothing does not match: `!at(a.b) 3` asks for an `a.b`, so a
document without one fails rather than matching vacuously, the same reading
`!has-path` gives a missing path. A wildcard path (`.*`, `[*]`, `{*}`) names
every node it reaches, and all of them have to match, as every field an object
pattern names has to match.

The path is a kpath, all of it, keyed segments included: `!at(resources(joe).x)`
reaches into the element keyed `joe` of a list the document tags `!key(name)`. A
key names nothing in a list which is not keyed -- the tag is what says which
field the key is -- so that is a mismatch, not an error.

Composition reaches either side of the walk, and the two are different
questions. `!not.at(a.b) 3` negates the whole thing: it holds when there is no
`a.b` as much as when `a.b` is 4. `!at(a.b).not 3` asks for an `a.b` which is
something other than 3.

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
