# friction: a mirror refusal names the source's path; push with no id is every issue; list outside a repository says so

Three from driving v0.2.0 by hand (2026-09-26):

1. A write to a mirror is refused as "is a mirror of tony's issue: it is edited there, not
   here" -- naming the source but not where it is. The source's path or URL is known
   (sources/<source>); say it, so the person can go there.

2. `git issue push --dry-run` answers a usage error: push wants an id or --all, and a dry run
   of "everything" is the first thing anyone types. No id means every issue, as pull means
   the whole repository; --all stays and means the same.

3. `git issue list` outside a repository answers "No issues found", which is true and
   misleading: there is no repository to have issues. Say that.