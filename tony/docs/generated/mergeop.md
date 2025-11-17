# Mergeop Operations

This page documents all mergeop operations.

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

**See also:** [`!type`](./mergeop.md#type), [`!field`](./mergeop.md#field)

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

The !replace operation replaces a matched value with a new value. It requires both 'from' and 'to' fields in its child object. The operation matches nodes that equal the 'from' value and replaces them with the 'to' value.

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

**See also:** [`!insert`](./mergeop.md#insert), [`!delete`](./mergeop.md#delete)

---

## `!type`

**Match by node type**

The !type operation matches nodes based on their type. The child must be a value of the desired type (e.g., `true` for bool, `1` for number, `""` for string).

!!! note "Schema Usage"
    Fundamental schema operation for type checking. Used in `define:` sections: `bool: !type true`, `number: !type 1`, `string: !type ""`

    See [Schema Tags Reference](../schema-tags.md) for more on using tags in schemas.

**Child:** Example value of the type to match

**Examples:**

1. ```tony
bool: !type true
```

2. ```tony
number: !type 1
```

3. ```tony
string: !type ""
```

**See also:** [`!all`](./mergeop.md#all), [`!field`](./mergeop.md#field)

---

