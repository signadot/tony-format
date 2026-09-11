# storage: an overlay entry in an old log becomes a live footprint statement, so a scoped read folds it and compaction drops the claims it retired

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53.

Baseline {a: {x: 1}} (c1); scope s1 writes {a: {x: 2}} (c2); baseline {b: 1} (c3).
Append an overlay entry the way the deleted writer made one --
dlog.NewEntry(nil, {a: {x: 2, z: 9}}, 3, ts, 2, &s1) with ScopeOverlay=true -- remove
index.manifest and reopen, forcing a rebuild from the log.

The only live statement is the overlay; the scope's own entry is live=false. The
footprint read answers {a: {x: 2, z: 9}, b: 1}; the historic read {a: {x: 2}, b: 1}.

index/log_segment.go:252-255 sets Statement without looking at ScopeOverlay, and
index.go:140 puts it in the footprint. Only the historic arm of the read skips overlays
(storage/cursor.go:220); the footprint arm (:231-242) does not. storage/doc.go states the
contract -- an old overlay is recognised and skipped.

Consequence: wrong scoped reads on any log written before 73e2637, and compaction drops,
beyond the cutoff, the scope's own entries the overlay retired -- permanently.