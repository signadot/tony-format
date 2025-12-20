# [logd storage] Streaming Patch Processor

## Problem

Apply a sequence of patches from multiple commits to a snapshot without loading the entire document into memory.

## Key Insight

**Snapshots ≠ Patches** in terms of streaming requirements:
- **Snapshots:** Huge collections (millions of items) → **must stream**
- **Patches:** Targeted updates to specific items → **can materialize affected subtree**

API usage typically patches individual items (`PATCH /users/42`), not entire collections. Bulk updates are done by iterating and patching each item separately.

## Approach

Stream events from snapshot, only materializing subtrees that are patched:

```
Stream snapshot events
    ↓
Track current path (stream.State)
    ↓
Is current path a patch root?
    ├─ YES → Buffer subtree
    │        Apply all patches sequentially
    │        Emit result
    │        Skip original events
    │
    └─ NO  → Emit events unchanged
```

**Memory:** O(largest patched subtree), not O(document size)

## Implementation

### 1. Tag Patch Roots at Commit Time

Mark the root of each patch in the IR tree:

```go
const PatchRootTag = "!logd-patch-root"

// At commit time, from api.Patch
for _, pd := range patcherData {
    path := pd.API.Patch.Path        // e.g., "users{42}"
    data := pd.API.Patch.Data

    // Tag the patch data root
    data.Tag = appendTag(data.Tag, PatchRootTag)
}

// After MergePatches within tx → tagged nodes preserved
// dlog.Entry.Patch contains full tree from root with tags at original paths
```

### 2. Build Patch Index

Walk dlog entries to find which commits affect which paths:

```go
type PatchIndex struct {
    // Map from path to commits affecting that path or descendants
    byPath map[string][]CommitPatch
}

type CommitPatch struct {
    Commit int64
    Entry  *dlog.Entry
}

func buildPatchIndex(entries []*dlog.Entry) *PatchIndex {
    index := &PatchIndex{byPath: make(map[string][]CommitPatch)}

    for _, entry := range entries {
        // Walk IR tree looking for !logd-patch-root tags
        walkIRTree(entry.Patch, func(node *ir.Node, path string) {
            if hasPatchRootTag(node) {
                index.byPath[path] = append(index.byPath[path],
                    CommitPatch{Commit: entry.Commit, Entry: entry})
            }
        })
    }

    return index
}
```

### 3. Filter Dominated Patch Roots (Cross-Commit Only)

Within a single transaction, patches are disjoint by construction (enforced by tx/merge.go).

For cross-commit scenarios (compaction, snapshot creation), filter dominated roots:

```go
func filterDominatedPatchRoots(node *ir.Node) {
    walkIRTree(node, func(n *ir.Node) {
        if !hasPatchRootTag(n) {
            return
        }

        // Walk up parent chain
        parent := n.Parent
        for parent != nil {
            if hasPatchRootTag(parent) {
                // Dominated by ancestor, remove tag
                removePatchRootTag(n)
                break
            }
            parent = parent.Parent
        }
    })
}
```

### 4. Stream with Patch Application

```go
type StreamingProcessor struct {
    patchIndex *PatchIndex
    state      *stream.State
}

func (p *StreamingProcessor) Process(sourceReader EventReader, sink EventWriter) error {
    for {
        event, err := sourceReader.ReadEvent()
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }

        currentPath := p.state.CurrentPath()

        if patches := p.patchIndex.byPath[currentPath]; len(patches) > 0 {
            // Entering a patched subtree
            events := collectEventsForSubtree(sourceReader, p.state)
            baseNode := stream.EventsToNode(events)

            // Apply patches sequentially (in commit order)
            result := baseNode
            for _, cp := range patches {
                // Extract subtree at currentPath from full patch tree
                patchAtPath := navigateTo(cp.Entry.Patch, currentPath)
                result = tony.Patch(result, patchAtPath)
            }

            // Emit patched result
            patchedEvents := stream.NodeToEvents(result)
            for _, evt := range patchedEvents {
                sink.WriteEvent(&evt)
            }

            // Skip original events (already consumed by collectEventsForSubtree)
            continue
        }

        // No patch here, stream through
        sink.WriteEvent(event)
        p.state.ProcessEvent(event)
    }

    return nil
}
```

### 5. Strip Internal Tags

Remove `!logd-patch-root` before returning to users:

```go
func stripInternalTags(node *ir.Node) {
    if node == nil {
        return
    }
    node.Tag = removeTag(node.Tag, PatchRootTag)
    for _, child := range node.Values {
        stripInternalTags(child)
    }
}

// In ReadStateAt()
func (s *Storage) ReadStateAt(kPath string, commit int64) (*ir.Node, error) {
    // ... existing logic ...
    node := stream.EventsToNode(events)
    stripInternalTags(node)
    return node, nil
}
```

## Example Walkthrough

### Scenario: Delete followed by Recreate

```
Snapshot: {a: {b: {c: 1, d: 2}}}

Commit 1: DELETE a.b
  api.Patch{Path: "a.b", Data: !delete}
  dlog.Entry.Patch: {a: {b: !delete}}
  Tag at a.b

Commit 2: PATCH a.b.c
  api.Patch{Path: "a.b.c", Data: 10}
  dlog.Entry.Patch: {a: {b: {c: 10}}}
  Tag at a.b.c
```

**Patch Root Filtering:**
- `a.b.c` walks up parent chain → finds `a.b` with tag → **dominated**
- After filtering: only `a.b` is a patch root

**Streaming Execution:**

When stream hits path `a.b`:
1. Lookup patches: commits 1 and 2 affect `a.b`
2. Buffer original subtree: `{c: 1, d: 2}`
3. Apply patches sequentially:
   ```
   result = {c: 1, d: 2}
   result = tony.Patch(result, !delete)    → null
   result = tony.Patch(null, {c: 10})      → {c: 10}
   ```
4. Emit: `{c: 10}`
5. Skip original events for `a.b.*` (already handled)

## Tag Naming Convention

Use `!logd-patch-root` to avoid user collisions:
- Namespaced to `logd` subsystem
- Unlikely user would choose this name
- Self-documenting purpose

Document in schema context (tony-format/storage/logd) for power users, but strip from normal API results.

## Memory Characteristics

- **Snapshot streaming:** O(depth + buffering) for unchanged paths
- **Patched subtree:** O(size of subtree) temporarily while applying patches
- **Overall:** O(largest patched subtree), independent of total document size

A document with 1M array elements where one element is patched uses memory proportional to that single element, not the entire array.

## Status

- Design: Complete
- Implementation: Not started
- Dependencies: Requires tag support in commit path
