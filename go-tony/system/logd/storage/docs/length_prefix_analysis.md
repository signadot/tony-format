# Length-Prefixed Entries: Analysis

## The Question

**User's Question**: "I've been re-considering having length prefixed entries. I guess that's easier, do you agree or not?"

## Current Design: Tony Wire Format (Self-Describing)

**Format**: Tony wire format, self-describing, no delimiters needed
- Parser knows where entry ends by parsing the structure
- No length prefix overhead
- Standard Tony format

**Reading at Offset**:
```go
// Read entry at offset 200
reader := io.NewSectionReader(file, 200, maxSize)
entry, bytesRead, err := LogEntryFromTony(reader)
// Parser reads until entry is complete
```

**Pros**:
- No storage overhead (no length prefix)
- Standard format (Tony wire format)
- Self-describing

**Cons**:
- Need to parse to find boundaries (might be slower)
- Can't seek to specific entry without parsing previous ones
- More complex to implement random access
- Need to read/parse to find where entry ends

## Alternative: Length-Prefixed Entries

**Format**: 4-byte length prefix (uint32) + entry data
```
[4 bytes: length][entry data in Tony format]
```

**Reading at Offset**:
```go
// Read entry at offset 200
// 1. Read 4 bytes to get length
lengthBytes := make([]byte, 4)
file.ReadAt(lengthBytes, 200)
length := binary.BigEndian.Uint32(lengthBytes)

// 2. Read exactly length bytes
entryBytes := make([]byte, length)
file.ReadAt(entryBytes, 200+4)

// 3. Parse entry
entry := LogEntryFromTony(bytes.NewReader(entryBytes))
```

