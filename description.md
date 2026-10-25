# git-issue mcp: attach a file to an issue, as `git issue attach` does

The CLI attaches a file to an issue and issue_show lists an issue's attachments by name, but the MCP server has no tool that attaches one: an agent that has a log, a trace or a diff to keep with an issue has to shell out to `git issue attach`, which an agent on a host without the CLI cannot do.

Add `issue_attach`: the issue, the file (a path on the host's disk, or the content given inline with a name), and the mime type where it cannot be inferred, doing what `git issue attach` does and answering what it answers. Reading one back is the matching half, if the CLI reads attachments; the tool mirrors what the command does and adds no operation.