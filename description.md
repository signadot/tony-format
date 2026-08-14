# eval: a tag is dropped when .[var] is substituted, so !not .[x] in a !let body silently matches equality

go-tony v0.0.132. Found building a charter predicate in verse; it is a wrong
ANSWER rather than an error, which is why it is worth a report rather than a
workaround.

## Repro

Matching against the document `{base: abc123, state: open}`:

| pattern | got | want |
|---|---|---|
| `!let {let: [{tip: abc123}], in: {base: .[tip]}}` | true | true |
| `{base: !not zzz999}` | true | true |
| `!let {let: [{tip: zzz999}], in: {base: !not .[tip]}}` | **false** | true |
| `!let {let: [{tip: abc123}], in: {base: !not .[tip]}}` | **true** | false |

The last two are inverted: inside a `!let` body, `!not .[x]` behaves as `.[x]`.
No error is returned -- `tony.Match` answers cleanly, with the opposite verdict.

Driven through verse's own matcher (entity.Pattern), and the raw
`tony.Match(doc, pattern)` agrees, so it is not verse's wrapper.

## Where it looks like it comes from

`eval/expand_env.go`, ExpandIRWithOptions. The container branches re-tag their
result:

    225:  return ir.FromKeyVals(kvs).WithTag(node.Tag), nil
    236:  return ir.FromSlice(res).WithTag(node.Tag), nil

The StringType branch -- the one that REPLACES a `.[var]` node with the bound
value -- carries the parent relationship across and not the tag:

    repl.Parent = node.Parent
    repl.ParentIndex = node.ParentIndex
    repl.ParentField = node.ParentField
    257:  return repl, nil          // and again at 271, via FromAny

`!not .[tip]` parses as a String node `.[tip]` carrying Tag "not", so the
substitution returns the bound value untagged and the negation is gone by the
time the matcher sees it.

(Scott's read is that this is about what the expr-lang result carries rather than
about expr-lang itself -- the value comes back as a plain node and nothing
re-applies the tag the reference was wearing.)

## Why it matters beyond this case

`!let` is the natural way to write "this field differs from that value", and the
differs half is the half that silently lies. Any operator wearing a tag over a
`.[var]` has the same shape: `!glob .[pat]`, `!type .[t]`, `!irtype .[t]`.

A tag that cannot be preserved would be better refused than dropped -- but
preserving it looks like the same one-liner the two container branches already
do.

## What it blocks

verse wants `base: !not .[tip]` as a charter predicate: a proposal is stale
exactly when the branch it was cut from is no longer the tip of main. That rule
would currently mark every FRESH proposal stale and leave the stale ones open --
the precise inversion above, in a rule that deletes branches.