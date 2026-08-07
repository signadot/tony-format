package issuelib

import (
	"fmt"
	"strconv"
	"strings"
)

// ANSI color codes used in issue listings. They are written unconditionally;
// git-issue does not check whether its output is a terminal.
const (
	ColorGreen = "\033[32m"
	ColorGray  = "\033[90m"
	ColorReset = "\033[0m"
)

// FormatID renders an issue ID for display. A legacy numeric ID is zero-padded
// to six digits; anything else, XIDRs included, is returned unchanged.
func FormatID(id string) string {
	// If it looks like a legacy numeric ID, format it
	if _, err := strconv.ParseInt(id, 10, 64); err == nil {
		if n, _ := strconv.ParseInt(id, 10, 64); n > 0 && n < 1000000 {
			return fmt.Sprintf("%06d", n)
		}
	}
	return id
}

// FormatLegacyID formats a legacy numeric ID as 6-digit zero-padded string.
func FormatLegacyID(id int64) string {
	return fmt.Sprintf("%06d", id)
}

// ParseLegacyID parses a legacy numeric issue ID string to int64.
func ParseLegacyID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid issue ID: %s", s)
	}
	return id, nil
}

// RefForXIDR returns the ref path for an open issue with XIDR.
func RefForXIDR(xidr string) string {
	return fmt.Sprintf("refs/issues/%s", xidr)
}

// ClosedRefForXIDR returns the ref path for a closed issue with XIDR.
func ClosedRefForXIDR(xidr string) string {
	return fmt.Sprintf("refs/closed/%s", xidr)
}

// IsClosedRef returns true if the ref is for a closed issue.
func IsClosedRef(ref string) bool {
	return strings.HasPrefix(ref, "refs/closed/")
}

// XIDRFromRef extracts the XIDR from a ref path.
func XIDRFromRef(ref string) (string, error) {
	if xidr, ok := strings.CutPrefix(ref, "refs/issues/"); ok {
		return xidr, nil
	}
	if xidr, ok := strings.CutPrefix(ref, "refs/closed/"); ok {
		return xidr, nil
	}
	return "", fmt.Errorf("invalid issue ref: %s", ref)
}

// IsLegacyRef returns true if the ref uses a legacy numeric ID (6 digits).
func IsLegacyRef(ref string) bool {
	xidr, err := XIDRFromRef(ref)
	if err != nil {
		return false
	}
	// Legacy IDs are 6-digit numeric strings
	if len(xidr) == 6 {
		_, err := strconv.ParseInt(xidr, 10, 64)
		return err == nil
	}
	return false
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

// FormatOneLiner renders an issue as the single colored line used by listings.
// The status comes from the ref the issue was read from when that says closed,
// which keeps a listing honest about an issue whose meta.tony disagrees.
func FormatOneLiner(issue *Issue) string {
	status := issue.Status
	if IsClosedRef(issue.Ref) {
		status = "closed"
	}
	return fmt.Sprintf("%s %s[%s]%s %s",
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
