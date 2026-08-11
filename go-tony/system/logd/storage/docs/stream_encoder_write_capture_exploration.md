# StreamEncoder Write Capture API Exploration

## Problem Statement

**Range descriptors need chunk data** (encoded bytes) for elements in a range. When streaming, we write directly to snapshot, but range descriptors need the encoded bytes separately.

**Current encoding approach** (`encode/encode.go`):
- Writes directly to `io.Writer` via `writeString(w, s)` → `w.Write([]byte(s))`
- No intermediate buffering or capture mechanism
- Simple, efficient, but no way to capture what was written

## Design Goals

1. ✅ **Efficient**: Minimize overhead when capture is disabled
2. ✅ **Flexible**: Enable/disable capture dynamically
3. ✅ **Simple**: Easy to use, clear API
4. ✅ **Compatible**: Works with existing `io.Writer` interface
5. ✅ **Memory-aware**: Can limit buffer size or provide callbacks

## API Design Options

### Option 1: Capture Buffer with Enable/Disable

**API**:
```go
type StreamEncoder struct {
    writer io.Writer
    captureBuffer *bytes.Buffer
    capturing bool
    // ...
}

func (e *StreamEncoder) EnableCapture() {
    e.capturing = true
    e.captureBuffer = &bytes.Buffer{}
}

func (e *StreamEncoder) DisableCapture() []byte {
    if !e.capturing {
        return nil
    }
    data := e.captureBuffer.Bytes()
    e.capturing = false
    e.captureBuffer = nil
    return data
}

func (e *StreamEncoder) GetCapturedBytes() []byte {
    if !e.capturing {
        return nil
    }
    return e.captureBuffer.Bytes()
}

// Internal write method
func (e *StreamEncoder) writeBytes(data []byte) error {
    if _, err := e.writer.Write(data); err != nil {
        return err
    }
    if e.capturing {
        e.captureBuffer.Write(data)
    }
    return nil
}
```

**Usage**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)
rangeState := &RangeState{...}

enc.BeginArray()
for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    if rangeState.IsBuildingRange {
        enc.EnableCapture()
    }
    
    enc.WriteInt(i)
    
    size := enc.Offset() - offsetBefore
    
    if size < threshold {
        // Small - add to range
        if !rangeState.IsBuildingRange {
            rangeState.StartRange(enc.Offset())
            enc.EnableCapture()
        }
        rangeState.AccumulatedSize += size
    } else {
        // Large - index individually
        if rangeState.IsBuildingRange {
            chunkData := enc.DisableCapture()
            rangeState.FinalizeRange(chunkData)
        }
        addIndexEntry(i, offsetBefore, size)
    }
}
```

**Pros**:
- ✅ Simple API
- ✅ Zero overhead when disabled
- ✅ Can enable/disable dynamically

**Cons**:
- ⚠️ **Memory**: Buffers entire range in memory
- ⚠️ **Manual management**: Must remember to disable capture
- ⚠️ **State tracking**: Need to track when to enable/disable

### Option 2: Capture Callback

**API**:
```go
type CaptureCallback func([]byte) error

type StreamEncoder struct {
    writer io.Writer
    captureCallback CaptureCallback
    // ...
}

func (e *StreamEncoder) SetCaptureCallback(cb CaptureCallback) {
    e.captureCallback = cb
}

func (e *StreamEncoder) ClearCaptureCallback() {
    e.captureCallback = nil
}

