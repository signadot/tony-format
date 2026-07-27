# parse: a comment at the end of a container is dropped instead of cascading to the next value (spec: 'dedented or higher')

A comment at the end of a container is dropped when a value follows outside it:

```
g: {
  a: 1
}
h: 3
```

The spec says it should cascade to the next value:

> All subsequent comments are attributed to the preceding comments of the next value, which
> may be **dedented or higher in the object notation**.

"Dedented or higher" is exactly this case: the next value after `# c` is `3`, one level up
and outside the braces. So `# c` should become the preceding comment of `h`'s value.

The end-of-*document* case is already handled — "all trailing comments at the end of a
document are associated as additional lines of the line comment of the top most element" is
implemented in `parseBalanced`'s `p == nil` branch. It is the end-of-*container* case that
has nowhere to go.


`parseObj` collects comments into `ycMap` keyed by the index of the pair they precede, so
comments after the last pair land at `ycMap[len(kvs)]` — an index with no pair to head.
`applyHeadComments` attaches `ycMap[i]` for `i < len(kvs)` and leaves that last bucket alone,
because an object has no place to hang a value with no key, and `parseObj` has no way to
return unconsumed comments to its caller.

The comment in `parseBalanced` anticipates this:

> most trailing comments are actually leading comments for the next item, and Tony defines
> them to be so associated. It is difficult in a recursive descent parser to associate
> comments with nodes correctly. So, they are associated in this incorrect way here and then
> corrected in a subsequent pass. see associateComments

`associateComments` already carries comment lines across siblings within one container and
handles empty `CommentType` nodes as trailing markers. The missing piece is getting the
container's leftover comments *out* of `parseObj` and into that pass — either by returning
them alongside the node, or by emitting an empty `CommentType` marker that
`associateComments` then carries to the next value at the enclosing level.

The same question applies to `parseArr`, which should be checked while this is being done.


`TestTrailingContainerCommentIsDroppedForNow` in `parse/object_comments_test.go` asserts the
comment is dropped, and fails loudly with a pointer to this issue if it ever starts being
preserved — so implementing the cascade will trip it deliberately rather than silently.

Context: found while fixing `911d959` (leading comment in a brace object failed to parse;
interior object comments were silently dropped).