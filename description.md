# git-issue mcp: several repositories in one server, as a view that holds nothing and keeps a relation within one repository

## Why

`git issue mcp` (1k3x9sp6h12ks5c0pxn0) is one server for one repository: `GitStore` runs git in the
working directory, `-C` picks another, and outside any repository the first tool call fails.
A person working across repositories -- tony-format and verse, say -- runs one server per
repository under different names, and the agent has to know which server to ask about an
issue it was only given the id of.

The ids already answer that. An XIDR is unique across repositories (machine, time, counter),
so an id names its repository without saying so: a server given several can find which one
holds it by looking, and a prefix that is unambiguous across all of them still works.

## The rule

**The server is a view and holds nothing.** It is N stores, each on its own repository's refs,
and what it adds is dispatch. Which repository holds an id is answered by searching the refs
every time -- derived, never kept. The list of repositories is the host's configuration, the
same kind of thing as the host's list of servers, and lives nowhere in git because it need
not survive anything: delete it and every issue is exactly where it was. The test of any
addition here is that nothing the server knows is unrecoverable from the repositories. An
id-to-repository index, a cache, a "workspace" file beside the repositories would each fail
it: a clone does not carry them, and two machines can then disagree.

**A relation stays within one repository.** `issue_relate` across repositories would write a
bare id into A's meta.tony that resolves only where B is also present; a clone of A alone
cannot follow it and reads it as "not found". The repository would no longer be
self-contained, which is what the distributed model is. So a relation -- related, blocks,
duplicate -- whose two ids resolve to different repositories is refused, naming both. A
reference that crosses repositories, if it is ever wanted, is a question for the format (a
reference that names its repository, and what a clone without that repository does with it),
not one a server answers by writing what it likes.

## Shape

    git issue mcp -C ~/src/tony-format -C ~/src/verse     # repeatable; each a repository

With one `-C`, or none inside a repository, nothing changes. With none outside a repository,
refuse and say so.

- **Tools taking an id** (`show`, `edit`, `comment`, `label`, `close`, `reopen`, `link`,
  `relate`, `for_commit`) resolve it across the repositories; an id found in none is "not
  found", one found in more than one -- a prefix -- is ambiguous, naming the matches with
  their repositories. `link` and `for_commit` take a commit too, which is looked up in the
  repository the id resolved to.
- **Tools that need a repository named** take `repo`: `create` (required, when more than one
  repository is served), `list` (none lists all, each row saying its repository), `push` and
  `pull` (required). `repo` is the directory's base name unless two collide, in which case
  the full path; the server says which in `issue_list`'s rows and its instructions.
- **Resources** (eg8zmb1sh12ksr48pxn0) fold the same way: `issue://<xidr>` resolves by
  search, `issue://` lists across repositories.

## What it takes

1. **`GitStore` takes a directory.** It acts on the process's working directory today, which
   is why `-C` is an `os.Chdir` and why nothing can hold two. A store made for a directory
   runs `git -C <dir>` for everything, and the commands, the tests (which chdir per test
   today, and so cannot run in parallel) and the server all get the same store. This is the
   step everything else rests on, and worth doing on its own.
2. **A multi-store in the server**: the list of stores, `find(id)` over them, and the `repo`
   parameter on the four tools that need it. The rule above is enforced in `relate`: both ids
   resolved, different stores, refused.
3. **Tests**: two scratch repositories under one server; an id resolved in each; a prefix
   ambiguous across them refused with both named; a relation across them refused; `list`
   with and without `repo`; `create` without `repo` refused when two are served.
4. **README**: the `-C` list and the rule.

## Not this

- No index, cache or workspace file. If the search over N repositories' refs ever costs too
  much, the answer is a faster search of the refs, not a copy of them.
- No cross-repository relation, however spelled, until the format says what one is.