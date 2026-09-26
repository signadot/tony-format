# git-issue mcp: repo_remove leaves the removed repository's issues listed as resources

After repo_remove, the server's look skipped the removed repository but kept its last look in m.seen and never resynced, so its open issues stayed registered as issue:// resources (and in resources/list) until some other change triggered a resync. issue_show already refused them, so the listing disagreed with the tools.

Fix: look drops the last look of any repository no longer served and resyncs the listing. Nothing is announced for its issues: they are where they were.