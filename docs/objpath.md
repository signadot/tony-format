# Querying Objects

`o get` and `o list` take a **kpath** — the same path syntax the rest of the system
uses: what `!at(...)` walks in a [match](matchpatch.md), what logd indexes by, what a
watch names, and what an error prints when it says where it is.

```bash
o get spec.replicas deploy.tony
o list 'items[*].name' inventory.tony
o get 'items(WIDGET).qty' inventory.tony
```

A path is a sequence of segments, and each segment says what KIND of thing it steps
through — which is what "kinded path" means:

| segment | steps into | example |
|---|---|---|
| `.field` | an object | `spec.replicas`, `.replicas` |
| `[i]` | a dense array | `items[0]` |
| `{i}` | a sparse array, **by key** | `items{42}` |
| `(key)` | a [keyed array](logd/keyed.md), by identity | `items(WIDGET)` |
| `.*` `[*]` `{*}` | all of them, at that step | `items[*].name` |
| `..` | any depth below here, this node included | `..name`, `spec..image` |

The leading `.` is optional at the start, so `spec.replicas` and `.spec.replicas` are
the same path, and by the same rule a bare `.` is the whole document. It is optional
only there: a field after an element or a key still takes its dot, so `items[0].name`
is a path and `items[0]name` is not. The root is
written `.`, or as the empty path — `o get . doc.tony` and `o get '' doc.tony` ask
the same thing. Giving no path at all is a usage error rather than the root: a
missing path and a path naming everything are different mistakes. A key or a field that needs quoting takes quotes: `pr."1".votes`. A `*` at the start
of a segment is always the wildcard, so a field whose name begins with one is written
quoted — `"*"` is the field called `*`, `*` is every field there is.

A sparse array is an object whose keys are numbers, so `{i}` names the value under
the key `i` rather than the `i`th value: in `!sparsearray {3: a, 7: b}`, `{7}` is `b`,
which sits second.

## `get` and `list`

`get` answers with the single node a path names, and `list` with every node it names.
A wildcard therefore belongs in a `list`: `get` refuses `[*]` or `..` rather than
picking one of the things they matched.

```bash
o list ..image deploy.tony        # every image, wherever it is
o list 'spec..name' deploy.tony   # every name under spec, at any depth
o list 'a..' doc.tony             # a and everything under it
```

`-paths` answers with **where** each node is rather than what it is, in this same
syntax — so the answer to one query is the input to the next:

```bash
$ o list -paths ..image deploy.tony
- spec.containers[0].image
- spec.containers[1].image

$ o get 'spec.containers[0].image' deploy.tony
nginx
```

Both read a stream of documents and ask the path of each. See
[What a write must be](logd/writes.md) for the paths logd accepts on the writing
side, which are the same syntax.

## A note on `$`

An earlier syntax, closer to JSONPath, began every path with `$` — `$.field[3]`. That
sigil said nothing, since every path had it, and the syntax could not name a keyed or
a sparse element at all.

A leading `$` is still accepted, so `$.spec.replicas` works and existing scripts keep
running. It is dropped rather than treated as a field name, which is what a kpath
would otherwise make of it.

That syntax spelled any-depth with three dots -- `$...x` -- because `$..x` was a
parse error. kpath spells it `..`, and the three-dot form is read as it.

## Where `..` may not go

A `..` is a question: it names the nodes at any depth rather than a step to one. So
it belongs in a query and nowhere a path has to name a place -- what a patch is
rooted at, what a watch names, what logd indexes by. Those refuse it, and say so:

    "a..c": `..` names nodes at any depth, which is a question and not a place:
    a path here has to name one

An empty field name is still sayable, in quotes: `a."".x`. That is the canonical
spelling, and what `..` used to parse as before it meant depth.
