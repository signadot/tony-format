# Range Descriptors: Single Write Approach

## User's Insight

> "Not sure it needs to be written twice or should be. The indexing case would write those chunks as they arrive and nothing in between."

**Key Point**: For indexing, we should:
1. **Buffer range elements** (don't write to snapshot yet)
2. **Write chunk when finalized** (write buffered data as a single chunk)
3. **Write non-range elements directly** (large elements indexed individually)

**No dual writing** - elements go to either buffer OR snapshot, not both.

## The Flow

### Current Understanding (Wrong)

```
For each element:
  - Write to snapshot ✅
  - Also write to buffer ❌ (dual writing)
  - When range finalized: use buffer for chunk
```

### Correct Understanding

```
For each element:
  - If small (in range): Write to buffer only
  - If large (indexed): Write to snapshot directly
  - When range finalized: Write buffer to snapshot as chunk
```

## Example Flow

```go
enc := parse.NewStreamEncoder(snapshotWriter)
rangeBuffer := &bytes.Buffer{}

enc.BeginArray()

// Element 0: Small → buffer
enc.SetWriter(rangeBuffer)  // Redirect writes to buffer
enc.WriteInt(0)

// Element 1: Small → buffer (continues)
enc.WriteInt(1)

// Element 2: Small → buffer (continues)
enc.WriteInt(2)

// Range finalized (threshold reached)
chunkData := rangeBuffer.Bytes()
chunkOffset := writeChunkToSnapshot(snapshotWriter, chunkData)  // Write buffer as chunk
createRangeDescriptor([0, 3), chunkOffset, len(chunkData))
rangeBuffer.Reset()

// Element 3: Large → snapshot directly
enc.SetWriter(snapshotWriter)  // Switch back to snapshot
enc.WriteInt(3)
addIndexEntry(3, offset, size)

// Element 4: Small → buffer (new range)
enc.SetWriter(rangeBuffer)
enc.WriteInt(4)
// ... continue
```

## API Design Options

### Option 1: Switch Writer Dynamically

**API**:
```go
func (e *StreamEncoder) SetWriter(w io.Writer)
```

**Usage**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)
rangeBuffer := &bytes.Buffer{}

enc.BeginArray()
for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    if shouldBuildRange(i) {
        enc.SetWriter(rangeBuffer)  // Redirect to buffer
    } else {
        enc.SetWriter(snapshotWriter)  // Write to snapshot
    }
    
    enc.WriteInt(i)
    
    size := enc.Offset() - offsetBefore
    
    if shouldFinalizeRange(size) {
        chunkData := rangeBuffer.Bytes()
        chunkOffset := writeChunkToSnapshot(snapshotWriter, chunkData)
        createRangeDescriptor(rangeState, chunkOffset, len(chunkData))
        rangeBuffer.Reset()
    }
}
```

**Pros**:
- ✅ Single write operation
- ✅ Clear control flow
- ✅ Efficient

**Cons**:
- ⚠️ **Offset tracking**: Offset needs to track snapshot position, not buffer position
- ⚠️ **State management**: Need to track which writer is active

### Option 2: Buffer Mode API

**API**:
```go
func (e *StreamEncoder) BeginBuffering() *bytes.Buffer
func (e *StreamEncoder) EndBuffering() []byte
func (e *StreamEncoder) WriteBufferToSnapshot() (int64, error)  // Returns offset
```

**Usage**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)

enc.BeginArray()
var rangeBuffer *bytes.Buffer

for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    if shouldBuildRange(i) {
        if rangeBuffer == nil {
            rangeBuffer = enc.BeginBuffering()  // Start buffering
        }
    } else {
        if rangeBuffer != nil {
            // Finalize range
            chunkData := enc.EndBuffering()
            chunkOffset, _ := enc.WriteBufferToSnapshot(chunkData)
            createRangeDescriptor(rangeState, chunkOffset, len(chunkData))
            rangeBuffer = nil
        }
        // Write directly to snapshot
        enc.WriteInt(i)
    }
    
    if rangeBuffer != nil {
        enc.WriteInt(i)  // Written to buffer
    } else {
        enc.WriteInt(i)  // Written to snapshot
    }
    
    size := enc.Offset() - offsetBefore
    
    if shouldFinalizeRange(size) {
        chunkData := enc.EndBuffering()
        chunkOffset, _ := enc.WriteBufferToSnapshot(chunkData)
        createRangeDescriptor(rangeState, chunkOffset, len(chunkData))
        rangeBuffer = nil
    }
}
```

**Pros**:
- ✅ Encapsulates buffering logic
- ✅ Handles offset tracking internally
- ✅ Clear API

