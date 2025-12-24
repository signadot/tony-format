# logd: no validation of snapshot index size in snap.Open

In `snap/snap.go:46`, the snapshot index size is read from the file and used directly without validation:

```go
indexSize := binary.BigEndian.Uint32(header[8:12])
// ...
index, err := OpenIndex(rc, int(indexSize))
```

## Problem
If the file is corrupted or maliciously crafted, `indexSize` could be:
- Very large, causing excessive memory allocation
- Larger than the actual file, causing read errors

## Fix
Add sanity checks:
```go
indexSize := binary.BigEndian.Uint32(header[8:12])

// Sanity check: index can't be larger than 100MB (arbitrary reasonable limit)
const maxIndexSize = 100 * 1024 * 1024
if indexSize > maxIndexSize {
    return nil, fmt.Errorf("snapshot index size %d exceeds maximum %d", indexSize, maxIndexSize)
}

// Index can't be larger than remaining file size
// (would need file size check here)
```