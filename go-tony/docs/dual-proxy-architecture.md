# Dual-Proxy Architecture for Agent Testing

## TL;DR

- **Two proxies**: LLM proxy (captures reasoning) + MCP proxy (captures tool calls)
- **Zero agent code changes**: Config only (env vars, MCP wrapper)
- **Correlation**: In-cluster via routing key injection; out-of-cluster via headers (like Helicone/Langfuse)
- **Enables**: Replay testing, what-if scenarios, regression detection, LLM-as-judge evaluation
- **Fidelity**: Given functional tools, mock-then-live produces valid alternative trajectories
- **Storage**: Pluggable; tonyapi/logd is a good fit but not required
- **Multi-tenant service**: Assumed from the start

## Overview

A dual-proxy architecture captures the complete picture of AI agent behavior by intercepting both sides of agent communication:

1. **LLM Proxy** - Captures agent ↔ LLM traffic (reasoning, decisions)
2. **MCP Proxy** - Captures agent ↔ tool traffic (actions, effects)

Together, these provide the foundation for agent testing: replay for regression, branching for what-if scenarios, and evaluation via LLM-as-judge.

## Core Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                      Customer Environment                         │
│                                                                   │
│  ┌──────────────────┐                                            │
│  │   Agent Process  │                                            │
│  │   (unchanged)    │                                            │
│  └────────┬─────────┘                                            │
│           │                                                       │
│           │  ANTHROPIC_BASE_URL=llm-proxy:8080                   │
│           │  MCP servers wrapped by mcp-proxy                    │
│           │                                                       │
│  ┌────────┴────────────────────────────────────────────────┐     │
│  │                                                          │     │
│  │   ┌─────────────┐              ┌─────────────┐          │     │
│  │   │  LLM Proxy  │              │  MCP Proxy  │          │     │
│  │   │  :8080      │              │  stdio/sse  │          │     │
│  │   └──────┬──────┘              └──────┬──────┘          │     │
│  │          │                            │                  │     │
│  │          │  ┌─────────────────────────┘                  │     │
│  │          │  │                                            │     │
│  │          ▼  ▼                                            │     │
│  │   ┌─────────────────┐                                   │     │
│  │   │   Correlation   │  tool_use_id mapping              │     │
│  │   │     Layer       │  conversation assembly            │     │
│  │   └────────┬────────┘                                   │     │
│  │            │                                             │     │
│  └────────────┼─────────────────────────────────────────────┘     │
│               │                                                   │
└───────────────┼───────────────────────────────────────────────────┘
                │
                │  Structured events
                │  (conversations, turns, tool calls)
                ▼
┌──────────────────────────────────────────────────────────────────┐
│                    External Storage System                        │
│                    (tonyapi, database, etc.)                      │
│                                                                   │
│   Capabilities:                                                   │
│   • Store conversation history                                    │
│   • Query by time, agent, conversation                           │
│   • Support branching/scopes for testing                         │
│   • Serve replay data for regression tests                       │
│                                                                   │
└──────────────────────────────────────────────────────────────────┘
```

## Data Flow

### Live Capture

```
Agent                LLM Proxy              Anthropic API
  │                     │                        │
  │── messages/create ─►│                        │
  │                     │─── forward request ───►│
  │                     │◄── response ───────────│
  │                     │                        │
  │                     │──► emit: {             │
  │                     │      conv_id: "c-123"  │
  │                     │      turn: 5           │
  │                     │      request: {...}    │
  │                     │      response: {...}   │
  │                     │      tool_use_ids: [   │
  │                     │        "toolu_01ABC"   │
  │                     │      ]                 │
  │                     │    }                   │
  │◄── response ────────│                        │


Agent                MCP Proxy              Real MCP Server
  │                     │                        │
  │── tools/call ──────►│                        │
  │   tool_use_id:      │─── forward ───────────►│
  │   "toolu_01ABC"     │◄── result ─────────────│
  │                     │                        │
  │                     │──► emit: {             │
  │                     │      tool_use_id:      │
  │                     │        "toolu_01ABC"   │
  │                     │      tool: "read_file" │
  │                     │      input: {...}      │
  │                     │      output: {...}     │
  │                     │      latency_ms: 23    │
  │                     │    }                   │
  │◄── result ──────────│                        │
