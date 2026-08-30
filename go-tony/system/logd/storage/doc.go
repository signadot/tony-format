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
// # Scopes
//
// A scope is a copy-on-write overlay on baseline: reads see baseline with the scope's own
// writes applied last, and those writes shadow later baseline writes to the same path.
// Baseline stores DIFFERENCES, whose delta replays against a base that never moves; a
// scope stores CLAIMS, because its patches replay over a baseline that advances
// underneath them. A difference cannot carry a claim, which is what makes the two layers
// lower differently (lower.go).
//
// A scoped read replays the scope's own patches, always: that is what a scope layer IS,
// and it is the only form in which !key identity and every other operation still mean
// what they meant when written. Scope patches are exempt from snapshotting and
// compaction, so replaying ALL of them costs the scope's whole history -- and a read at a
// PATH does not have to: narrowSubtreeAt takes a scope, looks both layers up at the read
// path, and replays only the patches which bear on it.
//
// logd used to cache a scope's layer as an overlay entry beside each baseline snapshot,
// derived by diffing baseline@T against scoped@T. That is gone. A difference between two
// documents cannot record that the scope removed a field baseline never had, so the
// scope stopped shadowing that path forever after (qth3kqe9h12ksxz9j9n0); over 200 seeded
// streams the derivation broke 44 standing claims where replay broke none. A log written
// by a build that had overlays still reads: the entries are recognised and skipped, and
// the scope's own patches were never removed to make room for them.
//
// # Subpackages
//
//   - [index] - Hierarchical path-based indexing
//   - [tx] - Transaction coordination
//   - [autoid] - Monotonic ID generation
package storage
