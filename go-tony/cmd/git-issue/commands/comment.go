package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

func Comment(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: git issue comment <id> [text]")
	}

	idStr := args[0]
	var commentText string

	// Parse ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid issue ID: %s", idStr)
	}

	// Get comment text from args or stdin
	if len(args) > 1 {
		commentText = strings.Join(args[1:], " ")
	} else {
		// Read from stdin
		fmt.Println("Enter comment (end with Ctrl+D):")
		scanner := bufio.NewScanner(os.Stdin)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		commentText = strings.Join(lines, "\n")
	}

	if strings.TrimSpace(commentText) == "" {
		return fmt.Errorf("comment cannot be empty")
	}

	repo := &GitRepo{}

	// Try open issues first
	ref := fmt.Sprintf("refs/issues/%06d", id)
	_, _, err = repo.ReadIssue(ref)
	if err != nil {
		// Try closed issues
		ref = fmt.Sprintf("refs/closed/%06d", id)
		_, _, err = repo.ReadIssue(ref)
		if err != nil {
			return fmt.Errorf("issue not found: %06d", id)
		}
	}

	// Get next comment number by counting existing discussion files
	commentNum := 1
	treeCmd := exec.Command("git", "cat-file", "-p", ref+"^{tree}")
	treeOut, err := treeCmd.Output()
	if err == nil {
		// Find discussion tree hash
		var discussionTreeHash string
		scanner := bufio.NewScanner(strings.NewReader(string(treeOut)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "discussion") && strings.Contains(line, "tree") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					discussionTreeHash = parts[2]
					break
				}
			}
		}

		// Count files in discussion tree
		if discussionTreeHash != "" {
			discCmd := exec.Command("git", "cat-file", "-p", discussionTreeHash)
			discOut, err := discCmd.Output()
			if err == nil {
				lines := strings.Split(string(discOut), "\n")
				for _, line := range lines {
					if strings.TrimSpace(line) != "" {
						commentNum++
					}
				}
			}
		}
	}

	// Create comment file content with timestamp
	timestamp := time.Now().Format(time.RFC3339)
	commentContent := fmt.Sprintf("<!-- Comment %03d - %s -->\n\n%s\n", commentNum, timestamp, commentText)

	// Update issue with new comment
	commentFile := fmt.Sprintf("discussion/%03d.md", commentNum)

	// Read current meta.tony to update timestamp
	metaCmd := exec.Command("git", "show", ref+":meta.tony")
	metaOut, err := metaCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to read meta.tony: %w", err)
	}

	metaNode, err := parse.Parse(metaOut)
	if err != nil {
		return fmt.Errorf("failed to parse meta.tony: %w", err)
	}

	issue, err := NodeToMeta(metaNode)
	if err != nil {
		return fmt.Errorf("failed to parse issue metadata: %w", err)
	}

	issue.Updated = time.Now()

	newMetaNode := issue.MetaToNode()
	newMetaContent := encode.MustString(newMetaNode)

	// Prepare updates
	updates := map[string]string{
		commentFile: commentContent,
		"meta.tony": newMetaContent,
	}

	// Create commit message from first line of comment
	firstLine := strings.Split(commentText, "\n")[0]
	if len(firstLine) > 60 {
		firstLine = firstLine[:57] + "..."
	}
	commitMsg := fmt.Sprintf("comment: %s", firstLine)

	if err := repo.UpdateIssueCommitByRef(ref, commitMsg, updates); err != nil {
		return fmt.Errorf("failed to add comment: %w", err)
	}

	fmt.Printf("Added comment #%03d to issue #%06d\n", commentNum, id)

	return nil
}