```

### Correlation

Correlation operates at two levels:

1. **Turn-level**: Linking a tool_use in an LLM response to the corresponding MCP tool call
2. **Conversation-level**: Grouping all LLM and MCP traffic for a single agent session

#### Turn-level Correlation (tool_use_id)

The `tool_use_id` links LLM decisions to tool executions:

```
LLM Response contains:                MCP call contains:
┌─────────────────────────┐          ┌─────────────────────────┐
│ content:                │          │ method: tools/call      │
│ - type: tool_use        │          │ params:                 │
│   id: "toolu_01ABC" ◄───┼──────────┼─► (implicit via agent)  │
│   name: "read_file"     │          │     name: "read_file"   │
│   input: {path: "..."}  │          │     arguments: {...}    │
└─────────────────────────┘          └─────────────────────────┘
```

**Problem**: The MCP protocol doesn't include tool_use_id - it's an LLM API concept. The agent knows the mapping internally, but proxies can't see it directly.

#### Conversation-level Correlation

To assemble a complete conversation view, both proxies need a shared conversation/session ID:

```
┌─────────────┐
│    Agent    │
└──────┬──────┘
       │
       ├────► LLM Proxy ────► needs conversation_id
       │
       └────► MCP Proxy ────► needs conversation_id
```

#### Industry Approach: Header Injection

[Helicone](https://docs.helicone.ai/features/sessions) and [Langfuse](https://langfuse.com/docs/tracing/sessions) both require the agent/application to inject session IDs via headers:

**Helicone:**
```
Helicone-Session-Id: unique-session-id
Helicone-Session-Path: /agent/step1
```

**Langfuse:**
```python
langfuse.trace(session_id="session-123")
# or via metadata in proxy mode
```

Neither has a proxy-only solution - correlation requires agent cooperation.

#### Correlation Options

| Option | In-cluster | Out-of-cluster | Agent changes |
|--------|------------|----------------|---------------|
| Routing key injection | ✓ | ✗ | None |
| Header injection (Helicone-style) | ✓ | ✓ | Config only |
| SDK wrapper | ✓ | ✓ | Wrapper install |
| Content + timing heuristic | ✓ | ✓ | None (lossy) |

**Option 1: Routing Key Injection (in-cluster agents)**

For agents running in-cluster, reuse Signadot's existing routing key injection mechanism:

```
┌────────────────────────────────────────────────────────────┐
│                        In-Cluster                           │
│                                                             │
│  ┌──────────┐     ┌─────────────────┐     ┌─────────────┐  │
│  │  Agent   │────►│ Signadot Proxy  │────►│  LLM Proxy  │  │
│  │  (Pod)   │     │ (injects key)   │     │             │  │
│  └──────────┘     └─────────────────┘     └─────────────┘  │
│       │                    │                                │
│       │           X-Correlation-Id: rk-abc123               │
│       │                    │                                │
│       │           ┌─────────────────┐     ┌─────────────┐  │
│       └──────────►│ Signadot Proxy  │────►│  MCP Proxy  │  │
│                   │ (injects key)   │     │             │  │
│                   └─────────────────┘     └─────────────┘  │
│                                                             │
└────────────────────────────────────────────────────────────┘

Both proxies receive same correlation ID via existing infra.
```

Advantages:
- Zero agent changes
- Leverages existing Signadot capability
- Works for any in-cluster agent

**Option 2: Header Injection (Helicone/Langfuse style)**

For out-of-cluster or customer-managed agents:

```
Agent config:
  ANTHROPIC_BASE_URL: https://llm-proxy.service.io
  headers:
    X-Conversation-Id: ${CONVERSATION_ID}

MCP config:
  proxy wraps servers, reads same header from init
```

Requires agent to support header configuration (most SDKs do).

**Option 3: SDK/Wrapper**

Provide a thin wrapper that injects correlation:

```bash
# Wrapper generates conversation_id, injects into env/headers
agent-wrapper -- my-agent-command

