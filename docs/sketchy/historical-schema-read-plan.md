# A read at a past commit is read under that commit's schema

Issue: `3n390bjwh12ksy61n9n0`. Follows `xnepz3sfh12ksn5qn9n0`, which fixed the same
mistake in the set walk's enumeration.

## The defect

A keyed array is stored as an object of names and raised to an array on the way out
(`storage/raise.go`). Which arrays are keyed changes over time, so raising asks the
schema **at the commit read** (`schemaHistory.ParsedAt`). Two decisions on the read path
ask the schema in force **now** instead, so a read at a commit before a keying change
answers under the wrong schema:

- **Whether to raise.** `Session.raises()` picks the encoded fast path when today's schema
  keys nothing. A body that was a keyed array at the commit read is then streamed as the
  store's object of names, and `RaiseState`, which would have got it right, never runs.
- **How a path is spelled.** `ident.CanonicalPath` turns `runs(r1)` into
  `runs."(id=r1)"` using today's identity, in `handleMatch` and in the walk's
  `canonicalChild`.

Measured on main (`dc401f31`): `runs` keyed by `id`, written, then its keying removed
(force); a read at the commit before the change:

| request | answer | should be |
|---|---|---|
| `{match: {path: runs, commit: N}}` | `{(id=r1): {…}, (id=r2): {…}}` | `[{…}, {…}]` |
| `{match: {path: "runs(r1)", commit: N}}` | `invalid_path`: runs has no identity | `{id: r1, …}` |

## Survey: every schema lookup outside the store's write path

| site | schema | verdict |
|---|---|---|
| `session_read.go` `handleMatch` → `CanonicalPath` | now | **fix**: the commit read's |
| `session_read.go` `raises()`, called by `handleMatch` and `sendSetMember` | now | **fix**: the commit read's |
| `session_read_set.go` `canonicalChild` (a concrete segment after a wildcard) | now | **fix**: the commit read's |
| `session_read_set.go` `identityAt` | at commit | fixed by `xnepz3sf` |
| `session_read.go` `iterTypeAt` → `Storage.KeyedAt` | at commit | correct |
| `session_read.go` `readValueAt` → `Storage.RaiseState` | at commit | correct |
| `storage` `raiseDelta` (watch and replay deltas) | at the delta's commit | correct |
| `session_write.go` patch path and precondition → `CanonicalPath` | now | correct: a write lands at the head, and `tx/keyed_form.go` checks the path again under the schema of the commit it takes |
| `retention.go` | now | correct: retention is a write at the head |
| `session_watch.go` `watchPath` → `CanonicalPath` | now | out of scope, below |
| docd, libctl | — | never consult keying; a docd read at a commit fans that commit to logd, and is fixed with it |

Tests with a schema: `keyed_path_test.go`, `unwatch_sugar_test.go`, `retention_test.go`,
`session_read_set_test.go` (`TestSetMatch_KeyedAtItsCommit`, `TestSetMatch_IterType`).

## Ordering: simpler than first described

The issue discussion said canonicalization must move after the commit is settled,
"including after a cursor is decoded". Half of that is not so. `CanonicalPath` answers a
path holding a wildcard **unchanged**, so for a set read the early canonicalization is a
no-op, and the cursor stores and compares that raw path. The only spelling that happens in
a set read is `canonicalChild`, inside the walk, which runs after the cursor has set the
commit. So:

- **single-node read:** move `CanonicalPath` below the commit's resolution in
  `handleMatch`. The commit is final there.
- **set read:** pass the commit to `canonicalChild`. Nothing moves.

## Contract decisions

1. **A path is judged by the schema of the commit read**, not by today's. `runs(r1)` at a
   commit before `runs` was keyed is `invalid_path` (`(r1)` named nothing then), not
   `not_found`; `runs[0]` at a commit before `runs` was keyed reads position 0. Same rule
   as the answer's shape: a historical read reads the document as it was.
2. **Error order follows:** a request with both an out-of-range commit and a key path the
   schema refuses answers `commit_not_found`. The path cannot be judged until the commit
   is known. Today it answers `invalid_path`.
3. A read at commit 0 has no schema; a key path there is `invalid_path`, where today it
   is `not_found` whenever today's schema keys the array. Consistent with 1.

## Changes

1. **Tests first**, in `session_read_set_test.go` beside `TestSetMatch_KeyedAtItsCommit`,
   one store per transition, each read at the commit before the change and at the head:

   | transition | before the change | at the head |
   |---|---|---|
   | **lose** keying (force) | `runs` is an array; `runs(r1)` reads r1; `runs(*).n` answers both | `runs` is an array; `runs(r1)` `invalid_path`; `runs[0]` reads |
   | **gain** keying | `runs[0]` reads; `runs(r1)` `invalid_path` | `runs(r1)` reads; `runs[0]` `invalid_path` |
   | **re-key** `id` → `sku` | `runs(r1)` reads; `runs(sku=A)` `invalid_path` | the reverse |

   Plus the fast-path case on its own: a store whose schema keys **nothing** now, read with
   no pattern at the old commit (`raises()` is the only thing that catches it, since any
   keyed array today sends the read down the raising path). Plus `return: "path,body"` on
   a set at the old commit, so `sendSetMember`'s `raises()` is covered too.

   Every "reads" row asserts the answer's shape, not just the absence of an error. Run the
   new tests against main first and record which rows fail: lose is measured, and gain and
   re-key are expected to fail by the same reading of the code, not yet measured.

2. **`raises()` → `raisesAt(commit)`**: `SchemaForAt(scope, commit)` has any keyed path.
   Both callers have the commit.

3. **`handleMatch`**: move the `CanonicalPath` block after the commit is resolved, and give
   it `SchemaForAt(scope, commit)`.

4. **`canonicalChild(prefix, seg, commit)`**: `SchemaForAt(scope, commit)`. Its one caller,
   `walkSet`, has the commit.

5. **Docs**: `docs/logd/session.md`, the paragraph on `commit`: a read at a past commit
   reads the document as it was **and under the schema in force then**, so a keyed
   element's path and an array's shape are both that commit's, with decision 1's example.

6. **Sweep**: `grep -rn "SchemaFor(" system/logd/server` should leave only the write path,
   retention and `watchPath`, each named in the survey above.

One commit, with the tests, `Issue: 3n390bjwh12ksy61n9n0`.

## Verification

- the new tests, failing on main as recorded in step 1, passing after;
- `go test ./system/logd/server` (≈27s);
- `go test ./system/libctl ./system/docd/...` (≈37s): docd reads at a commit go through
  here;
- storage is untouched, so its 2½-minute run is not needed.

## Out of scope

**A watch's path** is spelled once, when the watch is established, under today's schema
(`watchPath`), and the same spelling is used to unwatch. A watch replaying from a commit
before a keying change is still addressed by the path as it is now. Whether a watch on an
element should follow a keying change -- its canonical spelling changes, and the rewrite
arrives as a delta at the array -- is its own question about what a watch on an element
means across that change, not a spelling bug. Filed as `62r9amwph12krxfjn9n0`: measured, an
element watch reports the element deleted at the change and goes silent, replay reports it
created there, and after a re-key an id-less element watch cannot be unwatched.