**Cons**:
- ⚠️ **Complexity**: More API surface
- ⚠️ **State**: Need to track buffering state

### Option 3: Writer Stack (Push/Pop)

**API**:
```go
func (e *StreamEncoder) PushWriter(w io.Writer)
func (e *StreamEncoder) PopWriter() io.Writer
```

**Usage**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)
rangeBuffer := &bytes.Buffer{}

enc.BeginArray()
for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    if shouldBuildRange(i) {
        enc.PushWriter(rangeBuffer)  // Push buffer onto stack
    }
    
    enc.WriteInt(i)
    
    size := enc.Offset() - offsetBefore
    
    if shouldFinalizeRange(size) {
        enc.PopWriter()  // Pop buffer, back to snapshot
        chunkData := rangeBuffer.Bytes()
        chunkOffset := writeChunkToSnapshot(snapshotWriter, chunkData)
        createRangeDescriptor(rangeState, chunkOffset, len(chunkData))
        rangeBuffer.Reset()
    }
}
```

**Pros**:
- ✅ Supports nested buffering
- ✅ Clear push/pop semantics
- ✅ Handles state automatically

**Cons**:
- ⚠️ **Complexity**: Stack management
- ⚠️ **Offset tracking**: Still need to handle offset correctly

## The Offset Problem

**Challenge**: When writing to buffer, `Offset()` should track snapshot position, not buffer position.

**Example**:
```go
enc.SetWriter(rangeBuffer)
enc.WriteInt(0)  // Written to buffer
offset := enc.Offset()  // Should be snapshot offset, not buffer offset!
```

**Solutions**:

### Solution A: Track Snapshot Offset Separately

```go
type StreamEncoder struct {
    snapshotWriter io.Writer
    currentWriter io.Writer
    snapshotOffset int64  // Always tracks snapshot position
    // ...
}

func (e *StreamEncoder) SetWriter(w io.Writer) {
    e.currentWriter = w
    // snapshotOffset continues to track snapshot position
}

func (e *StreamEncoder) writeBytes(data []byte) error {
    if _, err := e.currentWriter.Write(data); err != nil {
        return err
    }
    // Only update snapshotOffset if writing to snapshot
    if e.currentWriter == e.snapshotWriter {
        e.snapshotOffset += int64(len(data))
    }
    return nil
}

func (e *StreamEncoder) Offset() int64 {
    return e.snapshotOffset
}
```

**Problem**: When writing buffer to snapshot, need to update offset.

### Solution B: Defer Offset Updates

```go
func (e *StreamEncoder) writeBytes(data []byte) error {
    if _, err := e.currentWriter.Write(data); err != nil {
        return err
    }
    // Always update offset (tracks where we WOULD be in snapshot)
    e.offset += int64(len(data))
    return nil
}

func (e *StreamEncoder) WriteBufferToSnapshot(data []byte) (int64, error) {
    offset := e.offset
    if _, err := e.snapshotWriter.Write(data); err != nil {
        return 0, err
    }
    e.offset += int64(len(data))
    return offset, nil
}
```

**This works!** ✅

## Recommended Approach: Writer Stack with Offset Tracking

**API**:
```go
type StreamEncoder struct {
    writerStack []io.Writer  // Stack of writers
    snapshotWriter io.Writer  // Original snapshot writer
    offset int64  // Tracks snapshot position
    // ...
}

func (e *StreamEncoder) PushWriter(w io.Writer) {
    e.writerStack = append(e.writerStack, w)
}

func (e *StreamEncoder) PopWriter() io.Writer {
    if len(e.writerStack) == 0 {
        return nil
    }
    w := e.writerStack[len(e.writerStack)-1]
    e.writerStack = e.writerStack[:len(e.writerStack)-1]
    return w
}

func (e *StreamEncoder) currentWriter() io.Writer {
    if len(e.writerStack) > 0 {
        return e.writerStack[len(e.writerStack)-1]
    }
    return e.snapshotWriter
}

func (e *StreamEncoder) writeBytes(data []byte) error {
    w := e.currentWriter()
    if _, err := w.Write(data); err != nil {
        return err
    }
    // Always update offset (tracks snapshot position)
    e.offset += int64(len(data))
    return nil
}

