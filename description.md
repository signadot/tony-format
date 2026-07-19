# docd: complete controller-facing library for dogfooding

Track the work to bring **docd** from its current handshake-only state to a
reasonably stable, reasonably complete library that a real controller (the
`connect`-controller from the entity-verse federation design) can dogfood.

This is the umbrella tracking issue. The body below is the **consolidated,
settled design**; the discussion comments are the provenance trail for how each
decision was reached. Sub-issues should be split off per component and linked here.

Related: MOUNT protocol and docd bootstrapping (tbn7ptxch12ks6999xmg), Async
Controller Session Architecture (tqn7ptxch12kspx1bnmg), docd decomposition
(hzn7ptxch12krwj18xmg), can docd mount another docd (j7n7ptxch12krpt78xmg),
storage watch support incl. EncodingOptions (hvn7ptxch12krra18xmg, closed).

First dogfooding consumer / design context:
`signadot/hackspace/verse/docs/content-federation.md` — `connect` becomes a
controller that mounts the working tree as a subtree and serves reads for the
paths it owns, on demand, without warehousing file bytes in logd.

---

## 1. Current state (commit ced8522)

Works:

- **libctl.LogdSession** (`system/libctl/logd.go`, `watch.go`) — solid. Async,
  multiplexed, one shared connection with a read-pump demuxing id-correlated
  responses from unsolicited watch events. Match / Patch / Watch / DeleteScope /
  COW scope / lazy (re)connect. **This is the piece to preserve.**
- **docd MOUNT handshake** (`system/docd/server/session.go`, `registry.go`,
  `api/mount_session.go`) — controller sends `hello`+`mount`, docd registers
  `path → MountSession` in `MountRegistry`, replies accepted. Exact-path only.
- **libctl.MountClient** (`system/libctl/mount.go`) — sync mount handshake; holds a
  `LogdSession` (via `Logd()`) for the controller to write to logd.
- **docd txpool** (`system/docd/txpool/pool.go`) — pre-fetches TxIDs from logd by
  participant count. Standalone; **not wired into the Server**.
- **CLI** — `o docd serve -addr -logd`. No `o sys up|down` composition yet.

Missing (the gap this issue closes):

- After the handshake `MountSession` **blocks reading and discards every message**
  (`session.go:70-95`, all `TODO`). No op is ever sent to a controller.
- **No client-facing session.** `tcp.go` hardwires every accepted connection into a
  `MountSession`; a logd-style client `hello` is misparsed.
- **docd holds no logd data client** for base-path reads/writes/watches.
- **No routing** from a client op to the owning controller or to logd.
- **No controller-side async runtime** — `MountClient` is a bare handshake with no
  read-pump dispatching incoming ops.

---

## 2. Settled architecture

### 2.1 Layered model: logd-backed document server with controller overlays
docd is **not** a pure router. It is a document server backed by logd, with
controller mounts overlaying subtrees — filesystem-like (Plan 9 framing in
`docs/tonyapi/design.md`): a real root store, special sources mounted at paths.

- **Base / unmounted paths** are served **directly against logd** (match, patch,
  watch) — fewer hops, and a working store with zero controllers.
- **Mounted subtrees** route to the owning controller. The controller is the owner
  and answers for its subtree; its content need not live in logd (the
  connect-controller serves file bytes on demand and never warehouses them).
- **Longest-prefix** decides: path under a registered mount → controller; else → logd.

Reads under a mount go through the controller *by design*: resolving mounted content
from logd would force the controller to warehouse every file's bytes first, defeating
"index, not warehouse / serve on demand."

### 2.2 Two TCP listeners
docd serves the MOUNT (controller) protocol on a **separate TCP address** from the
client face. No first-message role-sniffing.

- **client listener** (e.g. `:9124`) — logd session protocol, verbatim.
- **mount listener** (e.g. `:9125`) — MOUNT protocol (logd-shaped + additive `mount`).

`o docd serve` grows a second `-mount-addr`; `o sys up` composes both.

### 2.3 Full bidirectional MOUNT (MATCH + PATCH + WATCH)
A mount owns its subtree for **all three** data-plane ops. docd routes MATCH, PATCH,
and WATCH under a mount to the controller (not just PATCH).

- The library **fully supports every operation** — none is second-class or deferred.
  libctl's controller runtime exposes `HandleMatch` / `HandlePatch` / `HandleWatch`.
- A controller may **decline** any op it doesn't implement via a standard
  `unsupported` error, which docd relays to the client. This is a per-controller
  choice, not a library gap. The `local:*` connect-controller declines WATCH this way.
- ⇒ the MOUNT protocol includes a first-class **`unsupported` error code**.

