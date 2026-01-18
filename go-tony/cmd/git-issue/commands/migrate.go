package commands

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
	"github.com/signadot/tony-format/go-tony/encode"
)

type migrateConfig struct {
	*cli.Command
	store  issuelib.Store
	DryRun bool `cli:"name=dry-run aliases=n desc='show what would be done without making changes'"`
}

// MigrateCommand returns the migrate subcommand.
func MigrateCommand(store issuelib.Store) *cli.Command {
	cfg := &migrateConfig{store: store}
	opts, _ := cli.StructOpts(cfg)
	return cli.NewCommandAt(&cfg.Command, "migrate").
		WithSynopsis("migrate [--dry-run] - Migrate issues from numeric IDs to XIDs").
		WithOpts(opts...).
		WithRun(cfg.run)
}

func (cfg *migrateConfig) run(cc *cli.Context, args []string) error {
	args, err := cfg.Parse(cc, args)
	if err != nil {
		return err
	}

	// Step 1: List all issues (open and closed)
	issues, err := cfg.store.List(true) // includeAll=true
	if err != nil {
		return fmt.Errorf("failed to list issues: %w", err)
	}

	if len(issues) == 0 {
		fmt.Fprintln(cc.Out, "No issues to migrate")
		return nil
	}

	// Step 2: Sort by created time (oldest first for consistent XID generation)
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].Created.Before(issues[j].Created)
	})

	// Step 3: Generate XIDs and build mapping
	type mapping struct {
		oldIDStr   string // padded old ID like "000001"
		oldRef     string
		newXIDR    string // reversed XID for display/storage
		rawXID     issuelib.XID
		isClosed   bool
	}

	mappings := make([]mapping, len(issues))
	oldToNew := make(map[string]string) // "000001" -> "abc123..."

	fmt.Fprintln(cc.Out, "Generating XIDs...")
	for i, issue := range issues {
		xid := issuelib.NewXID(issue.Created)
		xidr := xid.XIDR()

		// Extract old ID from ref path (e.g., "refs/issues/000001" -> "000001")
		oldIDStr, err := issuelib.XIDRFromRef(issue.Ref)
		if err != nil {
			// This shouldn't happen, but fall back to issue.ID
			oldIDStr = issue.ID
		}

		mappings[i] = mapping{
			oldIDStr: oldIDStr,
			oldRef:   issue.Ref,
			newXIDR:  xidr,
			rawXID:   xid,
			isClosed: issuelib.IsClosedRef(issue.Ref),
		}
		oldToNew[oldIDStr] = xidr

		fmt.Fprintf(cc.Out, "  %s -> %s (created: %s)\n",
			oldIDStr,
			xidr,
			issue.Created.Format("2006-01-02"),
		)
	}

	// Step 4: Count cross-references that will be updated
	crossRefCount := 0
	for _, issue := range issues {
		crossRefCount += len(issue.RelatedIssues)
		crossRefCount += len(issue.Blocks)
		crossRefCount += len(issue.BlockedBy)
		crossRefCount += len(issue.Duplicates)
	}

	// Count git notes that need updating
	noteCount := 0
	for _, issue := range issues {
		for _, commit := range issue.Commits {
			notes, err := cfg.store.GetNotes(commit)
			if err == nil && notes != "" {
				noteCount++
			}
		}
	}

	fmt.Fprintf(cc.Out, "\nWill update %d cross-references\n", crossRefCount)
	fmt.Fprintf(cc.Out, "Will update up to %d git notes\n", noteCount)
	fmt.Fprintf(cc.Out, "Will migrate %d issues\n", len(issues))

	if cfg.DryRun {
		fmt.Fprintln(cc.Out, "\n--dry-run specified, no changes made")
		return nil
	}

	// Step 5: Migrate each issue
	fmt.Fprintln(cc.Out, "\nMigrating issues...")
	for i, m := range mappings {
		issue := issues[i]

		// Update cross-references
		issue.RelatedIssues = translateRefs(issue.RelatedIssues, oldToNew)
		issue.Blocks = translateRefs(issue.Blocks, oldToNew)
		issue.BlockedBy = translateRefs(issue.BlockedBy, oldToNew)
		issue.Duplicates = translateRefs(issue.Duplicates, oldToNew)

		// Update issue ID to new XIDR and generate proper meta.tony using ToTonyIR
		issue.ID = m.newXIDR
		metaNode, err := issue.ToTonyIR()
		if err != nil {
			return fmt.Errorf("failed to serialize issue %s: %w", m.oldIDStr, err)
		}
		metaContent := encode.MustString(metaNode)

		// Read description.md
		descContent, err := cfg.store.ReadFile(m.oldRef, "description.md")
		if err != nil {
			return fmt.Errorf("failed to read description for issue %s: %w", m.oldIDStr, err)
		}

		// Read any other files (discussion/, etc.)
		tree, err := cfg.store.ListDir(m.oldRef, "")
		if err != nil {
			return fmt.Errorf("failed to get tree for issue %s: %w", m.oldIDStr, err)
		}

		// Build new tree
		files := make(map[string][]byte)
		files["meta.tony"] = []byte(metaContent)
		files["description.md"] = descContent

		// Copy other files from the tree
		if err := copyTreeFiles(cfg.store, m.oldRef, tree, files, ""); err != nil {
			return fmt.Errorf("failed to copy tree files: %w", err)
		}

		// Determine new ref path
		var newRef string
		if m.isClosed {
			newRef = fmt.Sprintf("refs/closed/%s", m.newXIDR)
		} else {
			newRef = fmt.Sprintf("refs/issues/%s", m.newXIDR)
		}

		// Create the new ref with all files
		if err := createRefWithFiles(m.oldRef, newRef, files, "migrate: convert to XID"); err != nil {
			return fmt.Errorf("failed to create new ref for issue %s: %w", m.oldIDStr, err)
		}

		// Delete old ref
		deleteCmd := exec.Command("git", "update-ref", "-d", m.oldRef)
		if err := deleteCmd.Run(); err != nil {
			return fmt.Errorf("failed to delete old ref %s: %w", m.oldRef, err)
		}

		fmt.Fprintf(cc.Out, "  Migrated %s -> %s\n", m.oldIDStr, m.newXIDR)
	}

	// Step 6: Update git notes
	fmt.Fprintln(cc.Out, "\nUpdating git notes...")
	notesUpdated := 0
	for _, issue := range issues {
		for _, commit := range issue.Commits {
			notes, err := cfg.store.GetNotes(commit)
			if err != nil || notes == "" {
				continue
			}

			// Translate IDs in notes
			lines := strings.Split(notes, "\n")
			changed := false
			for i, line := range lines {
				line = strings.TrimSpace(line)
				if newXIDR, ok := oldToNew[line]; ok {
					lines[i] = newXIDR
					changed = true
				}
			}

			if changed {
				// Remove and re-add the note
				removeCmd := exec.Command("git", "notes", "--ref=refs/notes/issues", "remove", commit)
				removeCmd.Run() // Ignore error if note doesn't exist

				newContent := strings.Join(lines, "\n")
				addCmd := exec.Command("git", "notes", "--ref=refs/notes/issues", "add", "-m", newContent, commit)
				if err := addCmd.Run(); err != nil {
					fmt.Fprintf(cc.Out, "  Warning: failed to update note for %s: %v\n", commit[:7], err)
				} else {
					notesUpdated++
				}
			}
		}
	}
	fmt.Fprintf(cc.Out, "Updated %d git notes\n", notesUpdated)

	// Step 7: Delete counter ref
	fmt.Fprintln(cc.Out, "\nRemoving issue counter...")
	deleteCounterCmd := exec.Command("git", "update-ref", "-d", "refs/meta/issue-counter")
	if err := deleteCounterCmd.Run(); err != nil {
		fmt.Fprintf(cc.Out, "  Warning: failed to delete counter ref: %v\n", err)
	} else {
		fmt.Fprintln(cc.Out, "  Deleted refs/meta/issue-counter")
	}

	fmt.Fprintf(cc.Out, "\nMigration complete! %d issues migrated to XID format.\n", len(issues))
	fmt.Fprintln(cc.Out, "\nNote: You can now reference issues by their XID prefix, e.g.:")
	if len(mappings) > 0 {
		fmt.Fprintf(cc.Out, "  git issue show %s\n", mappings[0].newXIDR[:6])
	}

	return nil
}

