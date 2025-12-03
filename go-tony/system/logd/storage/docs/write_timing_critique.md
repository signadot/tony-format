# Write Timing Critique: Pre-Write to Temp File + Append on Commit

## Proposed Approach

1. **Pre-write phase**: Write all log entries to a temporary file
2. **Commit phase**: If commit succeeds, append temp file contents to `level0.log` atomically
3. **Abort phase**: If commit fails, delete temp file

## Analysis

### Pros

1. **Validation Before Commit**
   - Can validate all entries before committing
   - Catch errors early (malformed paths, invalid diffs, etc.)
   - No partial commits visible in main log

2. **Atomic Commit Operation**
   - Single append operation to `level0.log`
   - Either all entries committed or none
   - No race conditions with readers (they only see committed entries)

3. **Easy Rollback**
   - If commit fails, just delete temp file
   - No need to "undo" entries from main log
   - Cleaner abort semantics

4. **Crash Recovery**
   - Temp files can be detected on startup
   - Can determine which transactions were in-flight
   - Can decide to retry or abort based on transaction state

5. **Multi-Path Transaction Support**
   - All paths written to same temp file
   - Single atomic append for entire transaction
   - Natural fit for multi-participant transactions

### Cons

1. **Two Write Operations**
   - Write to temp file (disk I/O)
   - Append temp file to log (disk I/O + copy)
   - More expensive than single write
   - **Mitigation**: Temp file could be in-memory buffer, only written to disk if large

2. **Temp File Management**
   - Need unique temp file names (transaction ID?)
   - Need cleanup on abort
   - Need cleanup on crash recovery
   - Disk space management (what if many concurrent transactions?)

3. **Append Operation Complexity**
   - Need to copy temp file contents to log
   - Need to ensure atomicity (what if append fails mid-way?)
   - **Mitigation**: Use atomic rename (write to `.tmp`, rename to append)

4. **Concurrency Concerns**
   - Multiple transactions = multiple temp files
   - Need to serialize appends to `level0.log`
   - **Mitigation**: Single writer goroutine/channel for appends

5. **Index Update Timing**
   - When do we update index? After append succeeds?
   - If index update fails, log has entries but index doesn't
   - **Mitigation**: Update index atomically with append (same lock)

## Alternative Approaches

### Option A: Direct Append on Commit (Current Proposal)
```go
// Allocate commit
commit := NextCommit()

// Write directly to log
for _, entry := range entries {
    logPosition := appendToLog(entry)  // Direct append
    index.Add(path, level0, logPosition, commit, commit, txSeq, txSeq)
}
```

**Pros**: Single write, simpler
**Cons**: No validation before commit, harder rollback

### Option B: Pre-Write to Temp + Atomic Append (Proposed)
```go
// Pre-write phase
tempFile := createTempFile(txID)
for _, entry := range entries {
    writeToTempFile(tempFile, entry)  // Validate here
}
flushTempFile(tempFile)

// Commit phase
commit := NextCommit()
appendTempToLog(tempFile, commit)  // Atomic append
updateIndex(commit, logPositions)
deleteTempFile(tempFile)
```

**Pros**: Validation, atomic commit, easy rollback
**Cons**: Two writes, temp file management

### Option C: In-Memory Buffer + Append on Commit
```go
// Pre-write phase (in memory)
buffer := &bytes.Buffer{}
for _, entry := range entries {
    serializeToBuffer(buffer, entry)  // Validate here
}

// Commit phase
commit := NextCommit()
logPosition := appendBufferToLog(buffer, commit)  // Single append
updateIndex(commit, logPosition)
```

**Pros**: No temp file, validation, single append
**Cons**: Memory usage for large transactions, what if crash before commit?

## Detailed Critique of Proposed Approach

### 1. Temp File Location and Naming

**Question**: Where do temp files live?
- Option A: Same directory as `level0.log` (e.g., `level0.log.tmp.<txID>`)
- Option B: Separate temp directory (e.g., `tmp/level0.<txID>.tmp`)
- Option C: In-memory until commit (no disk temp file)

**Recommendation**: Option C (in-memory buffer) for most cases, Option A for very large transactions

### 2. Atomic Append Operation

