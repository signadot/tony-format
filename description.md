# logd: the storage-vocabulary chain walk reads past !raw, so correctly escaped data is refused

Found against the lowering branch — `worktree-lowering` at `6fb4a4a` — by pointing
[verse](https://github.com/signadot/verse) at it with a `go mod replace` and running verse's
suite. The rest of it came through clean: the new delta shapes and the scoped watch's
recompute-and-diff are fine from a consumer's side. This one **refuses writes that v0.0.194
accepts**.

## What happens

`firstRelativeOp` (`system/logd/api/lowering.go`) walks the whole tag chain and answers the
first registered label that is not storable. `raw` **is** storable, so the walk does not stop
there — it carries on and finds whatever the escape was put in front of.

But `!raw` is the one label where the rest of the chain does not bind. Everything after it is
data that happens to be shaped like an operation, which is the whole of what the escape says.
Escaping a leaf composes onto that node's own tag: `!irtype` escaped is `!raw.irtype`.

Both callers inherit it, and in both the `ir.TagHas(n.Tag, "!raw")` short-circuit sits **below**
the chain check, so for the composed form it is unreachable:

- `validateForStorage` (`storage_context.go:133`) — refuses the write outright
- `NeedsLowering` (`lowering.go:55`) — answers that a correctly escaped write needs lowering,
  and `ValidateForStorage` on the resulting delta then refuses it anyway

The comment on the dead guard is the statement of the bug, `6225etzfh12kr955fxn0` included:

> `!raw` says nothing beneath is interpreted, so nothing beneath is an operation to hold to
> this vocabulary — it is data that happens to be shaped like one. Walking into it refuses the
> one escape that lets a document holding operators be stored at all, which is what a charter,
> a stored rule and a stored patch are.

## Repro

```
says: !raw.irtype null            ValidateForStorage=at says: operation "!irtype" may not be
                                    stored: it transforms whatever it finds rather than
                                    stating what results
                                  NeedsLowering=("!irtype", true)

says: !raw {inner: !irtype null}  ValidateForStorage=<nil>   NeedsLowering=("", false)
says: !irtype null                ValidateForStorage=refused NeedsLowering=("!irtype", true)
```

The **subtree** form is correct — `!raw` on a node wrapping children reaches the short-circuit.
It is specifically composition onto one node's tag.

`raw_chain_test.go` is attached: four cases, of which only the composed escape fails. The
fourth is `!insert.strdiff`, the case `firstRelativeOp` was introduced for, so the test also
pins that a fix does not give that back.

## Why it matters

The composed form is what an escaping writer produces, because escaping a leaf has nowhere else
to put the label. In verse `entity.AsData`/`entity.Raw` walk a payload and rewrite each tagged
node's tag as `!raw.` + what it had — "the escape is consumed and the data keeps its tags" —
so every payload holding an operator-tagged **leaf** is now refused at the write.

That is not an edge case for a store: a charter, a stored rule, a stored patch and an agent's
answer echoing its contract are all documents written in the operator grammar and stored as
data. Four verse tests fail, and all four exist to guard exactly this class:

```
entity.TestAnOperatorTagInAPayloadDoesNotPoisonTheStore   at data.says:   !irtype
entity.TestTheStoreStillAcceptsEveryLegitimateWrite       at says:        !irtype
entity.TestTheWriteGuardSeesUnderAComment                 at shape:       !irtype
                                                          (plain, under a head comment, nested)
mirror.TestAnAnswerEchoingItsContractLandsAsData          at data.status: !or
```

The failure is loud rather than silent, which is the good half: the write is refused with a
clear message, not stored and choked on later.

## Suggested fix

Stop the chain walk at `raw` in `firstRelativeOp` — once the escape is seen, the rest of the
chain is data and there is no operation left to find:

```go
for tag != "" {
    head, _, rest := ir.TagArgs(tag)
    head = strings.TrimPrefix(head, "!")
    if head == string(rawName) {
        return ""
    }
    if mergeop.Lookup(head) != nil && !IsStorableTag(head) {
        return head
    }
    tag = rest
}
```

That keeps what the whole-chain walk was for — `!insert.retag(x,y)` and `!insert.strdiff` are
still caught, since neither is behind an escape — and makes the two `ir.TagHas(n.Tag, "!raw")`
guards below reachable again for the composed case they were written for.

Worth deciding whether those two guards then stay as the subtree case or fold into the walk;
they read as one rule stated in two places either way.