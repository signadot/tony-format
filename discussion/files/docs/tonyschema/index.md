# Tony Schema

!!! warning  ""
    This material is going to change.

Tony Schema are similar to [json schema](https://json-schema.org), a
class of documents in a format for describing constraints and information
about other documents in that format.

Despite Tony being a more complex data format, Tony schema strive to be simpler,
more light weight and more readable than json schema.

## Purpose

Schema of all kinds are documents which help folks concretely and precisely
create models of objects.  It is often more important that a schema _be
understood_ than it is to use a schema for validation of syntax errors.

Schema are used as a means of communicating models precisely between
stakeholders and also as a sketch pad for individual model designers.

However, once agreed upon by stakeholders schema continue to be useful for:

- describing precisely what system should be built; and
- validation of inputs to that system; and
- documentation; and
- automation


## Modeling

For modeling, schema need comments and succinct, precise ways of presenting
relations between things.

Perhaps something as follows would be a good start.

```tony
# example schema (sketch)
context: tony-format/context

# signature field defines how a schema can be referred to
signature:
  name: example # so we can use '!example' to refer to this
  args: []

# define provides a place for value definitions, like json-schema $defs.
define:

  # each field provides a definition of what it accepts, using
  # matches and other schema
  ttl:
    offsetFrom: !or
    - createdAt
    - updatedAt
    duration: .[duration]
  
  duration: !regexp |-
    \d+[mhdw]

  # recursive definitions are possible
  node:
    parent: .[node]  # .[name] refers to things under $.define using expr-lang
    children: .array(.[node]) # type params work to array ref below
    description: \.startswith.

accept:
  !or
  - !and
    - !not .[ttl]
    - !not .[node]
  - !example # reference to this schema itself
```

## Components

Tony Schema documents consist of several key components:

- **[Schema Structure](schema.md)**: The core structure with `signature`, `define`, and `accept` fields
- **[Contexts](contexts.md)**: Execution contexts that define where operations execute and which tags are available
- **[Tags](tags.md)**: Operations and type markers that work within specific contexts
- **[Validation](validation.md)**: How documents are validated against schemas


## Tony Base Schema

Below we model a base set of relations using the schema format above.

```tony
# example base
context: tony-format/context
define:
  bool: !irtype true
  "null": !irtype null
  number: !irtype 1
  int: !and
  - .[number]
  - int: !not null
  float: !and
  - .[number]
  - float: !not null
  string: !irtype ""
  array:
  - !irtype []
  array(t): !and
  - .[array]
  - !all.type t
  sparsearray: !and
  - !irtype {}
  - !all.field.type 0
  sparsearray(t): !and
  - .[sparsearray]
  - !all.type t
  # keyed lists
  keyed(p): !and
  - !irtype []
  - !all.hasPath p
```

