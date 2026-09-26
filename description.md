# git-issue mcp: several repositories in one server, as a view that holds nothing and keeps a relation within one repository

## Why

`git issue mcp` (1k3x9sp6h12ks5c0pxn0) is one server for one repository: `GitStore` runs git in the
working directory, `-C` picks another, and outside any repository the first tool call fails.
A person working across repositories -- tony-format and verse, say -- runs one server per
repository under different names, and the agent has to know which server to ask about an
issue it was only given the id of. In practice that makes the server unusable for the work it
was built for, which crosses repositories in one session.

The ids already answer that. An XIDR is unique across repositories (machine, time, counter),
so an id names its repository without saying so: a server given several can find which one
holds it by looking, and a prefix that is unambiguous across all of them still works.

## The rule

**The server is a view and holds nothing.** It is N stores, each on its own repository's refs,
and what it adds is dispatch. Which repository holds an id is answered by searching the refs
every time -- derived, never kept. The set of repositories is the user's configuration, the
same kind of thing as remotes in `.git/config` or the host's list of servers, and lives
nowhere in git because it need not survive anything: delete it and every issue is exactly
where it was. The test of any addition here is that nothing the server knows is unrecoverable
from the repositories. An id-to-repository index, a cache, a mirror of issues outside their
repository would each fail it: a clone does not carry them, and two machines can then
disagree.

**A relation stays within one repository, unless the other side is mirrored in.** A bare id
written into A's meta.tony that resolves only where B is also present would leave A no longer
self-contained, which is what the distributed model is. So a relation -- related, blocks,
duplicate -- whose two ids resolve to different repositories is refused, naming both, UNTIL
ext references exist (a3v2a4j6h12kse4ypxn0): with them, the server has both stores, mirrors
B's issue into A as `ext/<B>/<xidr>`, and records the relation as a plain id that A resolves
alone. That is the one place the two designs meet, and it is why ext references come first.

## Shape

**The working set** is the repositories the server serves. It comes from three places, in
order: `-C`, repeatable, on the command line; `~/.config/git-issue.tony`, a list of paths,
read at start; and the repository the server was started in, when it was started in one.
With none of the three the server refuses and says so. The set is user configuration, never
tracker state; it passes the rule above because it is recoverable from nothing and needs to
be: the issues are in the repositories.

    git issue mcp -C ~/src/tony-format -C ~/src/verse     # this session's set
    ~/.config/git-issue.tony:                              # every session's
      repos:
      - ~/src/tony-format
      - ~/src/verse

Two tools change it while the server runs, because an agent's session does not know at start
what it will touch: `repo_add(path)` opens a repository and serves it from then on, and
`repo_remove(repo)` stops; `repo_list` says what is served and where each came from. In
memory first; `repo_add(path, persist: true)` also writes the config file, so the next session
has it. The server started outside any repository loads the config and is usable at once,
which is what a host that starts servers from a home directory needs.

- **Tools taking an id** (`show`, `edit`, `comment`, `label`, `close`, `reopen`, `link`,
  `relate`, `for_commit`) resolve it across the set; an id found in none is "not found", one
  found in more than one -- a prefix -- is ambiguous, naming the matches with their
  repositories. `link` and `for_commit` take a commit too, looked up in the repository the id
  resolved to.
- **Tools that need a repository named** take `repo`: `create` (required when more than one
  is served), `list` (none lists all, each row saying its repository), `push` and `pull`
  (required). `repo` is the directory's base name unless two collide, then the full path; the
  server says which in `repo_list` and its instructions.
- **`relate` across repositories** mirrors the far issue as an ext reference (a3v2a4j6h12kse4ypxn0)
  and then relates; without ext references it refuses, naming both.
- **Resources** (eg8zmb1sh12ksr48pxn0) fold the same way: `issue://<xidr>` resolves by search,
  `issue://` lists across the set.

## What it takes

1. **`GitStore` takes a directory.** It acts on the process's working directory today, which
   is why `-C` is an `os.Chdir` and why nothing can hold two. A store made for a directory
   runs `git -C <dir>` for everything, and the commands, the tests (which chdir per test
   today, and so cannot run in parallel) and the server all get the same store. This is the
   step everything else rests on, and worth doing on its own.
2. **The working set**: `-C` repeated, the config file, `repo_add` / `repo_remove` /
   `repo_list`, `persist`.
3. **Dispatch**: `find(id)` over the set, and the `repo` parameter on the four tools that
   need it. `relate` across the set mirrors (a3v2a4j6h12kse4ypxn0) or refuses.
4. **Tests**: two scratch repositories under one server; an id resolved in each; a prefix
   ambiguous across them refused with both named; `list` with and without `repo`; `create`
   without `repo` refused when two are served; `repo_add` mid-session and the added
   repository's issues then resolving; a config file read at start.
5. **README**: the working set, where it comes from, and the rule.

## Not this

- No index, cache or workspace file of issues. If the search over N repositories' refs ever
  costs too much, the answer is a faster search of the refs, not a copy of them.
- No cross-repository relation spelled any way but an ext reference.
