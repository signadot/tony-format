# logd Session Request Examples

Example requests for use with `o system logd client`.

## Usage

Start the server:
```bash
o system logd serve -data /tmp/logd-data
```

Send requests:
```bash
cat requests.tony | o system logd client localhost:9123
```

Or run a specific scenario:
```bash
cat user-crud.tony | o system logd client localhost:9123
```

## Session Protocol

Each line is a separate request in Tony wire format. Comments (lines starting with `#`) are ignored.

**Hello** (handshake - must be first):
```tony
{hello: {clientId: "my-client"}}
```

**Match** (read):
```tony
{match: {body: {path: "users.alice"}}}
```

**Patch** (write):
```tony
{patch: {patch: {path: "users.alice", data: {name: "Alice"}}}}
```

**Watch** (subscribe to changes):
```tony
{watch: {path: "users", fromCommit: 0}}
```

**Unwatch** (unsubscribe):
```tony
{unwatch: {path: "users"}}
```

**NewTx** (create multi-participant transaction):
```tony
{newtx: {participants: 2}}
```

**Patch with transaction**:
```tony
{patch: {txId: 123, patch: {path: "orders.ord-1", data: {status: "shipped"}}}}
```

## Scopes (COW Isolation)

Connect with a scope for isolated data:
```tony
{hello: {clientId: "sandbox-client", scope: "sandbox-123"}}
```

Delete a scope (baseline sessions only):
```tony
{deleteScope: {scopeId: "sandbox-123"}}
```

## Files

- `requests.tony` - Reference examples of all request types
- `user-crud.tony` - User create/read/update/delete scenario
- `orders.tony` - Order management with keyed arrays
- `config.tony` - Configuration management example
- `auto-id.tony` - Auto-generated IDs for keyed arrays (requires schema)
- `auto-id-config.tony` - Server config with auto-id schema
- `auto-id-schema.tony` - Schema defining `!logd-auto-id` fields

## Auto-ID Example

Start server with schema:
```bash
o sys logd serve -config testdata/auto-id-config.tony -data /tmp/auto-id-store
```

Run the example:
```bash
cat testdata/auto-id.tony | o sys logd session :9123
```

The `!logd-auto-id` tag in the schema tells logd to generate monotonic IDs for null/missing fields.
