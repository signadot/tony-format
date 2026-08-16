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
| `{i}` | a sparse array | `items{42}` |
| `(key)` | a [keyed array](logd/keyed.md), by identity | `items(WIDGET)` |
| `.*` `[*]` `{*}` | all of them, at that step | `items[*].name` |

The leading `.` is optional at the start, so `spec.replicas` and `.spec.replicas` are
the same path. A key or a field that needs quoting takes quotes: `pr."1".votes`.

## `get` and `list`

`get` answers with the single node a path names, and `list` with every node it names.
A wildcard therefore belongs in a `list`: `get` refuses `[*]` rather than picking one
of the things it matched.

Both read a stream of documents and ask the path of each. See
[What a write must be](logd/writes.md) for the paths logd accepts on the writing
side, which are the same syntax.

## A note on `$`

An earlier syntax, closer to JSONPath, began every path with `$` — `$.field[3]`. That
sigil said nothing, since every path had it, and the syntax could not name a keyed or
a sparse element at all.

A leading `$` is still accepted, so `$.spec.replicas` works and existing scripts keep
running. It is dropped rather than treated as a field name, which is what a kpath
would otherwise make of it. The `$...x` any-depth form has no kpath spelling and is
refused with a message saying so; name the path, or use a wildcard.