# Or SDK helper
from our_sdk import with_correlation
client = with_correlation(anthropic.Anthropic())
```

**Option 4: Heuristic Matching (fallback)**

When no explicit correlation available, match by:
- Tool name + arguments
- Timing window
- Request ordering

Lossy - can fail with parallel tool calls or identical calls.

#### Assembled View

With correlation in place:

```
┌──────────────────────────────────────────────────────────┐
│ conversation: conv-123                                    │
│ turn: 5                                                   │
│   assistant: [tool_use: read_file]                       │
│     └─► read_file("/src/auth.go")                        │
│         result: [850 lines]                               │
│         latency: 23ms                                     │
└──────────────────────────────────────────────────────────┘
```

## Testing Capabilities

### 1. Replay Testing

Re-run a conversation with mocked responses from stored data for deterministic regression testing.

```
┌─────────────┐     ┌─────────────┐     ┌─────────────────┐
│    Agent    │────►│  LLM Proxy  │────►│ Storage System  │
│  (replay)   │     │ (mock mode) │     │                 │
└─────────────┘     └──────┬──────┘     │  conv c-123     │
                          │◄────────────│  turn 1: {...}  │
      Returns stored      │             │  turn 2: {...}  │
      response, no        │             │  turn 3: {...}  │
      API call made       │             └─────────────────┘

┌─────────────┐     ┌─────────────┐     ┌─────────────────┐
│    Agent    │────►│  MCP Proxy  │────►│ Storage System  │
│  (replay)   │     │ (mock mode) │     │                 │
└─────────────┘     └──────┬──────┘     │  tool calls     │
                          │◄────────────│  toolu_01: {..} │
      Returns stored      │             │  toolu_02: {..} │
      tool result         │             └─────────────────┘
```

**Use cases:**
- Regression testing: verify agent still handles known scenarios correctly
- Bug reproduction: replay exact conditions without burning API tokens
- CI/CD gates: deterministic test suites from recorded conversations
- Compliance: prove exact sequence of events occurred

### 2. What-If / Conditional Testing

Test alternative scenarios by modifying stored data or injecting responses.

```
Original conversation:              What-if branch:
┌─────────────────────┐            ┌─────────────────────┐
│ turn 1: user input  │            │ turn 1: user input  │
│ turn 2: assistant   │            │ turn 2: assistant   │
│ turn 3: tool call   │            │ turn 3: tool call   │
│ turn 4: assistant   │◄───fork────│ turn 4: [MODIFIED]  │
│ turn 5: ...         │            │   different prompt  │
└─────────────────────┘            │ turn 5: ?           │
                                   └─────────────────────┘
                                          │
                                          ▼
                                   Continue with live
                                   LLM from turn 4
```

**Modes:**
- **Full mock**: All responses from storage (deterministic replay)
- **Mock tools only**: Live LLM, but tools return stored results
- **Shadow**: Live execution, compare to stored (regression testing)
- **Branch**: Fork at turn N, modify context, continue live

#### Fidelity Analysis

How faithfully does mock-then-live reflect "what would have happened"?

**Assumption**: MCP tools are functional (pure) - same input produces same output.

```
Scenario: Mock turns 1-K, go live from turn K+1

Original:    [1] ──► [2] ──► [3] ──► [4] ──► [5]
                                      │
What-if:     [1] ──► [2] ──► [3] ──► [4'] ──► [5']
             ─────── mock ────────   ──── live ────
