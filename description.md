# mergeop: no escape — a patch cannot carry an operator tag as DATA, so a document containing tony operators cannot be stored (request: !raw)

The patch grammar and the data grammar share one tag namespace, and there is no escape. A
patch value carrying a registered operator tag is always *interpreted*, so a document that
contains tony operators — a match, a patch, a rule — cannot be written into a tony store at
all. I would like a patch-context escape (`!raw`, or whatever it should be called) whose
subtree is applied as VALUES, with no operator interpretation at any depth.

Found in [verse](https://github.com/signadot/verse) while making the trigger charter durable:
a charter rule IS a tony document full of operators (`id: !glob "hotfix-*"`,
`patch: {tmp: !delete null}`), and storing one as an entity payload is the natural thing to
want. All observations against **v0.0.98**.

---

## What happens today

`tony.Patch(doc, patch)`, one line per case:

```
plain          doc={}                patch={rule: {stage: open}}             → {rule: {stage: open}}
glob           doc={}                patch={rule: {id: !glob "hot-*"}}       → ERROR glob patching "null" gave cannot patch with glob operation
glob-over-val  doc={rule: {id: x}}   patch={rule: {id: !glob "hot-*"}}       → ERROR glob patching "x" gave cannot patch with glob operation
not            doc={}                patch={rule: {id: !not "1"}}            → ERROR not patching "null" gave cannot patch with not operation
delete         doc={rule: {tmp: 1}}  patch={rule: {tmp: !delete null}}       → {rule: {}}
quote          doc={}                patch={rule: !quote {id: "x"}}          → {rule: "null\n"}
pass           doc={}                patch={rule: !pass {id: !glob "hot-*"}} → {rule: null}
custom-tag     doc={}                patch={rule: !mytag {a: 1}}             → {rule: !mytag {a: 1}}
```

Three shapes of failure, and one that shows the way out:

- A **match-context** op (`!glob`, `!not`) in a patch is a hard error — the value cannot be
  stored at all.
- A **patch-context** op (`!delete`) is *executed*. It does not fail; it destroys neighbouring
  data instead of being stored, which is the worse of the two.
- The two ops that look like they might already be the escape are not. `!quote` quotes the
  **doc**, not the patch value (`QuoteY(doc)`, ignoring the child), so it yields `"null\n"`.
  `!pass` returns the doc, discarding the patch value entirely.
- **An unregistered tag round-trips perfectly.** `!mytag {a: 1}` is stored verbatim.

That last line is the argument. The data model already holds tagged values fine, and the
document side never executes anything — operators are read from the *patch*. The only values
that cannot be written are the ones whose tag happens to be a registered op. So this is not a
missing data capability; it is a missing way to say *"this tag is data"*.

## Why it is sharper through logd/docd

The store keeps the patch and replays it on every state read, so the failure is **deferred and
permanent**. The write reports success and the entity can never be read again:

```
write:  probe:rule:one = {patch: {tmp: !delete null, keep: 1}, id: !glob "hot-*"}   → committed=true, err=nil
read:   match error: failed to read state: failed to apply patches:
        glob patching "null" gave cannot patch with glob operation
```

Nothing rejects the write at the point it is made, and afterwards the entity's history contains
a patch that cannot be applied. A caller who did not know the rule about tags has lost that
entity, with an error that names neither the write nor the field that caused it.

## What I am asking for

A patch-context escape op — `!raw` (or `!lit` / `!data`):

```tony
patch:      { rule: !raw {id: !glob "hot-*", patch: {tmp: !delete null}} }
doc after:  { rule: {id: !glob "hot-*", patch: {tmp: !delete null}} }
```

The semantics I would want, though the model is yours:

1. **Recursive.** No operator is interpreted anywhere beneath it. A one-level escape does not
   help, because what needs storing is a whole document.
2. **Consumed at apply.** The `!raw` tag itself does not land in the document — the subtree
   lands as data, its own tags intact. This is consistent with the doc side never executing:
   the escape belongs to the patch, and the stored patch keeps it, so a replay escapes again.
3. **Match.** What a `!raw` subtree means to a matcher is the real design question here — I
   would expect `!raw` in a *pattern* to mean "compare these tags as literal data" rather than
   "evaluate them", so that a stored rule can be matched on. Happy with whatever is coherent;
   for my use case, storing is what matters and matching would be a bonus.
4. **Diff.** If a diff is ever computed between documents holding operator-named tags as data,
   the emitted patch needs to re-wrap them or it means something else when applied. I have not
   established this is broken today — `tony.Diff` → `tony.Patch` errors with `missing tag
   label` even for a plain `{rule: {id: "x"}}` in my hands, which is probably my misuse of the
   top-level pair — but it is the place I would look next.
5. No interaction with `RejectUnsafe`. `!raw` executes nothing; it is the opposite of `!pipe`.

## An alternative, and why I think the op is better

A whole-patch "interpret no operators" `PatchOpt`, alongside `RejectUnsafe`, would solve my
case with less surface. Two things put me off it: it is per-*write* rather than per-subtree, so
one patch cannot both delete a field and store a rule verbatim; and it does not travel with the
data — a stored patch replayed later needs the flag to come back with it, and the log carries
the patch, not the caller's options. A subtree op composes and is self-describing in the log.

There is also a forward-compatibility argument for having the escape be the *recommended* way
to store any tagged data: `!mytag` works today only because nothing has registered `mytag`. A
document holding it silently changes meaning the day that name becomes an op. An escape makes
"this is data" sayable rather than accidental.

## Workaround in verse today

The charter is stored as its tony **text** in a `spec` field and re-parsed on read
(`trigger:rule:<name>` = `{spec: "<the rule, as a document>"}`). It works and it stays legible,
but it costs structural matching over the stored rule — the payload is a document in a string,
so nothing can ask "which rules spawn with the scripted driver" — and every reader needs the
convention. I would drop it for `!raw`.