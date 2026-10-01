# git-issue: export and import refuse every repository ("export requires GitStore")

`git issue export <id> <dir>` fails with `export requires GitStore`, and `git issue import <dir>` with `import requires GitStore`, in any repository.

Both assert `store.(*issuelib.GitStore)` (`commands/export.go` in `exportDir` and `ExportToTempDir`, and `commands/import.go`). Since the repository-set work (185e64bb), the store the commands share is a `*targetStore` (`commands/set.go`) wrapping the repository's store, so the assertion never holds.

Found trying to export a verse issue (j5vbwvja) to rewrite it and import it back.

Fix: assert on the store the target points at. Test export and import through the CLI's own dispatch (Root), not with a bare GitStore, since that is how the regression went unseen.