```

**Tool calls (mocked)**: Perfect fidelity. If tools are functional, mocked results are identical to what live calls would return. The agent receives exactly what it would have received.

**LLM calls**: LLMs are stochastic - there is no single "what would have happened." Each call samples from a probability distribution conditioned on the context.

```
P(response | context) where context = [turn₁, turn₂, ..., turnₖ, input]
```

If mock phase preserves context exactly, turn K+1 samples from the **correct distribution** - the same distribution a live run would have sampled from.

| Phase | Component | Fidelity |
|-------|-----------|----------|
| Mock (1..K) | Tools | Perfect (functional assumption) |
| Mock (1..K) | LLM responses | Exact replay of recorded |
| Live (K+1) | LLM | Valid sample from correct distribution |
| Live (K+2...) | LLM | Valid samples, divergent timeline |

**After turn K+1**: Each live turn is a valid sample given its context, but we're now in a new timeline. Subsequent turns may diverge from the original - this is expected and is precisely what what-if testing explores.

**Where fidelity breaks:**

| Issue | Cause | Mitigation |
|-------|-------|------------|
| Stale tool results | World state changed since recording | Re-record, or accept staleness |
| Non-functional tools | Side effects didn't occur in mock | Mock-tools-only mode avoids this |
| Model drift | Provider updated model | Record model version, pin if possible |
| Time-dependent tools | Results depend on wall clock | Mock time-sensitive tools |

**Bottom line**: Given functional tools, mock-then-live produces valid trajectories through the space of possible agent behaviors. The mock phase provides deterministic context; the live phase explores alternative futures from that context.

### 3. Auditability

Complete, queryable history of agent behavior.

```
┌────────────────────────────────────────────────────────────────┐
│                        Storage System                           │
│                                                                 │
│  Query: "What did agent X do on Jan 15 between 10:00-11:00?"   │
│                                                                 │
│  ┌───────────────────────────────────────────────────────┐     │
│  │ conversations:                                         │     │
│  │   c-123:                                               │     │
│  │     agent: compliance-checker                          │     │
│  │     started: 2025-01-15T10:05:23Z                     │     │
│  │     turns:                                             │     │
│  │       1: {user: "Review transaction...", ts: 10:05:23}│     │
│  │       2: {assistant: "I'll check...", ts: 10:05:25}   │     │
│  │       3: {tool: query_db, input: {...}, ts: 10:05:26} │     │
│  │       ...                                              │     │
│  │     tokens: {input: 1250, output: 890}                │     │
│  │     cost: $0.0234                                      │     │
│  └───────────────────────────────────────────────────────┘     │
│                                                                 │
│  Features:                                                      │
│  • Point-in-time queries (state at any moment)                 │
│  • Diff between states (what changed)                          │
│  • Immutable history (append-only for compliance)              │
│  • Cost/token tracking                                         │
│                                                                 │
└────────────────────────────────────────────────────────────────┘
```

## Proxy Implementation Sketch

### LLM Proxy

```
┌──────────────────────────────────────────────────────────┐
│                       LLM Proxy                           │
│                                                           │
│  HTTP Server :8080                                        │
│    │                                                      │
│    ├─► POST /v1/messages                                 │
│    │     1. Extract/generate conversation ID             │
│    │     2. Capture request body                         │
│    │     3. Forward to upstream (api.anthropic.com)      │
│    │     4. Capture response                             │
│    │     5. Extract tool_use IDs → register correlation  │
│    │     6. Emit structured event to storage             │
│    │     7. Return response to agent                     │
│    │                                                      │
│    └─► Mode switch:                                      │
│          LIVE: forward to upstream                        │
│          REPLAY: return from storage (for testing)       │
│          SHADOW: forward + compare (regression)          │
│                                                           │
│  Config:                                                  │
│    upstream: https://api.anthropic.com                   │
│    storage_endpoint: http://storage:9000                 │
│    mode: live | replay | shadow                          │
│                                                           │
└──────────────────────────────────────────────────────────┘
```

### MCP Proxy

```
┌──────────────────────────────────────────────────────────┐
│                       MCP Proxy                           │
│                                                           │
│  Wraps real MCP server (stdio or SSE)                    │
│    │                                                      │
│    ├─► Intercept JSON-RPC messages                       │
│    │     tools/call:                                      │
│    │       1. Extract tool_use_id from params            │
│    │       2. Look up conversation via correlation       │
│    │       3. Forward to real server (or mock)           │
│    │       4. Capture result                             │
│    │       5. Emit structured event to storage           │
│    │       6. Return result to agent                     │
│    │                                                      │
│    └─► Mode switch:                                      │
│          LIVE: forward to real MCP server                │
│          REPLAY: return from storage (for testing)       │
│          SHADOW: forward + compare (regression)          │
│                                                           │
│  Deployment:                                              │
│    # Wrap existing MCP server                            │
│    mcp-proxy npx @mcp/server-filesystem /path            │
│                                                           │
│    # Or in MCP config                                    │
│    {"command": "mcp-proxy", "args": ["npx", "..."]}      │
│                                                           │
└──────────────────────────────────────────────────────────┘
```

## Storage Interface

The proxies emit events; the storage system is pluggable.

```
Interface:
┌──────────────────────────────────────────────────────────┐
│  // Write path                                            │
│  POST /conversations/{id}/turns                          │
│    body: {turn data}                                      │
│                                                           │
│  POST /conversations/{id}/tools/{tool_use_id}            │
│    body: {tool execution data}                            │
│                                                           │
│  // Read path (for replay/testing)                       │
│  GET /conversations/{id}                                 │
│  GET /conversations/{id}/turns/{n}                       │
│  GET /conversations?agent=X&from=T1&to=T2                │
│                                                           │
│  // Branching (for what-if testing)                      │
│  POST /conversations/{id}/branch?at_turn=N               │
│    returns: new conversation ID with copied history      │
│                                                           │
└──────────────────────────────────────────────────────────┘

