# logd: snapshot path reads scan to the end for an absent path -- use the directory wherever a path is sought

## Observed

Staging docd-0, 2026-09-22 06:19-06:51Z: 444 commits of 3.5-6.3s (median 3.9s), all in apply, while verse's git-issue source (after verse 2d927c2c) wrote each issue once. CPU profile: 41% in snap.(*PathEventReader).ReadEvent, 34% in stream.(*State).CurrentPath under it (rendering and quoting the full path on every event), 7% re-parsing the snapshot index (snap.OpenIndex) on every open. Commits themselves were ~0.3s of CPU each.

## Repro (copy of staging /data, 241MB)

Root snapshot at 13120: 5.2MB of events, 886 index entries, name order holds.

- read of a present issue: scans 167KB, 0.15ms
- read of an absent path (verse.git-issue.verse.issue.ytv880vyh12ks385gdn0 at 13150): scans all 5.2MB to EOF, 94ms
- lowerWrite of a whole new issue at 13150: 17 sites, one absent read each: 1.6s locally (4s on staging's 2 vCPUs)
- the same write once the issue exists: 7ms

## Cause

PathEventReader has no stop for an absent target: it reads every event to EOF, computing CurrentPath on each. A write of a new entity verifies at every leaf (LowerSites), so it pays one full-snapshot scan per leaf, growing with the root snapshot.

The snapshot has carried a directory -- a table of children per container, each entry with the child's offset and size -- since 6116dca5 (2026-09-17). Only Storage.Children (0babb3fc) uses it. Path reads predate it and still seek through the 4KB-chunk index and scan.

## What is needed

Use the directory wherever a path is sought in a snapshot, as it would have been had it been there from the beginning: walk the tables segment by segment, read exactly the child's byte range, and answer absent the moment a segment is not found. No per-event path rendering where the directory answers. The chunk-index seek and scan remain only for a snapshot written without a directory.

Unexplained, to check while there: the same absent read through Storage.Read was 13us for verse.git-issue.tony-format.issue.zz but 95ms for the verse issue, though both scan to EOF at the snapshot level.