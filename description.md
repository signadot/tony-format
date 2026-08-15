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

## 3. The line/head policy was split -- FIXED as a policy question (09e95be), one part REMAINS

mergeop.Comments(false) used to keep line comments and drop head ones, because a head comment is
a wrapper that anything descending through discards while a line comment rides on the node and
every clone carries it. Off now strips both. A store that asks for no comments gets none, which
is a decision rather than a leak.

What remains from this one is GRANULARITY, and it is not a flag:

    line comment on a.b   ->  a: {b: !replace{from: 1 # one, to: 1 # two}}   rooted at a.b
    head comment on a     ->  a: !replace{from: {b: 1}, to: {b: 1}}          rooted at a, restating
                                                                            the subtree

A line comment lands at its value's path because it is a field on the node. A head comment lands
one level up and restates everything beneath it, because it wraps. At the document root that is
the whole document per comment edit -- and in logd a root-rooted write is not merely large: the
index records a write at the root, every watcher of every path sees a change, and
scopeOwnedLeafPaths would take the whole document into the scope's ownership.

The fix is NOT to change the representation. A head comment as a node field would trade loud
failures for silent ones -- every wrapper bug in this class announced itself the first time a
walker met one, while the single silent bug was the field-based half above -- and the format's
association rule ("attributed to the preceding comments of the next value, which may be dedented
or higher in the object notation") is what a wrapper states directly and a field would leave each
walker to re-implement.

The fix is an OP: a comment operation, absolute and unconditional, emitted at the path of the
value the comment describes -- "the comment at a is now X" -- as !addtag and !rmtag are for tags.
Then a comment edit is a small storable delta at that path, no lowering special case, and nothing
structural is inserted. libdiff emits it when only the comment differs; StorageContext declares it
storable with its reason line.

## 4. NEW: the equality choice is now live

DeepEqual is comment blind as of 09e95be, which is right for "is this the same value" and is what
logd asks it at session.go:864, :974, :1199 and storage/head.go:155 -- those sites mean "did this
subtree change".

If comments become stored data, those four must move to DeepEqualWithComments, or a comment-only
edit is written to the log and then silently dropped from every watch: the store and its watchers
disagreeing about whether anything happened. One line each, but a deliberate choice.

## 5. Unchecked: path attribution through the event stream

The snapshot index builds paths from the event stream, where EventHeadComment precedes the
container it belongs to. Nothing has verified that a stored head comment leaves every path naming
what it named before a snapshot. Worth measuring before comments are stored, since that is the
same shape as the bug this issue came from: accepted on write, wrong on read.

## Order of work

  1. the comment op (3), which is the only representation-adjacent piece and does not change the
     representation
  2. verify path attribution across a snapshot (5)
  3. choose the equality policy at logd's four sites (4)
  4. then the store flag, which by then really is a flag

logd deliberately does not turn it on (168e8ca).