### 2.4 Switchability / additive protocol (hard constraint)
Switching between low-level **logd** and **docd** must be easy; the logd session
protocol and `LogdSession` API stay the same where reasonable, and docd-specific
behavior is **additive**.

1. **docd's client face speaks the logd session protocol verbatim** (`logd/api`
   `SessionRequest`/`SessionResponse`). A `LogdSession` client switches logd→docd by
   changing **only the address**. docd is an authority-routing layer, not a
   transparent logd: a *mounted-path* read is controller-served, so same client
   code/protocol yields richer data — that is the point of docd.
2. **docd-specific messages are additive**, not a parallel protocol: the MOUNT face =
   logd session protocol **plus** an additive `mount` (later `unmount`) message, and
   reuse of the `patch`/`match`/`watch` shapes for the docd→controller direction.
3. **LogdSession stays the API of record for writes.** Controllers keep using
   `MountClient.Logd()` → `LogdSession`. The controller-side async runtime is additive
   around it, never a replacement.

**No expressivity ceiling.** Full match/patch semantics already live in the logd
protocol: `MatchRequest.Body.Data` is a match pattern (`server/match_data.go`
`filterState` runs `tony.Match` + `tony.Trim` → field selection and filtering), and
`tx.Patch` carries `Match *PathData` as a precondition. The `!apiop {path, match,
patch}` triple maps directly onto this. docd's client face inherits it for free.
(libctl should add a `Match` variant taking a `Data` pattern — additive; the current
one-arg convenience method hides it.)

Reconcile with existing code: `docd/api.MountHello{Controller}` duplicates
`logd/api.Hello{ClientID, Scope}` — fold the mount handshake onto logd's `Hello` + an
additive `mount` field so scope/COW and encoding carry through and there is one
client plumbing. `MountClient` reimplements its own decoder loop — the async
`DocdSession` should mirror/share `LogdSession`'s read-pump/pending core.

### 2.5 Format / encoding negotiation is 1-hop (edge only)
Format is an edge/presentation concern. The client-facing hop negotiates encoding
(wire / json / yaml); everything behind it is wire.

- client ↔ docd: negotiated. docd ↔ controller / docd ↔ logd / controller ↔ logd: wire.
- docd transcodes at its own client edge; logd honors `EncodingOptions` on its own
  client edge (defined per hvn7ptxch12krra18xmg; **currently hard-coded to wire** via
  `gomap.EncodeWire(true)` everywhere — a known gap). No component propagates a
  client's requested format inward.

### 2.6 Governing notes vs. older tonyapi docs
The `docs/tonyapi/*` corpus predates the logd session-protocol implementation and the
MOUNT decision. Where they conflict, the newer decisions govern:

- **Transport:** TCP-only for all controllers (MOUNT issue) **supersedes** the older
  local spawned stdin/stdout controller model in `controllers.md`/`design.md`.
- **Framing:** the logd session protocol (newline-delimited Tony over TCP)
  **supersedes** the HTTP/`!apiop` framing — but `!apiop`'s match/patch **semantics**
  map onto logd's match/patch (see 2.4), so no expressivity is lost.
- **Blessed-but-deferred:** docd virtual-document cache; docd-orchestrated
  cross-controller MATCH composition; multi-mount transactions (participant-count
  machinery already exists in txpool + logd `NewTx`/`TxID`).

---

## 3. Components

### A. docd client-facing session (two listeners)
Client listener speaks the logd session protocol. Base/unmounted MATCH/PATCH/WATCH →
logd directly; mounted paths → owning controller. Mount listener speaks MOUNT.
Replaces the first-message role-sniffing idea and the "everything is a MountSession"
wiring in `tcp.go`.

### B. docd → controller operation channel (post-mount)
Replace the discard loop with an async op channel: docd sends `match`/`patch`/`watch`
(with id) down the mount connection; controller replies `result`/`error` (echoing id),
or `error{code: unsupported}`. Per-mount pending map keyed by id, mirroring
`LogdSession`. Timeouts, cancellation, fan-in of concurrent client ops.

### C. Controller-side async runtime (libctl) — Async Controller issue
`DocdSession`: post-mount read-pump dispatching incoming ops to a `Handler`
(`HandleMatch`/`HandlePatch`/`HandleWatch`), writing responses; reconnect + re-mount
with backoff. `Coordinator` ties `DocdSession` + `LogdSession`, tracks pending ops with
commit positions for replay. `RunController(ctx, cfg)` loop. Keep `LogdSession`
unchanged; a controller writes via the existing `LogdSession`.

