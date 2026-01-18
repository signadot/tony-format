package commands

import (
	"fmt"
	"strings"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

type showConfig struct {
	*cli.Command
	store issuelib.Store
}

// ShowCommand returns the show subcommand.
func ShowCommand(store issuelib.Store) *cli.Command {
	cfg := &showConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "show").
		WithSynopsis("show <id> - Show issue details").
		WithRun(cfg.run)
}

func (cfg *showConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: usage: git issue show <xidr>", cli.ErrUsage)
	}

	ref, err := cfg.store.FindRef(args[0])
	if err != nil {
		return err
	}

	issue, desc, err := cfg.store.GetByRef(ref)
	if err != nil {
		return err
	}

	// Print issue header
	status := issuelib.StatusFromRef(ref)
	fmt.Fprintf(cc.Out, "Issue %s [%s]\n", issuelib.FormatID(issue.ID), status)
	fmt.Fprintf(cc.Out, "Ref: %s\n", ref)
	if len(issue.Labels) > 0 {
		fmt.Fprintf(cc.Out, "Labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	fmt.Fprintln(cc.Out)

	// Print description
	fmt.Fprintln(cc.Out, desc)
	fmt.Fprintln(cc.Out)

	// Show linked commits
	if len(issue.Commits) > 0 {
		fmt.Fprintln(cc.Out, "Linked commits:")
		for _, commit := range issue.Commits {
			info, _ := cfg.store.GetCommitInfo(commit)
			fmt.Fprintf(cc.Out, "  %s\n", info)
		}
		fmt.Fprintln(cc.Out)
	}

	// Show linked branches
	if len(issue.Branches) > 0 {
		fmt.Fprintln(cc.Out, "Linked branches:")
		for _, branch := range issue.Branches {
			fmt.Fprintf(cc.Out, "  %s\n", branch)
		}
		fmt.Fprintln(cc.Out)
	}

	// Show related issues
	cfg.printRelatedIssues(cc, "Related issues:", issue.RelatedIssues)
	cfg.printRelatedIssues(cc, "Blocks:", issue.Blocks)
	cfg.printRelatedIssues(cc, "Blocked by:", issue.BlockedBy)
	cfg.printRelatedIssues(cc, "Duplicates:", issue.Duplicates)

	// Show discussion and attachments
	cfg.printDiscussion(cc, ref)

	return nil
}

func (cfg *showConfig) printRelatedIssues(cc *cli.Context, title string, xidrs []string) {
	if len(xidrs) == 0 {
		return
	}
	fmt.Fprintln(cc.Out, title)
	for _, xidr := range xidrs {
		ref, err := cfg.store.FindRef(xidr)
		if err != nil {
			fmt.Fprintf(cc.Out, "  %s (not found)\n", xidr)
			continue
		}
		issue, _, err := cfg.store.GetByRef(ref)
		if err != nil {
			fmt.Fprintf(cc.Out, "  %s (error)\n", xidr)
			continue
		}
		status := issuelib.StatusFromRef(ref)
		fmt.Fprintf(cc.Out, "  %s %s[%s]%s %s\n",
			xidr,
			issuelib.StatusColor(status),
			status,
			issuelib.ColorReset,
			issue.Title,
		)
	}
	fmt.Fprintln(cc.Out)
}

func (cfg *showConfig) printDiscussion(cc *cli.Context, ref string) {
	gitStore, ok := cfg.store.(*issuelib.GitStore)
	if !ok {
		return
	}

	tree, err := gitStore.GetTree(ref)
	if err != nil {
		return
	}

	discussionEntry, ok := tree["discussion"]
	if !ok || !strings.HasPrefix(discussionEntry, "tree:") {
		return
	}

	// Read discussion files
	var comments []string
	var attachments []string

	cfg.walkDiscussion(gitStore, ref, "discussion", &comments, &attachments)

	// Show comments
	if len(comments) > 0 {
		fmt.Fprintln(cc.Out, "Discussion:")
		fmt.Fprintln(cc.Out)
		for _, file := range comments {
			content, err := cfg.store.ReadFile(ref, file)
			if err == nil {
				fmt.Fprintf(cc.Out, "--- %s ---\n", file)
				fmt.Fprint(cc.Out, string(content))
				fmt.Fprintln(cc.Out)
			}
		}
	}

	// Show attachments
	if len(attachments) > 0 {
		fmt.Fprintln(cc.Out, "Attachments:")
		for _, file := range attachments {
			fmt.Fprintf(cc.Out, "  %s\n", file)
		}
		fmt.Fprintln(cc.Out)
	}
}

func (cfg *showConfig) walkDiscussion(gitStore *issuelib.GitStore, ref, path string, comments, attachments *[]string) {
	entries, err := gitStore.ListDir(ref, path)
	if err != nil {
		return
	}

	for name, entry := range entries {
		fullPath := path + "/" + name
		typ := strings.Split(entry, ":")[0]

		if typ == "tree" {
			cfg.walkDiscussion(gitStore, ref, fullPath, comments, attachments)
		} else if typ == "blob" {
			if strings.HasSuffix(name, ".md") && !strings.Contains(fullPath, "/files/") {
				*comments = append(*comments, fullPath)
			} else {
				*attachments = append(*attachments, fullPath)
			}
		}
	}
}
