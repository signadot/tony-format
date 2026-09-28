# git issue watch: the on/local marker can say "on origin" after another clone rewrote origin

The marker that a watch change carries (`(on origin)` / `(local)`, and `on` in issue_watch) is read from this clone's tracking refs. It describes origin as this clone last saw it, at its last fetch or push, not origin now (j9r3022vh12ks2a3q9n0).

If another clone rewrites origin (`git issue push --force`, or a ref deleted on the remote), a change can read `on origin` although origin no longer holds it. That is the harmful direction: a reader takes the change as pushed and leaves it. A pulling watch sees the rewrite at its next fetch, but a line already printed stays wrong. `issue_watch` and `git issue watch --local` fetch nothing, so they are wrong until something else in the clone fetches. A normal close, reopen or merge cannot cause it, because the remote's new commit carries the old one.

Stated as a limit in docs/commands.md, docs/mcp.md and the `on` schema (2196c652).

## What would fix it

The fix has to ask the remote. Two ways:

- Check origin when saying `on` (`git ls-remote` for the issue's refs). That is a network call per change, and it would make `issue_watch` and `watch --local` reach the network, which they deliberately do not today.
- Say only what is known. A watch that does not fetch says `on origin at last fetch`, or leaves the marker out. A pulling watch is wrong for at most one `-fetch`.