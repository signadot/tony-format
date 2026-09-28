# git-issue README: the set's read/sync commands, and serve

The README says "a command runs on the repository it is run in", and names only a command given an issue as reaching past that. Since 185e64bb, outside a repository, list, watch, pull and push cover every repository of the set. It also does not mention `git issue serve` or `serve -watch` (42a1429c, 0a090f51).

Done: the README says both in a line or two, and points to docs/commands.md for the rest.