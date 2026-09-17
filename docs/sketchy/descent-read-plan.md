# A read at any depth: `..` in a set read

Issue: `th7sdhvyh12ksjtfn9n0`. The wildcard match (`y2agz9dyh12kse24n9n0`) answers a set one
node at a time and left `..` out. This plan puts it in, on the same wire and the same walk,
and says where it stays refused.

The discussion on the issue asks that it hold for a path listing too. It does by
construction: `return: path` and `return: iterType` are answered by the same walk as a
body read, from what the walk enumerated, so a descent that is cheap for a listing is the
same descent. The listing is the case to test first, because it is the one where a
subtree walk shows its cost with nothing else in the way.

## What has been settled since the issue was filed

The issue listed four open questions. Three are closed by work that landed after it:

| question | settled by |
|---|---|
| cost control | `limit` and an opaque `cursor` are the protocol's, not the match's (session.md "Paging a set"). A descent pages the same way; the caller stops by not asking for the next page. No depth limit and no node budget. |
| whether the index can help | a snapshot is a directory now (`3kgxprskh12krjrmndn0`): each container's children are a table, listed from where a page starts. A descent is a walk of tables, one listing per container reached, and the store offers it rather than the client composing it from `.*` levels. |
| `a..b.c`, path after the descent | falls out of the walk below, and is said in the docs and pinned by a test. |

The fourth, order, is decided here: **pre-order**, node before its descendants, and at each
level the store's order (sorted keys, positions in order). That is document order, the
order `ir.visitAll` walks in (though not, today, the order `o list` answers in -- see
below), and it is what makes a cursor a path: the last node answered, resumed by seeking
down its branch.

## Semantics

`..` offers the node it follows and every node beneath it, at any depth, to the rest of
the path. `a..` is `a` and everything under it; `..name` is every `name` in the document;
`a..b.c` is every `c` under a `b` anywhere under `a`. It reaches children of every kind:
fields, positions, sparse keys and keyed elements alike, since it is not a step into a
container of one kind.

What follows a descent keeps its rules. A wildcard after it is kind-strict as it is
anywhere: `a..(*)` names the elements of the keyed arrays under `a` and `a..[*]` the
positions of the dense ones, and neither names the other's. A concrete segment after it is
a filter on the listing rather than a step: `..status` answers the `status` fields the walk
finds, and an array that has no such field contributes nothing, which is a non-match and
not a fault -- the rule the set read already states for a segment that does not fit the
node it meets.

**Each node once, in pre-order, in process and on the wire.** A set is a set. `a..b..c`
can reach one `c` by two derivations (descend to `a.b`, then to `a.b.b.c`; descend to
`a.b.b`, then to its `c`); the answer has it once. `ir.ListKPath` answers it twice today
and `a....c` (two descents in a row) three times per `c`: its descent (`ir/kpath.go`, the
`kp.Descend` arm of `listKPath`) offers every node beneath to the rest of the path with
`visitAll`, so it walks derivations, not nodes, and `o list` inherits that. Its order is
not pre-order either: the answers under each offered node come out together, so `..c` over
`{b: {c: 1}, c: 2}` answers the outer `c` before the inner one, ahead of where the document
has it.

This plan fixes ir, in the same change, rather than leaving the in-process walk to
disagree with the wire: `o list` over a document and `match` over the store answer the
same query with the same nodes in the same order, which is what the issue's discussion
asks for under "uniformity" and what lets one be tested against the other. The walk is
the one described below, over ir nodes instead of the store's tables: a pre-order walk
carrying a set of pattern positions, emitting a node once when the set holds the end.
The position set and its closure over `..` live in `kpath`, where the glob view of `..`
already is (`matchPath`), and both walkers step it with their own "does this position
take this child" -- ir's is the kind-strict switch `listKPath` already has, per child;
the store's is `eachChild`'s, keyedness at the commit included.

**The node itself included.** `a..` includes `a`, so `..` alone includes the root. The
root's path on the wire is empty, which the single-node read already reports for a
`return: path` at the root: the member arrives with no `path`, and it is the only member
that can. A client asking `..` with `return: path` sees the root as the member with none.
(This is the one shape a client has to know about, and the docs say it. The alternative,
spelling the root `.`, is not a path kpath parses, and the alternative of leaving the root
out breaks the rule that makes `a..` include `a`.)

**Where it stays refused.** Everything the issue said: a patch, the precondition a patch
carries, a watch, a retain rule, and the index. `validateDataPath` refuses `..` in every
role today; it will refuse it in `roleWrite` and `roleWatch`, and `rolePatternRead` takes
it. Retention has its own validator and keeps refusing. `..` is `Wild()` to kpath, so
`kpathHasWild` already routes a descent to the set read and `ident.CanonicalPath` already
leaves it as it came, as it does a wildcard.

