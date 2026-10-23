# logd compaction decodes every surviving entry three times, rewrites the whole file to drop a few percent, and runs back to back

On verse's staging docd-0 (go-tony v0.0.244, 2 vCPU, cutoff 1d, ~64k retained entries, ~645 MB per log): a compaction cycle takes 21–25 wall-minutes and the next starts the moment it ends, so the store is compacting all day (log: 10:02→10:27, 10:27→10:53, 10:53→11:18, 11:18→11:39, 11:54→…). Each removes 3–5k of ~64k entries.

Where the time goes, from a goroutine dump taken mid-cycle and the code:

- `compactionRecords` walks the inactive file decoding every entry (`FileIterator.Next` → `decodeEntry` → `EventsToNode`) to build the records.
- `dlog.writeCompactedEntries` calls `ReadEntryAt` on every survivor, a second full decode, to read `entry.SnapPos` and decide whether a blob precedes it; it then copies the bytes.
- `reindexEntry` calls `ReadEntryAt` on every survivor a third time, after the swap, to re-derive its segments.

Three decodes of ~60k entries (~645 MB of events to IR) per cycle, on the two cores reads already hold (baassnwth12krdjbvdn0). The copy itself rewrites ~640 MB to drop ~5%.

- The snapshot trigger (`maybeCompact`) fires as soon as `SwitchDLog` returns, since 1,000+ commits accrued during the cycle, so there is no gap between cycles.

Wanted, in order of leverage:
1. Decode once. The records walk already has every survivor's segments and whether it is a snapshot; carry `SnapPos`/blob extent and the segments on the record, and have the rewrite and the re-index use them. The re-index needs only the new position, which the rewrite answers.
2. Do not rewrite a file to drop a few percent: a threshold on `dropped/original` (or dropped bytes) below which compaction unindexes nothing and leaves the file for next time, or the policy's cutoff tiers make the drop worth it.
3. A floor between cycles: the trigger should not fire again inside some interval of the last completion, so a slow cycle does not become a permanent one.

Found while debugging staging with Scott, 2026-10-23.