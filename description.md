# mergeop: !lt, !le, !gt, !ge order RFC 3339 timestamps by instant

The comparison ops from nqe7v0j0 order numbers. Tony has no time type, so a timestamp is a string, and RFC 3339 is the format tony-format already uses for one: logd retention parses item timestamps with `time.RFC3339`. That makes it the one kind of string with a meaningful order. Comparing the text doesn't give that order: `2026-10-01T02:00:00+02:00` and `2026-10-01T00:00:00Z` are one instant, and fractional seconds break text order too.

## The rule

- When the operand is a string that parses as RFC 3339, the op compares instants. That covers a literal (`!lt "2026-10-01T00:00:00Z"`) and what a `!get-path` names. A number operand stays numeric.
- **Doc node:** an RFC 3339 string compares as an instant. Anything else doesn't match, including a number against a time operand or a timestamp against a number operand. That is the reading a non-number already gets.
- **Errors:**
  - A literal string that isn't RFC 3339, when the pattern is built.
  - A path that names a string that isn't RFC 3339, when the match runs.
  - A scale with a time operand, since a multiple of an instant means nothing.

## Not asked

"Older than an hour." That needs a clock, and a match would then depend on when it runs rather than on the document. As with logd retention's `now`, the caller computes the cutoff and writes it into the pattern, as a literal or through `!let`.