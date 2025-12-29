# logd: dual-write to pending index is not atomic with active index

During schema migration, patches are dual-indexed to both active and pending indexes. However, the current implementation in `commit_ops.go` is not atomic:

```go
// 1. Write to dlog
pos, logFile, err := c.s.dLog.AppendEntry(entry)

// 2. Index to active
if err := index.IndexPatch(c.s.index, ...); err != nil {
    return "", 0, err
}

// 3. Check for pending and index
c.s.schemaMu.RLock()
pendingIdx := c.s.pendingIndex
...
if pendingIdx != nil {
    if err := index.IndexPatch(pendingIdx, ...); err != nil {
        return "", 0, fmt.Errorf("failed to index to pending: %w", err)
    }
}
```

If step 3 fails, we have:
- dlog: has entry ✓
- active index: has entry ✓
- pending index: missing entry ✗

This violates the dual-write invariant.

## Impact

In practice this is likely harmless since `IndexPatch` uses the same code path for both indexes - if active succeeds, pending should too. The only difference is the schema used for indexing.

## Possible fixes

1. **Pre-check**: Check `pendingIndex != nil` before writing to dlog, prepare both index operations, then execute atomically
2. **Rollback**: If pending indexing fails, remove the entry from active index (complex, index may not support removal)
3. **Accept and document**: Document that this edge case can occur and that migration completion will rebuild pending index anyway

## Related

This was identified during review of issue #087 (schema migration).