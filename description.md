# logd: a patch rooted at a wildcard is not refused -- a.* writes a literal "*" field no read can reach, and [*] answers storage_error

logd refuses `..` where a path must name a place (server/path.go:20-32, `validateDataPath`), but it never asks `Wild()`. Reads catch a wildcard late, by re-classifying an absent read (session_read.go:284-292). The write path catches nothing, and the two outcomes are both wrong.

## `.*` is written as a field literally named `*`

`patch {path: "a.*", data: 1}` passes `validateDataPath` (only `Descend` is checked), passes `ident.CanonicalPath` -- which deliberately hands a wildcard path back untouched (storage/ident/ident.go:283-289) -- and passes `checkArrayWrite`, which again checks only `Descend` (storage/tx/array_write.go:182-192).

At commit, `kTree.childKind()` classifies a `FieldAll` child as an object write (storage/tx/merge.go:246-258), and the key falls through the `KPath.Field != nil` arm to `strings.TrimPrefix(f, ".")`, which is `"*"` (merge.go:163-180). `token.KPathQuoteField` quotes a name beginning with `*`, so the store spells the field `a."*"`.

So the write succeeds, and the read at the path that wrote it answers `invalid_path` ("segment \"*\" names a set of values, not one", match_data.go:78-81). The value is reachable only as `a."*"`. A client that meant "every field of a" gets a commit, a delta broadcast to watchers, and a field nobody asked for.

## `[*]`, `{*}` and `(*)` fail at commit as storage_error

No dedicated refusal, so each falls out of the rooting switch:

- `[*]` -> `invalid array index "[*]"` (merge.go:211-215)
- `{*}` -> a raw `strconv.ParseUint: parsing "*": invalid syntax` (merge.go:189-194)
- `(*)` -> `cannot root a patch at a (key) segment: ...` (merge.go:157-162)

All three reach session_write.go:209 as `ErrCodeStorage`, "failed to commit: ...". That is the code for a store that is unwell, and it sits directly below the `NoSuchElementError` and `WriteBudgetError` arms that answer `invalid_path` and `invalid_diff` for exactly this class of thing: the store is healthy and the remedy is the callers.

## The invariant the code states is not established anywhere

merge.go:205 says "paths are non-wildcard by construction, so [*] cannot occur". Nothing constructs that: `validateDataPath` checks `Descend`, `checkArrayWrite` checks `Descend`, and `CanonicalPath` passes a wildcard through on purpose. The comment describes an invariant a reader will rely on, and the `.*` case above is what happens when it does not hold.

## Also unvalidated: the CAS path

The patchs `match:` path is canonicalized and never validated (session_write.go:39-47). A wildcard there reads absent, matches as null, and answers `match_failed` -- a precondition that "did not hold" rather than a path that cannot mean anything.

## Shape of a fix

Refuse a wildcard where a path must name a place -- the patch path, the CAS path -- at the boundary, with the `invalid_path` message reads already use. Retention keeps its own grammar, where a wildcard is required in the last segment (server/retention.go:128-154). Nothing pins todays behaviour: `TestSession_DescendPathIsRefused` (server/session_test.go:1289-1348) covers `..` only, and no test anywhere writes a wildcard path.