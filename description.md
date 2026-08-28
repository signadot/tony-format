# mergeop: a comment about a deleted value moves onto its next sibling

A head comment is a wrapper node around the value it is about. When a patch deletes
that value the wrapper survives and re-attaches to whatever begins next, so a comment
about something that is gone ends up over something else.

```
$ cat doc.tony        $ cat patch.tony       $ o patch -c -f patch.tony doc.tony
a: 1                  # about a              # about a
b: 2                  a: !delete null        b: 2
```

The comment now says "about a" over `b`.

It is not confined to a delete of the commented node itself. Any patch that removes the
value a comment wraps moves the comment onto its next sibling:

```
doc:    # old            patch:  d:                result:  # old
        d:                         # new                    d:
          e: 1                     e: !delete 1               # new
          k: 9                                                k: 9
```

`# new` was written about `e`. `e` goes; `# new` lands on `k`.

## why it matters more than it looks

`!comment` exists so that changing a comment does not mean replacing the value it
describes -- `api.storableTags` says so, and it is in the storage vocabulary for that
reason. The other way to state a comment in a patch is to carry it as a wrapper, which
`patch.go` handles deliberately: "The patch's comment is what the writer just said
about the value."

So a store that keeps comments has two shapes that mean "this value, and this comment",
and one of them silently relocates the comment when the value goes away. Anything built
on the wrapper form inherits that.

Found while looking for a way to state a comment change and a value change at one node
for the lowering (xqpvk3ehh12ks89mj5n0). The wrapper form looked like the answer and is
not, on this evidence.

## a note on composition, which is NOT the defect

`!comment.replace` fails -- "comment op position is "head" or "line", got "from"" --
because the head of a composed chain takes the child, and `!comment` validates the
child's field names strictly. That is not a general rule about composing two
child-taking operations: `!replace.strdiff {from,to}` composes and answers correctly.
Whether `!comment` should accept its positions as tag ARGUMENTS, so it composes with
anything, is a separate question from this one.