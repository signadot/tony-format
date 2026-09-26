# git-issue: outside a repository, commands say something else went wrong

Run outside a git repository, only `list`, `watch` and `serve -watch` say so. The rest report whatever failed first, or nothing:

```
create t --body b      => failed to create issue: failed to hash meta.tony: exit status 128
ext add s https://x    => failed to write source.tony: exit status 128
show abcd              => issue not found: abcd
ext list               => No sources
ext refresh            => (nothing, and exit 0)
push --all             => remote not found: origin
for-commit HEAD        => commit not found: HEAD
```

Seen 2026-09-27: `git issue ext add tony-format https://github.com/signadot/tony-format` answered `failed to write source.tony: exit status 128` from a directory named signadot. `~/Dev/github.com/signadot`, which holds the clones, is not a repository and has the name of the one inside it.

## Fix

One check where commands are dispatched (Root): every command but `mcp`, which has its working set, and `version` refuses outside a repository with git's own words for it. A command asked for its help answers it anywhere.