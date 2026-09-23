# eval: an expression cannot fail, and absence is uneven -- null under a present parent, an error under a missing one

Measured on go-tony v0.0.234, from verse, where a charter's gate `pose` is already expanded with
`eval.ExpandString` and the rest of a charter's references are being converged onto `$[…]` /
`.[…]` (verse issue j28egpp2). Two things an expression cannot say, and one it says unevenly.

## 1. An expression cannot fail

There is no way to say "this should not have happened". `fail("no sha")`, `error("no sha")` and
`panic("x")` are each `invalid operation: cannot call nil`, and arithmetic does not help —
`$[1/0]` is `+Inf`, not an error.

The only spelling that works is raising a real evaluation error on purpose, by fetching through
something known to be nil:

    "sha" in subject.value ? subject.value.sha : subject.value.MISSING.x

That works — ternaries short-circuit, so the else branch is only evaluated when it is taken — and
it reads like a mistake rather than an intention. What it is spelling is "absent here is not a
default, it is a stop".

Ask: a builtin that raises the expression's own error with the author's message. `fail(msg)` or
`error(msg)`, answering nothing, so that `cond ? value : fail("what is wrong")` is how a caller
says an expression has no answer. A caller embedding eval (verse) turns that into a refusal
carrying the message; `o eval` prints it.

## 2. Absence is uneven: whether a missing field is null or an error depends on its parent

    $[subject.value.nope]           -> null      a leaf missing under a present object
    $[subject.value.nope.deeper]    -> error     cannot fetch deeper from <nil>
    $[subject.value.sha]            -> error     when subject.value itself is nil
    $[subject.value.sha ?? "unset"] -> error     …and ?? does not rescue that one

So one missing name renders null and two missing names is an error, for the same absent data.
The last line is the sharp edge: `??` catches a nil VALUE but not a fetch ON nil, so a default
written by an author who expects `??` to mean "when this is not there" silently fails to cover
the case where the parent is not there either — which, for us, is a fan-out member that is a
bare name with no payload. The author cannot tell from the expression which of the two they
are in, because it depends on the data.

Ask: one rule for a path that is not there, whichever segment stopped it. Either is defensible —
null throughout, so `??` covers all of it, or an error throughout, so nothing is silently absent —
but a caller can adapt to one rule and cannot adapt to "it depends where it stopped". If the two
are deliberately different, the difference is worth stating in the eval package docs, which today
say what `$[…]` and `.[…]` DO and nothing about what they do when the data is missing.

## What does work, and is worth keeping

`in` distinguishes absent from written-null, which `== nil` cannot:

    $["sha"  in subject.value]   -> true
    $["nul"  in subject.value]   -> true    (written null)
    $["nope" in subject.value]   -> false
    $[subject.value.nul  == nil] -> true
    $[subject.value.nope == nil] -> true

That is the distinction tony keeps in the document (docd tombstones with null), and it survives
ToAny into the environment — including when the whole payload is nil, where `"sha" in
subject.value` still answers false rather than erroring. It is what lets a caller leave the
absent-or-default policy to the author instead of pre-walking references itself, and it is the
reason #2 above is a wart rather than a blocker.