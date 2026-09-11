// Package seq provides atomic sequence counters.
//
// Generates monotonically increasing commit and transaction IDs.
//
// [Seq] keeps both counters in one 16-byte file, meta/seq under the store root: the
// commit counter, then the transaction sequence, each a little-endian int64 masked to 56
// bits. [Seq.NextCommit] and [Seq.NextTxSeq] increment a counter under the Seq's lock and
// write the file before returning, replacing it whole through a temp file and a rename.
// An absent file reads as zero for both.
//
// The counters are the allocator, not the reader's view of the head. The storage package
// raises them to the maxima its log holds when it opens a store, so a commit or
// transaction number the log already holds is never issued again.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/system/logd/storage - Storage layer
package seq
