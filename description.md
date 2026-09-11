# storage: a one-field commit into a set decodes the entry that wrote the whole set, so a commit costs the set -- 12.6ms and 41 MiB at 3000 entities

Observed while measuring qzmkhfjqh12ksydsmdn0; the same before and after that change.

    go test -run '^$' -bench BenchmarkCommitIntoSet ./system/logd/storage/   (c7a574a)

A one-field commit -- path verse.entities.e<i>, data {status: s<i>} -- into a set written
as one entry at verse.entities (benchstat, M2, n=6-8):

    200 entities     1.25ms    2.4 MiB     28k allocs per commit
    3000 entities   12.6ms    41.0 MiB    391k allocs per commit

Fifteen times the set, ten times the time and seventeen times the bytes, for a write
that touches one entity.

## Where (allocation profile, 60 commits at 3000)

95% of the bytes are lowering's read of the base at the write's site:

    commitOps.WriteAndIndex -> Storage.lowerWrite -> Storage.stateAt -> Storage.Read
      -> openRead -> Index.Segments -> dlog.ReadEntryAt -> decodeEntry   (58% flat)
      -> stream.EventsToNode (31% cum), ir.FromString (13%)

The read at verse.entities.e<i> as of commit-1 folds the writes that reach that path
since the nearest snapshot at or above it. One of them is the entry that wrote the whole
set at verse.entities, an ancestor, and ReadEntryAt decodes an entry whole -- so every
commit decodes the set to find one entity in it. (Reading of the profile; the benchmark
never switches the log, so no root snapshot stands in front of the seed entry. Whether a
path snapshot at the entity would be taken, and whether production reaches this between
switches as often as the benchmark does, is not measured.)

## Directions, not decided

- decode from an entry only the subtree the read's path names: a patch entry has no
  index of its own the way a snapshot's events do;
- let the path-snapshot policy count the bytes a read decodes, not only the records it
  folds, so a narrow read under a wide entry earns a snapshot;
- or give lowering a cheaper base: the write's own site may not need the fold at all
  when the write is absolute.