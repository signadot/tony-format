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
	return OpenPrefix + xidr
}

// ClosedRefForXIDR returns the ref path for a closed issue with XIDR.
func ClosedRefForXIDR(xidr string) string {
	return ClosedPrefix + xidr
}

// Gen0RefForXIDR and Gen0ClosedRefForXIDR are where the layout before this
// generation kept an issue. Nothing writes these; adoption reads them.
func Gen0RefForXIDR(xidr string) string {
	return Gen0OpenPrefix + xidr
}

func Gen0ClosedRefForXIDR(xidr string) string {
	return Gen0ClosedPrefix + xidr
}

// IsClosedRef returns true if the ref is for a closed issue, in either
// generation: status is the namespace, and gen0 had its own pair of them.
func IsClosedRef(ref string) bool {
	return strings.HasPrefix(ref, ClosedPrefix) || strings.HasPrefix(ref, Gen0ClosedPrefix)
}

// ExtRefForXIDR is where another repository's issue is mirrored here: under the
// name this repository gives that source.
func ExtRefForXIDR(source, xidr string) string {
	return ExtPrefix + source + "/" + xidr
}

// SourceRef is the ref that says where a source is.
func SourceRef(source string) string {
	return SourcesPrefix + source
}

// IsExtRef says the ref is a mirror of another repository's issue: read here,
// written there.
func IsExtRef(ref string) bool {
	return strings.HasPrefix(ref, ExtPrefix)
}

// ExtSource answers the source and the issue a mirror ref names.
func ExtSource(ref string) (source, xidr string, ok bool) {
	rest, isExt := strings.CutPrefix(ref, ExtPrefix)
	if !isExt {
		return "", "", false
	}
	source, xidr, ok = strings.Cut(rest, "/")
	return source, xidr, ok && source != "" && xidr != ""
}

// StatusOf is an issue's status as it should be read: the namespace for an issue
// of this repository, which cannot be wrong, and meta.tony for a mirror, which is
// what the source wrote and all this repository has.
func StatusOf(issue *Issue) string {
	switch {
	case IsClosedRef(issue.Ref):
		return "closed"
	case IsExtRef(issue.Ref) && issue.Status != "":
		return issue.Status
	}
	return "open"
}

// IsGen0Ref returns true if the ref is where the layout before this generation
// kept an issue.
func IsGen0Ref(ref string) bool {
	return strings.HasPrefix(ref, Gen0OpenPrefix) || strings.HasPrefix(ref, Gen0ClosedPrefix)
}

// XIDRFromRef extracts the XIDR from a local issue ref, of either generation. A
// tracking ref is not one: it names what a remote holds, and the code that reads
// those says so explicitly.
func XIDRFromRef(ref string) (string, error) {
	for _, prefix := range []string{OpenPrefix, ClosedPrefix, Gen0OpenPrefix, Gen0ClosedPrefix} {
		if xidr, ok := strings.CutPrefix(ref, prefix); ok {
			return xidr, nil
		}
	}
	if _, xidr, ok := ExtSource(ref); ok {
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
	status := StatusOf(issue)
	from := ""
	if source, _, ok := ExtSource(issue.Ref); ok {
		from = " " + ColorGray + "(ext: " + source + ")" + ColorReset
	}
	return fmt.Sprintf("%s %s[%s]%s %s%s",
		FormatID(issue.ID),
		StatusColor(status),
		status,
		ColorReset,
		issue.Title,
		from,
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
