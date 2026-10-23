# git issue ext add: a source's url is shared by every clone, and a local path is taken without a word

`git issue ext add <source> <url>` records the url in `refs/git-issues/v1/sources/<source>`, and `push` and `pull` carry that ref as they carry the issues. So the url is shared state: every clone that pulls refreshes the source's mirrors from it. `ext add` takes whatever was typed, a local directory included, and says only `Source <name> is <url>`.

In verse the `tony` source was added as a checkout's path, `/Users/<me>/Dev/github.com/signadot/tony-format`, and stood so for a month. The clone that added it refreshed fine. Every other clone's `git issue pull` printed

```
tony could not be reached; its mirrors are as they were: cannot fetch yg3ck1aah12ksx4ypxn0 from tony (/Users/<me>/Dev/github.com/signadot/tony-format): fatal: '/Users/<me>/Dev/github.com/signadot/tony-format' does not appear to be a git repository
```

which reads as that machine's problem, not the source's. The pull itself completed and the mirror stayed as it was, so nothing broke; it was wrong on every machine but one until somebody read the sources ref. Fixed by hand: `ext add` again with `https://github.com/signadot/tony-format`, then `git push origin refs/git-issues/v1/sources/tony`, since no git-issue command pushes a source on its own.

## What would fix it

- `ext add` refuses a url that is a local path, or takes it only with a flag that says it is this clone's alone (`--local`), and says that a source's url is what every clone will fetch from.
- A local path can still be useful offline. If it is wanted, the sources ref holds the shared url and the clone holds its own override outside the shared refs (git config, or `~/.config/git-issue.tony`), so a path never travels.
- `ext list` and the "could not be reached" line could show where the url came from, so a wrong url points at the ref and not at the machine.
- `ext add` on a source that exists should say it is replacing the url, and `push` should carry a changed sources ref, or say that it does not.