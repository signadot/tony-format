package commands

import (
	"fmt"
	"os/exec"
	"strings"
)

func Pull(args []string) error {
	// Get remote name (default to origin)
	remote := "origin"
	if len(args) > 0 {
		remote = args[0]
	}

	// Verify remote exists
	checkCmd := exec.Command("git", "remote", "get-url", remote)
	if err := checkCmd.Run(); err != nil {
		return fmt.Errorf("remote not found: %s", remote)
	}

	fmt.Printf("Fetching issues from %s...\n", remote)

	// Fetch all issue refs
	refspecs := []string{
		"+refs/issues/*:refs/issues/*",
		"+refs/closed/*:refs/closed/*",
		"+refs/meta/issue-counter:refs/meta/issue-counter",
		"+refs/notes/issues:refs/notes/issues",
	}

	for _, refspec := range refspecs {
		cmd := exec.Command("git", "fetch", remote, refspec)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Don't fail if ref doesn't exist on remote
			if !strings.Contains(string(output), "couldn't find remote ref") {
				fmt.Printf("Warning: failed to fetch %s: %s\n", refspec, string(output))
			}
		}
	}

	// Count how many issues we have now
	listCmd := exec.Command("git", "for-each-ref", "--count=1000", "--format=%(refname)", "refs/issues/*", "refs/closed/*")
	output, err := listCmd.Output()
	if err != nil {
		fmt.Println("Done.")
		return nil
	}

	refs := strings.Split(strings.TrimSpace(string(output)), "\n")
	var count int
	for _, ref := range refs {
		if ref != "" {
			count++
		}
	}

	fmt.Printf("Done. %d issue(s) in local repository.\n", count)
	return nil
}
