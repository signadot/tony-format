# parse: a bracketed tag followed by a line break, then `,` `}` or `]`, is a tagged null

The parser refuses a bracketed tag when a line break, or a line comment and a line break, stands between it and the `,`, `}` or `]` that ends it. The messages don't describe the input:

- `{a: !t\n}`: "unopened }"
- `{a: !t # c\n}`: "unopened }"
- `[!t # c\n]`: "unopened ]"
- `{a: !t\n , b: 1}`: "non separating ','"
- `[!t # c\n, 1]`: "non separating ','"

The wire decoder reads each one as a tagged null. Implicit Null's second place doesn't require the tag and the bracket to be on one line. Scott decided (2026-10-31) that the newline case is valid.

Cause: token.balanceOne's TTag case checks only the token right after the tag for endsValue. A TIndent or a comment there sends it on to balance a value that isn't there.

Fix: look past line breaks and comments for the token that ends the value. The comments stay where they are, after the null.