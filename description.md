# logd: what it would take for a store to keep comments

Could logd keep comments -- match blind to them, patch re-assembling with the patch's comments
where they overwrite (mergeop.Comments(true))?

For an existing store holding no comments, YES, and that has not changed: the machinery is inert
on comment-free documents. Uncomment() is a no-op on a node that is not a wrapper, patch's
re-wrap branch is entered only when one side carries one, StripComments finds nothing to strip,
and the event stream round trips a comment-free document unchanged. Nothing already stored
changes meaning.

Three obstacles were named. Two are gone.

## 1. A kinded path could not see through a comment wrapper -- FIXED (09e95be)

    GetKPath("a")  on  "# lead\na: 1"  ->  nil, "expected object, got Comment"
    ListPath       on the same         ->  panic("type")

All three walks see through comments now, and ir/nav.go carries the option for when something
wants the comment instead: GetKPathWith, GetPathWith, ListPathWith, ir.WithComments. A path names
the value whatever was said about it, which is what the index, reads, scope-owned paths and
watches need.

## 2. A comment-only change had no delta -- FIXED (09e95be)

    Diff("# old\nname: svc", "# new\nname: svc")  ->  nil

DiffWith takes options now (Diff cannot: it is passed around as a libdiff.DiffFunc), and
DiffComments(true) reports a comment change as a replace carrying both sides. The round trip
holds: Patch(a, DiffWith(a,b,DiffComments(true)), Comments(true)) == b, over head and line
comments, added, changed, removed, nested, and alongside a value change.

## 3. The line/head policy was split -- FIXED (09e95be), and its granularity part -- FIXED (dcdaead)

mergeop.Comments(false) used to keep line comments and drop head ones, because a head comment is
a wrapper that anything descending through discards while a line comment rides on the node and
every clone carries it. Off now strips both. A store that asks for no comments gets none, which
is a decision rather than a leak.

Granularity is fixed too. !comment states what the comments at a node are, so a comment change is
a delta about the comment rather than a replacement of the value it describes:

    head at the root   24 bytes, storable      was: the document, twice, unstorable
    head nested        27 bytes, storable
    line               28 bytes, storable
    removed            17 bytes, storable

The representation did not move: a head comment is still the wrapper the format's association
rule describes. What changed is that there is now something to say about it. The design that was
written here before building it is below, with one correction learned in the build -- the
positions live in one operand, because tag composition shares a child and two operators on one
node could carry only one set of lines.

## Design for (3): the comment operator

