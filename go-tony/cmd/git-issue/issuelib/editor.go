package issuelib

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// EditInEditor opens the user's preferred editor with initial content
// and returns the edited content. Returns error if editor exits non-zero
// or if the content is empty/unchanged.
func EditInEditor(initialContent string) (string, error) {
	// Create temp file
	tmpFile, err := os.CreateTemp("", "git-issue-*.md")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Write initial content
	if _, err := tmpFile.WriteString(initialContent); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	// Get editor
	editor := getEditor()

	// Open editor
	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	// Read result
	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to read temp file: %w", err)
	}

	// Strip comment lines (lines starting with #)
	lines := strings.Split(string(content), "\n")
	var resultLines []string
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			resultLines = append(resultLines, line)
		}
	}

	result := strings.TrimSpace(strings.Join(resultLines, "\n"))
	return result, nil
}

// getEditor returns the user's preferred editor
func getEditor() string {
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	// Try common editors
	for _, editor := range []string{"vim", "vi", "nano"} {
		if _, err := exec.LookPath(editor); err == nil {
			return editor
		}
	}
	return "vi" // fallback
}
