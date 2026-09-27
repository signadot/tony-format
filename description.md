# git-issue: a pull records a fetch on a source every time, so a watch that pulls grows the source's ref without end

A repository with a source (`git issue ext add`) and at least one mirror gets a new commit on `refs/git-issues/v1/sources/<name>` at every pull: `RefreshMirrors` fetches each mirror and then records the time with `touchSource`, whether or not any mirror moved.

A pull by hand is occasional, so this was one commit now and then. `git issue watch` and `issue_watch_remote` pull every `-fetch` (30s), so each running watch adds a commit every 30 seconds.

Seen 2026-09-27 in verse, which has one source, `tony`, and two watches running: 2693 commits on the source's ref since 2026-09-26 13:19, two more every 30 seconds. A push carries sources, so `git issue push <id>` sent them to origin (44ec33eb..b06dee1e).

Nothing reads wrong because of it: the record says what it said, with a later time. The cost is the ref's growth, and that every push has commits to send.

A repository with no source is not affected.

## Fix

A refresh records the fetch only when a mirror moved. One that brought nothing writes nothing.