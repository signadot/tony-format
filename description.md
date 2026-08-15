# logd: a read at commit N misses commit N when the newest snapshot is at N-1

A read that finds its snapshot base at the immediately preceding commit can lose
that commit's write. No error: the path reads as though the write never happened.
The stepped head keeps it, so the head-divergence check fires too -- which is how
this was found.

PRE-EXISTING. Reproduced on 48d3784, before any of the comment work, with no
comment anywhere in the data. It is unrelated to comments; the only reason it
surfaced now is that the op generator in read_equivalence_test.go draws one extra
random number per op for comment injection, which shifts every generated stream,
and the shifted stream for seed 88 reaches the shape.

## Reproducing

    LOGD_SEEDS=90 go test ./system/logd/storage/ -run TestReadEquivalence

fails at seed=88 (and seed=122 within 150) once the generator injects comments,
and fails identically on 48d3784 with the same generator and the comments
neutered -- same rng draws, plain sources. The default 25-seed run does not reach
it, before or after.

## What was measured

At the failing commit (39 in seed 88), with the newest snapshot at commit 38:

    index.AllSegments()                  has  path="" [38,39], "d" [38,39], "d.k1" [38,39]
    index.LookupRange("", from=38, to=39)  ->  3 segments
    index.LookupRange("", from=39, to=39)  ->  0 segments      <-- the entry is missed
    findSnapshotBaseReader(39)             ->  base starts at commit 39

So the read asks for [39,39], the entry for commit 39 is indexed and in range by
the documented predicate -- rangeFunc keeps a segment whose EndCommit is within
[from,to], and this one's EndCommit IS 39 -- and the lookup answers with nothing.
The read then applies no patches to a snapshot that predates the write.

## Where it probably is

rangeFunc (index/index.go) filters on EndCommit, while the Commits set the range
walk descends is ordered by LogSegCompare. If that ordering is keyed on anything
that is not EndCommit -- StartCommit, path, position -- then a range scan using
an EndCommit predicate to choose branches can prune a subtree that contains
matches. That would explain why it depends on the shape of the set rather than on
any one entry: a small store with the same [N-1,N] segment answers correctly.

That is a hypothesis from the outside. What is measured is the two LookupRange
answers above.

## Why it matters

It is a silent wrong answer on the read path, which is the shape this store cares
most about. A write is committed, indexed, acknowledged, and then invisible to a
read at its own commit. It needs a shape where a snapshot lands at N-1 and a read
asks for N, which the default seeds do not produce and a busy store with periodic
snapshots will.

Found while soaking the equivalence tests for the comment work
(3cdjz00jh12krns4g1n0).