**Pros**:
- Easy to seek: can jump to entry N by summing lengths
- Faster random access (don't need to parse previous entries)
- Simpler implementation for reading specific entries
- Can read length first, then read exact bytes needed
- No need to parse Tony format to find boundaries
- Sequential reads: read length, read entry, move to next (simple)

**Cons**:
- Extra storage (4 bytes per entry)
- Need to update length if entry changes (but we're append-only, so this doesn't matter)
- Not standard Tony format (but we can still use Tony format for entry data)

## Use Case Analysis

### Random Access (From Index)

**Scenario**: Read entry at LogPosition 200

**Tony Wire Format**:
```go
reader := io.NewSectionReader(file, 200, maxSize)  // Need maxSize estimate
entry, bytesRead, err := LogEntryFromTony(reader)
// Parser must parse entire entry to find boundary
```

**Length-Prefixed**:
```go
// Read length (4 bytes)
lengthBytes := make([]byte, 4)
file.ReadAt(lengthBytes, 200)
length := binary.BigEndian.Uint32(lengthBytes)

// Read entry (exact length)
entryBytes := make([]byte, length)
file.ReadAt(entryBytes, 200+4)
entry := LogEntryFromTony(bytes.NewReader(entryBytes))
```

**Winner**: Length-prefixed (simpler, no parsing needed to find boundary)

### Sequential Reads

**Scenario**: Read all entries sequentially

**Tony Wire Format**:
```go
reader := io.NewSectionReader(file, offset, maxSize)
for {
    entry, bytesRead, err := LogEntryFromTony(reader)
    if err != nil { break }
    offset += bytesRead
    // Process entry
}
```

**Length-Prefixed**:
```go
offset := 0
for {
    // Read length
    lengthBytes := make([]byte, 4)
    file.ReadAt(lengthBytes, offset)
    length := binary.BigEndian.Uint32(lengthBytes)
    
    // Read entry
    entryBytes := make([]byte, length)
    file.ReadAt(entryBytes, offset+4)
    entry := LogEntryFromTony(bytes.NewReader(entryBytes))
    
    offset += 4 + length
    // Process entry
}
```

**Winner**: Length-prefixed (simpler, explicit boundaries)

### Recovery (Rebuilding Index)

**Scenario**: Scan log file to rebuild index

**Tony Wire Format**:
```go
reader := io.NewSectionReader(file, 0, fileSize)
offset := 0
for offset < fileSize {
    entry, bytesRead, err := LogEntryFromTony(reader)
    if err != nil { break }
    // Index entry at offset
    index.Add(entry, offset)
    offset += bytesRead
}
```

**Length-Prefixed**:
```go
offset := 0
for offset < fileSize {
    // Read length
    lengthBytes := make([]byte, 4)
    file.ReadAt(lengthBytes, offset)
    length := binary.BigEndian.Uint32(lengthBytes)
    
    // Read entry
    entryBytes := make([]byte, length)
    file.ReadAt(entryBytes, offset+4)
    entry := LogEntryFromTony(bytes.NewReader(entryBytes))
    
    // Index entry at offset
    index.Add(entry, offset)
    offset += 4 + length
}
```

**Winner**: Length-prefixed (can skip entries without parsing, simpler)

### Writing

**Scenario**: Append entry to log

**Tony Wire Format**:
```go
// Serialize entry to Tony format
entryBytes := LogEntryToTony(entry)
file.Write(entryBytes)
```

**Length-Prefixed**:
```go
// Serialize entry to Tony format
entryBytes := LogEntryToTony(entry)

// Write length prefix
lengthBytes := make([]byte, 4)
binary.BigEndian.PutUint32(lengthBytes, uint32(len(entryBytes)))
file.Write(lengthBytes)

// Write entry
file.Write(entryBytes)
```

**Winner**: Length-prefixed (slightly more code, but clearer)

## Storage Overhead

**Length Prefix**: 4 bytes per entry

**Example**: 1 million entries
- Overhead: 4 MB (4 bytes × 1M entries)
- Average entry size: ~100 bytes
- Total data: ~100 MB
- Overhead percentage: ~4%

**Verdict**: Negligible overhead for significant benefits.

## Implementation Complexity

**Tony Wire Format**:
- Need parser that can find boundaries
- More complex error handling (partial reads, corrupted entries)
- Need to estimate maxSize for SectionReader

**Length-Prefixed**:
- Simple: read 4 bytes, read N bytes, parse
- Clear error handling (can detect truncated entries)
- No need for maxSize estimates

**Winner**: Length-prefixed (simpler implementation)

## Recommendation: Use Length-Prefixed Entries ✅

**Why**:
1. **Simpler random access**: Read length, read exact bytes
2. **Easier recovery**: Can skip entries without parsing
3. **Clearer boundaries**: Explicit entry boundaries
4. **Better error handling**: Can detect truncated entries
5. **Negligible overhead**: 4 bytes per entry (~4% for typical entries)

**Format**:
```
[4 bytes: uint32 length (big-endian)][entry data in Tony wire format]
```

**Implementation**:
```go
// Write
func AppendEntry(file *os.File, entry *LogEntry) (int64, error) {
    // Serialize entry
    entryBytes := LogEntryToTony(entry)
    
    // Get current offset
    offset, _ := file.Seek(0, io.SeekEnd)
    
    // Write length prefix
    lengthBytes := make([]byte, 4)
    binary.BigEndian.PutUint32(lengthBytes, uint32(len(entryBytes)))
    if _, err := file.Write(lengthBytes); err != nil {
        return 0, err
    }
    
    // Write entry
    if _, err := file.Write(entryBytes); err != nil {
        return 0, err
    }
    
    return offset, nil
}

// Read
func ReadEntryAt(file *os.File, offset int64) (*LogEntry, error) {
    // Read length
    lengthBytes := make([]byte, 4)
    if _, err := file.ReadAt(lengthBytes, offset); err != nil {
        return nil, err
    }
    length := binary.BigEndian.Uint32(lengthBytes)
    
    // Read entry
    entryBytes := make([]byte, length)
    if _, err := file.ReadAt(entryBytes, offset+4); err != nil {
        return nil, err
    }
    
    // Parse entry
    return LogEntryFromTony(bytes.NewReader(entryBytes))
}
```

## Answer: Yes, Length-Prefixed is Easier ✅

**Agreement**: Length-prefixed entries are simpler to implement and provide better random access, with negligible storage overhead.
