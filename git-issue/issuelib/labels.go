package issuelib

import (
	"slices"
	"strings"
)

// Labels.
//
// A label is a lowercased, trimmed string. One containing "=" is a key and a
// value, split at the first "=", and a key holds one value: git-issue-phase=landed
// is the key git-issue-phase with the value landed. A label without "=" is plain,
// and plain labels are a set.
//
// The prefix "git-issue-" is reserved for conventions git-issue or a program
// driving it defines, such as a phase machine. git-issue gives no key under it a
// meaning of its own; what it provides is that a key holds one value, and that a
// merge of two chains decides each key by what each side changed (mergeLabels).
//
// A key with more than one value is a shape no client of this one writes. A
// merge made by an older git-issue, which unions labels, can leave one: the read
// keeps the last value in the list and warns (GetByRef), and setting the key
// again leaves the one value.

// ReservedLabelPrefix begins every label git-issue or a program driving it
// defines the meaning of.
const ReservedLabelPrefix = "git-issue-"

// NormalizeLabel is a label as it is stored: lowercased and trimmed.
func NormalizeLabel(label string) string {
	return strings.ToLower(strings.TrimSpace(label))
}

// SplitLabel answers a label's key and value, and whether it has them. A label
// without "=" is plain, and answers itself as the key.
func SplitLabel(label string) (key, value string, keyed bool) {
	return strings.Cut(label, "=")
}

// SetLabel adds label to the issue. Where it is key=value, any other value the
// issue held for the key is removed first, so the key holds this one.
func (i *Issue) SetLabel(label string) {
	if key, _, keyed := SplitLabel(label); keyed {
		i.Labels = slices.DeleteFunc(i.Labels, func(l string) bool {
			k, _, ok := SplitLabel(l)
			return ok && k == key && l != label
		})
	}
	if !slices.Contains(i.Labels, label) {
		i.Labels = append(i.Labels, label)
	}
}

// RemoveLabel removes label from the issue. A key given without "=" removes
// the key whatever its value, as well as a plain label of that name.
func (i *Issue) RemoveLabel(label string) {
	_, _, keyed := SplitLabel(label)
	i.Labels = slices.DeleteFunc(i.Labels, func(l string) bool {
		if l == label {
			return true
		}
		k, _, ok := SplitLabel(l)
		return !keyed && ok && k == label
	})
}

// singleValued answers labels with each key holding one value -- the last one
// the list gives it -- and, for every key that held more, all it held in list
// order. A list already in shape comes back as it is and with no keys.
func singleValued(labels []string) ([]string, map[string][]string) {
	values := map[string][]string{}
	for _, l := range labels {
		if k, _, ok := SplitLabel(l); ok && !slices.Contains(values[k], l) {
			values[k] = append(values[k], l)
		}
	}
	multi := map[string][]string{}
	for k, vs := range values {
		if len(vs) > 1 {
			multi[k] = vs
		}
	}
	if len(multi) == 0 {
		return labels, nil
	}
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if k, _, ok := SplitLabel(l); ok && len(multi[k]) > 0 && l != multi[k][len(multi[k])-1] {
			continue
		}
		if !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out, multi
}
