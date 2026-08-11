# Streaming Migration Summary

## Quick Reference

**Goal**: Replace `token.{Sink,Source}` with `parse.StreamEncoder` and `parse.StreamDecoder` providing explicit stack management.

**Status**: Planning phase - ready to implement

**Timeline**: ~3 weeks

**Key Simplifications**:
1. **Bracketed-Only**: Only `{...}` and `[...]` structures (no block style)
2. **token.Reader**: Simpler tokenization (replaces `token.Source`)
3. **Sparse Arrays as Objects**: Use `BeginObject()` in streaming layer

## Key Documents

1. **Migration Plan**: `stream_encoder_decoder_migration_plan.md`
   - Complete implementation plan
   - Phase-by-phase breakdown
   - Timeline and risk assessment

2. **API Specification**: `stream_encoder_decoder_api_spec.md`
   - Detailed API design
   - Usage examples
   - Implementation notes

3. **Assessment**: `token_vs_jsontext_assessment.md`
   - Why explicit stack management is needed
   - Comparison with jsontext pattern
   - Benefits for indexing

## Why This Change?

### Problem
- `token.{Sink,Source}` have **implicit state** (hard to query)
- Path tracking is **embedded** in token processing (hard to control)
- **Block style complexity**: Path tracking doesn't work for block style (`TArrayElt`)
- **Impedance mismatch** for indexing use cases

### Solution
- `parse.StreamEncoder/Decoder` provide **explicit stack management**
- **Queryable state** (depth, path, parent context)
- **Bracketed-only**: Eliminates ~100+ lines of complexity
- **token.Reader**: Simpler tokenization, path tracking in parse package
- **Foundation for streaming** (wider applicability than indexing)

## Key Benefits

1. ✅ **Explicit State**: Query depth, path, structure type
2. ✅ **Better for Indexing**: Build indexes with explicit control
3. ✅ **Foundation**: Streaming documents is core capability
4. ✅ **Clean API**: Separate tokenization from streaming
5. ✅ **Simplified**: Bracketed-only eliminates ~100+ lines of complexity
6. ✅ **Reliable**: Path tracking works accurately (no ambiguous boundaries)

## API Overview

### StreamEncoder
```go
enc := parse.NewStreamEncoder(writer)
enc.BeginObject()
enc.WriteKey("key")
enc.WriteString("value")
path := enc.CurrentPath()  // "key"
offset := enc.Offset()      // Byte offset
enc.EndObject()
```

### StreamDecoder
```go
dec := parse.NewStreamDecoder(reader)
for {
    tok, err := dec.ReadToken()
    if err == io.EOF { break }
    path := dec.CurrentPath()  // Query current path
    depth := dec.Depth()        // Query current depth
}
```

## Implementation Phases

0. **Phase 0**: Create `token.Reader` (simplified tokenization)
1. **Phase 1**: Core infrastructure (state management, bracketed-only)
2. **Phase 2**: StreamEncoder implementation (bracketed-only)
3. **Phase 3**: StreamDecoder implementation (uses `token.Reader`)
4. **Phase 4**: Integration (update `NodeParser` to use `token.Reader`)
5. **Phase 5**: Remove legacy code (`token.{Sink,Source}` and `token.Source`)

## Current Status

- ✅ **No existing users**: `token.{Sink,Source}` not used in codebase
- ✅ **Safe to remove**: No production dependencies
- ✅ **Ready to implement**: Plan and API spec complete
- ✅ **Simplifications agreed**: Bracketed-only, eliminate token.Source, sparse arrays as objects

## Next Steps

1. Review migration plan and API spec
2. Start Phase 0: Create `token.Reader` (simplified tokenization)
3. Start Phase 1: Core infrastructure (bracketed-only)
4. Implement incrementally with tests
5. Remove legacy code once complete

## Simplifications Summary

### Bracketed-Only
- ✅ Eliminates ~100+ lines of `TArrayElt` handling
- ✅ Reliable path tracking (explicit boundaries)
- ✅ Simple state machine (just push/pop)
- ✅ Matches jsontext pattern

### token.Reader
- ✅ Simpler token package (just tokenization)
- ✅ Path tracking in parse package (where used)
- ✅ Cleaner separation of concerns

### Sparse Arrays
- ✅ Simpler API (`BeginObject()` for both)
- ✅ Semantic distinction at parse layer
- ✅ Matches structural reality

## Questions?

See detailed documents:
- **Migration Plan**: Complete implementation guide
- **API Spec**: Detailed API design and examples
- **Assessment**: Why this change is needed
