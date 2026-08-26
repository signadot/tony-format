# Mergeop Operations

Every operation the library registers has an entry here, and a test keeps it that
way: `mergeop.TestReferenceCoversEveryOperation` fails when an operation is added
without one. `o match -tags` and `o patch -tags` print the same list from the
binary, and [matchpatch.md](../matchpatch.md) has it as a one-line table.

Whether an operation matches, patches, or both is what the registry says, not a
property of its shape: `!and` is match-only, `!insert` patch-only, and `!all`,
`!field`, `!key`, `!pass` and `!raw` are both.

## `!all`

**Apply match/patch to all array or object elements**

The !all operation applies its child match or patch to all elements of an array or object. As a match, it matches when all elements match. As a patch, it applies the patch to all elements.

!!! note "Schema Usage"
    Used in schema `define:` sections to constrain array/object elements. Example: `array(t): !and [.[array], !all t]`

    The element type goes where `!all`'s operand goes, not into its tag: a type
    parameter stands for a match, which has no spelling as a tag component, so
    `!all.t null` substituted nothing and the element check disappeared.

    See [Schema Tags Reference](../schema-tags.md) for more on using tags in schemas.

**Child:** Match or patch to apply to all elements

**Examples:**

```tony
array(t): !and
  - .[array]
  - !all t
```

