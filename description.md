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

## 5. Path attribution through the event stream -- OPEN, recorded in the code

The snapshot index builds paths from the event stream, where EventHeadComment precedes the value
it belongs to, and snap.Builder starts a chunk at a VALUE start -- so a head comment can fall at
the end of the chunk before the one holding what it describes. A partial read from a chunk offset
would then miss it, or attribute it to another value. Harmless while no comments are stored;
unverified beyond that, and it is the same shape as the bug this issue came from: accepted on
write, wrong on read.

Recorded where it would be found: stream/state.go's comment case, which is where the path
bookkeeping ignores them, and snap.Builder.onEvent, where a chunk begins.

## Order of work

  1. the comment op (3) -- DONE, dcdaead
  2. verify path attribution across a snapshot (5)
  3. choose the equality policy at logd's four sites (4)
  4. then the store flag, which by then really is a flag

logd deliberately does not turn it on (168e8ca).
