# logd: bounded op-preserving scope overlay (scope compaction)

Follow-up to eagjggjdh12ksg00bsn0 (COW isolation fix).

The correctness fix makes a scoped read = baseline_read + the scope's OWN
patches replayed op-preserving via tony.Patch. That is correct but UNCOMPACTED:

- scoped reads are O(scope-write-count) (replay all scope patches since divergence)
- scoped WATCH recompute-and-diff replays the overlay on every baseline commit to
  the watched path -> ~O(baseline-activity * scope-write-count)
- the scope's patches pin their log segments until DeleteScope, and can block
  baseline compaction from reclaiming shared log files

Materialized scope snapshots (today's mechanism) are removed by the fix because
they destroy !key (keyedListOp.Patch resolves to a plain array; re-applying it onto
a changed baseline falls into positional min-length merge -> truncates). So this
issue is the SOUND replacement.

Approach (does NOT need general patch composition): the index already decomposes
writes to per-key/leaf granularity, with the key encoded IN the index path
(indexPatchRec: keyed arrays -> items<key=VAL>, keyField from schema or the !key
tag in the data). So build a bounded materialized overlay from the latest patch per
leaf-path + tombstones for !delete, keying by the index path. Because keying lives
in the path (not a consumable op), the overlay is sound where the old snapshot was
not. Keyed arrays reconstruct cleanly against baseline; non-keyed arrays stay
positional/atomic (bounded by array size).

To confirm during impl: leaf writes are absolute (latest-per-path is sound) for
set/!key/!insert/!delete; a relative leaf op (numeric +1) would need more, but those
are not believed to exist here.

Depends on: eagjggjdh12ksg00bsn0 (read-correctness lands first).