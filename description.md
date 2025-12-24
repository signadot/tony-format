# logd: session handleMatch doesn't support SeqID for historical reads

The session protocol's `handleMatch` always reads at the current commit:

```go
// session.go:245-246
commit, err := s.storage.GetCurrentCommit()
```

However, the HTTP `handleMatchData` supports reading at a specific historical commit via `req.Meta.SeqID`:

```go
// match_data.go:24-27
if req.Meta.SeqID != nil {
    commit = *req.Meta.SeqID
} else {
    commit, err = s.Spec.Storage.GetCurrentCommit()
}
```

## Impact
- API inconsistency between HTTP and session protocol
- Session clients cannot read historical state
- Could cause issues for clients that need point-in-time consistency

## Fix
Add SeqID support to session's `handleMatch`:
```go
var commit int64
if req.SeqID != nil {
    commit = *req.SeqID
} else {
    var err error
    commit, err = s.storage.GetCurrentCommit()
    // ...
}
```

Note: May need to update `api.MatchRequest` to include SeqID field if not already present.