# One delta shape

Prerequisite 4 of wk5w1ddkh12krj1tkxn0: "a commit's notification is built from the client's
merged patch while a replay reads the STORED, lowered entry, so live and replay deliver
different deltas for the same commit and only one of them is absolute."

The shape is decided, and it is not a new one:

    THE DELTA IS storableDelta(base, next, keys) -- absolute, validated against the storage
    vocabulary -- AND THERE IS NO OTHER. Stored, replayed, notified, and delivered to a
    watch are the same bytes.

`storableDelta` already exists as one function (4wpqh7t2h12ks1fvj5n0 step 1, b39eca8). This
document is not proposing it. It is stating that its output is the only delta anyone sees,
and working out what that costs.

## Why the absolute one wins, in one paragraph

`whyNotStorable` already argues it, per operation: `!replace` and `!retag` are checked, so
against a base that has moved they error rather than applying; `!strdiff`, `!arraydiff`,
`!rename`, `!json-patch` re-evaluate against what they find; `!if` and `!let` are conditional
on the document they meet; `!pipe` calls out to the system, so storing it means re-running
it on every replay. A relative delta is a promise about a base. A stored delta outlives every
base it was written against, and a replay is exactly the case where the base has moved. There
was never a second candidate.

## What diverges today, and it is conditional

`newCommitNotification(commit, txSeq, timestamp, mergedPatch, scopeID)` builds the
notification from THE CLIENT'S MERGED PATCH, before the write, because `verifyApplies` needs
a stripped copy. `lowerWrite` then decides whether the log keeps something else:
`api.NeedsLowering` says whether the patch carries a relative op, and only then is the stored
entry a diff. So the two agree for a patch that was already absolute and disagree for one
that was not, which means THE SHAPE A WATCHER SEES DEPENDS ON WHAT SOME OTHER CLIENT WROTE.
`LowerEverything` makes the disagreement universal rather than fixing it.

## What has to change

1. THE NOTIFICATION IS DERIVED FROM THE STORED ENTRY, not from the merged patch. The ordering
   the current comment gives -- built before the write because the check needs it -- is about
   `verifyApplies` wanting a stripped copy of what the CLIENT sent, which is a different need
   and stays a local. The notification is built after lowering, from what was written.

2. `NeedsLowering` STOPS BEING A CONDITION ON THE SHAPE. Every write lowers. It may remain as
   an optimisation that skips the diff when it can prove the answer is the input, but it may
   not decide what a watcher receives. `LowerEverything` stops being a toggle and becomes
   the behaviour.

3. THE PATCH-ROOT MARKER GOES, or one shape is not literally one shape. `!logd-patch-root` is
   stored deliberately and stripped on delivery (`DeliverablePatch`), which is a real
   difference between the stored bytes and the delivered ones -- and it has already produced
   the bug where a replaying watcher saw `!delete.logd-patch-root` where a live one saw
   `!delete`, so a consumer testing for `!delete` read a deletion as an ordinary write, and
   the change gate stopped suppressing identical writes (xmxt2p85h12ksjp1gsn0). The marker
   exists so the read path can find which subtrees a commit patched. Under
   read_write_interface.md the INDEX answers that -- a commit's segments are its touched
   paths -- so the marker is a second answer to a question that already has one. With it
   gone, stored and delivered are the same bytes and `DeliverablePatch` has nothing to do.

4. ONE ROOTING RULE. A delta delivered for path P is rooted at P, by the same projection the
   read uses. That is rg5nd1psh12kse7dddn0 -- a watch's state event rooted at the watched
   path while its patch events are rooted at the document -- answered by there being one
   rooting rather than two, and by projection being a function on a delta rather than a
   property of how the delta was made.

## The part that makes it affordable, and why it comes after prerequisite 3

Lowering needs a base and a next to diff, and `verifyApplies` supplies them as WHOLE
DOCUMENTS: the head and the stepped head. Under read_write_interface.md there is no stepped
head and no whole next state, so lowering as written has nothing to diff.

    LOWERING IS PER PATH. For each path the patch writes: read the current value there
    (a bounded read), apply the op there, diff old against new there. The delta is the union,
    rooted at those paths, and its size is the write's own footprint.

That is only sound because an op's effect stays under the node it tags, and the exception was
always the array: `patchMayAffect` names `!key` and `!arraydiff` as ops whose effect extends
beyond their structural location, because a positional array shifts. Element identity removes
the first -- a keyed element is a named field and a merge into it touches that field -- and
element_identity.md makes the second the array's own business, since an array that declares
no identity is a value and the whole array is the unit. So prerequisite 4 is affordable
because of prerequisite 3, for the same reason the notification can be the stored delta.