**Question**: How do we ensure atomic append?
- Option A: Write temp file to `level0.log.tmp`, then rename (but rename doesn't append!)
- Option B: Use `O_APPEND` flag, write temp file contents directly
- Option C: Lock log file, append, unlock

**Problem**: File rename doesn't append - it replaces!

**Solution**: Need to actually append:
```go
// Open log file with O_APPEND
logFile, err := os.OpenFile("level0.log", os.O_APPEND|os.O_WRONLY, 0644)
defer logFile.Close()

// Copy temp file contents to log
tempFile, err := os.Open(tempFileName)
io.Copy(logFile, tempFile)

// Sync to ensure durability
logFile.Sync()
```

**Atomicity**: Use file lock to ensure only one append at a time

### 3. Index Update Timing

**Question**: When do we update index?
- Option A: After append succeeds (risk: log has entries, index doesn't)
- Option B: Before append (risk: index has entries, log doesn't)
- Option C: Same transaction/lock as append (atomic)

**Recommendation**: Option C - update index atomically with append:
```go
logMu.Lock()
defer logMu.Unlock()

// Append to log
logPosition := appendToLog(entries)

// Update index (same lock)
indexMu.Lock()
for i, entry := range entries {
    index.Add(entry.Path, level0, logPosition[i], commit, commit, txSeq, txSeq)
}
indexMu.Unlock()
```

### 4. Crash Recovery

**Question**: What happens on crash?
- Temp files exist → transaction was in-flight
- Check transaction state:
  - If transaction state says "committed" → append temp file to log, update index
  - If transaction state says "aborted" → delete temp file
  - If no transaction state → assume aborted (safe default)

**Implementation**:
```go
func RecoverTempFiles() error {
    tempFiles := findTempFiles()
    for _, tempFile := range tempFiles {
        txID := extractTxID(tempFile)
        state, err := readTxState(txID)
        if err != nil {
            // No state = aborted, delete temp file
            os.Remove(tempFile)
            continue
        }
        if state.Status == "committed" {
            // Commit was successful, append temp file
            appendTempToLog(tempFile, state.Commit)
            updateIndex(state.Commit, ...)
        } else {
            // Aborted, delete temp file
            os.Remove(tempFile)
        }
    }
}
```

### 5. Concurrency Model

**Question**: How do we handle concurrent transactions?
- Multiple transactions can pre-write to temp files concurrently (no contention)
- Only one transaction can append to log at a time (serialized by lock)
- Index updates happen under same lock as append (atomic)

**Flow**:
```
Transaction 1: Pre-write → Wait for log lock → Append → Update index → Release lock
Transaction 2: Pre-write → Wait for log lock → Append → Update index → Release lock
```

**Bottleneck**: Single append operation serializes all commits
- **Mitigation**: This is expected - commits are serialized by commit number anyway
- **Benefit**: Ensures commit order matches log order

## Simplified Proposal (Adopted)

### In-Memory Buffer Only (No Temp Files)

```go
type TransactionWriter struct {
    txID    int64
    buffer  *bytes.Buffer
    entries []LogEntry
}

// Pre-write phase
func (tw *TransactionWriter) WriteEntry(entry LogEntry) error {
    // Validate entry
    if err := validateEntry(entry); err != nil {
        return err
    }
    
    // Serialize to in-memory buffer
    serializeToBuffer(tw.buffer, entry)
    tw.entries = append(tw.entries, entry)
    return nil
}

// Commit phase
func (tw *TransactionWriter) Commit(commit int64) error {
    logMu.Lock()
    defer logMu.Unlock()
    
    // Append buffer contents to log atomically
    logPositions := appendBufferToLog(tw.buffer, commit)
    
    // Update index atomically (same lock ensures consistency)
    for i, entry := range tw.entries {
        index.Add(entry.Path, level0, logPositions[i], commit, commit, entry.Seq, entry.Seq)
    }
    
    return nil
}

// Abort phase - just discard buffer (no cleanup needed)
func (tw *TransactionWriter) Abort() error {
    // Buffer is garbage collected, no cleanup needed
    return nil
}
```

### Benefits of In-Memory Only Approach

1. **Simplicity**: No temp file management, no cleanup, no recovery complexity
2. **Performance**: No disk I/O until commit (faster)
3. **Validation**: Can validate before commit
4. **Atomic commit**: Single append operation
5. **Easy rollback**: Just discard buffer (GC handles it)
6. **Focus**: Allows focusing on bigger picture questions without temp file complexity
7. **Crash handling**: Client sees failure and retries - no need for transaction resumption

### Design Constraint

**Transaction size must fit in memory** - this is a design constraint, not a limitation:
- Enforces reasonable transaction sizes
- Prevents unbounded memory growth
- Simplifies implementation significantly
- Can be relaxed later if needed (but probably won't be needed)

## Open Questions (Resolved)

1. ~~**Buffer threshold**: When to switch from buffer to temp file?~~ → **Not needed** - in-memory only
2. ~~**Temp file cleanup**: How often to clean up orphaned temp files?~~ → **Not needed** - no temp files
3. ~~**Concurrent temp files**: Limit on number of concurrent temp files?~~ → **Not needed** - no temp files
4. **Index consistency**: What if index update fails after append? 
   - **Answer**: Index update happens under same lock as append, so atomic
   - If append succeeds but index update fails, we have a problem (but this is rare and can be handled by recovery)
5. **Transaction size limit**: What's the maximum transaction size?
   - **Answer**: Must fit in memory (design constraint)
   - Can enforce a configurable limit (e.g., 10MB) to prevent abuse
   - Client will see error if transaction too large, can split into multiple transactions

## Recommendation (Adopted)

**Adopt in-memory buffer only approach**:
- ✅ Validation before commit
- ✅ Atomic append operation
- ✅ Easy rollback (just discard buffer)
- ✅ Efficient (no disk I/O until commit)
- ✅ Simple (no temp file management)
- ✅ Focus on bigger picture (no recovery complexity)
- ✅ Crash handling: Client sees failure and retries (no transaction resumption needed)

**Design constraint**: Transaction size must fit in memory (enforced by design, not a limitation)

**Remove `Pending` flag** - not needed since entries are only written when committed.
