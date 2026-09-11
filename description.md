# logd: a schema from the config file is adopted without Schema.Validate, so an array with two identities is served

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

A config schema `define: {items: {sku: !logd-key string, id: !logd-auto-id string}}` is
served in the hello, and a write stores {(sku=A): {id: a1a0, sku: A, qty: 1}} -- keyed by
sku, with a generated id. The same schema sent as `schema: {set: ...}` is refused:
"declared keyed by "sku" and auto-id on "id"; one array has one identity".

server.New (server/server.go:77-79) parses the config schema and installs it without
Schema.Validate, which runs only in StartMigration (storage/schema.go:141).

Consequence: the store runs a schema its own rules call ambiguous. The other things
Validate refuses -- key field names holding , ( = < > -- would pass the config path too
(suspected, not probed).