### D. docd's logd data client (base paths) + txpool
docd holds a logd client (reuse `LogdSession` server-side) to serve base-path
MATCH/PATCH/WATCH directly, including watch handling/fan-out for base paths. **Wire the
existing txpool into `Server`** (today orphaned) for participant-count tx coordination.

### E. Mount routing & path model
Longest-prefix routing (a PATCH at `/users/123` resolves to the `/users` mount).
Overlap/nesting rules (`/users` vs `/users/admins`), normalization, validation. Ties to
nested docd (j7n7ptxch12krpt78xmg).

### F. Schema composition
Compose controller schemas into a unified schema, publish at `/.meta/schema`. Decide
ownership vs. logd's schema/migration machinery (`SchemaSet`, pending/active, auto-id).
Optional client-patch validation before routing.

### G. Lifecycle / process management — `o sys up|down`
Single entrypoint composing logd + docd with correct ordering (logd listens, docd
connects, controllers mount). `o sys docd`, `o sys up|down`. Readiness, graceful
shutdown (drain mounts, fail in-flight deterministically).

### H. Reliability: reconnect, replay, failure semantics
Controller crash mid-op ⇒ deterministic **"outcome unknown"** to the client (not a hang,
not a false success). docd↔logd reconnect; client↔docd reconnect (clients inherit
`LogdSession` reconnect — a switchability dividend). Mount-session drop ⇒ unregister
mount, fail routed ops, surface to clients. Idempotency/dedup on replay (tx ids help).

### I. Observability
Metrics (active mounts, in-flight ops, routing latency, logd reconnects), `o sys docd
mounts` listing, tracing one op across client→docd→controller→logd. Structured logging
already present.

### J. Testing & correctness harness
End-to-end: client→docd→(controller|logd) round trips (today only handshake + registry
are covered). Concurrency/race and failure-injection tests (drop controller mid-tx, drop
logd), following the logd precedent (`snapshot_stress_test.go`). A **reference
controller** (the connect-controller) as example + integration test.

### K. Docs & examples
Update `libctl/doc.go` to the real controller loop. A "switching between logd and docd"
guide (the additive-protocol promise made concrete). A **passthrough controller** worked
example for controller authors (demoted from required plumbing). Link the
content-federation design as the first controller. Add "superseded by" pointers on the
stale `docs/tonyapi/*` transport/framing sections.

### L. Format / encoding negotiation (1-hop)
Client requests wire/json/yaml via the hello handshake (`EncodingOptions` already
defined). logd honors it (currently hard-coded to wire); docd transcodes at its client
edge and uses wire for all internal hops. Additive on the protocol; do not hard-code
`EncodeWire`.

---

## 4. Milestones toward dogfooding

- **M1a — transparent logd proxy (switchability proof):** docd client face forwards
  MATCH/PATCH/WATCH to a `LogdSession`. Point a `LogdSession` client at docd →
  reads/writes unchanged. Near-zero code, proves the additive-protocol promise.
- **M1b — mount routing (unblocks the connect-controller MVP):** mount registry +
  route mounted subtrees to controllers (B); controller async runtime with
  `RunController`/`Handler` incl. `HandleMatch`/`HandleWatch` (C); `unsupported` error;
  base paths still hit logd directly (D); txpool wired; one end-to-end test (J).
- **M2 — robustness:** reconnect/replay + "outcome unknown" (H); longest-prefix routing
  (E); `o sys up|down` (G); 1-hop format negotiation (L); failure-injection tests (J).
- **M3 — completeness:** schema composition (F); observability + `mounts` listing (I);
  cross-controller MATCH composition + multi-controller transactions; docd virtual-doc
  cache; nested docd (E / j7).

---

## 5. Open items (deferred, recommendation recorded)

- **Read caching** — docd could cache controller reads keyed on change-stream commit,
  but that reintroduces a second copy the federation design warns against. None in M1;
  if added, invalidate on the change stream, never TTL.
- **Read/commit coherence** — a controller-served read should echo the `commit` it
  corresponds to, so a client can pair (metadata@commit, content). Carry `commit` on
  routed reads.

## 6. Definition of done (dogfooding-ready)

- A `LogdSession` client, pointed at docd instead of logd, works unchanged for base
  paths (reads and writes).
- The connect-controller mounts a subtree, serves client MATCH/PATCH routed by path,
  serves file content on demand without warehousing it in logd, declines WATCH via
  `unsupported`, and survives a restart with deterministic client outcomes.
- `o sys up` brings up logd+docd; `o sys docd mounts` shows the live mount.
- A client may request json/yaml on the client edge; internal hops stay wire.
- End-to-end and failure-injection tests are green in CI.
