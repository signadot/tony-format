# parse: a lone `- !delete` parses as `[]`, dropping the element, and docs/tony.md's Key Sets sentence still refuses a bracketed `p: !delete`

In brackets, a tag with no value is a tagged null, decided in 0c466dff (pkj422gkh12kr24gj1n0): `{a: !delete}`, `[1, !delete]` and `{a !delete}` read as `!delete null`, the Tags section of docs/tony.md shows them, and token.balanceOne says so. Indented, `a: !delete` is refused with "a ':' must be followed by a value" (3ad0bf18). Two things are left out of step with that.

**A lone `- !delete` loses the element.** Checked at 773a0aae with parse.Parse:

```
- !delete               -> []        the element is gone, no error
- !delete\n- 1          -> refused, as "extraneous indent" at the second element
a: !delete              -> refused: a ':' must be followed by a value ...
```

An array of one tagged element becomes an empty array. The indented `a: !delete` is refused, so the matching answer for `- !delete` would be the same refusal, but either a refusal or a tagged null beats the silent drop.

**The Key Sets sentence says more than the decision.** docs/tony.md, Key Sets, says a `p:` with no value "is refused wherever it is written" and that `p: !delete` "is the same case as `p:`". That sentence came before 0c466dff, which made the bracketed `p: !delete` a tagged null. Read as written, it says the bracketed form is refused. It misled the first fix for yc5vx3z2h12kr89yvdn0 (5988e0a9), which refused it on the wire. The sentence needs to say it is about the indented form.

Also stale since 0c466dff: system/logd/storage/scope_premise_test.go says `{b: !delete}` inline "does not parse". It does.
