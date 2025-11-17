# Tony IR

Tony has a simple recursive intermediate representation, which itself is
readily representable in JSON, YAML, and Tony.  This IR provides a model of all
Tony documents and can be used to manipulate Tony documents in contexts which
lack parsing and encoding support.

Here is an example:
```tony
!head-tag
# comment
field: # field comment
  - 1
  - !two-tag two
  - 3.01e777
  -
    "hello multiline"
    'string'

# trailing comment
---
# IR of the above
type: Comment
values:
- type: Object
  fields:
  - type: String
    string: field
  values:
  - type: Array
    values:
    - type: Number
      comment:
        type: Comment
        lines:
        - " # z"
      int: 1
  tag: "!head-tag"
  comment:
    type: Comment
    lines:
    - ""
    - "# line comment 1"
lines:
- "# head comment"
```


## Schema

### Tony Schema

Here is a Tony schema for the Tony IR defined in terms of the
[base schema](/tonyschema/#tony-base-schema).

```tony
signature:
  name: tony-ir
define:
  field: !or
  - .string
  - .int
  - .null
  ir:
    type: !or [Comment Null Bool Number String Array Object]
    fields: .array(.field)
    values: .array(.tony)
    string: .string
    int: .int
    float: .float
    number: .string
    bool: .bool
    lines: .array(.string)
accept: .ir
```

### JSON Schema

The JSON schema for this IR is below expressed in Tony format for clarity.

```tony
$defs:
  tony:
    type: object
    properties:
      tag:
        type: string
      type:
        enum: [Object Array String Bool Number Null Comment]
      bool:
        type: bool
      string:
        type: string
      number:
        type: number
      int:
        type: integer
      float:
        type: number
      fields:
        type: array
        items:
          $ref: "#/$defs/tony"
      values:
        type: array
        items:
          $ref: "#/$defs/tony"
      comment:
        $ref: "#/$defs/tony"
      lines:
        type: array
        items:
          type: string
$ref: "#/$defs/tony"
```

Note that the IR contains absolutely no position information of some input
document.  As a result, the IR is purely semantic.

## Additional Constraints

As often happens with schema, the schema itself fails to capture
all that is needed. The same is true of the tony schema.  Here
we describe those constraints.

In the following, let's keep in mind that the Tony IR works as a recursive
tagged union structure, where values are placed in fields depending on the node
type.

### Objects

For objects, `.fields[i]` is the key for the value at `.values[i]`, so there
will always be the same number of fields as there are values.

fields in turn are always either string typed (and not multiline) or int typed
(and fitting in a uint32), or null typed.  The null typed field represents a
merge key and may occur several times.  Other keys should appear only once.

Objects must either have all keys int typed, or all keys not int typed.

### Strings

Strings canonical values are stored under `.string`.  If the string was a
multiline folding tony string, then the `.lines` may contain the folding
decomposition.  Producers of Tony IR should not populate .lines where .string
is not equal to the concatenation of .lines.  Consumers of Tony IR should check
if they are equal and if not, remove the .lines decomposition and consider
.string canonical.

Additionally, in case there is a mismatch between the lines and .string fields,
comments for a multiline string should be considered as appended to
the head comment of the string.


### Numbers

Numbers values are placed under `.int` if it is an integer and this should work
with 64 bit signed integers.  Likewise, they are placed under `.float` if they
are floating point numbers and should work with 64 bit IEEE floats.

In case that doesn't work, they are stored as a string under `.number`.

### Comments

Comment typed nodes define node comment association.

The content of a comment typed node, meaning the text of the comments,
is placed in the `.lines` field of the IR.

A comment typed node either

1. Contains 1 element in `.values`, a non-comment node to which it is associated
as a _head comment_; or
1. Contains 0 elements and resides the `.comment` field of a non-comment node and
represents its _line comment_ plus possibly any trailing comment material in
the event it is associated with the root non-comment node of the document.

A comment typed node may not represent both a head comment and a line comment/document
trailing comments.

In the second case, normally it represents a single 'line comment'  such as
`null # this is null b/c ...` and there is only 1 entry in `.lines`.  All such
comments must contain all whitespace between the end of the value they comment
and the `#`.  This makes it so that vertically aligned comments retain vertical
alignment.

But there is exactly 1 exception.   All comments not preceding any value nor
occuring on the same line of any value are collected and appended to the lines
of the comment node residing in the `.comment` field of the root non-comment
node.  If that node has no line comment, a dummy line comment is present with
value "".

Note that in this representation, a document consisting of only comments will
have 1 IR Comment node and `.values` of length 0.

Here are some examples:

```tony
null
# end of document
---
# IR of above
type: Null
comment:
  type: Comment
  lines:
  - ""
  - "# end of document"
---
null               # this is null b/c ...
# end of document  ^
---
# IR of above
type: Null
comment:
  type: Comment
  lines:
  - "               # this is null b/c"
  - "# end of document  ^"
```
