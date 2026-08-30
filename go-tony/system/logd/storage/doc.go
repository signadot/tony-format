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
// A scoped read replays the scope's own patches. That is not an optimisation choice but
// what a scope layer IS: only the patches carry op semantics, so !key merges by identity
// at read time exactly as it did at the write, which nothing materialized can reproduce.
// Scope patches are exempt from snapshotting and compaction, so replaying all of them
// costs the scope's whole history -- and a read at a PATH does not have to. narrowSubtreeAt
// takes a scope, looks both layers up at the read path, and replays only the patches
// bearing on it, which is flat in the scope's history where the whole read is linear.
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
//   - [autoid] - Monotonic ID generation
package storage
