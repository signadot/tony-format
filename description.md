# logd: extractPathValue returns error for missing paths instead of null

In `match_data.go:113-115`, when a path segment is not found in the document, an error is returned:

```go
if !found {
    return nil, fmt.Errorf("path segment %q not found in document", part)
}
```

## Problem
This conflates two different scenarios:
1. Storage/system error (should return error)
2. Path doesn't exist in data (should return null)

For reads of non-existent paths, returning `nil` (null) is often more appropriate than an error. This matches typical document store semantics where reading a missing key returns null/undefined.

## Current behavior
```
MATCH {path: "nonexistent.path"}
-> HTTP 500 "path segment 'nonexistent' not found in document"
```

## Expected behavior
```
MATCH {path: "nonexistent.path"}
-> HTTP 200 {data: null}
```

## Fix
Return null instead of error for missing paths:
```go
if !found {
    return ir.Null(), nil  // Path not found = null value
}
```

Or distinguish between "path not found" (return null) and "wrong type at path" (return error).