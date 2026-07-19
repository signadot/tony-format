# docd: complete controller-facing library for dogfooding

Track the work to bring **docd** from its current handshake-only state to a
reasonably stable, reasonably complete library that a real controller (the
`connect`-controller from the entity-verse federation design) can dogfood.

This is the umbrella tracking issue. It captures the current state, a hard design
constraint (logd/docd switchability), the major components that must land, and a
milestone ordering toward dogfooding. Sub-issues should be split off per component
and linked back here.

Related: MOUNT protocol and docd bootstrapping (tbn7ptxch12ks6999xmg), Async
Controller Session Architecture (tqn7ptxch12kspx1bnmg), docd decomposition
(hzn7ptxch12krwj18xmg), can docd mount another docd (j7n7ptxch12krpt78xmg).

First dogfooding consumer / design context:
`signadot/hackspace/verse/docs/content-federation.md` — `connect` becomes a
controller that mounts the working tree as a subtree and serves reads for the
paths it owns. That is the concrete target this issue is scoped against.

---

## 1. Current state (commit ced8522)

What exists and works:

- **libctl.LogdSession** (`system/libctl/logd.go`, `watch.go`) — solid. Async,
  multiplexed, one shared connection with a read-pump that demuxes id-correlated
  responses from unsolicited watch events. Supports Match / Patch / Watch /
  DeleteScope / COW scope / lazy (re)connect on next request. This is the piece to
  **preserve**.
- **docd MOUNT handshake** (`system/docd/server/session.go`, `registry.go`,
  `api/mount_session.go`) — a controller connects, sends `hello`+`mount`, docd
  registers `path → MountSession` in `MountRegistry`, replies accepted. Exact-path
  registry only.
- **libctl.MountClient** (`system/libctl/mount.go`) — sync mount handshake; holds
  a `LogdSession` (via `Logd()`) for the controller to write results to logd.
- **docd txpool** (`system/docd/txpool/pool.go`) — pre-fetches TxIDs from logd by
  participant count. Standalone; **not wired into the Server**.
- **CLI** — `o docd serve -addr -logd` exists. No `o sys up|down` composition yet.

What is missing (the gap this issue closes):

- After the handshake the `MountSession` **blocks reading and discards every
  message** (`session.go:70-95`, all `TODO`). No PATCH is ever sent to a
  controller; no controller response is ever read.
- **No client-facing session at all.** `tcp.go` hardwires every accepted
  connection into a `MountSession`. A client that connects and sends a logd-style
  `hello`/`match` is misparsed as a mount request.
- **docd holds no logd connection for reads.** MATCH/WATCH cannot be served.
- **No routing.** Nothing maps a client PATCH at `/users/123` to the `/users`
  controller.
- **No controller-side async runtime.** `MountClient` is a bare handshake; there
  is no read-pump dispatching incoming ops to a handler.

Net: docd can accept a mount and then does nothing with it. Everything on the data
path is still to build.

---

## 2. Hard constraint: logd ⇄ docd switchability (additive protocol)

**Requirement:** it must be easy to switch between talking to low-level **logd**
and talking to **docd**. The logd session protocol and `libctl.LogdSession` API
stay the same wherever reasonable; docd-specific behavior is **additive** on the
protocol, not a fork of it.

Design rules that follow:

1. **docd's client face speaks the logd session protocol verbatim.** Reuse
   `system/logd/api` `SessionRequest`/`SessionResponse` (hello, match, patch,
   watch, unwatch, newtx, schema, deleteScope). A client built on
   `libctl.LogdSession` switches from logd to docd by changing **only the
   address** — reads behave identically; docd adds behavior on the write path
   (routing PATCH through the owning controller) transparently to the client.
2. **docd-specific messages are additive fields, not a parallel protocol.** The
   controller (mount) face = logd session protocol **plus** an additive `mount`
   (and later `unmount`) message, and reuse of the existing `patch` shape for the
   docd→controller direction. Prefer extending the session union over inventing a
   disjoint envelope.
3. **LogdSession stays the API of record for writes.** A controller keeps using
   `MountClient.Logd()` → `LogdSession` to commit to logd exactly as today. The
   new controller-side runtime (DocdSession/Coordinator) is additive around it,
   never a replacement.

Divergences to reconcile against this constraint (current code):

- `docd/api.MountHello{Controller}` duplicates `logd/api.Hello{ClientID, Scope}`.
  Fold the mount handshake onto logd's `Hello` + an additive `mount` field so the
  same client plumbing serves both, and scope/COW carries through to controllers.
- `MountClient` reimplements its own decoder/`readDocument` loop instead of
  sharing `LogdSession`'s read-pump/pending machinery. The async `DocdSession`
  should mirror (ideally share) `LogdSession`'s core so both sides feel identical
  and there is one correlation/reconnect implementation to keep correct.

---

## 3. Major components

### A. docd client-facing session (the "user face") — biggest gap
- Split connection role by first message: `mount` ⇒ controller (MOUNT protocol);
  `hello` ⇒ client (logd session protocol). `tcp.go` must stop assuming
  `MountSession` for every connection.
