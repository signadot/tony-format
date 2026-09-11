# logd: a keying conflict, a transaction whose participants never joined, and a refused schema are all answered storage_error

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

All answered `storage_error`:

- a keying conflict -- !key(qty) on an array the schema keys by sku, !key(x) on one it
  does not key, an element with no sku: "failed to commit: patch conflicts with the
  schema's keying: ..." (plain fmt.Errorf at tx/coord.go:433; preconditions :368);
- a transaction with tx.timeout 300ms and one of two participants: "transaction timeout:
  not all participants joined within 300ms" (:263), though TxConfig.Timeout promises a
  timeout error -- ErrCodeTimeout is sent only for the per-request timeout;
- a schema proposal Validate refuses: "failed to start migration: schema cannot be
  adopted..." (server/session_schema.go:84).

handlePatch falls through to storage_error at session_write.go:170.

Consequence: a client branching on the code, as api/doc.go says to, reads its own
mistake or an absent peer as a store fault, and may retry a write that can never
succeed.