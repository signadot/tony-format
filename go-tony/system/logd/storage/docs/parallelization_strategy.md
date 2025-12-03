# Parallelization Strategy: Intent-Based Compaction Implementation

## Overview

This document outlines how to parallelize the implementation work between human developer and AI assistant to maximize efficiency while maintaining code quality.

## Work Division Principles

**AI Assistant (Composer) Strengths:**
- Mechanical refactoring (moving code, updating imports)
- Following existing patterns
- Writing comprehensive unit tests
- Code generation and boilerplate
- Documentation updates

**Human Developer Strengths:**
- Design decisions and trade-offs
- Integration and debugging
- Understanding system behavior
- Performance optimization
- Review and validation

## Parallelization Plan

### Track A: Foundation (AI-Primary)

**Phase 1: Types Package** - AI can do independently
- Create `storage/types/` package
- Move `TxLogEntry` and `FileRef`
- Update imports in `log.go`, `tx.go`, `storage_gen.go`
- Fix test imports in `log_test.go`
- Run codegen
- **Deliverable:** Working types package with all tests passing

**Phase 2: Path Extraction** - AI can do independently
- Create `path_extraction.go` with helper functions
- Write comprehensive unit tests
- **Deliverable:** Tested path extraction utilities

**Phase 3: Transaction Log Reading** - AI can do independently
- Add `readCommits` and `parseTxLogLines` methods
- Write unit tests
- **Deliverable:** Tested log reading functionality

**AI can work on Track A phases in sequence or prepare all code, then human reviews.**

---

### Track B: Integration (Human-Primary, AI-Support)

**Phase 4: Alignment Detection** - Human leads, AI assists
- **Human:** Design the `OnAlignmentPointReached` integration
- **AI:** Implement method following human's design
- **Human:** Review and test integration points
- **AI:** Write unit tests for the method
- **Deliverable:** Alignment detection working end-to-end

**Phase 5: DirCompactor Integration** - Human leads, AI assists
- **Human:** Decide how to pass `Compactor` reference to `DirCompactor`
- **AI:** Implement struct changes (`storageEnv.compactor`, `DirCompactor.alignmentPoint`)
- **Human:** Review and fix test breakage (`compact_test.go`, `read_state_test.go`)
- **AI:** Write tests for alignment point detection
- **Deliverable:** DirCompactor detects alignment points

**Phase 6: Notification Handling** - Human leads, AI assists
- **Human:** Design notification handling logic
- **AI:** Implement `pendingAlignments` and update `shouldCompactNow`
- **Human:** Test and debug integration
- **AI:** Write integration tests
- **Deliverable:** Notifications trigger compaction correctly

---

### Track C: Testing & Polish (Collaborative)

**Phase 7: New Tests** - Collaborative
- **AI:** Write test helpers and edge case tests
- **Human:** Write integration tests for real scenarios
- **AI:** Write performance tests
- **Human:** Review and refine all tests
- **Deliverable:** Comprehensive test coverage

**Phase 8: Cleanup** - Human leads, AI assists
- **Human:** Code review and cleanup
- **AI:** Documentation updates
- **Human:** Final validation and performance checks
- **Deliverable:** Production-ready code

---

## Recommended Workflow

### Option 1: Sequential Tracks (Safer)

**Track A:**
- **AI:** Complete Track A (Phases 1-3)
- **Human:** Review Track A, fix any issues
- **Deliverable:** Foundation ready

**Track B:**
- **Human:** Design Phase 4 integration
- **AI:** Implement Phase 4 following design
- **Human:** Test and iterate
- **Deliverable:** Alignment detection working

- **Human:** Design Phase 5 integration
- **AI:** Implement Phase 5 following design
- **Human:** Fix test breakage, test integration
- **Deliverable:** DirCompactor integration complete

- **Human:** Design Phase 6 integration
- **AI:** Implement Phase 6 following design
- **Human:** Test and debug
- **Deliverable:** Notification handling complete

**Track C:**
- **Collaborative:** Phase 7-8 testing and polish
- **Deliverable:** Complete implementation

---

### Option 2: Parallel Tracks (Faster, requires coordination)

**Parallel Work:**
- **AI:** Works on Track A (Phases 1-3) independently
- **Human:** Reviews design, prepares for Track B integration

**After Track A Complete:**
- **Human:** Reviews Track A, provides feedback
- **AI:** Fixes issues while human starts Track B design
- **Human:** Implements Track B with AI assistance
- **AI:** Writes tests in parallel

**Advantage:** Faster overall, but requires good communication

---

## Specific Parallelization Opportunities

### 1. Test Writing (Can be done in parallel)

**AI writes:**
- Unit tests for path extraction
- Unit tests for log reading
- Unit tests for alignment detection
- Edge case tests

**Human writes:**
- Integration tests with real transactions
- Performance/load tests
- Complex scenario tests

### 2. Code Review (Parallel review)

**AI prepares:**
- Complete implementation for a phase
- All tests passing
- Documentation updated

**Human reviews:**
- Design decisions
- Integration points
- Performance implications
- Edge cases

### 3. Documentation (AI can do independently)

**AI updates:**
- Code comments
- Package documentation
- Design document updates
- Implementation notes

**Human reviews:**
- Accuracy
- Completeness
- Clarity

---

## Communication Protocol

### For Each Phase:

1. **AI Prepares:**
   - Complete implementation
   - All tests passing locally
   - Documentation updated
   - Summary of changes

2. **Human Reviews:**
   - Code quality
   - Design alignment
   - Test coverage
   - Integration concerns

3. **Iterate:**
   - AI fixes issues
   - Human tests integration
   - Repeat until ready

### Checkpoints:

- **After Phase 1:** Verify types package works, no circular deps
- **After Phase 3:** Verify log reading works independently
- **After Phase 4:** Verify alignment detection works end-to-end
- **After Phase 5:** Verify DirCompactor integration works
- **After Phase 6:** Verify full notification flow works
- **After Phase 7:** Verify all tests pass
- **After Phase 8:** Ready for production

---

## Risk Mitigation

### Dependency Risks:
- **Mitigation:** AI completes Track A first, human reviews before Track B starts
- **Fallback:** If Track A has issues, human can fix while AI works on tests

### Integration Risks:
- **Mitigation:** Human leads integration phases, AI follows design
- **Fallback:** Frequent checkpoints, iterate quickly

### Test Breakage Risks:
- **Mitigation:** AI fixes tests immediately in each phase
- **Fallback:** Human reviews test fixes, ensures they're correct

---

## Recommended Approach

**Start with Option 1 (Sequential Tracks):**
1. AI completes Track A (Phases 1-3) - foundation work
2. Human reviews and validates Track A
3. Human and AI collaborate on Track B (Phases 4-6) - integration work
4. Collaborative work on Track C (Phases 7-8) - testing and polish

**Benefits:**
- Clear handoff points
- Foundation validated before building on it
- Human maintains control over integration decisions
- AI handles mechanical work efficiently

**Note:** Timeline depends on:
- Codebase complexity and test coverage
- Integration challenges discovered during implementation
- Review and iteration cycles
- Your development velocity and priorities

---

## Next Steps

1. **AI starts:** Phase 1 (Types Package) - can begin immediately
2. **Human prepares:** Review design, prepare test environment
3. **After Phase 1:** Human reviews, provides feedback
4. **Continue:** Follow sequential track approach with parallel test writing
