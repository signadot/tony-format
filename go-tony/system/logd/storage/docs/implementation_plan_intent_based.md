# Implementation Plan: Intent-Based Compaction

## Overview

This plan implements the intent-based compaction design that coordinates compaction across the virtual path hierarchy using commit intents stored in the transaction log. Paths are extracted from `TxLogEntry.PendingFiles` and used to notify relevant `DirCompactor` instances at alignment points.

## Design Reference

See `docs/intent_with_paths.md` for the complete design specification.

## Implementation Phases

### Phase 1: Create Types Package (Foundation)

**Goal:** Extract shared types to avoid circular dependencies.

**Tasks:**
1. Create `storage/types/` directory
2. Create `storage/types/types.go`:
   - Move `TxLogEntry` from `log.go`
   - Move `FileRef` from `log.go`
   - Keep codegen annotations (`//tony:schemagen`)
3. Run codegen to generate `storage/types/types_gen.go`
4. Update imports in `storage/log.go`:
   - Change `TxLogEntry` → `types.TxLogEntry`
   - Change `FileRef` → `types.FileRef`
5. Update imports in `storage/tx.go`:
   - Change `FileRef` → `types.FileRef`
   - Change `TxLogEntry` → `types.TxLogEntry`
6. Update imports in `storage/storage_gen.go` (if it references these types)

**Testing:**
- Run existing tests: `go test ./storage/...`
- **BREAKING:** Tests will fail - need to update imports immediately
- Fix `storage/log_test.go`: Change `TxLogEntry` → `types.TxLogEntry`, `FileRef` → `types.FileRef`
- Fix `storage/tx_test.go` (if it uses these types)
- Verify transaction log still works: `go test -run TestTxLog`
- Verify codegen produces correct methods
- Check no circular import errors

**Files Changed:**
- `storage/types/types.go` (new)
- `storage/types/types_gen.go` (generated)
- `storage/log.go`
- `storage/tx.go`
- `storage/storage_gen.go` (if needed)
- `storage/log_test.go` (fix imports)
- `storage/tx_test.go` (fix imports if needed)

**Rollback:** If issues, revert types package and keep types in `storage`.

---

### Phase 2: Add Path Extraction Utilities

**Goal:** Implement helper functions to extract paths from `PendingFiles`.

**Tasks:**
1. Create `storage/compact/path_extraction.go`:
   ```go
   package compact
   
   import "github.com/signadot/tony-format/go-tony/system/logd/storage/types"
   
   // extractPathsFromPendingFiles extracts unique virtual paths from PendingFiles
   func extractPathsFromPendingFiles(pendingFiles []types.FileRef) []string {
       pathSet := make(map[string]bool)
       for _, ref := range pendingFiles {
           pathSet[ref.VirtualPath] = true
       }
       paths := make([]string, 0, len(pathSet))
       for path := range pathSet {
           paths = append(paths, path)
       }
       return paths
   }
   
   // addAncestors adds all ancestor paths of a given path to the pathSet
   func addAncestors(pathSet map[string]bool, path string) {
       if path == "" || path == "/" {
           return
       }
       parts := strings.Split(strings.Trim(path, "/"), "/")
       for i := 1; i < len(parts); i++ {
           ancestor := "/" + strings.Join(parts[:i], "/")
           pathSet[ancestor] = true
       }
       pathSet["/"] = true
   }
   ```