// Internal write method
func (e *StreamEncoder) writeBytes(data []byte) error {
    if _, err := e.writer.Write(data); err != nil {
        return err
    }
    if e.captureCallback != nil {
        if err := e.captureCallback(data); err != nil {
            return err
        }
    }
    return nil
}
```

**Usage**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)
rangeState := &RangeState{
    Buffer: &bytes.Buffer{},
}

enc.BeginArray()
for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    if rangeState.IsBuildingRange {
        enc.SetCaptureCallback(func(data []byte) error {
            rangeState.Buffer.Write(data)
            return nil
        })
    } else {
        enc.ClearCaptureCallback()
    }
    
    enc.WriteInt(i)
    
    size := enc.Offset() - offsetBefore
    
    if size < threshold {
        if !rangeState.IsBuildingRange {
            rangeState.StartRange(enc.Offset())
            enc.SetCaptureCallback(func(data []byte) error {
                rangeState.Buffer.Write(data)
                return nil
            })
        }
        rangeState.AccumulatedSize += size
    } else {
        if rangeState.IsBuildingRange {
            chunkData := rangeState.Buffer.Bytes()
            rangeState.FinalizeRange(chunkData)
            enc.ClearCaptureCallback()
        }
        addIndexEntry(i, offsetBefore, size)
    }
}
```

**Pros**:
- ✅ **Flexible**: Callback can do anything (write to buffer, stream, etc.)
- ✅ **Memory control**: Callback can limit buffer size
- ✅ **Zero overhead**: No allocation when callback is nil

**Cons**:
- ⚠️ **Complexity**: More complex API
- ⚠️ **State management**: Still need to manage callback lifecycle

### Option 3: Dual Writer (Tee Writer)

**API**:
```go
type StreamEncoder struct {
    writer io.Writer
    teeWriter io.Writer  // Optional tee writer
    // ...
}

func (e *StreamEncoder) SetTeeWriter(w io.Writer) {
    e.teeWriter = w
}

func (e *StreamEncoder) ClearTeeWriter() {
    e.teeWriter = nil
}

// Internal write method
func (e *StreamEncoder) writeBytes(data []byte) error {
    if _, err := e.writer.Write(data); err != nil {
        return err
    }
    if e.teeWriter != nil {
        if _, err := e.teeWriter.Write(data); err != nil {
            return err
        }
    }
    return nil
}
```

**Usage**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)
rangeState := &RangeState{
    Buffer: &bytes.Buffer{},
}

enc.BeginArray()
for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    if rangeState.IsBuildingRange {
        enc.SetTeeWriter(rangeState.Buffer)
    } else {
        enc.ClearTeeWriter()
    }
    
    enc.WriteInt(i)
    
    size := enc.Offset() - offsetBefore
    
    if size < threshold {
        if !rangeState.IsBuildingRange {
            rangeState.StartRange(enc.Offset())
            enc.SetTeeWriter(rangeState.Buffer)
        }
        rangeState.AccumulatedSize += size
    } else {
        if rangeState.IsBuildingRange {
            chunkData := rangeState.Buffer.Bytes()
            rangeState.FinalizeRange(chunkData)
            rangeState.Buffer.Reset()
            enc.ClearTeeWriter()
        }
        addIndexEntry(i, offsetBefore, size)
    }
}
```

**Pros**:
- ✅ **Simple**: Standard `io.Writer` interface
- ✅ **Flexible**: Can use any writer (buffer, file, etc.)
- ✅ **Zero overhead**: No allocation when tee writer is nil

**Cons**:
- ⚠️ **State management**: Still need to manage tee writer lifecycle
- ⚠️ **Memory**: Still buffers entire range

### Option 4: Scoped Capture with Defer Pattern

**API**:
```go
type CaptureScope struct {
    buffer *bytes.Buffer
    encoder *StreamEncoder
}

func (e *StreamEncoder) BeginCapture() *CaptureScope {
    scope := &CaptureScope{
        buffer: &bytes.Buffer{},
        encoder: e,
    }
    e.captureBuffer = scope.buffer
    return scope
}

func (s *CaptureScope) End() []byte {
    s.encoder.captureBuffer = nil
    return s.buffer.Bytes()
}

