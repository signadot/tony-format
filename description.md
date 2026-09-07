# mergeop: a !raw subtree should MERGE -- escaping is not replacing, and !insert.raw already says both

!raw as a patch does two separable things at once: it ESCAPES (nothing beneath it is read
as an operation) and it REPLACES (the document at that path is discarded rather than merged
into). Only the first is what the escape is for, and only the first is what the operator's
documentation says. Measured today:

    {c: {b: 1}}  <-  {c: !raw {a: !nullify null}}         =>  {c: {a: !nullify null}}
    {c: {b: 1}}  <-  {c: !raw {a: 2}}                     =>  {c: {a: 2}}
    {c: {b: 1}}  <-  {c: {a: 2}}                          =>  {c: {a: 2, b: 1}}

The third line is the control: an ordinary container patch merges. b survives it and does
not survive !raw, and nothing about "this tag is data" asked for that.

WHAT IT SHOULD DO. A raw subtree should merge like any other patch of the same shape, with
its operator tags carried as data instead of dispatched:

    {c: {b: 1}}  <-  {c: !raw {a: !nullify null}}         =>  {c: {a: !nullify null, b: 1}}

ESCAPING IS NOT REPLACING. "Nothing beneath it is interpreted" does not entail "does not
merge", because a merge that treats mergeops as raw tags is also a merge in which nothing
beneath is interpreted. The two are already separate in the code: objMergeFast (patch.go)
does the COMBINING -- it walks two ordered field lists and calls PatchWith per field --
while the INTERPRETING happens inside doPatchWith's dispatch on the patch node's operator
tag. Merge-as-data is that same walk with the dispatch off, which is a mode on OpContext
(RejectUnsafe is the precedent), not a second traversal. The match side already has the
data-only recursion: RawEqual compares structurally with tags compared rather than
evaluated. There is simply no patch-side counterpart to it.

So mergeop/raw.go's "nothing beneath it is interpreted, at any depth, since the subtree is
never walked as a patch at all" (ad0e201) describes how it was built, not a constraint that
forced it. Not walking is ONE way to not interpret; it is not the only one, and it is the
one that also throws away the document.

THE VOCABULARY ALREADY SEPARATES THEM. This is what makes the change small: composition
already says "replace, as data", and it says it in the shape libdiff already emits.

    {c: {b: 1}}  <-  {c: !insert {a: 2}}                  =>  {c: {a: 2}}
    {c: {b: 1}}  <-  {c: !insert.raw {a: !nullify null}}  =>  {c: {a: !nullify null}}

!insert applies its child against ABSENCE and answers with the result (insert.go, and the
comment there on why it stopped merging), so it is the absolute "the value is what
results". Composed over !raw it is "the value is what results, and it is data" -- byte for
byte what !raw alone does today. libdiff.escaped already builds exactly that chain for a
value holding an operation. So !raw's replacing half is not a capability that would be lost;
it is a capability that is already spelled somewhere else, and spelled more honestly.

WHY THE EXISTING JUSTIFICATION DOES NOT HOLD. ad0e201 argues duality: "the comparison is
exact ... which is what makes it the dual of the patch side: !raw X means 'this is X' in
both directions". That derives the MATCH side from the PATCH side, so it explains why
matching is exact given that patching replaces. As a reason for patching to replace it is
circular.

The one independent argument is about removal, and it is partial: inside an escaped region
there is no vocabulary left to say "remove", since a !delete written there is data like
everything else, so a merging !raw is append-only at the depth it is placed. But that is an
argument for placing the escape at the depth the data actually starts -- which is the advice
the match side already gives ("Put !raw at the depth where literal comparison starts") --
and for using !insert.raw where a whole subtree really is being stated. It is ergonomics,
not impossibility.

WHAT DEPENDS ON THE CURRENT BEHAVIOUR. Two places, both of which want the REPLACING half
and reach for !raw to get it:

  - claimValue (storage/lower.go): wraps every lowered scope claim. Its comment is the
    clearest statement of the bundling anywhere in the tree -- "it says both halves of what
    a claim needs at once" -- and names the failure it avoids: "a scope claiming a: {y: 1}
    over a baseline a: {x: 1} read back {x: 1, y: 1}, so a !rename in a scope left the old
    field standing."
  - statedAsData (storage/read_subtree.go), added by the projection change for
    rkb7p8v5h12ksdnmgsn0: projects a stated subtree at a path below it and re-wraps in !raw
    so the projection replaces rather than merges.

Both become !insert.raw. Neither needs a new operator.

WHAT THIS COSTS.

  - Stored scope claims already in logs are !raw and would read back as merges. That is a
    migration, and it is the real cost of this: a claim written before the change and read
    after it means something different. Either the read path treats a stored bare !raw as
    !insert.raw by vintage, or claims are rewritten at compaction.
  - The MATCH side has to be decided rather than assumed. If patching becomes partial, the
    dual reading is that matching becomes partial too -- an ordinary object match with tags
    compared literally rather than evaluated -- which would change the documented example
    `rule: !raw {id: !glob "hot-*"}` from no-match to match. The alternative is to keep
    matching exact and give up the duality, which is defensible but should be said out loud
    rather than left as the thing that used to justify the patch side.
  - mergeop/raw.go and docs/matchpatch.md both state the escaping half and the match-side
    exactness, and neither says the patch side replaces. Whatever is decided, that sentence
    is missing today and a reader cannot get the behaviour from either.

Related: rkb7p8v5h12ksdnmgsn0 (statedAsData is the second dependent), 7f8rsk22h12ks2vscxn0
(the issue !raw was introduced for).