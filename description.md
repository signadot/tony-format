# logd compaction

logd needs compaction

probably on inactive log after snapshot would work with atomic rename
of compacted tmp to the inactive log.

compaction needs config and automation

compaction configs should make it easy to estimate total size over time
for 2 use cases
1. append/add dominated stores
1. balanced append/delete stores

need to iron out algo details of deleting snapshots vs merging diffs