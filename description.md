# docd: a watch on an element by key sugar is refused as invalid_path, where logd accepts it

Measured while testing 62r9amwp: through docd (startDocdProxy), `Watch(ctx, "runs(r1)")` on a store whose schema keys runs by id answers `invalid_path: invalid watch path "runs(r1)"`. The same request straight to logd is accepted and canonicalized to `runs."(id=r1)"` (session_watch.go watchPath). The store's own spelling, `runs."(id=r1)"`, is accepted by docd.

Where: docd/server/watch.go -- the mount coordinator's beginRead parses the path into fields (pathFields), and a (key) segment is not a field, so admission refuses before the watch is routed.

Cost: switchability -- a client that works against logd fails against docd for the spelling logd documents as the way to address an element. Match and patch through docd with sugar were not checked.