# eval: a whole .[…] gives back a value, not the node it names, so tags, comments and text-only numbers are lost

Context: verse writes patches and derives with `eval.ExpandIRWithOptions` (verse PR #74, branch `verse/patch-eval`, `launch.ExpandValue`). A patch value that is one `.[…]` copies what it names onto an entity, typed: `contract: .[subject.value.contract]`. Verse's old deref copied the NODE (`ir.Node.GetKPath`). Through eval it gets a value back, and verse cannot recover the tony it read. Verse commit ca06c4365 documents the loss as it stands.

## What is lost

The environment is `map[string]any`, so an embedder fills it with `ToAny` of its nodes, and `ToAny` keeps only data:

- **Tags:** a `!raw` contract read from the subject is written back as a plain object. Any tag on the node read, or on anything under it, goes the same way.
- **Comments:** a `CommentType` node is unwrapped, so head and line comments on what is read are dropped.
- **Numbers held only as text:** a `NumberType` node with neither `Int64` nor `Float64` set (a big integer, a decimal kept exactly) becomes `node.Number`, a Go string. `FromAny` then writes it back as a STRING: `n: 123456789012345678901234567890` arrives as `n: "123456789012345678901234567890"`.
- **Field order:** an object is a Go map, so key order comes back as `FromAny` decides it.

A tag the CHARTER writes on the place, as in `tmp: !delete .[x]`, is the embedder's to keep, and verse keeps it. The issue is the node being read.

## What already points the way

- `FromAny` keeps a `*ir.Node` as it is ("preserves tags/comments"), and `ExpandIRWithOptions` clones and returns a `*ir.Node` result. So a node only has to reach the evaluator's result for the whole-value case to keep it.
- `strict.go` already picks out a plain path (a name, then `.field` steps and constant indexes) and looks it up itself before expr-lang runs.

## Ask

Let an embedder give the environment as nodes, and let a whole `.[…]` that is a plain path answer the node it names. For example:

- an option (or an `Env` form) whose values may be `*ir.Node`;
- a plain path resolved by walking the ir (fields, indexes, through a comment wrapper), answering a clone of the node it reaches, with its tags, comments, number text and key order;
- any other expression (`a ?? b`, `x + 1`, a call) evaluated as now, over `ToAny` of those nodes, done lazily or up front. Its result is a computed value, and it has no tags to keep.

The plain path keeps the same strictness as now: an absent name, field or index is an error.

Inside `$[…]`, in a string, the result is text either way. The `Composite` form would then write a node read whole with its tags (`CompositeTony`), which it cannot do today.

Verse would then bind `subject.value`, `data` and the payload's fields as nodes, and a patch copying a contract would write the contract.