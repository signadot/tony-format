package commands

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

type migrateCommentsConfig struct {
	*cli.Command
	store    issuelib.Store
	Apply    bool `cli:"name=apply desc='rewrite refs (default: dry-run preview only)'"`
	NoBackup bool `cli:"name=no-backup desc='skip creating refs/issue-backup/<ts>/ safety refs before rewriting'"`
}

// MigrateCommentsCommand renames legacy discussion/NNN.md comment files to the
// collision-free content-addressed scheme (discussion/<ts>-<hash>.md). It is
// dry-run by default; --apply performs the rewrite, backing each rewritten ref up
// to refs/issue-backup/<runts>/ first (unless --no-backup). It is idempotent:
// already-migrated comments are left untouched, so it is safe to re-run.
func MigrateCommentsCommand(store issuelib.Store) *cli.Command {
	cfg := &migrateCommentsConfig{store: store}
	opts, _ := cli.StructOpts(cfg)
	return cli.NewCommandAt(&cfg.Command, "migrate-comments").
		WithSynopsis("migrate-comments [--apply] - Rename discussion/NNN.md comments to collision-free names").
		WithOpts(opts...).
		WithRun(cfg.run)
}

func (cfg *migrateCommentsConfig) run(cc *cli.Context, args []string) error {
	if _, err := cfg.Parse(cc, args); err != nil {
		return err
	}

	gitStore, ok := cfg.store.(*issuelib.GitStore)
	if !ok {
		return fmt.Errorf("migrate-comments requires the git store")
	}

	refs, err := cfg.store.ListRefs(true) // include closed
	if err != nil {
		return fmt.Errorf("failed to list issue refs: %w", err)
	}
	sort.Strings(refs)

	runTS := time.Now().UTC().Format(commentTSLayout)
	totalRenames, changedRefs := 0, 0

	for _, ref := range refs {
		renames, warnings, err := planRenames(gitStore, ref)
		if err != nil {
			return fmt.Errorf("%s: %w", ref, err)
		}
		for _, w := range warnings {
			fmt.Fprintf(cc.Err, "warning: %s: %s\n", ref, w)
		}
		if len(renames) == 0 {
			continue
		}
		changedRefs++
		totalRenames += len(renames)

		// Deterministic preview order.
		olds := make([]string, 0, len(renames))
		for o := range renames {
			olds = append(olds, o)
		}
		sort.Strings(olds)
		fmt.Fprintf(cc.Out, "%s (%d):\n", ref, len(renames))
		for _, o := range olds {
			fmt.Fprintf(cc.Out, "  %s -> %s\n",
				strings.TrimPrefix(o, "discussion/"), strings.TrimPrefix(renames[o], "discussion/"))
		}

		if !cfg.Apply {
			continue
		}

		if !cfg.NoBackup {
			backupRef := "refs/issue-backup/" + runTS + "/" + strings.TrimPrefix(ref, "refs/")
			commit, err := cfg.store.GetRefCommit(ref)
			if err != nil {
				return fmt.Errorf("%s: reading commit for backup: %w", ref, err)
			}
			if err := exec.Command("git", "update-ref", backupRef, commit).Run(); err != nil {
				return fmt.Errorf("%s: creating backup ref %s: %w", ref, backupRef, err)
			}
		}

		files, err := rewriteTree(gitStore, ref, renames)
		if err != nil {
			return fmt.Errorf("%s: %w", ref, err)
		}
		if err := gitStore.ReplaceTree(ref, "migrate-comments: collision-free comment filenames", files); err != nil {
			return fmt.Errorf("%s: rewriting tree: %w", ref, err)
		}
	}

	fmt.Fprintln(cc.Out)
	if cfg.Apply {
		fmt.Fprintf(cc.Out, "Migrated %d comment(s) across %d issue(s).\n", totalRenames, changedRefs)
		if !cfg.NoBackup && changedRefs > 0 {
			fmt.Fprintf(cc.Out, "Backups at refs/issue-backup/%s/ (restore with git update-ref, or delete once verified).\n", runTS)
		}
	} else {
		fmt.Fprintf(cc.Out, "Dry run: %d comment(s) across %d issue(s) would be renamed. Re-run with --apply.\n",
			totalRenames, changedRefs)
	}
	return nil
}

// planRenames computes old->new comment path renames for one ref. It never
// mutates. warnings lists comments that could not be migrated (e.g. no parseable
// timestamp) and are left as-is.
func planRenames(store *issuelib.GitStore, ref string) (renames map[string]string, warnings []string, err error) {
	tree, err := store.ListDir(ref, "discussion")
	if err != nil {
		return nil, nil, nil // no discussion dir → nothing to do
	}

	renames = make(map[string]string)
	taken := make(map[string]string) // newPath -> sourcePath, to catch genuine collisions
	for name, entry := range tree {
		if !strings.HasPrefix(entry, "blob:") {
			continue // skip files/ subtree (attachments) and any nested trees
		}
		path := "discussion/" + name
		if !isCommentFile(path) || isMigratedCommentName(path) {
			continue
		}
		content, rerr := store.ReadFile(ref, path)
		if rerr != nil {
			return nil, nil, fmt.Errorf("reading %s: %w", path, rerr)
		}
		ts, ok := parseCommentTime(string(content))
		if !ok {
			warnings = append(warnings, fmt.Sprintf("%s has no parseable timestamp; left as-is", name))
			continue
		}
		newPath := commentFileName(ts, string(content))
		if src, exists := taken[newPath]; exists {
			// Content-addressed, so this only happens on byte-identical content at
			// the same second — a true duplicate. Refuse to silently drop either.
			return nil, nil, fmt.Errorf("%s and %s map to the same name %s (identical content); resolve manually",
				src, path, newPath)
		}
		taken[newPath] = path
		renames[path] = newPath
	}
	return renames, warnings, nil
}

// rewriteTree reads every file under ref and returns the full file set with the
// given comment paths renamed (content preserved byte-for-byte).
func rewriteTree(store *issuelib.GitStore, ref string, renames map[string]string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	// meta.tony and description.md are carried through unchanged; copyTreeFiles
	// skips them by design, so read them explicitly.
	for _, top := range []string{"meta.tony", "description.md"} {
		if b, err := store.ReadFile(ref, top); err == nil {
			files[top] = b
		}
	}
	tree, err := store.ListDir(ref, "")
	if err != nil {
		return nil, fmt.Errorf("reading tree: %w", err)
	}
	if err := copyTreeFiles(store, ref, tree, files, ""); err != nil {
		return nil, err
	}
	for old, next := range renames {
		if b, ok := files[old]; ok {
			files[next] = b
			delete(files, old)
		}
	}
	return files, nil
}
