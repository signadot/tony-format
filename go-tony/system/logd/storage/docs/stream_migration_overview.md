# Stream Migration Overview

## Goal

Replace `token.{Sink,Source}` and `parse.NodeParser` with new `stream` package providing structural event-based streaming encode/decode optimized for indexing.

**Note**: Types are `stream.Encoder` and `stream.Decoder` (not `StreamEncoder`/`StreamDecoder` to avoid stuttering).

## Key Principles

1. **Coexistence**: `stream` package is NEW capability, coexists with `parse`/`encode` (no replacement)
2. **Bracketed-Only**: Only supports `{...}` and `[...]` structures (no block style)
3. **Structural Events**: Decoder returns events (BeginObject, Key, String, etc.) matching encoder API
4. **Explicit Stack Management**: Queryable state (depth, path, offsets) like jsontext

## Two Phases + Sanity Check

### Phase 1: Create stream Package (~12-15 days)
- Implement `State` (minimal core)
- Implement `Encoder` (structural encoding)
- Implement `Decoder` (structural event-based decoding)
- Add `NodeToEvents()` / `EventsToNode()` conversion utilities
- **API Fix**: `NewDecoder/NewEncoder` require bracketing option (return error if not specified)
- **Comment-Ready API**: Add comment event types and methods (no-ops in Phase 1, aligned with IR spec)

### Sanity Check: Evaluate Phase 1 Results
- Review API design and implementation
- Test basic functionality
- Evaluate readiness for Phase 2
- **Decision point**: Proceed to Phase 2 or adjust?

### Phase 2: Remove Old Code (~3 days)
- Remove `token.Sink` and `token.Source`
- Remove `parse.NodeParser`
- Clean up dependencies
- Verify no regressions

**Total**: ~15-18 days (~3-4 weeks)

## What Gets Removed

- `token/sink.go` + tests (~542 lines)
- `token/source.go` + tests (~603 lines)
- `parse/incremental.go` + tests (~1,282 lines)

**Total**: ~2,427 lines removed

## What Gets Added

- `stream/` package (~1,000-1,300 lines)
  - `state.go` (~200 lines)
  - `decoder.go` (~400 lines)
  - `encoder.go` (~400-500 lines)
  - `event.go` (~100 lines)
  - `conversion.go` (~200 lines)
  - Tests

**Net**: ~1,100-1,400 lines reduction (45-55%)

## API Design

### Encoder
```go
enc, err := stream.NewEncoder(writer, stream.WithBrackets())
enc.BeginObject()
enc.WriteHeadComment([]string{"# Head comment"})  // Comment-ready API
enc.WriteKey("name")
enc.WriteString("value")
enc.WriteLineComment([]string{"# Line comment"})  // Comment-ready API
enc.EndObject()
```

### Decoder
```go
dec, err := stream.NewDecoder(reader, stream.WithBrackets())
if err != nil {
    return err  // Bracketing required!
}

event, _ := dec.ReadEvent()  // EventBeginObject
// Phase 1: Comments are skipped
// Phase 2: event, _ := dec.ReadEvent()  // EventHeadComment
event, _ := dec.ReadEvent()  // EventKey("name")
event, _ := dec.ReadEvent()  // EventString("value")
// Phase 2: event, _ := dec.ReadEvent()  // EventLineComment
event, _ := dec.ReadEvent()  // EventEndObject
```

### Key API Fix
- `NewDecoder/NewEncoder` return `(*Decoder, error)` / `(*Encoder, error)`
- Must specify `stream.WithBrackets()` or `stream.WithWire()`
- Returns error if bracketing not specified

## Benefits

1. ✅ **Cleaner API**: Structural events match encoder methods
2. ✅ **Better for Indexing**: Queryable state enables range descriptors
3. ✅ **Less Code**: Remove ~2,400 lines, add ~1,200 lines
4. ✅ **No Risk**: `parse`/`encode` unchanged, `stream` is new capability
5. ✅ **Proven Pattern**: Similar to jsontext.{Encoder,Decoder}
6. ✅ **Comment-Ready**: API designed for comments (aligned with IR spec), implementation deferred

## Success Criteria

### Phase 1 Complete
- ✅ `stream` package implemented and tested
- ✅ All tests passing
- ✅ API reviewed and approved
- ✅ Documentation complete

### Sanity Check Passed
- ✅ API design validated
- ✅ Basic functionality verified
- ✅ Ready for Phase 2

### Phase 2 Complete
- ✅ Old code removed
- ✅ No regressions
- ✅ Codebase cleaner

## See Also

- `stream_migration_plan_comprehensive.md` - Detailed step-by-step plan
- `stream_package_design.md` - Package structure and API details
- `streaming_indexing_api_design.md` - Indexing usage patterns
