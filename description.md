# docd: scoped multi-mount transactions

Multi-mount (multi-participant) transactions currently only work for **baseline**
(unscoped) clients. Make them work under a COW scope.

## Current state (as of txpool wiring, commit a926d8b)

- docd serves a baseline client's NewTx from its pre-fetched pool.
- A scoped client's NewTx is forwarded to logd on the client's scoped connection
  (so the TxID itself is scope-correct).
- BUT the participating controllers write to logd with their own **baseline**
  LogdSessions, so a scoped tx can never actually complete: logd rejects a
  baseline participant joining a scoped tx (tx_scope_mismatch).

## What is needed

- Controllers must be able to write within a client's scope for the duration of
  a routed transaction — either per-scope controller logd sessions, or
  scope-passing on the participant write.
- docd must propagate the client's scope to the controllers it routes the tx to
  (the scope is already captured from the client hello in ClientSession).
- Decide the ownership model: does a mount participate in arbitrary client
  scopes, or does each scope get its own controller session/state?

## Relation

Part of the docd umbrella (wcabztj2h12ksb9qbnn0). Depends conceptually on the
docd-coordinated patch splitting work, since that defines how a single client
patch becomes a multi-participant tx.