func (e *StreamEncoder) WriteBufferToSnapshot(data []byte) (int64, error) {
    offset := e.offset
    if _, err := e.snapshotWriter.Write(data); err != nil {
        return 0, err
    }
    e.offset += int64(len(data))
    return offset, nil
}
```

**Usage**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)
rangeBuffer := &bytes.Buffer{}

enc.BeginArray()
for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    if shouldBuildRange(i) {
        if !isBuffering {
            enc.PushWriter(rangeBuffer)
            isBuffering = true
        }
    } else {
        if isBuffering {
            enc.PopWriter()
            chunkData := rangeBuffer.Bytes()
            chunkOffset, _ := enc.WriteBufferToSnapshot(chunkData)
            createRangeDescriptor(rangeState, chunkOffset, len(chunkData))
            rangeBuffer.Reset()
            isBuffering = false
        }
    }
    
    enc.WriteInt(i)
    
    size := enc.Offset() - offsetBefore
    
    if shouldFinalizeRange(size) {
        enc.PopWriter()
        chunkData := rangeBuffer.Bytes()
        chunkOffset, _ := enc.WriteBufferToSnapshot(chunkData)
        createRangeDescriptor(rangeState, chunkOffset, len(chunkData))
        rangeBuffer.Reset()
        isBuffering = false
    }
}
```

## Simpler Alternative: Explicit Buffer Mode

**API**:
```go
func (e *StreamEncoder) BeginBuffer() *bytes.Buffer
func (e *StreamEncoder) EndBuffer() []byte
func (e *StreamEncoder) WriteChunk(data []byte) (int64, error)  // Write buffer to snapshot, return offset
```

**Usage**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)

enc.BeginArray()
var buffer *bytes.Buffer

for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    if shouldBuildRange(i) {
        if buffer == nil {
            buffer = enc.BeginBuffer()  // Start buffering
        }
    } else {
        if buffer != nil {
            // Finalize range
            chunkData := enc.EndBuffer()
            chunkOffset, _ := enc.WriteChunk(chunkData)
            createRangeDescriptor(rangeState, chunkOffset, len(chunkData))
            buffer = nil
        }
    }
    
    enc.WriteInt(i)  // Written to buffer if buffering, snapshot otherwise
    
    size := enc.Offset() - offsetBefore
    
    if shouldFinalizeRange(size) {
        chunkData := enc.EndBuffer()
        chunkOffset, _ := enc.WriteChunk(chunkData)
        createRangeDescriptor(rangeState, chunkOffset, len(chunkData))
        buffer = nil
    }
}
```

**Implementation**:
```go
type StreamEncoder struct {
    writer io.Writer
    buffer *bytes.Buffer
    offset int64
    // ...
}

func (e *StreamEncoder) BeginBuffer() *bytes.Buffer {
    e.buffer = &bytes.Buffer{}
    return e.buffer
}

func (e *StreamEncoder) EndBuffer() []byte {
    if e.buffer == nil {
        return nil
    }
    data := e.buffer.Bytes()
    e.buffer = nil
    return data
}

func (e *StreamEncoder) WriteChunk(data []byte) (int64, error) {
    offset := e.offset
    if _, err := e.writer.Write(data); err != nil {
        return 0, err
    }
    e.offset += int64(len(data))
    return offset, nil
}

func (e *StreamEncoder) writeBytes(data []byte) error {
    target := e.writer
    if e.buffer != nil {
        target = e.buffer
    }
    if _, err := target.Write(data); err != nil {
        return err
    }
    // Always update offset (tracks snapshot position)
    e.offset += int64(len(data))
    return nil
}
```

**This is cleaner!** ✅

## Final Recommendation

### Buffer Mode API (Simplest)

**API**:
```go
func (e *StreamEncoder) BeginBuffer() *bytes.Buffer
func (e *StreamEncoder) EndBuffer() []byte
func (e *StreamEncoder) WriteChunk(data []byte) (int64, error)
```

**Key Points**:
- ✅ **Single write**: Elements go to buffer OR snapshot, not both
- ✅ **Offset tracking**: Always tracks snapshot position
- ✅ **Simple API**: Clear begin/end semantics
- ✅ **Efficient**: No dual writing

**Usage Pattern**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)

enc.BeginArray()
var buffer *bytes.Buffer

for each element {
    if shouldBuildRange {
        if buffer == nil {
            buffer = enc.BeginBuffer()
        }
    } else {
        if buffer != nil {
            chunkData := enc.EndBuffer()
            chunkOffset, _ := enc.WriteChunk(chunkData)
            finalizeRange(chunkOffset, chunkData)
            buffer = nil
        }
        // Write directly to snapshot
    }
    
    enc.WriteValue(element)  // Goes to buffer if buffering, snapshot otherwise
    
    if shouldFinalizeRange {
        chunkData := enc.EndBuffer()
        chunkOffset, _ := enc.WriteChunk(chunkData)
        finalizeRange(chunkOffset, chunkData)
        buffer = nil
    }
}
```

This matches the user's insight: **write chunks as they arrive, nothing in between**.
