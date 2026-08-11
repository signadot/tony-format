# Mergeop Operations

This page documents mergeop operations. It is **not yet complete** -- the library
registers considerably more than are described here, including `!key`, `!retag`,
`!addtag`, `!rmtag`, `!rename`, `!strdiff`, `!arraydiff` and the match-side operations.
See `mergeop/register.go` for the full set.

## `!all`

**Apply match/patch to all array or object elements**

The !all operation applies its child match or patch to all elements of an array or object. As a match, it matches when all elements match. As a patch, it applies the patch to all elements.

!!! note "Schema Usage"
    Used in schema `define:` sections to constrain array/object elements. Example: `array(t): !and [.array, !all.type t]`

    See [Schema Tags Reference](../schema-tags.md) for more on using tags in schemas.

**Child:** Match or patch to apply to all elements

**Examples:**

1. ```tony
array(t): !and
  - .array
  - !all.type t
```

**See also:** [`!irtype`](./mergeop.md#irtype), [`!field`](./mergeop.md#field)

---

## `!and`

**Match all conditions (logical AND)**

The !and operation matches when all child conditions match. If the child is an array, all elements must match. If the child is a single value, it must match.

!!! note "Schema Usage"
    Used in schema `define:` sections to combine multiple constraints. Example: `array(t): !and [.array, !all.type t]`

    See [Schema Tags Reference](../schema-tags.md) for more on using tags in schemas.

**Child:** Array of match conditions or single match condition

**Examples:**

1. ```tony
!and
  - name: "test"
  - version: 1
```

2. ```tony
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

1. ```tony
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

1. ```tony
- !insert
  id: "new-id"
  name: "New Item"
```

2. ```tony
- !insert !key(id)
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

1. ```tony
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

1. ```tony
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

1. ```tony
name: !replace
  from: "old"
  to: "new"
```

2. ```tony
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

1. ```tony
# updates only WIDGET, however many other items the document holds
items: !key(sku)
- sku: WIDGET
  qty: 5
```

2. ```tony
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
  which is why logd declares it in schema instead (see `!logd-key`).
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

1. ```tony
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

1. ```tony
bool: !irtype true
```

2. ```tony
number: !irtype 1
```

3. ```tony
string: !irtype ""
```

**See also:** [`!all`](./mergeop.md#all), [`!field`](./mergeop.md#field)

---