Implementations:
  • tonyapi/logd  - structured diffs, scopes, time-travel
  • PostgreSQL    - simple relational storage
  • S3 + DynamoDB - cloud-native
  • SQLite        - local development
```

## Integration Points

### Agent Integration (Zero Code Change)

```bash
# Point LLM SDK at proxy
export ANTHROPIC_BASE_URL=http://localhost:8080

# Wrap MCP servers
mcp-proxy npx @modelcontextprotocol/server-filesystem /path
```

### Storage Integration

```
Proxies ──emit──► Storage System
                       │
                       ├── tonyapi: PATCH /conversations/...
                       ├── REST API: POST /api/conversations/...
                       └── Queue: publish to kafka/nats/...
```

## Summary

| Component | Responsibility | Deployment |
|-----------|---------------|------------|
| LLM Proxy | Capture LLM traffic, mode switching | Sidecar or standalone |
| MCP Proxy | Capture tool traffic, correlation lookup | Wraps each MCP server |
| Correlation | Map tool_use_id → conversation | Shared state (in-memory or external) |
| Storage | History, queries, branching | Pluggable backend |

**Key design principles:**
1. **Non-invasive**: Agent code unchanged, just environment config
2. **Pluggable storage**: Proxies define interface, storage is swappable
3. **Correlation-first**: tool_use_id is the join key
4. **Mode-aware**: Same proxies support live capture, replay testing, and shadow comparison
5. **Testing-first**: Infrastructure designed to enable replay, what-if, and evaluation workflows

## Open Questions

### Assumptions

- **Multi-tenant service**: Proxies serve multiple tenants, each with multiple agents
- **Correlation**: Mechanism for linking LLM traffic to tool traffic (to be detailed separately)

### Architecture

**MCP proxy deployment**
- One proxy per MCP server, or single multiplexing proxy?
- stdio wrapping vs HTTP/SSE interception?
- How to handle MCP servers that agent spawns dynamically?

**Tenant isolation**
- Shared proxy instances with logical isolation, or dedicated per tenant?
- How are tenant credentials/API keys managed and forwarded?
- Storage partitioning by tenant

### Modes & Control

**Mode switching**
- Per-request (header)? Per-conversation? Global config?
- How does proxy know which stored conversation to replay from?
- Mixed mode: replay some tools, live others?

**Streaming responses**
- LLM APIs stream via SSE - capture incrementally or buffer?
- How to mock streaming in replay mode?
- Latency simulation for realistic replay?

### Storage Interface

**Minimal viable interface**
- What's the smallest API surface that enables all modes?
- Push (proxy emits events) vs pull (storage polls)?
- Sync (wait for ack) vs async (fire and forget)?

**Branching semantics**
- Copy-on-write vs full copy?
- How deep does a branch go (just conversation, or referenced data)?
- Branch naming/identification scheme?

### Edge Cases

**Non-functional tools**
- Tools with side effects (write file, send email, DB mutation)
- In mock mode, side effects don't happen - is this acceptable?
- Shadow mode: side effects happen twice?

**Multi-agent scenarios**
- Multiple agents sharing MCP servers
- Conversation boundaries when agents hand off
- Correlation across agent boundaries

**Error handling**
- What if storage is unavailable during live capture?
- What if mocked tool result doesn't exist?
- Graceful degradation vs hard failure?

### Scope & Prioritization

**First application**
- Replay for debugging?
- Audit trail for compliance?
- What-if testing for prompt engineering?
- Which drives the minimal implementation?

**What's out of scope (for now)?**
- Multi-provider LLM support (OpenAI, Google)?
- Real-time dashboards/UI?
- Cost tracking and budgets?
- Agent orchestration (running the agent, not just observing)?

---

## Annex: TonyAPI Fit

### logd (Implemented)

logd provides structured, append-only storage with features well-suited to this architecture:

| Feature | Relevance |
|---------|-----------|
| Structured diffs | Store conversation deltas, not full state |
| Append-only history | Immutable audit trail |
| Scopes (COW branches) | What-if branching without duplication |
| Time-travel queries | Point-in-time state reconstruction |
| Watch support | Real-time conversation streaming |

logd could serve as the storage backend, with proxies emitting PATCH operations for each turn/tool call.

### docd / Controllers (Not Implemented)

docd is the planned coordination layer where controllers register mount points and handle domain logic. **Status: not yet implemented.**

Potential controller mapping:

```
Mount Point        Controller              Function
──────────────────────────────────────────────────────────
/conversations     Correlation Controller  Assemble LLM + MCP traffic
/replay            Replay Controller       Serve mock responses
/experiments       Experiment Controller   Manage scope branches
/judgments         Judge Controller        LLM-as-judge evaluation
/rubrics           Rubric Controller       Evaluation criteria
```

### With vs Without docd

**Without docd (near-term):**
```
Proxies ──PATCH──► logd directly
                     │
                     └── storage only, apps query logd
