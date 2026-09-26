# mcp: the starting set is the union of -C, the config and the working directory, not the first of them

## What happens

`git issue mcp` takes its working set from the first of `-C`, `~/.config/git-issue.tony`, the
working directory: a config file present means the repository the server was started in is not
served unless it is also in the config, and a `-C` means the config is ignored. Read as
precedence when it was written; wanted as a union (7qfhwth7h12ksxcwpxn0 said "in order", which
was the order of listing, not of winning).

## The fix

The set is every repository the three name, in that order, once each: `-C` repositories, then
the configured ones, then the working directory when it is a repository. A repository named
twice is served once, under the first name it came in with. With none of the three the server
refuses, as before.