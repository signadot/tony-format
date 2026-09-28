# git issue watch: a change made in this clone and a change pulled from the remote print the same line, so a reader cannot tell whether a change is pushed

A change line from `git issue watch` says what was done to an issue. It does not say where the change came from: this clone, or the remote by a pull. So a person or an agent that reads the watch cannot tell if the change is on the remote yet.

Seen 2026-09-28. In one session, an agent commented on verse issue 9ce3bva0 and edited its body, in the local clone. A `git issue watch` running in another terminal printed:

```
verse  9ce3bva0h12ksjc7q5n0  open  proposal: a timed, two-way rendezvous with two-party authority, as the building block of verse's user facing interactive UX  -- comment: **Decided (Scott, 2026-09-28): the view is a second build...
verse  9ce3bva0h12ksjc7q5n0  open  proposal: a timed, two-way rendezvous with two-party authority, as the building block of verse's user facing interactive UX  -- edit: body
```

Both changes were local and not yet pushed. A change that a pull brought from a teammate prints the same kind of line. The watch prints a `pull origin: <id> <what>` note before a pulled change, but only when this watch made the pull. A change that another pull brought, or a change made locally, has no note. So the absence of a note does not mean "local".

`issue_watch` and `issue_watch_remote` answer the same `watchChange`, with the same gap.

## What would fix it

Each change says where it came from, and whether it is on the remote:

- **local**: made in this clone, and not on the remote. Later, when a push sends it, the watch can say so.
- **pulled**: brought in by a pull from the remote.

The watch already reads the tracking refs that a fetch writes (`refs/git-issues/v1/remotes/<remote>/…`). A change whose new commit the tracking ref holds is on the remote; one it does not hold is local.