func (s *CaptureScope) Bytes() []byte {
    return s.buffer.Bytes()
}
```

**Usage**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)
rangeState := &RangeState{...}

enc.BeginArray()
var captureScope *CaptureScope

for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    enc.WriteInt(i)
    
    size := enc.Offset() - offsetBefore
    
    if size < threshold {
        // Small - add to range
        if !rangeState.IsBuildingRange {
            rangeState.StartRange(enc.Offset())
            captureScope = enc.BeginCapture()
        }
        rangeState.AccumulatedSize += size
    } else {
        // Large - index individually
        if rangeState.IsBuildingRange {
            chunkData := captureScope.End()
            rangeState.FinalizeRange(chunkData)
            captureScope = nil
        }
        addIndexEntry(i, offsetBefore, size)
    }
}

// Finalize any remaining range
if captureScope != nil {
    chunkData := captureScope.End()
    rangeState.FinalizeRange(chunkData)
}
```

**Pros**:
- ✅ **Scoped**: Clear lifecycle with `BeginCapture()` / `End()`
- ✅ **Safe**: Can't forget to disable capture
- ✅ **Flexible**: Can query bytes during capture

**Cons**:
- ⚠️ **Memory**: Still buffers entire range
- ⚠️ **State tracking**: Need to track capture scope

### Option 5: Streaming Capture with Size Limit

**API**:
```go
type CaptureConfig struct {
    MaxSize int64  // Maximum size to capture (0 = unlimited)
    OnChunk func([]byte) error  // Called for each chunk
}

func (e *StreamEncoder) BeginCapture(config CaptureConfig) error {
    e.captureConfig = config
    e.captureBuffer = &bytes.Buffer{}
    return nil
}

func (e *StreamEncoder) EndCapture() ([]byte, error) {
    data := e.captureBuffer.Bytes()
    e.captureConfig = CaptureConfig{}
    e.captureBuffer = nil
    return data, nil
}

// Internal write method
func (e *StreamEncoder) writeBytes(data []byte) error {
    if _, err := e.writer.Write(data); err != nil {
        return err
    }
    if e.captureConfig.OnChunk != nil {
        if err := e.captureConfig.OnChunk(data); err != nil {
            return err
        }
    }
    if e.captureBuffer != nil {
        if e.captureConfig.MaxSize > 0 {
            if int64(e.captureBuffer.Len()) + int64(len(data)) > e.captureConfig.MaxSize {
                return fmt.Errorf("capture buffer exceeded max size")
            }
        }
        e.captureBuffer.Write(data)
    }
    return nil
}
```

**Usage**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)
rangeState := &RangeState{...}

enc.BeginArray()
for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    if rangeState.IsBuildingRange {
        enc.BeginCapture(CaptureConfig{
            MaxSize: threshold * 2,  // Safety limit
        })
    }
    
    enc.WriteInt(i)
    
    size := enc.Offset() - offsetBefore
    
    if size < threshold {
        if !rangeState.IsBuildingRange {
            rangeState.StartRange(enc.Offset())
            enc.BeginCapture(CaptureConfig{})
        }
        rangeState.AccumulatedSize += size
    } else {
        if rangeState.IsBuildingRange {
            chunkData, _ := enc.EndCapture()
            rangeState.FinalizeRange(chunkData)
        }
        addIndexEntry(i, offsetBefore, size)
    }
}
```

**Pros**:
- ✅ **Memory control**: Can limit buffer size
- ✅ **Streaming**: Can use callback for streaming
- ✅ **Flexible**: Supports both buffering and streaming

**Cons**:
- ⚠️ **Complexity**: More complex API
- ⚠️ **Error handling**: Need to handle size limit errors

## Comparison

| Option | Simplicity | Memory | Flexibility | Overhead |
|--------|-----------|--------|-------------|----------|
| **1. Enable/Disable** | ⭐⭐⭐⭐⭐ | ⚠️ Buffers all | ⭐⭐⭐ | ✅ Zero when disabled |
| **2. Callback** | ⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ✅ Zero when nil |
| **3. Tee Writer** | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ | ✅ Zero when nil |
| **4. Scoped** | ⭐⭐⭐⭐ | ⚠️ Buffers all | ⭐⭐⭐ | ✅ Zero when not capturing |
| **5. Config** | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⚠️ Slight overhead |

## Recommendation

### For Initial Implementation: **Option 3 (Tee Writer)**

**Rationale**:
- ✅ **Simple**: Standard `io.Writer` interface, familiar pattern
- ✅ **Flexible**: Can use any writer (buffer, file, streaming writer)
- ✅ **Zero overhead**: No allocation when tee writer is nil
- ✅ **Compatible**: Works with existing `io.Writer` ecosystem

**API**:
```go
type StreamEncoder struct {
    writer io.Writer
    teeWriter io.Writer  // Optional tee writer
    // ... other fields
}

