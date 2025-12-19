package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
)

func Create(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: git issue create <title>")
	}

	title := strings.Join(args, " ")

	repo := &GitRepo{}

	// Get next issue ID
	id, err := repo.GetNextIssueID()
	if err != nil {
		return fmt.Errorf("failed to allocate issue ID: %w", err)
	}

	// Prompt for description
	fmt.Printf("Creating issue #%06d: %s\n", id, title)
	fmt.Println("Enter description (end with Ctrl+D):")
	fmt.Println()

	var descLines []string
	descLines = append(descLines, "# "+title)
	descLines = append(descLines, "")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		descLines = append(descLines, scanner.Text())
	}

	description := strings.Join(descLines, "\n")

	// Create issue metadata
	issue := &Issue{
		ID:       id,
		Status:   "open",
		Created:  time.Now(),
		Updated:  time.Now(),
		Commits:  []string{},
		Branches: []string{},
		ClosedBy: nil,
	}

	// Convert to tony format
	metaNode := issue.MetaToNode()
	metaContent := encode.MustString(metaNode)

	// Create git ref
	if err := repo.CreateIssueRef(id, metaContent, description); err != nil {
		return fmt.Errorf("failed to create issue: %w", err)
	}

	fmt.Printf("\nCreated issue #%06d\n", id)
	fmt.Printf("Ref: refs/issues/%06d\n", id)

	return nil
}
