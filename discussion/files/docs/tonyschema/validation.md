# Validation

Validation is the process of checking whether a document conforms to a schema's
constraints. Tony Schema uses match operations to describe what documents are
valid.

## How Validation Works

A schema's `accept` field defines what documents it accepts. The `accept` field
uses match operations to describe constraints that valid documents must satisfy.

```tony
accept:
  !and
  - .[post]
  - !not null
```

This `accept` clause says: "accept documents that match `.[post]` AND are not null".

## The `accept` Field

The `accept` field is optional. If omitted, the schema accepts all documents
(no validation is performed).

If present, `accept` must evaluate to a boolean match operation. Common patterns:

### Accept a Single Definition

```tony
accept: .[post]
```

Accept any document that matches the `post` definition.

### Accept Multiple Options

```tony
accept:
  !or
  - .[post]
  - .[comment]
  - .[page]
```

Accept documents that match any of these definitions.

### Accept with Constraints

```tony
accept:
  !and
  - .[post]
  - status: "published"
  - !not null
```

Accept documents that match `post`, have `status` equal to `"published"`, and
are not null.

### Accept with Negation

```tony
accept:
  !and
  - !not .[draft]
  - !not .[archived]
```

Accept documents that are NOT drafts and NOT archived.

## Match Operations

The `accept` field uses match operations from the match context. Common operations:

- `!and` - All conditions must match
- `!or` - At least one condition must match
- `!not` - Condition must not match
- `!type` - Type must match
- `!field` - Field must exist and match
- `.[definition]` - Reference to a definition in `define` (expr-lang format)

See the [match operations documentation](../matchpatch.md) for more details.

## Validation Process

When validating a document against a schema:

1. **Resolve Context**: Determine which context the schema uses
2. **Evaluate Accept**: Evaluate the `accept` field as a match operation
3. **Check Result**: If the match succeeds, the document is valid

If validation fails, the system should provide information about why:

- Which constraint failed
- What was expected vs. what was found
- Path to the failing node

## Example: Complete Validation Schema

```tony
context: tony-format/context/match

signature:
  name: user-schema

define:
  # Define what a user looks like
  user:
    name: !irtype ""
    email: !irtype ""
    age: !and
    - .[number]
    - age: !not null
    - age: !type number
  
  # Define what a valid user is
  valid-user: !and
  - .[user]
  - name: !not null
  - email: !not null
  - age: !and
    - !not null
    - !type number

accept:
  .[valid-user]
```

This schema:
1. Defines what a `user` looks like
2. Defines what a `valid-user` is (a user with required fields)
3. Accepts only documents that match `valid-user` (using `.[valid-user]` expr-lang reference)

## Validation Status

!!! note "Implementation Status"
    Validation is currently not fully implemented. The `Schema.Validate()` method
    exists but returns an error indicating validation is not yet implemented.

The validation system will use the match operations to check documents against
the `accept` clause, providing detailed error messages when validation fails.

## Future Enhancements

Planned enhancements to validation:

- **Detailed Error Messages**: Path to failing nodes, expected vs. actual values
- **Partial Validation**: Continue validating even after first failure
- **Schema Composition**: Validate against multiple schemas
- **Custom Validators**: Allow schemas to define custom validation logic
