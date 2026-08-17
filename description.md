# logd: no way to ask for the head without paying for a document

There is no cheap revision query. A read reports the store head, which is the right
answer, and it hands back the whole document to do it -- at any path, under any
pattern. So a caller which only needs to know where the store is pays for the state.

MEASURED, against a bare logd (o system logd session):

    {patch: {path: "a.b", data: {k: v}}}          -> commit 1
    {patch: {path: "big.list", data: {...}}}      -> commit 2

    {match: {path: ""}}                           -> commit 2, whole document
    {match: {path: "nope.nothing.here"}}          -> commit 2, whole document
    {match: {path: "nope.nothing.here",
             data: {absent: !irtype ""}}}         -> commit 2, whole document
    {match: {path: "a.b", data: {k: !irtype ""}}} -> commit 2, whole document

Every read reports the HEAD rather than the path's own last change, which is what
makes it a revision query at all. Narrowing the path does not narrow the payload
(ReadStateAt answers a rooted superset), and a pattern which matches nothing does not
either. Ping carries no commit. So the cheapest way to learn the revision is a full
read.

WHY IT MATTERS. A readiness probe is the natural caller: it runs every few seconds,
it wants one number, and it currently reads 455 KB (measured on a staging verse) to
get it. That cost is what pushed a probe past its own 5s budget, and the timeout then
drove a watch leak (g02yc3r4h12ks8ksgdn0) -- one root watch per probe, none
reclaimed. The leak is verse's, but the reason the probe was expensive is this.

It also shapes a decision verse cannot settle without it: what its Rev() means. The
two candidate answers are the two numbers logd has --

  - the head, which advances on every commit including a write that changes nothing
    (measured: four writes, two of them same-value, gave commits 1,2,3,4), and which
    a read reports synchronously;
  - the last commit that changed something under a path, which only a watch reports,
    which arrives AFTER the write returns, and which therefore falls permanently
    behind the head by one per no-op write.

A caller wanting the first has to buy a document; a caller wanting the second has to
hold a subscription. Neither is what "what revision are we at" should cost.

WHAT WOULD SERVE IT. Any of these, and the choice is a protocol decision:

  - a commit on the Pong, so a liveness probe and a revision probe are one request;
  - a request which answers the head and nothing else (a Rev/Head request);
  - a Match option which suppresses the body -- the read already computes the commit
    before it materializes anything, so the state is the part being paid for;
  - or a documented cheap path: if some pattern DOES avoid materializing, it should
    be written down, because three plausible ones do not.

Related: the rooted-superset read is the reason narrowing does not help
(verify-patches-by-applying notes the same shape). If a narrow read ever answers a
narrow document, this gets cheaper without a new request.