```

**With docd (future):**
```
Proxies ──events──► docd ──► Controllers ──► logd
                      │
                      └── controllers add logic:
                          correlation, replay modes,
                          experiment branching, etc.
```

The dual-proxy architecture doesn't require tonyapi, but logd is available and a good fit for the storage layer. docd and controllers will be available in the future, offering a framework for building features (correlation, replay, experiments) as controllers.

### Judge Controller (LLM-as-Judge)

The Judge Controller enables automated evaluation of agent conversations using another LLM as evaluator. Key insight: the judge's evaluation is itself an LLM conversation, captured by the same infrastructure.

```
┌──────────────────────────────────────────────────────────┐
│                    Judge Controller                       │
│                                                           │
│  Input:                                                   │
│    - conversation to evaluate: /conversations/X          │
│    - rubric: /rubrics/Y                                  │
│                                                           │
│  Process:                                                 │
│    - LLM call with conversation + rubric                 │
│    - judge session captured via same dual-proxy          │
│                                                           │
│  Output:                                                  │
│    /judgments/{id}                                       │
│      conversation_ref: /conversations/X                  │
│      rubric_ref: /rubrics/Y                              │
│      judge_session: /conversations/J  ◄── recursive      │
│      score: 0.85                                         │
│      verdict: pass                                       │
│      explanation: "..."                                  │
│                                                           │
└──────────────────────────────────────────────────────────┘
```

Because the judge session is a first-class conversation (`/conversations/J`), it supports:

- **Replay**: Re-judge with same rubric, check verdict stability
- **What-if**: Branch judge session, modify rubric, see new verdict
- **Audit**: Trace why the judge gave a particular score
- **Meta-judge**: Evaluate the judge's reasoning with another LLM

This recursive property makes evaluation itself testable and auditable.
