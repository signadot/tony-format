# match: a mismatch is a bare false — no path, no expected, no found (request: an explanation MatchOpt)

`tony.Match` answers yes or no. When the answer is no, nothing says WHY: not which path failed,
not what was expected there, not what was found. I would like an option — `MatchOpt` is already
declared in `match.go` and consumed by nothing — that collects a structured explanation as the
match walks. Both polarities are useful: why this document did NOT match (a repair loop), and
which branch of an `!or` DID match (a rule debugger).

Found in [verse](https://github.com/signadot/verse) while designing an ANSWER CONTRACT: a spawned
agent declares the shape its final message must have, the loop validates the answer against it,
and on failure feeds the validation error back to the model to repair, bounded at one or two
attempts. The whole design turns on that feedback being specific enough to act on — "it did not
match" is the useless kind of error, the kind that asks the model to guess again. Observations
against **v0.0.99**, identical on v0.0.100.

---


A schema's `accept` clause is the natural way to write such a contract, and `schema.Validate` is
`tony.MatchWith` plus a wrapper (`schema/schema.go:52-85`). Against this schema:

```
signature: {name: answer}
accept:
  class: !or [bug, nit, risk]
  severity: !or [low, high]
  why: !irtype string
```

one line per document, `schema.Validate`:

```
ok             {class: bug, severity: high, why: "nil deref"}                 -> nil
bad-enum       {class: critical, severity: high, why: "nil deref"}            -> document does not match schema
missing-field  {class: bug, why: "nil deref"}                                 -> document does not match schema
wrong-type     {class: bug, severity: high, why: 3}                           -> document does not match schema
two-failures   {class: critical, why: "nil deref"}                            -> document does not match schema
extra-field    {class: bug, severity: high, why: "x", note: "chatty"}         -> nil
```

Every rejection is the same sentence, and it names neither the path nor the reason. `two-failures`
has a value outside the enumeration AND a missing field; the message reports the count as well as
it reports the identity, which is to say not at all.

The last two rows are the interesting ones. `extra-field` passing is correct and good — object
matching is open-world (`match.go:88-111`), so an answer carrying more than it was asked for is
still an answer. But `wrong-type` fails *identically* to `missing-field`, so a caller cannot even
distinguish ABSENT from PRESENT-BUT-WRONG, which is the one distinction it could plausibly have
recovered by itself.

Underneath, `tony.Match(doc, pat)` returns `matched=false, err=nil`. The error return is for
malformed patterns (`no mergeop for tag %q`), not for mismatches — which is right, and is exactly
why the explanation needs a channel of its own rather than a richer `error`.


Enough is visible from outside to do the boring half. Object structure is a plain field walk, so an
external explainer can recurse over the pattern, notice `severity` is absent from the document, and
report `severity: required, absent` without help.

It stops at the first tag. Every operator goes through `Op.Match(doc, ctx, f) (bool, error)`
(`mergeop/op.go:9`), so at a tagged node the outside gets one bit:

- `!or [bug, nit, risk]` — the caller can at least render the alternatives it can see in the
  pattern, so this one degrades gracefully.
- `!all {severity: !or [low, high]}` over a twelve-element list — `matched=false`, and WHICH
  element failed is unrecoverable without re-running the match per element, which means
  reimplementing the operator outside with semantics that are only guessed to agree.
- `!not`, `!and`, `!glob`, `!irtype`, `!has_path`, and every operator added after this issue —
  the same, and the last clause is the real cost: an outside explainer is wrong by default for
  anything new.

So an external explainer can explain structure and nothing else, while the tagged parts are
precisely the parts an author or a model gets wrong.


Shape, not a demand on naming:

```go
var why tony.Explanation
ok, err := tony.Match(doc, pat, tony.Explaining(&why))
// ok == false
// why.Failures == []tony.Mismatch{
//   {Path: ".class",               Op: "or", Expected: <ir [bug,nit,risk]>, Found: <ir "critical">},
//   {Path: ".severity",            Reason: tony.Absent, Expected: <ir !or [low,high]>},
//   {Path: ".findings[7].severity", Op: "or", Expected: ..., Found: ...},
// }
```

What we would use, in rough order of value:

1. **A path into the document**, kpath-shaped, carrying list indices — including indices chosen
   inside `!all`, which is the part only the operator knows.
2. **Expected as an `*ir.Node`**, not a rendered string. The caller renders it for whatever
   audience it has; we would print it as tony, because tony is what we asked the agent to write.
3. **Absent distinguished from present-but-wrong.**
4. **All failures, not just the first.** One repair round should be able to fix everything that is
   wrong rather than everything-minus-one. First-failure-only is a reasonable default with an
   option to collect all; the reverse ordering of defaults would cost us a round trip per defect.

The positive polarity deserves the same fields. Which `!or` branch matched and which `!all` element
matched is what makes a rule debuggable, and a verse trigger rule is literally a tony match — today
"why did this trigger fire?" has the same one-bit answer as "why did it not?".

Non-goals from our side: the explanation need not be human prose, and it need not be stable across
versions — we format it ourselves and we do not test against its wording. One caution: `Found` can
be an arbitrarily large subtree, and ours goes into a model prompt, so a size bound or a
caller-supplied truncation would earn its keep.


`MatchConfig` and `MatchOpt` (`match.go:11-23`) are declared and consumed by nothing: `Match` and
`MatchWith` take no options, and the `Comments`/`Tags` fields have no effect on matching. The
vehicle for this request already exists as a stub. Whatever wires an explanation option through
probably has to decide what those two mean, or delete them.