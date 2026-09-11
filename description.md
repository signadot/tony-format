# docd: a mount path with a leading "/" is accepted as a field named "/users", though the handshake says it refuses one

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

libctl.Mount with Path "/users" is accepted and registered as "/users". Client paths
`users` and `users.a` route to base (logd); `/users` and `/users.a` route to the mount.

The handshake check (docd/server/session.go:554, likewise clock.go:38) uses pathFields,
which parses "/users" as one field, though its error message says "(no leading /)".

Consequence: a controller given a slash path -- libctl's own examples used one until
this sweep -- silently owns a top-level key literally named "/users" and never sees
`users` traffic.