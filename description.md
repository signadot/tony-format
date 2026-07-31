# logd: a watch's state event is rooted at the watched path but its patch events are rooted at the document

A watch on a non-root path delivers its two event kinds at different roots, so a
client cannot apply the deltas to the state it was given.

  - The initial state event carries the SUBTREE at the watched path.
    forwardEvents reads the document and then extracts: `if path != "" { state =
    extractPathValue(state, path) }`, and sends that.
  - Patch events carry a DOCUMENT-rooted patch. A baseline watcher forwards the
    raw committed delta verbatim (deliberately, to preserve op fidelity for !key
    and friends), and a scoped watcher sends `tx.RootPatchAt(path, Diff(...))`,
    which wraps its subtree diff back up to the document root.

Observed on a watch of "doc" (state first, then every delta applied to it):

    deltas: doc:
              back: changed
              n: 3
            keep: base
            n: 0

    fresh:  back: changed
            keep: base
            n: 3

The client's document grows a spurious "doc" wrapper from the patches while the
subtree fields it started with go stale. Watching at the root ("") is the only
case where the two agree, because there is no extraction.

## Why it matters

The consumer contract for a logd watch is APPLY, not pattern-match: logd may
reshape a delta, and consumers are expected to apply what arrives rather than
inspect its shape. That contract is not satisfiable here without out-of-band
knowledge that state and patches need different rooting.

Any consumer that does work today must be compensating -- extracting at the
watch path from each delta, or keeping its state document-rooted and ignoring
the rooting of the state event. Worth confirming what docd and libctl actually
do before changing anything, since whichever convention they rely on is the one
in production.

## Options

 1. Root the state event at the document too, matching the patches. Smallest
    change on the server, but it sends a whole document where the client asked
    about a subtree, which the extraction exists to avoid.
 2. Root the patch events at the watched path. Consistent with what a
    path-scoped consumer asked for, but for baseline it means rewriting the
    committed delta rather than forwarding it, which is what the raw forward
    deliberately avoids.
 3. Leave the wire alone and document it, so a consumer knows to extract at the
    watch path before applying.

Not urgent and not a data-loss bug -- it is an inconsistency a consumer has to
know about. Filing so the choice is made deliberately rather than inherited.

Found while adding an end-to-end test that applies a watch's deltas and compares
against a fresh read (system/libctl/watch_stepping_test.go); the test watches the
root for exactly this reason, and says so.