**See also:** [`!irtype`](./mergeop.md#irtype), [`!field`](./mergeop.md#field)

---

## `!and`

**Match all conditions (logical AND)**

The !and operation matches when all child conditions match. If the child is an array, all elements must match. If the child is a single value, it must match.

!!! note "Schema Usage"
    Used in schema `define:` sections to combine multiple constraints. Example: `array(t): !and [.[array], !all t]`

    See [Schema Tags Reference](../schema-tags.md) for more on using tags in schemas.

**Child:** Array of match conditions or single match condition

**Examples:**

```tony
!and
  - name: "test"
  - version: 1
```

```tony
!and
  status: "active"
  enabled: true
```

**See also:** [`!or`](./mergeop.md#or), [`!not`](./mergeop.md#not)

---

## `!delete`

**Delete a matched value**

The !delete operation removes a matched value from its parent. For arrays, it removes the matched element. For objects, it removes the matched field.

**Child:** Match condition for value to delete

**Examples:**

```tony
- !delete
  id: "old-id"
```

**See also:** [`!insert`](./mergeop.md#insert), [`!replace`](./mergeop.md#replace)

---

## `!insert`

**Insert a new value into an array**

The !insert operation inserts a new value into an array. It can optionally take a tag argument to apply a tag to the inserted value.

**Child:** Value to insert

**Arguments:** Optional: tag name to apply

**Examples:**

```tony
- !insert
  id: "new-id"
  name: "New Item"
```

```tony
- !insert.key(id)
  id: "new-id"
  name: "New Item"
```

**See also:** [`!delete`](./mergeop.md#delete), [`!replace`](./mergeop.md#replace)

---

## `!not`

**Negate a match condition**

The !not operation matches when its child condition does not match.

!!! note "Schema Usage"
    Used in schema `accept:` sections to exclude certain types. Example: `accept: !or [!and [!not .ttl, !not .node], !schema other]`

    See [Schema Tags Reference](../schema-tags.md) for more on using tags in schemas.

**Child:** Match condition to negate

**Examples:**

```tony
!not
  status: "deleted"
```

**See also:** [`!and`](./mergeop.md#and), [`!or`](./mergeop.md#or)

---

## `!or`

**Match any condition (logical OR)**

The !or operation matches when any child condition matches. If the child is an array, at least one element must match.

!!! note "Schema Usage"
    Used in schema `accept:` and `define:` sections to allow multiple valid types. Example: `offsetFrom: !or [createdAt, updatedAt]`

    See [Schema Tags Reference](../schema-tags.md) for more on using tags in schemas.

**Child:** Array of match conditions

**Examples:**

```tony
!or
  - name: "test"
  - name: "prod"
```

**See also:** [`!and`](./mergeop.md#and), [`!not`](./mergeop.md#not)

---

## `!replace`

**Replace a value with another value**

The !replace operation replaces a value with another. It requires both 'from' and 'to'
fields in its child object.

As a patch it is **checked**: it verifies that the node still equals 'from' and returns an
**error** if it does not -- it does not skip the node or apply anyway. That is what makes
it safe in a diff, which describes the step between two specific documents; it is also why
a patch which is stored, or re-applied to a document expected to have moved on, wants
`!insert` instead.

**Child:** Object with 'from' and 'to' fields

**Examples:**

```tony
name: !replace
  from: "old"
  to: "new"
```

```tony
version: !replace
  from: 1
  to: 2
```

**See also:** [`!insert`](./mergeop.md#insert), [`!delete`](./mergeop.md#delete), [`!retag`](./mergeop.md#retag)

---

## `!key`

**Identify array elements by a field rather than by position**

The !key operation merges a list by identity. `!key(f)` names the field which identifies
an element; elements of the patch are matched against elements of the document by that
field, so a patch naming one element leaves the others alone and a repeated write to the
same key updates in place rather than appending.

A list patched without `!key` merges positionally instead, element 0 against element 0,
which replaces whatever happened to sit there.

**Child:** Array of elements, each carrying the key field

**Examples:**

```tony
# updates only WIDGET, however many other items the document holds
items: !key(sku)
- sku: WIDGET
  qty: 5
```

```tony
# the key may be a path, not just a field name
items: !key(meta.name)
- meta:
    name: alpha
  qty: 1
```

**Notes:**

- The result carries the **document's** tag, not the patch's. A document already tagged
  `!key(f)` stays keyed; a plain list patched with `!key(f)` merges by identity for that
  patch and comes back untagged, so the next patch must name the key again. Keying is
  therefore a property of each write unless something puts the tag on the stored value --
  which is why logd declares it in schema instead (see
  [Keyed arrays](../logd/keyed.md)).
- A diff keys its output only when **both** sides carry `!key(f)` with the same field;
  otherwise it falls back to a positional array diff, silently.
- A bare `!key` keys each element by the whole element.

---

## `!retag`

**Replace a node's tag**

`!retag(from,to)` replaces the tag `!from` with `!to`.

Like `!replace` it is **checked**: it verifies the node's tag is already `!from` and
returns an **error** otherwise. `!addtag(t)` and `!rmtag(t)` are the unconditional halves
-- they state the resulting tag without asserting the previous one.

**Examples:**

```tony
# the output of a diff between a node tagged !tag1.tag2(a,b) and one tagged !tag2(z).other(x)
f: !retag(tag1.tag2(a,b),tag2(z).other(x))
```

**See also:** [`!replace`](./mergeop.md#replace)

---

## `!irtype`

**Match by node type**

The !irtype operation matches nodes based on their type. The child must be a value of the desired type (e.g., `true` for bool, `1` for number, `""` for string).

!!! note "Schema Usage"
    Fundamental schema operation for type checking. Used in `define:` sections: `bool: !irtype true`, `number: !irtype 1`, `string: !irtype ""`

    See [Schema Tags Reference](../schema-tags.md) for more on using tags in schemas.

**Child:** Example value of the type to match

**Examples:**

```tony
bool: !irtype true
```

```tony
number: !irtype 1
```

```tony
string: !irtype ""
```

**See also:** [`!all`](./mergeop.md#all), [`!field`](./mergeop.md#field), [`!ir`](./mergeop.md#ir)

---

## `!ir`

**Match the node's IR representation, not its value**

The !ir operation matches its child against an object over the fields of
`ir.Node`, under the names it serializes them with, rather than against the value
the node holds. It is how a pattern asks a question about the node itself:

```
3         !ir {type: Number, int: 3}
3.5       !ir {type: Number, float: 3.5}
"x"       !ir {type: String, string: "x"}
!k v      !ir {type: String, tag: "!k", string: "v"}
{a: 1}    !ir {type: Object, fields: [a], values: [1]}
```

A field the node does not have is absent, not null. `{int: null}` therefore says
only that the field is there, which is enough for `int` and `float` -- they are
pointers -- but not for `string`, `bool` and `number`, which are omitted when they
hold their zero value: `!ir {bool: null}` does not match `false`. A pattern which
says what the field HOLDS does not have to know which kind it is asking about.

A key which is not a field of an IR node is refused where the pattern is built,
rather than never matching: `!ir {itn: 3}` is a misspelling, and a pattern which
silently declines to match every document there is is the shape of wrongness
nobody finds. The fields are `type`, `fields`, `values`, `tag`, `lines`,
`comment`, `string`, `bool`, `number`, `float` and `int`.

`fields` and `values` are answered as a list, built when a pattern asks for one,
and `!ir` applies again wherever it is written. The list is one level deep: a
node whose values are nodes gives a list of those nodes, not of views of them.

!!! note "Schema Usage"
    `int` and `float` are the one distinction no pattern over a value can make:
    both are Number nodes, and there is one Number type. base.tony defines them
    as `int: !ir {int: .[number]}` and `float: !ir {float: .[number]}`.

    This is a question about the node, not about the document, which is why it is
    an operator. A document which happens to look like an IR encoding --
    `{type: Number, int: 3}` written out by hand -- is matched by the ordinary
    object pattern, and by `!ir` only through ITS representation, which is
    `{type: Object, ...}`. base.tony's `_ir` describes the first, `!ir` asks the
    second.

**Child:** Match to apply to the node's IR representation

**Examples:**

```tony
int: !ir
  int: .[number]
```

```tony
!ir {tag: "!secret"}
```

**See also:** [`!irtype`](./mergeop.md#irtype), [`!tag`](./mergeop.md#tag), [`!field`](./mergeop.md#field)

---

## `!at`

**Walk to a path and apply the match there** (match)

`!at(kpath)` walks down the path and applies the pattern it holds to the node it lands
on, saying nothing about the rest of the document.

A path that names nothing does not match: `!at(a.b) 3` asks for an `a.b`, so a document
without one fails rather than matching vacuously -- the reading `!has-path` gives a
missing path. A wildcard path (`.*`, `[*]`, `{*}`) names every node it reaches, and all
of them have to match.

**Arguments:** the kinded path to walk

**Examples:**

```tony
# spec.replicas is an integer, whatever else the document holds
!at(spec.replicas).irtype 0
```

**See also:** [`!has-path`](./mergeop.md#has-path)

---

## `!has-path`

**The document has the path the operand names** (match)

**Examples:**

```tony
!has-path spec.replicas
```

```tony
# and its negation, for "this must be absent"
!not !has-path spec.replicas
```

**See also:** [`!at`](./mergeop.md#at), [`!not`](./mergeop.md#not)

---

## `!glob`

**Glob-match a string** (match)

**Examples:**

```tony
name: !glob "sv*"
```

---

## `!subtree`

**Match any subtree of the document** (match)

The match succeeds if it holds ANYWHERE beneath the node, at any depth, rather than at
the node itself.

**Examples:**

```tony
# somewhere in here, something has replicas: 3
!subtree {replicas: 3}
```

---

## `!tag`

**Match a node's tag rather than its value** (match)

The operand is matched against a node describing the tag -- `{name: ..., args: [...]}` --
so a tag with arguments can be matched by name alone or by name and arguments. An
untagged node presents an empty name and no arguments.

**Examples:**

```tony
# name carries some !svc tag
name: !tag {name: svc}
```

```tony
# a list keyed by sku
items: !tag {name: key, args: [sku]}
```

**See also:** [`!retag`](./mergeop.md#retag), [`!addtag`](./mergeop.md#addtag)

---

## `!get-path`

**Answer with the node at a path** (match and patch)

`!at` walks to a path and applies a match there, `!embed` hands over the whole node
with no path, and `!has-path` answers whether. None of them answers WITH the value
at a path, so nothing could be compared against one, written from one, or bound to
a name.

The path is relative to the node the operator is written at. `!get-path(root)`
anchors it at the document instead. The anchor is a tag component rather than a
sigil in the path, because kpath spells the root as the empty path and the `$` was
taken out of the query surface deliberately.

A path which names nothing is an **error**, where `!at` reads the same absence as a
mismatch: `!at` relocates a pattern, so a document without an `a.b` is a document
which fails it, while this answers with a value and there is none. A null would be
read as "anything" by a match and WRITTEN by a patch, and a silent no-match says
nothing about why.

A wild path (`.*`, `[*]`, `{*}`, `..`) names a set rather than a node and is
refused where the pattern is built; `!get-paths` is the operation for those.

The answer is a copy, detached, so a walk up from it stops at what was asked for:
`!get-path(root)` inside such a value means that value, not the document it came
from.

**Examples:**

```tony
# the sidecar takes the image the main container has
sidecar: {image: !get-path(root) spec.containers.main.image}
```

```tony
# a match on a RELATION between two parts of one document
status: {replicas: !get-path(root) spec.replicas}
```

```tony
# bound once, written wherever the body says
!let
  let:
  - img: !get-path(root) spec.image
  in:
    main: {image: .[img]}
    side: {image: .[img]}
```

**See also:** [`!at`](./mergeop.md#at), [`!has-path`](./mergeop.md#has-path), [`!get-paths`](./mergeop.md#get-paths)

---

## `!get-paths`

**Answer with the nodes at a path, as a list** (match and patch)

`!get-path`'s plural, and it takes the paths its singular refuses. A wild segment
names a set rather than a node, and kpath already knows which is which, so the two
operators are that distinction made into two names rather than a rule about what
one of them does with a wild path.

A path which names one node gives a list of one, so there is no special case. A
path which names nothing is the **empty list**, where `!get-path` errors: each
keeps the promise its name makes, and an empty list is a list.

The values are copies, each parented to the list, and the list is detached.

**Examples:**

```tony
# every image in the document, as a list
images: !get-paths(root) "containers[*].image"
```

**See also:** [`!get-path`](./mergeop.md#get-path), [`!at`](./mergeop.md#at)

---

## `!let`

**Bind names, then match or patch with them** (match and patch)

`let:` is a list of bindings and `in:` is the match or the patch, which refers to a
binding as `.[name]`. A binding list is read in the scope enclosing it, so a nested
`!let` may bind a name from a value the outer one bound, and a name it rebinds
shadows the outer one for the length of its own `in:`.

A name the let does not bind is an error rather than a null.

**Examples:**

```tony
# as a match
!let
  let:
  - n: 3
  in:
    spec:
      replicas: .[n]
```

```tony
# as a patch: the name is written wherever the body says it
!let
  let:
  - image: registry.example.com/app:1.4.2
  in:
    spec:
      template:
        spec:
          containers:
            main: {image: .[image]}
            sidecar: {image: .[image]}
```

---

## `!field`

**Match or rename the FIELD, not the value** (match and patch)

As a match the operand applies to the field name, a string. As a patch,
`!field(from,to)` renames a field of the object it is applied to, leaving the value
alone; the operand is unused, so `null` is the ordinary thing to write.

**Examples:**

```tony
# spec.replicas becomes spec.count, keeping its value
spec: !field(replicas,count) null
```

**See also:** [`!rename`](./mergeop.md#rename)

---

## `!pass`

**Accept anything, or leave the document as it is** (match and patch)

As a match it accepts any node. As a patch it is the identity -- useful where a patch is
structurally required but nothing should change, such as one arm of `!if`.

**Examples:**

```tony
spec: !pass {}
```

---

## `!raw`

**Treat the subtree as data, interpreting no operation at any depth** (match and patch)

The escape. The patch grammar and the data grammar share one tag namespace, so without
it a value whose tag happens to name an operation is always interpreted rather than
stored -- and a Tony document that itself holds Tony operators (a match, a patch, a
stored rule) could not be written at all.

**Examples:**

```tony
# the document gets a literal {insert: 1}, not an insertion
spec: !raw {insert: 1}
```

**Notes:**

- A store that keeps documents holding operators depends on this; logd's storage
  vocabulary stops its walk at `!raw` for exactly that reason.
- The node's own tag chain is still read: `!strdiff.raw` is a strdiff whose operand is
  raw, while `!insert.raw` inserts raw data.

---

## `!addtag`

**Add a tag; the tag is what results** (patch)

`!addtag(t)` is unconditional: it states the resulting tag without asserting the previous
one, which is what `!retag` does. A `null` operand means the statement is about the tag
alone and the value stays as it is.

**Examples:**

```tony
# spec keeps its value and gains !mine
spec: !addtag(mine) null
```

**See also:** [`!rmtag`](./mergeop.md#rmtag), [`!retag`](./mergeop.md#retag)

---

## `!rmtag`

**Remove a tag; its absence is what results** (patch)

The other unconditional half of `!retag`.

**Examples:**

```tony
spec: !rmtag(mine) null
```

**See also:** [`!addtag`](./mergeop.md#addtag), [`!retag`](./mergeop.md#retag)

---

## `!comment`

**State the comments at a node** (patch)

The operand is an object naming `head`, `line`, or both. A position the operand does not
name is left alone, as a field an object patch does not name is; setting one to `[]`
removes it.

Both positions live in one operand rather than one operator per position because tag
composition shares a child -- `!comment.comment` could only ever carry one set of lines,
and two changes at one node need one statement.

**Examples:**

```tony
a: !comment {head: ["# new"]}                 # the comment above a is now this
a: !comment {line: []}                        # the one after a is gone
a: !comment {head: ["# h"], line: [" # l"]}   # both, in one statement
```

**Notes:**

- It states what the comment IS, never what it was, so it applies to a base that has
  moved and may be stored -- which is why a comment change is a delta about the comment
  rather than a replacement of the value it describes.
- The lines are the child rather than tag arguments because comment text is arbitrary and
  the format keeps the whitespace before a line comment's `#` as part of it.

---

## `!strdiff`

**A string edit, relative to the string that is there** (patch)

Produced by `o diff` for strings that differ in part. It is RELATIVE: the result depends
on what it meets, so applying it to a different string produces a different answer.

**Examples:**

```tony
# what diff writes for "svc" -> "svcx"
name: !strdiff(false)
  3: !insert x
```

**Notes:**

- Relative operations are not storable in logd: re-applied against a base that has moved,
  they re-evaluate. See `system/logd/api.StorageContext`.

---

## `!arraydiff`

**An array edit, relative and positional** (patch)

The array counterpart of `!strdiff`, and relative in the same way. A list merged by
identity rather than position is `!key`'s business instead.

The operand is keyed by position in the sequence the two sides share, which is not an
offset into either one of them -- see [What a key means](../diffpatch.md#what-a-key-means).
At each position the element says what to do there:

| element | means | needs |
|---|---|---|
| a patch | patch the element at this position | an element there |
| `!insert v` | insert `v` before this position | nothing; the end is a position |
| `!delete v` | remove the element, which must equal `v` | an element there |
| `!replace {from, to}` | replace, checking `from` first | an element there |

**Examples:**

```tony
v: !arraydiff {1: !insert 99}       # [1, 2]    ->  [1, 99, 2]
v: !arraydiff {2: !insert 99}       # [1, 2]    ->  [1, 2, 99]   (the end appends)
v: !arraydiff {0: !delete 1}        # [1, 2]    ->  [2]
v: !arraydiff {0: {b: 2}}           # [{a: 1}]  ->  [{a: 1, b: 2}]
```

**Notes:**

- An operand key the document has no element for is an error, not a no-op: a patch
  claiming more of the document than it has is malformed and says so.
- The operation is the first label of the element's tag chain the registry **knows**,
  not simply the first label. A composed tag may carry labels that are not operations
  ahead of the one that is -- parsing alone puts one there -- and those belong to the
  value, so they are put back on it. `!delete {by: scott}` compares a braced object
  against the braced element it is deleting.

**See also:** [`!key`](./mergeop.md#key), [`!strdiff`](./mergeop.md#strdiff)

---

## `!rename`

**Rename fields, relative to the keys that are there** (patch)

The operand is a list of `{from, to}` pairs: each field of the object named by a
`from:` is renamed to its `to:`, keeping its value and its place.

The pairs are **simultaneous**. They are a list of statements about one object,
not a program, so each is read against the document as it stands and all of them
are installed together: `[{from: a, to: b}, {from: b, to: a}]` exchanges the two
names, and the result does not depend on the order the pairs are written in.

A `from:` which names no field renames nothing -- the operation is relative to the
keys that are there. A `to:` which collides with a field that is still there is
refused, since one of the two would have to be lost; rename that one too, in the
same operand, if the collision is what you meant.

**Examples:**

```tony
!rename
- from: spec
  to: sp
```

**See also:** [`!field`](./mergeop.md#field), which is the same operation for a
single field, written on the field rather than on the object holding it.

---

## `!dive`

**Apply patches to the subtrees that match** (patch)

The operand is a list of `{match, patch}` pairs: each subtree of the document is offered
to each pair, and where the match holds the patch is applied. `patch` is required;
`match` may be omitted.

**Examples:**

```tony
!dive
- match: {replicas: 3}
  patch: {replicas: 9}
```

---

## `!embed`

**The operand is the result, with the key replaced by the document** (patch)

`!embed(k)` names a key; the operand becomes the result, and each occurrence of that key
in it is replaced by the node being patched. It is how a value is wrapped in new
structure rather than overwritten.

**Examples:**

```tony
# spec becomes {before: 1, was: <the old spec>}
spec: !embed(X)
  before: 1
  was: X
```

---

## `!if`

**Patch conditionally** (patch)

`if:` is a match against the node, `then:` and `else:` are patches.

**Examples:**

```tony
spec: !if
  if: {replicas: 3}
  then: {replicas: 4}
  else: {replicas: 0}
```

**See also:** [`!pass`](./mergeop.md#pass)

---

## `!json-patch`

**Apply a JSON patch to the node** (patch)

An RFC 6902 sequence, applied to the node it decorates.

**Examples:**

```tony
spec: !json-patch
- op: replace
  path: "/replicas"
  value: 7
```

**Notes:**

- Relative, like `!strdiff` and `!arraydiff`: the sequence is expressed against the
  document it meets.

---

## `!nullify`

**Turn a node into null without removing it** (patch)

The difference from `!delete` is that the key stays, holding null.

**Examples:**

```tony
spec: !nullify {}
```

**See also:** [`!delete`](./mergeop.md#delete)

---

## `!quote`

**Quote a document as a string** (patch)

**Examples:**

```tony
# spec becomes the text of what it held
spec: !quote {}
```

**See also:** [`!unquote`](./mergeop.md#unquote)

---

## `!unquote`

**Unquote a string as a document** (patch)

**Examples:**

```tony
# doc holds Tony text; this parses it in place
doc: !unquote ""
```

**See also:** [`!quote`](./mergeop.md#quote)

---

## `!pipe`

**Replace the node with a program's output** (patch)

The node is written to a program's standard input and its output replaces the node.

**Examples:**

```tony
name: !pipe "tr a-z A-Z"
```

**Notes:**

- This calls out to the system, so it is unsafe by design and cannot be stored: applying
  a stored `!pipe` twice runs the program twice, so it states no value. logd refuses it
  at the write and never applies one ([What a write must be](../logd/writes.md)); the
  library itself refuses it under the `RejectUnsafe` option, which is what logd sets.