**docd** already treats a descent as a set (`hasWildSegment` is `Wild()`), and
`patternReaches` answers that a descent reaches a mount at or beneath its concrete prefix,
so a descent that could cross a mount is `unsupported` and one no mount is near passes
through. This needs a test, not a change. Composing one is the docd follow-up
(`5f6vrzw0h12ksrtfn9n0`), and the issue is right that a descent is the harder case there.

## The walk

`walkSet` handles the path left to right: a concrete segment steps, a wildcard lists a
level and recurs. It gains one arm: a `..` segment hands the prefix and the rest of the
pattern to `walkDescend`.

`walkDescend` is a pre-order walk of the subtree at the prefix, carrying a **set of
pattern positions** -- the glob-matching view of `..` that `kpath.matchPath` already
takes, as a state set rather than backtracking, since the walk visits each node once and
has to know at each what the pattern could still be. A position is a `*kpath.KPath`
pointer into the rest of the pattern, `nil` meaning "the pattern is spent". The set is
kept closed: a position at a `..` also holds the position after it, because a descent may
take zero segments.

- **At a node**: emit it if `nil` is in the set (as a member, through `sendSetMember`, with
  `proven` and `kind` from the listing that found it). Then, if the node is a container
  and any position is live, list its children.
- **On a child**: the next set is the closure of, for each live position: a `..` keeps
  itself (it absorbs the child); a wildcard that matches the child's kind advances; a
  concrete segment advances if the child's segment is the one it names. Empty set: the
  child is not walked.

Matching a child follows `eachChild` exactly, keyedness at the commit included: `.*` names
a field of an unkeyed object, `(*)` a field of a keyed one, `[*]` a position of an unkeyed
array, `{*}` a sparse key. A concrete segment names one child, spelled as the store spells
it: `canonicalChild` says which, once per node per concrete position, before the listing
runs. A concrete segment the store cannot spell here -- `(r1)` at an unkeyed array, or
against an array whose identity is other fields -- names nothing here and the position is
dropped; the same query meets many containers, and a fault at one is not the walk's
answer.

Every position after the first `..` is reached by listing, so the walk after it is exactly
one `storage.Children` per container it enters and nothing held per level beyond the
listing's own position: the cost is the subtree's containers, listed, which is what a
listing of that subtree costs level by level. Before the first `..` the walk is what it is
now.

**A leaf** lists nothing; the walk does not call `Children` on a node whose listed kind
is not a container. The node the descent starts at may be named rather than listed (`a..`
where `a` is concrete): its kind is not known, `Children` is asked and says nothing for a
leaf, and `sendSetMember` settles whether it is there as it does for any named member.

**The cursor** is unchanged in shape: the read's commit, its path, the last member's path.
Resuming is the seek `walkSet` already does, generalised: while the cursor's remaining
segments are non-nil, the node the walk is at was answered or passed on an earlier page,
so it is not emitted; if segments remain, descend into that child first with the rest of
the cursor, then list the later siblings whole; if none remain, list every child. The
position set is stepped along the cursor's branch the same way it is on a listed child,
so a resumed walk carries the state the first page had at that node. A walk that reaches
each node once in a total order is what makes this a seek rather than a rescan; the
one-node-once rule above is load-bearing here as well as in the answer.

## What to touch

- `ir/kpath`: the position set -- a small type over `[]*KPath` with its closure over `..`,
  a step that takes a per-position predicate, and whether the set holds the end. Unit
  tests on it directly: closure through consecutive `..`, a step that both keeps a `..`
  and advances past it, a spent pattern that takes nothing.
- `ir/kpath.go`: the `kp.Descend` arm of `listKPath` becomes a pre-order walk of the
  subtree with the position set; `visitAll` and `appendAll` go with it, since the walk
  is the only thing that used them (check before removing). The kind-strict switch is
  factored so that one child can be asked whether a position takes it, and the arm for a
  path with no `..` is untouched. Tests: `a..b..c` and `....c` answer once; `..c` is in
  document order; everything `TestListKPath` already pins still holds.
- `server/path.go`: `validateDataPath` refuses `..` for `roleWrite` and `roleWatch` and
  takes it for `rolePatternRead`; the comment on `pathRole` and on the function say the
  new rule.
- `server/session_read_set.go`: the `..` arm in `walkSet`; `walkDescend`; the position set
  and its step; a `childMatches` shared with `eachChild` (or `eachChild` rewritten over
  it) so a wildcard means one thing in both.
