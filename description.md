# logd: panic on HTTP write failure in match_data.go

The HTTP handler in `match_data.go:79` panics when writing the response fails:

```go
if _, err := w.Write(d); err != nil {
    panic(fmt.Sprintf("failed to write response: %v", err))
}
```

This can crash the server when a client disconnects mid-response, which is a normal occurrence in production.

## Fix
Replace panic with log and return:
```go
if _, err := w.Write(d); err != nil {
    // Client likely disconnected, just log and return
    s.Spec.Log.Debug("failed to write match response", "error", err)
    return
}
```

## Impact
- Server crash on client disconnect
- Potential denial of service vector