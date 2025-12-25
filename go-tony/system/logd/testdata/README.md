# logd Client Request Examples

Example requests for use with `o system logd client`.

## Usage

Start the server:
```bash
o system logd serve -data /tmp/logd-data
```

Send requests:
```bash
cat requests.tony | o system logd client localhost:9000
```

Or run a specific scenario:
```bash
cat user-crud.tony | o system logd client localhost:9000
```

## Request Format

Each line is a separate request in Tony wire format. Comments (lines starting with `#`) are ignored.

**MATCH requests** (reads):
```tony
{meta: {}, body: {path: "users.alice"}}
```

**PATCH requests** (writes):
```tony
{meta: {}, patch: {path: "users.alice", data: {name: "Alice"}}}
```

**Conditional PATCH** (only applies if match succeeds):
```tony
{meta: {}, match: {path: "users.alice.status", data: "active"}, patch: {path: "users.alice.last_seen", data: "now"}}
```

## Files

- `requests.tony` - Reference examples of all request types
- `user-crud.tony` - User create/read/update/delete scenario
- `orders.tony` - Order management with keyed arrays
- `config.tony` - Configuration management example
