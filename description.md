# token: a '- ' with only a comment after it, or a tag and a comment, is refused

`- # c\n` and `- !t # c\n` parse as `[]` and drop the element. `- !t # c\n- 1\n` and `- # c\n- 1\n` are refused, but as "extraneous indent" rather than as a '- ' with no value.

Cause: danglingElt looks for the TIndent that ends the line right after the dash, or after the tag. A comment on the same line is a TComment token standing between them, so the check answers "not dangling".

3618245f made `- !t` refused, and this is the comment case it missed. `a: !t # c` is refused correctly.

Fix: danglingElt skips a comment on the dash's own line. `- # c\n  x\n` and `- !t # c\n  x\n` stay accepted, as [x] and [!t x].