# docs: the logd article overstates four claims, and three doc sites still say compaction keeps every scope record

A review of `docs/articles/logd-document-store.md` against the code and `docs/logd/` found claims that are wrong or stale.

## In the article

- "A read crossing a swap neither reads stale bytes nor fails." A read indexed two compactions back is refused with `ErrCompactionInterrupted` (`dlog/doc.go`), and `compaction.md` says a reader can be cut off after `gracePeriod`. Never stale bytes; not never an error.
- "anything that cannot tolerate a single writer": the limit is a single process with no failover. etcd also writes through one leader.
- "until the holdover lands": the term is defined nowhere. The article's own HA section calls it active/passive over shared storage.
- "etcd compares a revision or a value, and comparing ships it": only a value compare ships the value. The contrast that holds is granularity: a revision guards the whole key.
- "compaction never drops scope records ... grows without bound. Open issue.": stale. `5hmq80f3h12krh1mbsn0` is closed; past the cutoff compaction drops a scope entry that later entries of the scope dominate (`scope_compaction.go`).
- The 5.8s stall is given two causes (lock in section 1, residency in section 4); section 2 says a read seeks a snapshot "at or below the commit" unconditionally; "newest first", "a decade behind Kubernetes" and the MyISAM analogy read wrongly; several sentences are compressed past a new reader.

## In docs/logd

- `compaction.md` "Scope records are kept regardless of cutoff ... tracked in 5hmq80f3": the same stale claim.
- `index.md` Scopes: "the scope's records are kept whatever compaction would otherwise drop": the same.
- `writes.md` names the three appliers as "a full read, the stepped head, a watch's deltas"; the stepped head is gone.