Attribution is already right -- a comment-only delta roots at the node the comment describes, not
above it. What is wrong is the PAYLOAD: the only way to say "the comment here changed" is a
whole-node !replace, which carries the subtree twice.

    head comment on a  ->  a: !replace{from: {b: 1}, to: {b: 1}}       the subtree, twice
    line comment on b  ->  a: {b: !replace{from: 1 # one, to: 1 # two}}

So comments get an operator, on the model tags already have: !retag is checked, !addtag and
!rmtag are its absolute halves, and the absolute ones are what may be stored.

    a: !comment(head) ["# new"]     the head comment at a is now this
    a: !comment(line) []            the line comment at a is now nothing

One op, the position as its argument, the lines as its CHILD -- not as tag arguments, because
comment text is arbitrary and the format keeps the leading whitespace of the line as part of it,
which a tag argument cannot hold cleanly. Setting to an empty list is how a comment is removed, so
set and clear are the same absolute statement and there is no second operator to keep in step.

Properties, which are the point:

  - absolute: it states what the comment is, never what it was, so it applies to a moved base
  - idempotent: applying twice is applying once
  - proportional: the delta carries the comment, not the value it describes
  - storable: declared in logd's StorageContext with its reason line, beside insert and addtag

Three things it must get right:

  1. create and remove are structural -- setting a head comment on a node without one inserts a
     wrapper, clearing unwraps -- and the two directions have to be exact inverses or Diff and
     Reverse stop being symmetric
  2. head and line are different positions in the IR and on the page; the argument names which,
     and a delta says which it meant
  3. a value write and a comment write at one node: an operation states the whole value including
     its comment (settled in 09e95be), so diff emits one or the other for a node, never both

Nothing in the representation changes. This is an operator and a branch in diff.

## 4. The equality choice -- DECIDED (48d3784): it counts comments

DeepEqual is comment blind as of 09e95be, which is right for "is this the same VALUE" and stays
that way. What was wrong is that four places asked it a different question -- a watch replaying
the log, a watch following it live, a scoped delta, and the head's agreement with a full read all
mean "did the state at this path change" -- in four files, which is how four answers drift apart.

The question is now one function, api.SameState, and it counts comments. Not because comments
matter, but because the question is what the STORE holds, and it is not that function's business
to decide that part of it does not count.

While nothing stored carries a comment the two are the same function, so the change is inert: the
whole suite, watches and head divergence included, passes unchanged. The day a store keeps them,
blind equality would put the store and its watchers into disagreement about whether anything
happened -- a commit in the log that every watch dropped. On the head it reads the same way: a
stepped head that lost a comment a read kept is two materializations disagreeing about stored
content, which is exactly what that check exists to catch.

Equality alone would have been half a decision. What a delta CARRIES has to be what the equality
COUNTS, or they disagree in the other direction. With a comment-sensitive equality and a blind
diff, a comment-only change made emitScopedDeltaFrom hand tx.RootPatchAt an empty diff and fail the
watch outright ("ir node unspecified"). So the scoped delta diffs with comments, and so does the
scope overlay -- which states what a scope holds that baseline does not, and would otherwise drop a
scope's comment on the floor. That is a fifth site, found by asking where else the same question
was being answered separately.

Both halves are held by the test at the site: with blind equality no event is sent, with a blind
diff the watch errors.

Two things this rests on, worth knowing before the flag goes on:

  - the overlay and the head check both assume the two materializations they compare are equally
    faithful about comments; if a snapshot read keeps them and a replay does not, what they report
    is the reader's difference rather than the writer's
  - !comment is in the storage vocabulary and is absolute, so the deltas this produces are storable
    and survive the overlay's lowering like any other statement of what is

## 5. Path attribution through the event stream -- FIXED (edabbd6, 490e220, 1c8dc80, 5d40481)

Attribution is decided in three places that do not agree: the IR fixes a comment's owning path at
parse; the event stream cannot name that owner while the comment passes; and two indexes -- the
snapshot's chunk offsets and the dlog's paths -- are built from that stream. Everything below was
measured, not read off the source.

### A. the IR attributes a head comment upward, and that is the spec

    a:
      # note
      b: 1

parses to Object -> field a -> Comment[# note] -> Object -> field b. The wrapper is at path a, not
a.b: the next value to BEGIN after the comment is a's object, which begins at b. The spec's "may be
dedented or higher" gives exactly this.

So a comment a person wrote about b belongs to a. Nothing to fix -- but it decides what
ReadPath("a") owes its caller, and every layer below inherits it. Diff already agrees: it emits
!comment at the wrapper's path.

### B. stream.State cannot name a head comment's owner

The owner is announced after the comment. Offsets for "a: 1 # line on a" / "# head above b" /
"b: 2":

    off=  9 LineComment  [ # line on a]
    off= 24 HeadComment  [# head above b]
    off= 41 Key b

At 24 CurrentPath() is "a", the previous sibling; for a first field it is the enclosing container.
Never the owner. Line comments are fine -- they follow their value, and after EndObject the pop
restores the container's own path. That asymmetry is the whole problem, and it is why
stream/state.go's comment case can only be a no-op.

### C. the snapshot read window drops a node's OWN comments, at both ends

No chunking involved -- one index entry, whole stream scanned:

    read "a"    -> 1                                     lost: its line comment
    read "b"    -> c: 2 # line on c / # head above d / d: 3   lost: its own head comment
    read "b.c"  -> 2                                     lost: its line comment
    read "b.d"  -> 3                                     lost: its head comment

PathFinder.FindEvents starts collecting at the key or value-start for the target -- the head
comment has already gone by -- and closes on the scalar or on depth 0, before the line comment
arrives. Interior comments survive because they fall inside the window. A node keeps every comment
except its own.

### D. the chunk offset puts a head comment before the seek point

With SNAP_MAX_CHUNK_SIZE=1, the index entry for b is at offset 41 while "# head above b" occupies
24-41, inside chunk a's range. PathFinder never reads before initOffset, so fixing C alone still
loses it whenever the target begins a chunk.

snap.Builder writes a comment immediately because it is not a value start, and captures chunkOffset
later at the value. lastKey already solves precisely this for keys: buffer, then write inside the
value's chunk. Comments need the same treatment. Line comments are placed right by accident -- they
land in the post-flush gap, which Open counts into the previous chunk, which is their value's.

### E. the dlog index truncates at a comment wrapper -- this one loses DATA

indexPatchRec switches on n.Type with cases for Object and Array only. CommentType falls through
and returns. Indexed paths:

    a: / b: 1                     ->  "", a, a.b
    # note / a: / b: 1            ->  "" only
    a: / # note / b: 1            ->  "", a

A watch on a.b does not see that commit. One comment at the top of a patch unindexes the whole
document beneath it. Latent only because parse defaults comments off and nothing in logd turns them
on: it goes live the day comments do. Same shape as every wrapper bug this issue has found, and the
same audit applies to the other hand-written walkers -- scope_overlay.go, commit_ops.go,
internal/patches/processor.go, tx/key_tags.go, tx/auto_id.go. Generated gomap code already unwraps;
only hand-written type switches are exposed.

One of them carries a decision the unwrap records rather than settles.
walkAndCollectPatchRoots finds a patch root by the !logd-patch-root tag, which sits on the node
INSIDE the wrapper, so it now unwraps before looking -- and what the wrapper SAYS is dropped there
rather than carried into the value being installed. Carrying it would hand the streaming processor
a comment node where it expects the tagged patch, and whether a patch's own comments should reach
the stored value is a comment-policy question, not a question about finding roots. It belongs with
the store flag, step 4 below. Recorded at the call site.

### the rule to build to -- HOLDS as of 5d40481

For every path in a commented document, ReadPath(p) equals ir.GetKPath(doc, p) under
DeepEqualWithComments. That composes, it is the only rule consistent with (A) -- a node's own head
comment is part of the value at its path -- and it settles (4) by using it.

Verified at chunk sizes 1, 64 and 4096, so the boundary falls somewhere different in each, over a
document commented at every position: above the document, above a field, after a value, inside a
nested object, above and after an array element. ReadPathEventReader is held to the same window and
checked against ReadPath rather than trusted to have been edited alongside it.

### what the building turned up

Three things, none of which the analysis predicted, each fixed with the piece it blocked.

The IR had a shape it does not have. docs/ir.md says a comment node holds "a non-comment node", and
the parser nested them: a comment above a key and a comment above the first line of the block that
follows it are attributed to the same value -- it is the next value to BEGIN in both cases -- and
each wrapped it in turn. The stream cannot carry the difference, since one wrapper of two lines and
two wrappers write the same two events, so what went in did not come back and the OUTER comment
was the one lost. Preceding comments now compose as lines of one node, which is what the spec
describes; NodeToEvents and EventsToNode refuse the shape rather than losing it (490e220).

A line comment was attached a level too high. EventsToNode put it on the wrapper when a value had
both a head and a line comment, where the parser and !comment both put it on the value inside. A
value carrying both came back from the stream comparing as different to the document it was written
from (490e220).

A path that exists read as absent, with no comments involved at all. snap.Index.Lookup binary
searched entries held in DOCUMENT order with a comparator that orders by NAME. logd's own snapshots
satisfy both, because storage sorts object keys -- an unstated, unchecked precondition. On a
document in any other field order the search landed past the target and the read scanned to the end
and found nothing, without an error. The precondition is checked now, once per index; when it holds
the search is unchanged, and when it does not the answer falls back to the deepest ancestor, which
needs no ordering assumption (1c8dc80).

The last one is worth reading twice: it is the same shape as the bug this issue came from, it was
live for any store holding a document logd did not write, and nothing in the comments work caused
it. It was found because comments forced a read to be checked against the document it came from.

## Order of work

  1. the comment op (3) -- DONE, dcdaead
  2. path attribution (5) -- DONE:
       E, the dlog index -- edabbd6, and ir.listKPath with it
       D, the chunk offset -- 5d40481
       C, the read window -- 5d40481
       and the three above, 490e220 and 1c8dc80
  3. the equality policy (4) -- DONE, 48d3784: one function, api.SameState, and it counts comments
  4. the store flag -- DONE, fb23481: there is no flag

## 6. The flag, and why there is none (fb23481)

The last step was going to be a flag, and working out what it would do across restarts is
what retired it. A flag makes a store's comment policy a property of the PROCESS rather
than of the data:

  - off -> on is safe but not retroactive: what was stripped on the way in is gone
  - on -> off does not hide comments, it LOSES them, a subtree at a time. Reads go blind
    immediately while the log still holds them, and then the snapshot builder forwards
    untouched base events verbatim while rebuilding patched subtrees with comments off --
    so they survive where nobody wrote and vanish where anybody did. Turning it back on
    returns half a document, which is worse than none because it looks like it worked.
  - it does not even need a restart: a server and a compactor on one directory with
    different values do the same silent narrowing
  - and !comment is in the storage vocabulary, so with comments off a store would accept
    and store an operation that does nothing, against that vocabulary's own rule

So comments are always kept, and a client that wants data alone strips them from the
answer -- one call, and one that cannot be applied to somebody else's store by accident.

What that took, beyond the decision: the wire had to carry comments at all (the decoder
dropped the tokens; wire encoding refused them because a '#' runs to the end of a line,
which is answered by letting a comment end its line and leaving everything else compact),
and eleven patch sites had to stop stripping. Three carriage bugs surfaced once the
randomized equivalence tests had comments in their data -- the patch-root tag written onto
a comment wrapper, which nothing carries and which cost the whole subtree; the subtree
collector taking a head comment FOR its value, which duplicated a key; and two
re-emission sites asking GetKPath for a node when they wanted it as it stands. All three
are the same shape: a comment is not a value, and a tag, a patch root and a path answer
each belong to the value.

The equivalence tests now run with comments throughout, which is where a stepped head and
a folded read are held to the same document.

One thing that soak turned up is NOT ours: seeds 88 and 122 fail because a read whose
snapshot base is the immediately preceding commit misses that commit's write. It
reproduces on 48d3784 with the same streams and no comments in them --
gx8xvgmph12krbjpg1n0.

logd no longer deliberately does not turn it on (168e8ca is superseded).