- `server/session_read_set_test.go`: the tests below.
- `server/session_test.go`: `TestSession_DescendPathIsRefused` keeps the patch and gains
  the precondition and a watch; the match half moves to the set tests as an answer.
- `docd/server`: a test that a descent no mount is near passes through, and one that
  reaches a mount is `unsupported`; `TestMatchSet_ThroughDocd` is the place.
- `libctl`: a test that `MatchPaths` / `MatchIDs` / the each-node API take a descent. No
  change expected.
- docs: `docs/logd/session.md` "Reading a set" (the bullet saying `..` is not a read
  becomes the paragraph that says what it answers, with an example over `return: path`),
  the `invalid_path` row, and "Paging a set" (a cursor resumes a descent the same way);
  `docs/objpath.md` "Where `..` may not go" (a read takes it now, as it takes a
  wildcard, and the refusal list drops "what logd reads"); `docs/logd/retention.md:126`
  stays as it is. `docs/articles/logd-document-store.md` if it mentions the refusal.
  `docs/objpath.md` also says what a `list` answers for a descent: each node once, in
  document order, the same set and order `match` answers over the store.

## Tests

Over one document holding an object, a dense array, a sparse array, a keyed array, and
leaves, at more than one depth, so that every kind is met by the descent:

1. `..` at the root lists every node once, in pre-order, the root first; `a..` starts at
   `a` and includes it.
2. `..name` finds the field at every depth, inside arrays and keyed elements included;
   `a..b.c` needs the `b` and finds the `c` under it wherever it is.
3. Kind-strictness after a descent: `..(*)` answers keyed elements and not object fields;
   `..[*]` positions and not keyed elements; `..{*}` sparse keys.
4. `..b..c` and `....c` answer each node once.
5. `return: path` and `return: "path,iterType"` over a descent read no bodies: the answer
   comes from the listings, and `iterType` is right for every member. This is the
   listing case the issue's discussion asks for. A store-stats or read-count assertion
   pins that no member was read, the way the listing tests already do.
6. A pattern selects per node: `..` with `data: {status: done}` answers the nodes that
   have it, and nothing at the depths that do not.
7. Paging: a descent with `limit: 1` walked page by page equals the unpaged answer in the
   same order, cursor boundaries falling at every depth including between a node and
   its first child; a write between pages does not change the later pages.
8. Empty: `..nope` is the marker alone; `nope..` (a prefix that is not there) is too.
9. At a past commit, under that commit's schema: an array keyed then and not now is
   listed by `..(*)` when read then.
10. Refused where it must be: patch, precondition, watch, retain rule, each with the
    message that says why.
11. docd: pass-through and unsupported, as above.

12. In process against the wire: the same document written to a store and held as a
    node, every descent query in tests 1-4 asked of both, the paths equal and in the
    same order. This is the test that makes "uniformity" a fact rather than a rule.

## Steps

0. The position set in `kpath`, and ir's descent rewritten over it, with its tests. One
   commit, first, since the server's walk is built on the same type.
1. Walk and validator, with tests 1-4, 6, 8, 10, 12. One commit.
2. Listing and iterType over a descent, test 5. One commit, or folded into 1 if the walk
   gets it for free -- which it should, since `proven` and `kind` come from the listing.
3. Paging, test 7. One commit.
4. Schema at the commit, test 9; docd and libctl tests, test 11. One commit.
5. Docs. One commit.
6. Close the issue with the merge.

## What landed

Everything above, in six commits on `issue-th7sdhvy`, and four things the plan did not
foresee:

- **A cursor after the root** (step 3). The root is a member of `..` and its path is
  empty, which was also what "no cursor" looked like, so a page of one over `..` answered
  the root forever. Whether there is a cursor is now said apart from where it points.
- **A keyed element's fields** (step 1). `runs(*).*` listed nothing on main: the walk
  asked the schema by the element's path, which elides to the array's. The walk asks
  `storage.KeyedAt` now, which knows an element is not an array.
- **ir's wildcards were not kind-strict** (after step 1). `.*` over a sparse array named
  its entries, `.""` the first of them, and `{*}` and `{n}` over a dense array its
  elements -- second spellings, under which a document cannot be rebuilt from its paths.
  One rule now, for get, list and the descent.
- **`kpath.Join` after a descent** loses the dot in `..(*).*` and `..{3}.*`
  (5rfjcqz9h12ksq27ndn0). The walk carries the parsed pattern instead; filed, not fixed.

Measured: a listing from a container's table is about a millisecond, so a descent costs
the containers it enters at that rate.
