# logd: a replay materializes its whole range before emitting any of it

`ReadPatchesInRange` reads EVERY patch in the range into one slice before the watch emits any of
them (storage/read_patches.go): `result []*CommitNotification`, appended per segment, sorted, then
returned to `watchStream.replay`, which walks it. So the peak memory of a replay is the whole
decoded range, whatever the consumer's buffer is and however fast it reads.

MEASURED, with the range read isolated — a FromCommit watch drained as fast as it arrives, no
initial state, nothing done per event, so what is measured is logd materialising the range plus
delivery. Peak Go heap over the process's idle heap:

     2000 commits   idle   43.2 MB   peak  179.7 MB   (+ 136.5)
     8000 commits   idle  244.4 MB   peak  712.0 MB   (+ 467.6)
    20000 commits   idle  745.2 MB   peak 2051.9 MB   (+1306.7)

Linear in the range, about 65 KB per commit for entities of a few small fields. The store is
docd-backed (`o sys up`), go-tony v0.0.202, harness attached.

WHAT IT COST US, so the scale is not hypothetical. A verse of 225k commits with a 442 MB delta
log, one `o sys up` container (logd+docd) on a 7.2 Gi node: a client asking for a whole-verse
catch-up drove the process to 3.7–7.1 GB and the kubelet evicted the pod. Four times in fifteen
minutes, each one taking the store down for everything else — the client was retrying a refused
`--since`, and the refusal path asked for a full-history replay to phrase its error message.
That second part was our defect and it is fixed on our side (signadot/verse
3qf2z59dh12ksdazg9n0, commit 28a7acfe); what remains is that a single legitimate request for a
wide range is unbounded here.

THE SHAPE OF A FIX, as we see it from outside: emit as the range is read rather than collecting
it first. `replay` already walks the slice in order and `Watcher` already has a bounded buffer
with slow-consumer handling, so the streaming version has somewhere to push back — a consumer
that cannot keep up is failed, which is the existing contract, rather than the server holding
the whole range on its behalf. The dedup-by-commit and the final sort are what make it not a
one-liner: both want the segment list, which is cheap, rather than the entries, which are not.
`LookupRange` already gives the segments up front.

A SMALLER ONE BESIDE IT, filed here only if you want it as its own issue: when a replay IS
refused for being below the floor, `failWatch` logs the floor as `detail` and sends the client
the reason code alone (`replay_compacted`). So a client cannot tell a person what the store
still holds — only that this cursor is gone. The relative cursor (`-N`, clamped) is the workaround
we use, and it answers a different question than "what is the oldest commit you have".