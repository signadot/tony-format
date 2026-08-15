# logd: what it would take for a store to keep comments

Could logd keep comments -- match blind to them (already true), patch re-assembling with the
patch's comments where they overwrite (mergeop.Comments(true), 168e8ca)?

For an existing store holding no comments, YES: the changes are inert on comment-free documents,
measured rather than assumed.

  - match: uncomment() is a no-op on a node that is not a comment wrapper
  - patch: the re-wrap branch is entered only when one side carries a wrapper, so a comment-free
    patch takes the same path it always did -- what TestPatchCommentsLeaveTheDataAlone pins
  - the event stream already round trips comment-free documents unchanged

So nothing already stored changes meaning. The obstacles are all on the FIRST commented write.

## 1. A kinded path cannot see through a comment wrapper

    doc:  "# lead\na:\n  # about b\n  b: 1\n"
    GetKPath("a")    -> nil, "expected object, got Comment"
    GetKPath("a.b")  -> nil, "expected object, got Comment"

Before and after the event round trip, so a snapshot does not launder it. The index, ReadStateAt,
scope-owned paths and watches are all kpath-addressed, so ONE head comment at the root makes the
whole document unaddressable. That is the same shape as the failure this came from: accepted on
write, broken on read, and the session finds out.

## 2. A comment-only change has no delta

    a: "# old\nname: svc\n"
    b: "# new\nname: svc\n"
    Diff(a, b) -> nil

A comment edit therefore cannot be recorded. Comments would be write-once -- arriving only when a
value is written whole, never updated, never deleted. And two materializations of the same commit
could agree on the data while disagreeing on the comments, which is the head-divergence shape:
the store cannot tell which is right because neither is wrong about the data.

## 3. The policy is already split, and nobody chose it

Line comments ride on node.Comment rather than a CommentType wrapper, so they flow through patch
UNCONDITIONALLY -- the "# the latch" in the storage test survives a write today, with the option
off -- while head comments need mergeop.Comments(true). Whatever a store decides, it should
decide once for both.

## Order of work, if stored comments are wanted

  1. kpath sees through comment wrappers (and ListPath, and whatever else navigates)
  2. decide whether a comment-only change is a delta, and what operator represents it
  3. unify the line/head policy so one flag governs both
  4. then the store flag, which by then really is a flag

Until 1, turning it on writes documents that cannot be read by path. Until 2, comments in a store
are write-once. logd deliberately does not turn it on (168e8ca).