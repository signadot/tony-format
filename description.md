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

## 4. The equality choice -- OPEN, recorded in the code

DeepEqual is comment blind as of 09e95be, which is right for "is this the same value" and is what
logd asks it at session.go:864, :974, :1199 and storage/head.go:155 -- those sites mean "did this
subtree change".

If comments become stored data, those four must move to DeepEqualWithComments, or a comment-only
edit is written to the log and then silently dropped from every watch: the store and its watchers
disagreeing about whether anything happened. One line each, but a deliberate choice.

Recorded at the sites themselves, so it is found by whoever flips the flag: emitScopedDeltaFrom
carries the note and the two watch paths point at it, and storage/head.go's nodeEqual carries its
own.

## 5. Path attribution through the event stream -- OPEN, measured

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

### the rule to build to

For every path in a commented document, ReadPath(p) equals ir.GetKPath(doc, p) under
DeepEqualWithComments. That composes, it is the only rule consistent with (A) -- a node's own head
comment is part of the value at its path -- and it settles (4) by using it.

Recorded in the code at stream/state.go's comment case and snap.Builder.onEvent.

## Order of work

  1. the comment op (3) -- DONE, dcdaead
  2. path attribution (5), in this order, because each one makes the next testable:
       E, the dlog index -- data loss, blocks enabling comments at all
       D, the chunk offset -- C is untestable at a boundary until this lands
       C, the read window
  3. choose the equality policy at logd's four sites (4)
  4. then the store flag, which by then really is a flag

logd deliberately does not turn it on (168e8ca).
