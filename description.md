# git-issue: migrate cannot read a numeric-id issue, so it skips every legacy issue and re-mints every current one

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced at 546bd53 in a
throwaway repo.

With a numeric-id issue: `show 000001` fails `field "id": expected string, got Number`,
`list` omits it, and `migrate --dry-run` lists only the current issue
("18wcxmgch12ks0dvmdn0 -> 23nexjgdh12ks0dvmdn0", "Will migrate 1 issues").

4508daf hand-edited the generated codec to accept a numeric id ("TEMPORARY: Remove after
migration is complete"); 7253066's regeneration dropped it two days later.

This repository has 0 numeric refs (277 XIDRs), so the migration is done and the command
is obsolete -- but still shipped, and harmful: run for real it migrates nothing and
re-mints every current issue (MigrateCommand is not idempotent, by its own doc),
breaking every recorded id. Remove it, or refuse to run where nothing is legacy.