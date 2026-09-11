// Package tony provides support and tooling for the Tony format.
//
// It holds the operations on documents, which are ir.Node trees: package parse
// reads them and package encode writes them.
//
//   - [Diff] answers the patch that turns one document into another; [DiffWith]
//     takes options such as [DiffComments] and [DiffAbsolute].
//   - [Patch] applies a patch to a document, and [PatchWith] does so under a
//     mergeop.OpContext. The operations a patch names are the ones package
//     mergeop registers.
//   - [Match] reports whether a document matches a pattern, and [MatchWith] does
//     so under a mergeop.OpContext; [Explaining] and [Tracing] collect why into
//     an [Explanation].
//   - [Trim] cuts a document down to the shape of a pattern, and [FilterState]
//     puts Match and Trim together the way logd and docd answer a match.
//   - [Tool] evaluates the operations package eval registers.
//
// Field order is not content. Diff is blind to the order of an object's fields,
// and Patch answers each object it merges with its fields in sorted key order.
package tony
