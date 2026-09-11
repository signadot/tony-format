// Package patches applies stored patches to an event stream without
// materializing the document the stream describes.
//
// [StreamingProcessor] streams the base events and materializes only the subtrees a
// patch reaches: [SubtreeCollector] gathers the events at each patched path, the
// subtree is patched with api.NextState and re-emitted, and every other event passes
// through as it arrived. A patch at a path the base does not reach is grafted where its
// key sorts, so the output keeps the object key order storage keeps. Storage builds its
// snapshots, and answers a read at a path, by folding log entries onto a snapshot's
// events, or onto an empty stream, through it.
//
// [Roots] is the reading an entry is applied by: the nodes it states something at, each
// with its path.
//
// See docs/patch_design_reference.md for the full streaming design (Piece 2).
package patches