// translateRefs translates old numeric IDs to new XIDs.
func translateRefs(refs []string, oldToNew map[string]string) []string {
	result := make([]string, len(refs))
	for i, ref := range refs {
		if newXIDR, ok := oldToNew[ref]; ok {
			result[i] = newXIDR
		} else {
			result[i] = ref // Keep as-is if not found
		}
	}
	return result
}

// copyTreeFiles recursively copies files from a tree to the files map.
func copyTreeFiles(store issuelib.Store, ref string, tree map[string]string, files map[string][]byte, prefix string) error {
	for name, typeHash := range tree {
		fullPath := name
		if prefix != "" {
			fullPath = prefix + "/" + name
		}

		// Skip meta.tony and description.md (we handle those separately)
		if fullPath == "meta.tony" || fullPath == "description.md" {
			continue
		}

		parts := strings.SplitN(typeHash, ":", 2)
		if len(parts) != 2 {
			continue
		}
		typ := parts[0]

		if typ == "blob" {
			content, err := store.ReadFile(ref, fullPath)
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", fullPath, err)
			}
			files[fullPath] = content
		} else if typ == "tree" {
			// Recurse into subdirectory
			subTree, err := store.ListDir(ref, fullPath)
			if err != nil {
				return fmt.Errorf("failed to list dir %s: %w", fullPath, err)
			}
			if err := copyTreeFiles(store, ref, subTree, files, fullPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// createRefWithFiles creates a new ref with the given files, using the old ref as parent.
func createRefWithFiles(oldRef, newRef string, files map[string][]byte, message string) error {
	// Get current commit from old ref as parent
	showCmd := exec.Command("git", "show-ref", oldRef)
	showOut, err := showCmd.Output()
	if err != nil {
		return fmt.Errorf("old ref not found: %s", oldRef)
	}
	parentCommit := strings.Fields(string(showOut))[0]

	// For files with nested paths, use update-index approach
	tmpIndex := fmt.Sprintf("/tmp/git-issue-migrate-%d", time.Now().UnixNano())
	defer os.Remove(tmpIndex)

	indexEnv := append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex)

	// Start fresh - read parent tree
	readTreeCmd := exec.Command("git", "read-tree", parentCommit)
	readTreeCmd.Env = indexEnv
	readTreeCmd.Run() // Ignore error - parent might not have a tree

	// Add all files to index
	for path, content := range files {
		hashCmd := exec.Command("git", "hash-object", "-w", "--stdin")
		hashCmd.Stdin = strings.NewReader(string(content))
		hashOut, err := hashCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to hash %s: %w", path, err)
		}
		hash := strings.TrimSpace(string(hashOut))

		updateCmd := exec.Command("git", "update-index", "--add", "--cacheinfo", "100644", hash, path)
		updateCmd.Env = indexEnv
		if err := updateCmd.Run(); err != nil {
			return fmt.Errorf("failed to add %s to index: %w", path, err)
		}
	}

	// Write tree
	writeTreeCmd := exec.Command("git", "write-tree")
	writeTreeCmd.Env = indexEnv
	treeOut, err := writeTreeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to write tree: %w", err)
	}
	treeHash := strings.TrimSpace(string(treeOut))

	// Create commit with parent
	commitCmd := exec.Command("git", "commit-tree", treeHash, "-p", parentCommit, "-m", message)
	commitOut, err := commitCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to create commit: %w", err)
	}
	commitHash := strings.TrimSpace(string(commitOut))

	// Create new ref
	updateCmd := exec.Command("git", "update-ref", newRef, commitHash)
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("failed to create ref %s: %w", newRef, err)
	}

	return nil
}
