# Range Descriptors Solution Summary

## The Problem

**User's Critical Insight**:
> "When a container contains more elements than can be fit in memory, we cannot store an index entry for each element -- there's needs to be a way to handle state across the breadth of a container as well as the depth."

**Core Challenge**:
- Range descriptors need **chunk data** (encoded bytes) for elements in a range
- When streaming, we write directly to snapshot
- Range descriptors need the encoded bytes **separately** to create chunks

## What StreamEncoder Provides

✅ **For depth (nested structures)**:
- Explicit boundaries (`Begin/End`)
- Queryable state (depth, path, parent)
- Queryable offsets

✅ **For breadth (range descriptors)**:
- Can track range boundaries (start index, element count)
- Can calculate sizes (offset differences)
- Can track accumulated size
- ❌ **CAN'T get chunk data** without buffering

## Solution Options

### Option A: Buffer Mode API (Recommended)

**Approach**: Buffer range elements, write chunk when finalized

**API**:
```go
func (e *StreamEncoder) BeginBuffer() *bytes.Buffer
func (e *StreamEncoder) EndBuffer() []byte
func (e *StreamEncoder) WriteChunk(data []byte) (int64, error)
```

**Usage**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)

enc.BeginArray()
var buffer *bytes.Buffer

for each element {
    if shouldBuildRange {
        if buffer == nil {
            buffer = enc.BeginBuffer()  // Start buffering
        }
        enc.WriteInt(i)  // Written to buffer
    } else {
        if buffer != nil {
            // Finalize range: write buffer as chunk
            chunkData := enc.EndBuffer()
            chunkOffset, _ := enc.WriteChunk(chunkData)
            finalizeRange(chunkOffset, chunkData)
            buffer = nil
        }
        enc.WriteInt(i)  // Written directly to snapshot
    }
}
```

**Pros**:
- ✅ **Single write**: Elements go to buffer OR snapshot, not both
- ✅ **Efficient**: No dual writing
- ✅ **Clear semantics**: Buffer mode vs. direct write
- ✅ **Offset tracking**: Always tracks snapshot position

**Cons**:
- ⚠️ **State management**: Need to track buffering state

**Verdict**: ✅ **Recommended - matches user's insight**

### Option B: Tee Writer API Extension (Alternative)

**Approach**: Add `SetTeeWriter()` / `ClearTeeWriter()` to StreamEncoder

**API**:
```go
func (e *StreamEncoder) SetTeeWriter(w io.Writer)
func (e *StreamEncoder) ClearTeeWriter()
```

**Usage**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)
rangeBuffer := &bytes.Buffer{}

enc.BeginArray()
for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    // Enable capture when building range
    if shouldBuildRange(i) {
        enc.SetTeeWriter(rangeBuffer)
    }
    
    enc.WriteInt(i)
    
    size := enc.Offset() - offsetBefore
    
    if shouldFinalizeRange(size) {
        chunkData := rangeBuffer.Bytes()
        finalizeRange(chunkData)
        rangeBuffer.Reset()
        enc.ClearTeeWriter()
    }
}
```

**Pros**:
- ✅ **Simple**: Standard `io.Writer` interface, familiar pattern
- ✅ **Flexible**: Can use any writer (buffer, file, streaming writer)
- ✅ **Zero overhead**: No allocation when tee writer is nil
- ✅ **Compatible**: Works with existing `io.Writer` ecosystem
- ✅ **Efficient**: Single write operation, writes to both destinations

**Cons**:
- ⚠️ **Memory**: Still buffers entire range (but can use streaming writer)
- ⚠️ **State management**: Need to manage tee writer lifecycle

**Verdict**: ✅ **Recommended for initial implementation**

### Option C: Capture Callback API

**Approach**: Add callback that receives bytes as they're written

**API**:
```go
type CaptureCallback func([]byte) error
func (e *StreamEncoder) SetCaptureCallback(cb CaptureCallback)
func (e *StreamEncoder) ClearCaptureCallback()
```

**Pros**:
- ✅ **Most flexible**: Callback can do anything
- ✅ **Memory control**: Can limit buffer size or stream
- ✅ **Zero overhead**: No allocation when callback is nil

**Cons**:
- ⚠️ **Complexity**: More complex API
- ⚠️ **State management**: Need to manage callback lifecycle

**Verdict**: ⚠️ **Good for future enhancement**

### Option D: Scoped Capture API

**Approach**: Capture scope with `BeginCapture()` / `End()`

**API**:
```go
func (e *StreamEncoder) BeginCapture() *CaptureScope
func (s *CaptureScope) End() []byte
```

**Pros**:
- ✅ **Scoped**: Clear lifecycle
- ✅ **Safe**: Can't forget to disable capture

**Cons**:
- ⚠️ **Memory**: Still buffers entire range
- ⚠️ **State tracking**: Need to track capture scope

**Verdict**: ⚠️ **Good alternative, slightly more complex**

## Recommendation

### Phase 1: Implement Buffer Mode API (Option A)

**Rationale**:
1. ✅ **Single write**: Elements go to buffer OR snapshot, not both
2. ✅ **Efficient**: No dual writing overhead
3. ✅ **Matches user's insight**: "Write chunks as they arrive, nothing in between"
4. ✅ **Clear semantics**: Buffer mode vs. direct write
5. ✅ **Offset tracking**: Always tracks snapshot position correctly

**Implementation**:
- Add `BeginBuffer()` / `EndBuffer()` / `WriteChunk()` methods
- Refactor all write methods to check buffer state
- Update offset tracking to always track snapshot position
- Add tests for buffering functionality

**Timeline**: ~1-2 days

### Phase 2: Evaluate Need for Advanced Features

**If needed later**:
- Memory-constrained environments → Add config-based capture with size limits
- Streaming requirements → Add callback-based capture
- Better ergonomics → Add scoped capture API

**Can be added without breaking existing API**

## Updated Understanding

### What We Need for Range Descriptors

1. ✅ **Range boundaries**: `[from, to]` - StreamEncoder enables this
2. ✅ **Chunk offset**: Where chunk starts - StreamEncoder provides
3. ✅ **Chunk size**: Size of chunk - StreamEncoder enables calculation
4. ✅ **Chunk data**: Encoded bytes - **Buffer Mode API provides this**

### StreamEncoder Capabilities

**Provides**:
- ✅ Explicit boundaries (depth)
- ✅ Queryable offsets (size calculation)
- ✅ Queryable state (path, parent, etc.)
- ✅ **Tee writer** (chunk data capture)

**Enables**:
- ✅ Building Index entries (simple indexing)
- ✅ Building range descriptors (large containers)
- ✅ Building `ir.Node` index structure (from offset/size info)

## Conclusion

**Answer to User's Question**:

> "Are we sure index building only requires index entries and not full ir.Node's?"

**Answer**:
- ✅ **For simple indexing**: Index entries are sufficient
- ✅ **For range descriptors**: Need chunk data (encoded bytes)
- ✅ **Solution**: Tee Writer API extension to StreamEncoder
- ✅ **Index structure**: IS an `ir.Node`, but we can build it from offset/size/chunk data

**StreamEncoder + Buffer Mode API** provides everything needed for both simple indexing and range descriptors!

**Key Insight**: Write chunks as they arrive, nothing in between - single write, no dual writing overhead.
