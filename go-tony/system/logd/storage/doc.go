// Package storage provides the persistence layer for logd.
//
// [Storage] manages:
//
//   - Patch storage in a double-buffered write-ahead log (dlog)
//   - Path-based indexing for efficient lookups
//   - Multi-participant transactions
//   - Copy-on-write scopes for isolation
//   - Snapshots for read optimization
//
// # Reading
//
// There is one read of state, [Storage.Read], and one read of change, [Storage.Deltas].
// Both are at a path, and the root is a path. Read answers a [Cursor] over the events of
// the subtree at the path as of a commit: it seeks the nearest snapshot at or above the
// path -- the root snapshot a switch takes ([Storage.SwitchDLog]), or a snapshot of a
// path that reads there scheduled -- and folds the writes since it that reach the path,
// one log record at a time. [Collect] builds a node from a cursor, under a budget the
// caller states. Deltas walks the commits in a range that reach a path, one entry at a
// time.
//
// The head is a number: [Storage.GetCurrentCommit] is the published watermark, and a
// precondition or a write's verification is a read at the path it concerns, under the
// write budget ([Storage.SetWriteBudget]).
//
// A keyed array is stored as an object whose fields are its elements' names
// (tx.LowerKeyed), so a cursor's events carry that form. The boundary puts array-ness
// back: [Storage.RaiseState] for a state, and the deltas a notification or Deltas
// carries are raised already.
//
// # Lowering: what the log keeps
//
// A patch may be written with whatever expressivity tony offers. What is STORED is held
// to a narrower vocabulary: operations whose result states what the value IS, so that
// re-applying one to a base that has moved gives what it gave at the write. A patch
// carrying a RELATIVE operation -- one whose result depends on what it lands on -- is
// applied and its result stored in its place.
//
// The user of a store believes they are working with data, and an operation that
// re-evaluates later breaks that belief. It costs almost nothing: nearly every write is
// already absolute and is kept as it arrived, and the read a lowering needs was taken
// anyway, because the commit path reads the state a patch applies to in order to refuse
// one that does not. See lower.go, and api.StorageContext for the vocabulary.
//
// # Scopes
//
// A scope is a copy-on-write layer over baseline: reads see baseline with the scope's own
// writes applied last, and those writes shadow later baseline writes to the same path.
//
// The two layers store different things, and this is the distinction the rest follows
// from. Baseline stores DIFFERENCES: its delta replays against a base that never moves. A
// scope stores CLAIMS -- what the scope holds at a path, whatever baseline does next --
// because its patches replay over a baseline that advances underneath them. A difference
// cannot carry a claim: a scope's delete of a field baseline has not created yet IS no
// difference between the two states, so a delta built from one says nothing and the scope
// stops shadowing that path. Hence the two lower differently (lower.go).
//
// A scoped read folds the scope's own patches over baseline. That is not an optimisation
// choice but what a scope layer IS: a claim is carried only by the patch that made it,
// and a difference taken between documents cannot carry one (above). Scope patches are
// not snapshotted. What bounds the fold is the FOOTPRINT (index.Footprint): a stored
// scope write is absolute, so a later statement of the scope that covers an earlier one
// dominates it, and the index keeps, per scope, the statements no later one dominates,
// deciding at each write. A scoped read folds the live statements on its path's ancestor
// chain, at the path and beneath it, each entry once, in commit order -- bounded by what
// the scope holds there, not by its history. A read at a commit older than one of those
// live statements folds the scope's history from the index instead. Compaction drops a
// scope's entry beyond the cutoff once none of its statements is live
// (scope_compaction.go).
//
// Compatibility: logd once cached a scope's layer as an "overlay" entry beside each
// baseline snapshot, derived by diffing the two documents. It was removed -- a difference
// between documents cannot carry a claim, which is the same wall as above
// (qth3kqe9h12ksxz9j9n0) -- and a log holding such entries still reads, because they are
// recognised and skipped and a scope's own patches were never removed to make room for
// them.
//
// # Subpackages
//
//   - [index] - Hierarchical path-based indexing
//   - [tx] - Transaction coordination
//   - [ident] - An element's identity spelled as a path segment
//   - autoid - Monotonic ID generation
package storage
