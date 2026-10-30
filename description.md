# git-issue: push, pull, close, label and unlabel take a list of ids, not one

Today each of these takes at most one issue:

- `git issue close <xidr> [--commit <sha>]` closes one issue.
- `git issue label <xidr> <label> [label...]` and `git issue unlabel <xidr> <label> [label...]` act on one issue.
- `git issue push [<xidr>] [remote]` pushes one issue, or every issue when none is named.
- `git issue pull [remote]` has no per-issue form at all: a pull is the whole repository.

The MCP tools mirror this: `issue_close`, `issue_label` and `issue_push` each take a single `id` (`issue_push` takes it as optional, empty meaning every issue), and `issue_pull` takes none.

Closing several issues with one commit, labelling a batch, or pushing the handful of issues a branch touched means one invocation per issue, and an agent does the same over the MCP, one call each.

## Change

Each of these takes a list of ids (full ids or unambiguous prefixes), applied in order:

- `git issue close <xidr>... [--commit <sha>]`: every listed issue closed, all by the same commit when one is given.
- `git issue label <xidr>... <label>...` and `unlabel`: the labels applied to every listed issue.
- `git issue push [<xidr>...] [remote]`: the listed issues pushed; none still means every issue.
- `git issue pull [<xidr>...] [remote]`: the listed issues pulled; none still means the whole repository.

MCP: `issue_close`, `issue_label`, `issue_push` and `issue_pull` take `ids` (a list) in place of `id`. The outputs become lists too (a `statusOut`/`labelsOut` per issue; push and pull already report per issue).

## Points to settle in the design

- **Separating ids from labels and the remote.** `label` and `unlabel` put labels after the id positionally, and `push`/`pull` put the remote after the id; with a list of ids the boundary is no longer at `args[1]`. Ids resolve as xidr prefixes and labels are lowercased words, so a word that resolves to no issue is not a reliable boundary. Pick one: labels and the remote move behind flags (`--add`/`--remove` as the MCP already has them; `--remote`), or a separator, or ids become the tail. Whatever is picked, the CLI and the MCP say the same thing.
- **Partial failure.** An id that resolves to nothing, or an issue already closed, is found before anything is written where it can be (resolve all ids first), so a batch does not half-apply because of a typo in the third id. What cannot be known up front (a push refusal for one issue) is reported per issue, as push already does, and the rest proceed.
- **Compatibility.** The single-id forms stay valid as the one-element case. On the MCP, `id` can be accepted alongside `ids` for a release, or dropped at once; say which in the change.

## Done when

- The five CLI commands and the four MCP tools accept lists as above, with tests covering a multi-id call for each, an unresolvable id in the middle of a batch, and `close --commit` across several issues.
- The MCP tool descriptions and the CLI usage strings describe the list forms.