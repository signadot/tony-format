# docd: compose a wildcard match across mounts -- fan out and interleave, rather than answering unsupported

Follow-up to y2agz9dyh12kse24n9n0, which gives logd a wildcard `match` answered one node at a time. There, docd answers `unsupported` for a wildcard match whose set spans mounts. This is the issue for composing it.

## Why docd answers unsupported first

A wildcard names a set, and the members of that set can live in different places: the base store, a mounted subtree, several mounts. docd routes a request by its field prefix (docd/server/client_session.go `routeFor`, docd/server/registry.go:136-165 `LookupPrefix`), and classifies a path whose segments stop being fields as "indexed", for which `MountsUnder` answers nil (docd/server/registry.go:175-184, paths.go:63-78). A wildcard segment is classified that way today, so a wildcard match is forwarded to one owner as though the set were that owner's.

That is the shape to refuse. Forwarding a set to one participant answers a different question than the one asked, and does it silently -- the caller cannot tell a complete answer from a partial one. `unsupported` is the honest interim answer, and it is the code docd already uses for an operation a responder does not implement (logd/api/session.go:592).

## What composing it means

`jobs.*` where `jobs.a` is a mount and the rest is base: the set is the union of what each participant answers, and docd owes the client one sequence:

- fan out to every participant whose subtree can contribute, as a composed read already does for a path spanning mounts (docd/server/compose_read.go);
- interleave the per-participant sequences into one, under the client's request id, ending the sequence once -- when every participant has ended its own;
- keep the snapshot honest: a composed read is one consistent snapshot because logd has one commit sequence and every mount commits through it (docs/docd/composition.md). The composed wildcard match has to name the one commit the whole set was read at, not a commit per participant;
- a participant that fails mid-sequence fails the whole read, rather than leaving the client a partial set it cannot tell from a complete one.

## What has to be decided

- **Which participants a wildcard reaches.** For `jobs.*`, every mount at or under `jobs`, plus the base -- the same question `MountsUnder` answers for a concrete path, asked of a set. The prefix up to the first wildcard is what decides it.
- **Ordering across participants.** Each participant answers in its own order. Does docd interleave as answers arrive (cheap, order undefined across participants), or merge by path (ordered, and it must hold results to do it)? A caller that asked for a set probably wants the first answer fast.
- **A mount that arrives or leaves mid-sequence.** A watch is ended with `session_mounted`/`session_unmounted` so it never observes the change mid-stream (logd/api/session.go:594-606). A read is a snapshot at one commit, so the answer is probably that the set is the mount membership at that commit -- worth stating rather than leaving to the implementation.
- **Whether a controller must implement it.** A mount answers the session protocol (docd/api/mount_session.go:76-88), so a wildcard match reaches controllers too. A controller that does not implement it answers `unsupported`, and docd has to decide whether that fails the composed read or contributes nothing.

Related: th7sdhvyh12ksjtfn9n0 (`..` over the wire) is strictly harder here, since a descent decides for itself which mounts it reaches.