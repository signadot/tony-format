# git-issue mcp: extract an issue's attachments into a directory

The MCP server can attach a file to an issue (issue_attach, v0.3.9) but cannot give one back: issue_show and the issue:// resources list attachments by name only. An agent without a shell cannot get at a log or trace someone attached.

The agent says where the files go, so this is not a viewer: a tool would write an issue's attachments, or one of them, or a directory of them, into a directory on this machine that the agent names, each at its path under discussion/files/, and answer the paths it wrote. Bytes are written as stored, so a binary attachment survives.

Rules:
- The attachment is named as issue_show lists it, or relative to discussion/files/ as issue_attach takes it; none means all of them.
- A name that climbs out of the directory is refused, as attach refuses one on the way in; an attachment arriving from another clone is not trusted to stay inside.
- A file already at a destination is refused, naming it, and nothing is written: the tool does not overwrite what is on the machine.
- A mirror's attachments extract like any issue's: reading is not writing to it.