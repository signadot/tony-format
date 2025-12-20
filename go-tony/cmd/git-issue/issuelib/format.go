package issuelib

import (
	"fmt"
	"strconv"
	"strings"
)

// ANSI color codes
const (
	ColorGreen = "\033[32m"
	ColorGray  = "\033[90m"
	ColorReset = "\033[0m"
)

// FormatID formats an issue ID as a 6-digit zero-padded string.
func FormatID(id int64) string {
	return fmt.Sprintf("%06d", id)
}

// ParseID parses an issue ID string to int64.
func ParseID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid issue ID: %s", s)
	}
	return id, nil
}

// RefForID returns the ref path for an open issue.
func RefForID(id int64) string {
	return fmt.Sprintf("refs/issues/%s", FormatID(id))
}

// ClosedRefForID returns the ref path for a closed issue.
func ClosedRefForID(id int64) string {
	return fmt.Sprintf("refs/closed/%s", FormatID(id))
}

// IsClosedRef returns true if the ref is for a closed issue.
func IsClosedRef(ref string) bool {
	return strings.HasPrefix(ref, "refs/closed/")
}

// IDFromRef extracts the issue ID from a ref path.
func IDFromRef(ref string) (int64, error) {
	if idStr, ok := strings.CutPrefix(ref, "refs/issues/"); ok {
		return ParseID(idStr)
	}
	if idStr, ok := strings.CutPrefix(ref, "refs/closed/"); ok {
		return ParseID(idStr)
	}
	return 0, fmt.Errorf("invalid issue ref: %s", ref)
}

// StatusFromRef determines the status based on the ref path.
func StatusFromRef(ref string) string {
	if IsClosedRef(ref) {
		return "closed"
	}
	return "open"
}

// StatusColor returns the ANSI color code for a status.
func StatusColor(status string) string {
	if status == "open" {
		return ColorGreen
	}
	return ColorGray
}

// FormatOneLiner formats an issue as a one-line summary.
func FormatOneLiner(issue *Issue) string {
	status := issue.Status
	if IsClosedRef(issue.Ref) {
		status = "closed"
	}
	return fmt.Sprintf("#%s %s[%s]%s %s",
		FormatID(issue.ID),
		StatusColor(status),
		status,
		ColorReset,
		issue.Title,
	)
}

// Contains checks if a string slice contains a value.
func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
