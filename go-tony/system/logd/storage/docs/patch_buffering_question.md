# Why Buffer Events? - Fundamental Question

## The Assumption

We've been assuming we need to:
1. Buffer events until we have a complete node
2. Convert events → node (`EventsToNode`)
3. Apply patch (`Patch(node, subPatch)`)
4. Convert patched node → events (`NodeToEvents`)
5. Emit patched events

## Why Do We Think We Need Buffering?

**Constraint**: `Patch(doc *ir.Node, patch *ir.Node)` requires complete `ir.Node` structures.

**Therefore**: To apply patches, we need complete nodes, not incremental events.

**Therefore**: We must accumulate events until we have a complete node.

## But... Do We Actually Need to Buffer Events?

### Alternative 1: Don't Apply Patches During Snapshot Building
- **Question**: Should patches be applied when **building** snapshots, or when **reading** from snapshots?
- **If applied when reading**: No buffering needed during building - just write events as-is
- **If applied when building**: Need to patch before writing

### Alternative 2: Apply Patches Before Streaming
- **Question**: Can we patch the source document before streaming it?
- **If yes**: No buffering needed - stream already-patched document
- **If no**: Need to patch during streaming

### Alternative 3: Patches Don't Need Complete Nodes
- **Question**: Could patches be applied incrementally at event level?
- **Current answer**: No - `Patch()` fundamentally requires complete nodes
- **But**: Is this a fundamental constraint or an implementation detail?

### Alternative 4: Different Patch Application Model
- **Question**: Could we restructure how patches work to avoid needing complete nodes?
- **Current answer**: Patches are JSON Merge Patch style - recursive structural merge
- **But**: Is there a way to do this incrementally?

## Key Questions

1. **When should patches be applied?**
   - During snapshot building? (write patched events to snapshot)
   - During snapshot reading? (read base events, apply patches on-the-fly)
   - Before snapshot building? (patch source document first)

2. **What is the actual use case?**
   - Are patches meant to modify what gets stored in snapshots?
   - Or are patches meant to modify what gets read from snapshots?

3. **Is buffering the only way?**
   - Given that `Patch()` requires complete nodes, is buffering unavoidable?
   - Or is there a way to restructure the problem?

## Current State

- `Builder` has `patches []*ir.Node` but they're not currently being applied
- `Builder.WriteEvent()` just writes events directly - no patching happening
- Patches are stored but unused

## The Real Question

**Why do we need to buffer events?**

If the answer is "to apply patches", then we need to understand:
- When should patches be applied?
- Is there a way to avoid buffering?
- Or is buffering an acceptable trade-off?
