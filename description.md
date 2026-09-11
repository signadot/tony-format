# logd: a patch response returns a keyed array in the store's form, an object of names, rather than the array the client wrote

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced on the wire at 546bd53.

A server.Session with schema {define: {items: {sku: !logd-key null}}}:

    -> {id: p1, patch: {path: "", data: {items: [{sku: A, q: 1}]}}}
    <- {id: p1, result: {patch: {commit: 3, data: {items: {(sku=A): {sku: A, q: 1}}}}}}

LowerKeyed rewrites the participant's node in place (tx/keyed_form.go:273), Commit
returns it as Result.Data (tx/coord.go:316), and server/session_write.go:185 sends it;
the "Strip internal tags" comment above that line does nothing. docd relays it
(docd/server/client_session.go:494).

Consequence: a patch response for a keyed array is an object of names, not the array
the client wrote. Reads and watch deltas raise it back; the response does not. The
stored data is fine.