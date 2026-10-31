# parse: a tag with no value is accepted inside brackets, and a lone `- !delete` parses as `[]`, where the spec refuses both

docs/tony.md (Key Sets) says a tag is not a value: `p: !delete` is the same case as `p:` and is refused; write `p: !delete null`. The indented parser does so, with that message (token.errDanglingColon, 3ad0bf18). Inside brackets it does not, and one indented shape drops data instead of refusing.

Checked at 773a0aae with parse.Parse:

```
{a: !delete}            -> {a: !delete null}          accepted
{a: 1, b: !delete}      -> {a: 1, b: !delete null}    accepted
[!delete, 1]            -> [!delete null, 1]          accepted
{3: !delete}            -> !sparsearray {3: !delete null}  accepted
!delete                 -> !delete null               accepted
a: !delete              -> refused: a ':' must be followed by a value ...
- !delete               -> []                         the element is gone, no error
- !delete\n- 1          -> refused, but as "extraneous indent" at the second element
```

The bracketed acceptance comes from parse.noComments, which answers a tagged null when the tokens run out under a tag, and from parseBalanced recursing into `}` or `,` with the tag in hand. The `- !delete` case loses the element: an array of one tagged nothing becomes an empty array.

The stream decoder now refuses the bracketed shapes with the spec's wording (yc5vx3z2h12kr89yvdn0), so a document the parser accepts is one the wire refuses. Either the parser refuses them too, as the spec says, or the spec changes and the decoder follows. Several storage tests write `{k: !delete}` as a patch through parse.Parse (lower_claim_test.go, children_test.go), and scope_premise_test.go notes that `{b: !delete}` "does not parse" where it actually does, so the tests have been leaning on the gap.