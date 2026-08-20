# tony: is a comment allowed after | on a block literal's opening line?

A question the spec does not answer and the tokenizer answers by refusing:

    key: | # comment
      content

    unexpected   at `...ey: | # co...` at offset 6

WHAT ARGUES FOR REFUSING, which is what it does today. docs/tony.md makes the whitespace
after `|` significant -- the leading-space example turns on it -- and it deliberately drops
YAML's block-scalar header grammar ("Tony block literals do not support folding or any other
of the myriad of YAML variants"). With no header grammar, `#` after `|` has no defined place
to live, and reading it as a comment would make `| ` and `| #x` differ in a way nothing else
in the format does.

WHAT ARGUES FOR ALLOWING IT. Every other line in a Tony document may carry a trailing
comment, and a reader who writes one here is not doing anything strange. YAML allows it. The
refusal is also worded as a tokenizer surprise ("unexpected") rather than as a rule, so a
person meeting it learns nothing about why.

WHY IT IS FILED. The test which covers this case used to accept EITHER outcome: it validated
"we don't panic, and if there is an mLit that's good enough", and logged any error as "may be
expected". That is not a pin, it is a test that cannot fail, and it hid which behaviour was
intended. It now asserts the refusal and points here, so whichever way this is decided, the
test says so (found sweeping for tests that pin bugs as bugs).

If the answer is "refuse", the error deserves to name the rule rather than say "unexpected".