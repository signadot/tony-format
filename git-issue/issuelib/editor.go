package issuelib

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// EditInEditor opens the user's editor on initialContent and returns what they
// wrote, in the current working directory. See EditInEditorWithDir.
func EditInEditor(initialContent string) (string, error) {
	return EditInEditorWithDir(initialContent, "")
}

// EditInEditorWithDir opens the user's editor -- $VISUAL, else $EDITOR, else the
// first of vim, vi or nano on PATH -- on a temporary file holding
// initialContent, and returns the result once the editor exits. It errors if the
// editor exits non-zero; an empty result is not an error, and callers that
// require text are expected to reject it themselves.
//
// Lines whose first non-space character is "#" are stripped, so a prompt can
// carry instructions the user does not have to delete. The result is then
// trimmed. Note that this takes markdown ATX headings with it: text that must
// survive cannot start a line with "#".
//
// The editor runs in workDir when it is set, which is how commands hand it a
// directory of context -- an exported copy of the issue -- for the user to read
// from inside the editor. An empty workDir leaves the process's own.
func EditInEditorWithDir(initialContent, workDir string) (string, error) {
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
	if workDir != "" {
		cmd.Dir = workDir
	}

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

// getEditor returns the user's preferred editor, falling back to vi.
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