## Scopes stop being a special case

`lowerWrite` skips lowering for a scope on purpose: "an absolute patch is already the claim a
scope stores, so forcing it through claimDelta would replace what the client said with the
subtree it landed in, taking baseline's siblings into the scope's ownership". That is two
things in one object -- WHAT CHANGED and WHAT IS OWNED -- and the fix is to say them
separately: one `storableDelta` for the change, and the owned-path union as the overlay's own
step, which 4wpqh7t2h12ks1fvj5n0 already identifies as the one step genuinely the overlay's.

And the keyed fallback goes with it. `scope_keyed.go` gives up on a keyed array the schema
does not declare -- `patchHasUndeclaredKey`, `keyedArrayPaths` and `annotateKeyed`, reached
from `lowerWrite` at two sites -- because only the schema can say what keys an array while a
client's own `!key` rides in a patch that lowering replaces. element_identity.md makes the schema the authority and refuses a patch that keys an
array the schema does not, so there is no undeclared keyed array to fall back for. That is
"the last thing standing between a scope and being ordinary", removed by prerequisite 3
rather than by scope code.

## What is given up, and it was already the contract

A live consumer stops seeing the operation the client wrote. A `!strdiff` arrives as the
string it produced. This is not a regression to argue about: a reshaped delta is already
legitimate, consumers are already required to APPLY a delta rather than pattern-match it, and
xmxt2p85 is what pattern-matching costs when the shape differs by delivery path. One shape is
the strongest form of that contract, because there is no longer a shape to match against.

A commit that changed nothing still takes a commit number and still notifies, with an empty
delta. `lowerWrite` already answers nil for it and the caller already keeps going; the rule is
that "nothing changed" is a delta a diff can express and a patch cannot, so it is one more
reason the absolute shape is the one.

## The property, and it is testable in one line each

  - IDENTITY. For any commit, the delta a live watcher receives and the delta a replaying
    watcher receives are the same bytes. Not equivalent: the same.
  - ABSOLUTENESS. Applied to the state at C-1, the delta gives the state at C, whatever base
    the applier reached it from. `Patch(a, Diff(a, b)) == b` is the existing property
    (diff_absolute_property_test.go) and this is it, asserted at the commit rather than in
    the library.

## What is deliberately not decided

  - Whether `NeedsLowering` survives as an optimisation. It may not survive as a condition.
  - The order of the two changes against 4wpqh7t2h12ks1fvj5n0's own plan, which says remove
    the scope gate only after the two pipelines are one. That ordering still holds and this
    document does not disturb it.

## As built

The notification is built from the stored entry, after lowering, and is a deep copy of it
(`deliverable`, storage/tick.go). `Deltas` hands out the same copy of the same entry. Both
are raised into the client's vocabulary by the one function (`raiseDelta`), so the live and
the replayed delta for a commit are the same bytes by construction. delta_identity_test.go
asserts IDENTITY and ABSOLUTENESS at the commit, with lowering as shipped and forced.

`!logd-patch-root` is gone: storage/tx/patch_root.go, `DeliverablePatch`, `markDeltaRoots`,
and the strip at every hop. Where an entry is applied from is read from its shape
(patches.walkAndCollectPatchRoots): an operation is about the node it is on, and its operand
is not descended into; a leaf, an array, or an empty container is a write at its path; a
commented node is a statement at the comment's path, wrapper and value together; a plain
object with fields is passed through. That is the reading the index (PatchChildren) and the
lowering (LowerSites) already make, so the three agree because they are one rule. A bare
array in an entry is applied as a unit at the array's path, which is what the fold
(api.NextState) does with it.

`NeedsLowering` survives as the optimisation and only that: it decides whether an absolute
write is diffed or kept as sent, and cannot change what a watcher receives, because the
watcher receives the stored delta either way. `lowerEverything` is the unexported test knob
that forces the diff.

Item 4, ONE ROOTING RULE, landed with the wire (phase 5). A watch's deltas are rooted at
the watched path, as its state event always was: a baseline watch PROJECTS the stored delta
onto its path with the read's own projection (`api.ProjectDelta`, the one function; storage's
`projectAt` is it), sends what the projection says, and steps the value it holds by it --
no whole document is held for a watch at any width. A projection that says nothing is a
commit that did not reach the path; one that is blocked, an operator above the path, is
answered by a read at the path and the diff. A scoped watch re-reads at its path per event
that can reach it and sends the diff, rooted there. docd lifts a sub-watch's delta from its
mount to the document to trim it and projects it back to the composed path with the same
function. A client applies what arrives to what it holds (rg5nd1psh12kse7dddn0).
