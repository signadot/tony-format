# logd: typo in error message snap_storage.go

Minor typo in `snap_storage.go:63`:

```go
return nil, 0, fmt.Errorf("errro translating node to events: %w", err)
```

Should be "error" not "errro".