- Serve the logd protocol to clients: MATCH/WATCH/UNWATCH → passthrough to logd;
  PATCH → route to owning controller; NEWTX/SCHEMA/DeleteScope → passthrough or
  explicit reject with a clear code.
- This is what makes rule #1 real.

### B. docd → controller operation channel (post-mount)
- Replace the discard loop with an async op channel: docd sends `patch{id,...}`
  down the mount connection; controller replies `result{id}`/`error{id}`.
- Per-mount-session pending map keyed by id, mirroring `LogdSession`. Timeouts and
  cancellation. Fan-in of many concurrent client PATCHes onto one mount session.

### C. Controller-side async runtime (libctl) — Async Controller issue
- `DocdSession`: post-mount read-pump dispatching incoming `patch` ops to a
  `Handler`, writing responses; reconnect + re-mount with backoff.
- `Coordinator`: ties `DocdSession` + `LogdSession`, tracks pending ops with
  commit positions for replay.
- `RunController(ctx, cfg)` + `Handler` interface (`HandlePatch` → write via the
  **existing** `LogdSession` → return result). Keep `LogdSession` API unchanged.

### D. docd's logd client(s)
- docd needs its own logd connection(s) for MATCH/WATCH passthrough (a read
  session, likely a `LogdSession` reused server-side) plus the existing txpool.
- **Wire txpool into `Server`** (today it is orphaned). Consider watch fan-out so
  one upstream logd watch feeds many client watches.

### E. Mount routing & path model
- **Longest-prefix routing** (a PATCH at `/users/123` must resolve to the `/users`
  mount). Registry is exact-match today — insufficient for real use.
- Overlap/nesting rules (`/users` vs `/users/admins`), path normalization,
  validation. Ties into "can docd mount another docd" (j7n7ptxch12krpt78xmg).

### F. Schema composition
- Compose controller schemas into a unified schema, publish at `/.meta/schema`.
- Decide ownership vs logd's schema/migration machinery: does docd own the
  composed schema in logd, or each controller its subtree? Interaction with logd
  migrations (`SchemaSet`, pending/active, auto-id). Optional client-patch
  validation before routing.

### G. Lifecycle / process management — `o sys up|down`
- Single entrypoint composing logd + docd with correct ordering (logd listens,
  docd connects, controllers mount). `o sys docd`, `o sys up|down` per the MOUNT
  issue's first comment. Readiness, graceful shutdown ordering (drain mounts,
  fail in-flight deterministically).

### H. Reliability: reconnect, replay, failure semantics
- Controller crash mid-PATCH ⇒ docd returns a deterministic **"outcome unknown"**
  to the client (not a hang, not a false success).
- docd↔logd reconnect; client↔docd reconnect (clients already get LogdSession
  reconnect for free — a switchability dividend).
- Mount-session drop ⇒ unregister mount, fail PATCHes routed to it, surface to
  clients. Idempotency/dedup on replay (tx ids help).

### I. Observability
- Metrics (active mounts, in-flight ops, routing latency, logd reconnects),
  `o sys docd mounts` listing, and a way to trace one PATCH across
  client→docd→controller→logd. Structured logging already present.

### J. Testing & correctness harness
- End-to-end: client→docd→controller→logd round trip (today only handshake +
  registry are covered — `session_test.go`).
- Concurrency/race and failure-injection tests (drop controller mid-tx, drop
  logd), following the logd precedent (`snapshot_stress_test.go`, session tests).
- A **reference controller** (the connect-controller) as example + integration
  test — the actual dogfooding driver.

### K. Docs & examples
- Update `libctl/doc.go` lifecycle to the real controller loop.
- A "switching between logd and docd" guide making the additive-protocol promise
  concrete.
- Link the content-federation design as the first controller.

---

## 4. Milestones toward dogfooding

- **M1 — walking skeleton (unblocks the connect-controller MVP):** client face
  speaking the logd protocol (A); PATCH routing to a single exact-path controller
  (B); controller async runtime with `RunController`/`Handler` (C, minimal);
  MATCH/WATCH passthrough + txpool wired in (D); one end-to-end test (J). Proves
  rule #1: a `LogdSession` client works against docd for reads unchanged.
- **M2 — robustness:** reconnect/replay + crash "outcome unknown" semantics (H);
  longest-prefix routing (E); `o sys up|down` (G); failure-injection tests (J).
- **M3 — completeness:** schema composition (F); observability + `mounts` listing
  (I); multi-controller transactions; nested docd (E/j7); switchability guide (K).

## 5. Definition of done (dogfooding-ready)

- A `LogdSession` client, pointed at docd instead of logd, reads unchanged.
- The connect-controller mounts a subtree, serves client PATCHes routed by path,
  writes to logd, and survives a controller restart with deterministic client
  outcomes.
- `o sys up` brings up logd+docd; `o sys docd mounts` shows the live mount.
- End-to-end and failure-injection tests are green in CI.