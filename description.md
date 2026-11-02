# proposal: logd would drop the page cache of log ranges a whole-log pass has finished with (fadvise DONTNEED)

## Today

On verse staging (2026-11-02, image `signadot/verse-staging:86b81caa0`, 30 min after a restart), docd-0's cgroup stood at 5.04 GB of its 5 GiB limit:

| | |
|---|---|
| anon (the process) | 1.02 GB; the live Go heap was 458 MB, `GOMEMLIMIT=3500MiB` |
| file (page cache) | 3.95 GB, of which `inactive_file` 3.76 GB |
| /data | logA 906 MB, logB 931 MB, index.regions 77 MB, index.manifest 16 MB |

The page cache is reclaimable and kubelet's working set leaves out `inactive_file`, so this is not memory pressure today and is not what made staging slow (CPU and allocation were; see the profiles of that day). It does mean the container's reported usage sits at its limit, with the rest of what it holds being pages of log files.

logd does not call `fadvise`, `madvise`, or open anything `O_DIRECT` (`git grep -i -e fadvise -e O_DIRECT -e madvise -- '*.go'` is empty).

## Proposed

After a pass that reads a whole log, or a large range of one, from start to end, logd would call `unix.Fadvise(fd, off, len, unix.FADV_DONTNEED)` (golang.org/x/sys/unix) over the range it has finished. The candidates are the passes seen on staging:

- the snapshot write (`triggering snapshot` … `snapshot complete`, 83–93s each on staging)
- compaction: `Storage.Compact` → `SwitchDLog` → `reindexEntry`, which reads every surviving entry (see ym5vx3z2h12kstjbvdn0)
- the index rebuild at open, if it reads the log through

Ordinary reads, the active log's tail, and the index files would be left alone: those are read again, and the cache is what makes them fast.

## Open

- Measured, not assumed: whether these passes are in fact what fills the 3.76 GB of inactive cache. A before/after of `memory.stat` `file` around one compaction would say.
- Whether a range that was just written (the compaction output) should be dropped too, or kept because the next reads land there.
- Linux only; elsewhere the call would be a no-op behind a build tag.