**Testing:** Focus on correctness of path extraction and ancestor calculation
- Create `storage/compact/path_extraction_test.go`:
  - **Correctness:** Test `extractPathsFromPendingFiles` deduplicates paths correctly
  - **Correctness:** Test `extractPathsFromPendingFiles` handles empty `PendingFiles` (no false positives)
  - **Correctness:** Test `addAncestors` for `/a/b/c` produces exactly `/a/b/c`, `/a/b`, `/a`, `/` (no missing ancestors)
  - **Correctness:** Test `addAncestors` for root path `/` produces only `/` (no false ancestors)
  - **Correctness:** Test `addAncestors` for single-level path `/a` produces `/a`, `/` (correct ancestors)
  - **Correctness:** Test paths are extracted in deterministic order (or verify order doesn't matter)

**Files Changed:**
- `storage/compact/path_extraction.go` (new)
- `storage/compact/path_extraction_test.go` (new)

---

### Phase 3: Add Transaction Log Reading to Compactor

**Goal:** Enable compactor to read transaction log entries.

**Tasks:**
1. Add import to `storage/compact/compact.go`:
   ```go
   import (
       "github.com/signadot/tony-format/go-tony/system/logd/storage/types"
       // ... existing imports
   )
   ```

2. Add `CompactionIntent` type to `storage/compact/compact.go`:
   ```go
   // CompactionIntent represents paths affected by a commit
   type CompactionIntent struct {
       Commit int64
       Paths  []string
   }
   ```

3. Add `readCommits` method to `Compactor`:
   ```go
   func (c *Compactor) readCommits(startCommit, endCommit int64) ([]CompactionIntent, error) {
       logFile := filepath.Join(c.Config.Root, "meta", "transactions.log")
       
       data, err := os.ReadFile(logFile)
       if err != nil {
           if os.IsNotExist(err) {
               return []CompactionIntent{}, nil
           }
           return nil, err
       }
       
       return c.parseTxLogLines(data, startCommit, endCommit)
   }
   ```

4. Add `parseTxLogLines` method to `Compactor`:
   ```go
   func (c *Compactor) parseTxLogLines(data []byte, startCommit, endCommit int64) ([]CompactionIntent, error) {
       intents := []CompactionIntent{}
       lines := strings.Split(string(data), "\n")
       
       for _, line := range lines {
           line = strings.TrimSpace(line)
           if line == "" {
               continue
           }
           
           entry := &types.TxLogEntry{}
           if err := entry.FromTony([]byte(line)); err != nil {
               c.Config.Log.Warn("skipping invalid transaction log entry", "error", err)
               continue
           }
           
           if entry.Commit >= startCommit && entry.Commit <= endCommit {
               paths := extractPathsFromPendingFiles(entry.PendingFiles)
               intents = append(intents, CompactionIntent{
                   Commit: entry.Commit,
                   Paths:  paths,
               })
           }
       }
       
       return intents, nil
   }
   ```

**Testing:** Focus on correctness of log reading and path extraction
- Create `storage/compact/read_commits_test.go`:
  - **Correctness:** Test reading from non-existent log file returns empty (no false positives)
  - **Correctness:** Test reading valid entries within range extracts ALL commits in range (no false negatives)
  - **Correctness:** Test filtering by commit range excludes commits outside range (no false positives)
  - **Correctness:** Test skipping invalid entries doesn't break parsing of subsequent entries
  - **Correctness:** Test extracting paths from `PendingFiles` matches actual file paths (paths match files)
  - **Correctness:** Test with empty `PendingFiles` returns empty paths (no false notifications)
  - **Correctness:** Test paths are deduplicated correctly (no duplicate notifications)

**Files Changed:**
- `storage/compact/compact.go`
- `storage/compact/read_commits_test.go` (new)

---

### Phase 4: Add Alignment Point Detection and Notification

**Goal:** Add `OnAlignmentPointReached` method and alignment point channel to `DirCompactor`.

**Tasks:**
1. Add `alignmentPoint` channel to `DirCompactor` in `storage/compact/dir_compact.go`:
   ```go
   type DirCompactor struct {
       // ... existing fields
       alignmentPoint chan int64  // Alignment commit notifications
   }
   ```

2. Initialize channel in `newDirCompactor`:
   ```go
   dc := &DirCompactor{
       // ... existing fields
       alignmentPoint: make(chan int64, 1),  // Buffered to avoid blocking
   }
   ```

3. Add `OnAlignmentPointReached` method to `Compactor` in `storage/compact/compact.go`:
   ```go
   func (c *Compactor) OnAlignmentPointReached(level int, alignCommit int64) {
       startCommit := alignCommit - int64(c.Config.Divisor) + 1
       intents, err := c.readCommits(startCommit, alignCommit)
       if err != nil {
           c.Config.Log.Warn("failed to read commit log", "error", err)
           return
       }
       
       // Extract all paths from intents and add their ancestors
       pathSet := make(map[string]bool)
       for _, intent := range intents {
           for _, path := range intent.Paths {
               pathSet[path] = true
               addAncestors(pathSet, path)
           }
       }
       
       // Notify next level DirCompactors for paths + ancestors
       nextLevel := level + 1
       c.dcMu.Lock()
       defer c.dcMu.Unlock()
       
       for path := range pathSet {
           dc := c.getOrInitDC(&index.LogSegment{RelPath: path})
           if dc == nil {
               continue
           }
           
           // Traverse to next level in chain
           for i := 0; i < nextLevel; i++ {
               if dc.Next == nil {
                   dc.Next = NewDirCompactor(&c.Config, dc.Level+1, dc.Dir, dc.VirtualPath, c.env)
               }
               dc = dc.Next
           }
           
           // Notify alignment point reached (non-blocking)
           select {
           case dc.alignmentPoint <- alignCommit:
           default:
               // Channel full, skip (shouldn't happen with buffer)
           }
       }
   }
   ```

**Testing:** Focus on correctness of alignment notification and path coordination
- Create `storage/compact/alignment_test.go`:
  - **Correctness:** Test `OnAlignmentPointReached` with no commits in range doesn't notify any DirCompactors (no false positives)
  - **Correctness:** Test `OnAlignmentPointReached` notifies DirCompactors for ALL paths that had commits (no false negatives)
  - **Correctness:** Test ancestor addition: commit to `/a/b/c` notifies `/a/b/c`, `/a/b`, `/a`, `/` (all ancestors notified)
  - **Correctness:** Test notification goes to CORRECT level DirCompactors (nextLevel = level + 1)
  - **Correctness:** Test notification doesn't block (channel non-blocking) - doesn't affect commit performance
  - **Correctness:** Test with multiple paths in same commit: all paths + all their ancestors are notified
  - **Correctness:** Test paths without segments don't cause errors (DirCompactor may not exist yet)
  - **Correctness:** Test notification window is correct (last `divisor` commits, not all commits)

**Files Changed:**
- `storage/compact/dir_compact.go`
- `storage/compact/compact.go`
- `storage/compact/alignment_test.go` (new)

---

### Phase 5: Integrate Alignment Point Detection in DirCompactor

**Goal:** Make `DirCompactor` detect alignment points and call `OnAlignmentPointReached`.

**Tasks:**
1. Modify `DirCompactor.shouldCompactNow` or add alignment check in `processSegment`:
   - Check if `CurSegment.EndCommit % align == 0` (where `align = divisor^level`)
   - If alignment point reached AND `Inputs >= Divisor`, call `OnAlignmentPointReached`

2. Add alignment calculation helper:
   ```go
   func alignmentForLevel(divisor int, level int) int64 {
       align := int64(1)
       for i := 0; i <= level; i++ {
           align *= int64(divisor)
       }
       return align
   }
   ```

3. Modify `DirCompactor.run()` or `processSegment()`:
   - After processing segment, check if alignment point reached
   - Calculate alignment: `align := alignmentForLevel(c.Config.Divisor, dc.Level)`
   - Check: `dc.CurSegment.EndCommit % align == 0` AND `dc.Inputs >= c.Config.Divisor`
   - If true, call `c.OnAlignmentPointReached(dc.Level, dc.CurSegment.EndCommit)`
   - Note: Need access to `Compactor` - may need to pass it via `storageEnv` or store reference

4. Add `Compactor` reference to `storageEnv`:
   ```go
   type storageEnv struct {
       // ... existing fields
       compactor *Compactor  // For calling OnAlignmentPointReached
   }
   ```

5. Update `NewCompactor` to set `env.compactor = c`

**Testing:** Focus on correctness of alignment point detection
- **Correctness:** Test alignment calculation: level 0 with divisor 2 → align=2, level 1 → align=4, level 2 → align=8
- **Correctness:** Test `OnAlignmentPointReached` is called EXACTLY when `EndCommit % align == 0` AND `Inputs >= Divisor` (no false positives)
- **Correctness:** Test `OnAlignmentPointReached` is NOT called when `EndCommit % align != 0` (no false negatives)
- **Correctness:** Test `OnAlignmentPointReached` is NOT called when `Inputs < Divisor` (correctness: need enough segments)
- **Correctness:** Test alignment detection works at all levels (level 0, 1, 2, etc.)
- **Correctness:** Test alignment point is detected at the CORRECT commit (not off-by-one errors)
- **BREAKING:** Tests will fail - need to update `storageEnv` creation
- Fix `storage/compact/compact_test.go`: Update `testSetup` to set `env.compactor = c`
- Fix `storage/compact/read_state_test.go`: Update `storageEnv` creation if it exists

**Files Changed:**
- `storage/compact/storage_env.go`
- `storage/compact/compact.go`
- `storage/compact/dir_compact.go`
- `storage/compact/compact_test.go` (fix `storageEnv` creation)
- `storage/compact/read_state_test.go` (fix `storageEnv` creation if needed)

---

### Phase 6: Handle Alignment Notifications in DirCompactor

**Goal:** Make `DirCompactor` listen for alignment notifications and use them in compaction decisions.

**Tasks:**
1. Add `pendingAlignments` list to `DirCompactor`:
   ```go
   type DirCompactor struct {
       // ... existing fields
       pendingAlignments []int64  // Alignment commits waiting to be processed
   }
   ```

2. Modify `DirCompactor.run()` to listen for alignment notifications:
   ```go
   select {
   case seg, ok := <-dc.incoming:
       // ... existing segment handling
   case alignCommit := <-dc.alignmentPoint:
       dc.pendingAlignments = append(dc.pendingAlignments, alignCommit)
   case <-dc.done:
       return nil
   }
   ```

3. Modify compaction decision logic:
   - Check `pendingAlignments` when deciding to compact
   - Compact if: `Inputs >= Divisor` AND `CurSegment.EndCommit >= min(pendingAlignments)`
   - After compacting, remove processed alignments from list

4. Update `shouldCompactNow` or equivalent logic:
   ```go
   func (dc *DirCompactor) shouldCompactNow() bool {
       if dc.Inputs < dc.Config.Divisor {
           return false
       }
       
       // Check if we've reached an alignment point
       align := alignmentForLevel(dc.Config.Divisor, dc.Level)
       if dc.CurSegment.EndCommit%align == 0 {
           return true
       }
       
       // Check if we've processed segments up to a pending alignment
       for _, alignCommit := range dc.pendingAlignments {
           if dc.CurSegment.EndCommit >= alignCommit {
               return true
           }
       }
       
       return false
   }
   ```

**Testing:** Focus on correctness of notification handling and compaction triggering
- **Correctness:** Test `DirCompactor` receives alignment notifications for paths that had commits (no false negatives)
- **Correctness:** Test `DirCompactor` does NOT receive notifications for paths without commits (no false positives)
- **Correctness:** Test compaction happens EXACTLY when `Inputs >= Divisor` AND `CurSegment.EndCommit >= alignCommit` (correctness: both conditions)
- **Correctness:** Test compaction does NOT happen when `CurSegment.EndCommit < alignCommit` (correctness: must process up to alignment)
- **Correctness:** Test pending alignments are processed in order (FIFO) - earlier alignments processed first
- **Correctness:** Test alignments are removed after processing (no duplicate processing)
- **Correctness:** Test compaction at alignment point produces correct state (coherent read results)

**Files Changed:**
- `storage/compact/dir_compact.go`

---

### Phase 7: Add New Tests for Intent-Based Compaction

**Goal:** Add tests that verify correctness criteria for intent-based compaction.

**Correctness Criteria to Test:**

1. **Coherent Read Results:** "Read results for any compaction state are coherent w.r.t. compaction windows. Querying `/a/b @N` at compaction boundary `N` (where `N % (divisor^level) == 0`) yields a precise view of `/a/b` at that commit."

2. **Path Coordination:** Only DirCompactors for paths that had commits (plus ancestors) are notified.

3. **Ancestor Notification:** Ancestors are notified for read optimization (paths + ancestors strategy).

4. **Alignment Coordination:** Compaction coordinates across virtual path hierarchy at alignment points.

**Tasks:**
1. **Integration tests for correctness:**
   - **Correctness:** Test querying `/a/b @N` at alignment point `N` returns correct state (coherent read results)
   - **Correctness:** Test compaction across multiple paths at alignment points - all paths compact correctly
   - **Correctness:** Test ancestor notification: commit to `/a/b/c` notifies `/a`, `/a/b` AND they compact correctly
   - **Correctness:** Test multiple levels compacting at alignment points - coordination works across levels
   - **Correctness:** Test that non-aligned commits don't trigger compaction (no false positives)
   - **Correctness:** Test that aligned commits DO trigger compaction (no false negatives)

2. **Edge case tests for correctness:**
   - **Correctness:** Test with empty `PendingFiles` doesn't notify any DirCompactors (no false positives)
   - **Correctness:** Test with duplicate paths in same commit deduplicates correctly (no duplicate notifications)
   - **Correctness:** Test with very large number of paths - all paths + ancestors notified (no false negatives)
   - **Correctness:** Test alignment point detection at various commit numbers - correct alignment calculation
   - **Correctness:** Test reading from non-existent transaction log returns empty (graceful handling)
   - **Correctness:** Test failed/aborted commits don't appear in log (only fully committed transactions)

3. **Test helpers for correctness verification:**
   - Helper to verify alignment notifications were sent to correct paths (verify notification correctness)
   - Helper to check which paths were notified (verify no false positives/negatives)
   - Helper to create transaction log entries in tests (test setup)
   - Helper to verify read results are coherent at alignment points (verify correctness property)

**Note:** Existing tests should already be fixed in earlier phases. This phase adds NEW tests focused on correctness criteria.

**Testing Strategy:**
- Run all existing tests: `go test ./storage/...`
- Run compaction-specific tests: `go test -run Compact`
- Run concurrent tests: `go test -run Concurrent`
- Run with race detector: `go test -race ./storage/...`
- Run new intent-based tests: `go test -run Intent`

**Files Changed:**
- `storage/compact/alignment_test.go` (new)
- `storage/compact/intent_integration_test.go` (new)
- Test helper files as needed

---

### Phase 8: Cleanup and Documentation

**Goal:** Clean up, document, and verify everything works.

**Tasks:**
1. **Code cleanup:**
   - Remove any debug logging
   - Ensure consistent error handling
   - Verify no unused code
   - Run `go vet` and `golangci-lint`

2. **Documentation:**
   - Update package doc in `compact/compact.go`
   - Add doc comments to new functions
   - Update `docs/intent_with_paths.md` with any implementation notes
   - Document alignment point calculation

3. **Performance verification:**
   - Verify no performance regressions
   - Check that alignment notifications don't block commits
   - Verify log reading is efficient for small windows

4. **Final testing:**
   - Run full test suite multiple times
   - Run stress tests if available
   - Verify no race conditions
   - Check memory usage

**Files Changed:**
- All modified files (cleanup)
- Documentation files

---

## Migration Strategy

**Backward Compatibility:**
- Old transaction logs (without paths in `PendingFiles`) will still work
- `extractPathsFromPendingFiles` handles empty `PendingFiles` gracefully
- Compaction will work even if no paths are extracted (falls back to old behavior)

**Rollout:**
1. Deploy Phase 1 (types package) - no behavior change
2. Deploy Phases 2-4 (reading and notification) - new code, but not yet called
3. Deploy Phase 5 (integration) - starts using new code
4. Monitor for issues
5. Deploy remaining phases

**Rollback Plan:**
- Each phase should be independently revertible
- Keep old code paths available initially (feature flag if needed)
- Can disable alignment-based compaction by not calling `OnAlignmentPointReached`

---

## Testing Checklist

**Phase-by-Phase:**
- [ ] Phase 1: All tests pass after fixing imports
- [ ] Phase 2: Path extraction unit tests pass
- [ ] Phase 3: Transaction log reading tests pass
- [ ] Phase 4: Alignment detection tests pass
- [ ] Phase 5: All tests pass after fixing `storageEnv` creation
- [ ] Phase 6: Notification handling tests pass
- [ ] Phase 7: New intent-based tests pass
- [ ] Phase 8: All tests pass, no regressions

**Final Verification - Correctness Criteria:**
- [ ] **Coherent Read Results:** Querying `/a/b @N` at alignment point returns correct state
- [ ] **Path Coordination:** Only paths with commits (+ ancestors) are notified (no false positives/negatives)
- [ ] **Ancestor Notification:** Ancestors are notified correctly (paths + ancestors strategy)
- [ ] **Alignment Coordination:** Compaction coordinates across hierarchy at alignment points
- [ ] **Alignment Detection:** Compaction happens exactly when `Inputs >= Divisor` AND `EndCommit % align == 0`
- [ ] **Path Extraction:** Paths correctly extracted from `PendingFiles`, deduplicated
- [ ] **Edge Cases:** Empty `PendingFiles`, duplicates, aborted commits handled correctly
- [ ] **Race Conditions:** No race conditions (`go test -race`)
- [ ] **Performance:** No regression in commit performance (notifications non-blocking)
- [ ] **Backward Compatibility:** Works with existing transaction logs

---

## Risk Areas

1. **Circular dependencies:** Types package must be created first
2. **Race conditions:** Alignment notifications must be thread-safe
3. **Performance:** Reading transaction log shouldn't block commits
4. **Test breakage:** Tests WILL break in Phase 1 and Phase 5 - fix immediately
   - Phase 1: Import updates needed in `log_test.go`
   - Phase 5: `storageEnv` struct changes break test setup functions
5. **Edge cases:** Empty `PendingFiles`, missing log entries, etc.
6. **Struct changes:** Adding fields to `DirCompactor` and `storageEnv` may break tests that create them directly

---

## Success Criteria

1. ✅ Compaction coordinates across virtual path hierarchy
2. ✅ Only relevant `DirCompactor` instances are notified
3. ✅ Ancestors are notified for read optimization
4. ✅ Alignment points are detected correctly
5. ✅ No performance regressions
6. ✅ All tests pass
7. ✅ No race conditions
8. ✅ Backward compatible with existing logs