func (e *StreamEncoder) SetTeeWriter(w io.Writer) {
    e.teeWriter = w
}

func (e *StreamEncoder) ClearTeeWriter() {
    e.teeWriter = nil
}

// Internal: all write methods call this
func (e *StreamEncoder) writeBytes(data []byte) error {
    if _, err := e.writer.Write(data); err != nil {
        return err
    }
    if e.teeWriter != nil {
        if _, err := e.teeWriter.Write(data); err != nil {
            return err
        }
    }
    return nil
}
```

**Usage Pattern**:
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

### For Future Enhancement: **Option 5 (Config-based)**

**When needed**:
- Memory-constrained environments
- Very large ranges
- Streaming requirements

**Can be added later** without breaking existing API.

## Implementation Considerations

### Internal Write Method

**All write methods** (`WriteInt`, `WriteString`, `BeginObject`, etc.) should call a common `writeBytes()` method:

```go
func (e *StreamEncoder) writeBytes(data []byte) error {
    // Update offset
    e.offset += int64(len(data))
    
    // Write to main writer
    if _, err := e.writer.Write(data); err != nil {
        return err
    }
    
    // Write to tee writer if set
    if e.teeWriter != nil {
        if _, err := e.teeWriter.Write(data); err != nil {
            return err
        }
    }
    
    return nil
}
```

### Example: WriteInt Implementation

```go
func (e *StreamEncoder) WriteInt(value int64) error {
    // Format number
    data := []byte(strconv.FormatInt(value, 10))
    
    // Write via common method
    return e.writeBytes(data)
}
```

### Example: WriteString Implementation

```go
func (e *StreamEncoder) WriteString(value string) error {
    // Quote string
    quoted := token.Quote(value, true)
    data := []byte(quoted)
    
    // Write via common method
    return e.writeBytes(data)
}
```

## Testing Considerations

### Test Cases

1. **Basic capture**:
   ```go
   buf := &bytes.Buffer{}
   enc := NewStreamEncoder(buf)
   enc.SetTeeWriter(captureBuf)
   enc.WriteInt(42)
   assert.Equal(captureBuf.Bytes(), []byte("42"))
   ```

2. **Disable capture**:
   ```go
   enc.SetTeeWriter(captureBuf)
   enc.WriteInt(42)
   enc.ClearTeeWriter()
   enc.WriteInt(43)
   assert.Equal(captureBuf.Bytes(), []byte("42"))
   ```

3. **Nested structures**:
   ```go
   enc.SetTeeWriter(captureBuf)
   enc.BeginObject()
   enc.WriteKey("key")
   enc.WriteString("value")
   enc.EndObject()
   // Verify captured bytes match structure
   ```

4. **Error handling**:
   ```go
   // Test tee writer error propagation
   ```

## Conclusion

**Recommended Approach**: **Option 3 (Tee Writer)**

- ✅ Simple, standard pattern
- ✅ Flexible (can use any writer)
- ✅ Zero overhead when disabled
- ✅ Easy to implement
- ✅ Can be enhanced later with config-based approach

**Next Steps**:
1. Implement `SetTeeWriter()` / `ClearTeeWriter()` API
2. Refactor all write methods to use common `writeBytes()` method
3. Add tests for capture functionality
4. Document usage pattern for range descriptors
