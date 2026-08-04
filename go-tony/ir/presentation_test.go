package ir

import "testing"

func TestIsPresentation(t *testing.T) {
	tests := []struct {
		label string
		want  bool
	}{
		{BracketTag, true},
		{LiteralTag, true},
		{"!mytag", false},
		{KeyTag, false},
		{IntKeysTag, false},
		{"", false},
		// The label is '!' prefixed, as TagArgs yields it.
		{"bracket", false},
	}
	for _, tt := range tests {
		if got := IsPresentation(tt.label); got != tt.want {
			t.Errorf("IsPresentation(%q) = %v, want %v", tt.label, got, tt.want)
		}
	}
}

func TestStripPresentation(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{"", ""},
		{"!bracket", ""},
		{"!literal", ""},
		// Every presentation label goes, wherever it sits in the composition.
		{"!bracket.literal", ""},
		{"!mytag", "!mytag"},
		{"!mytag.bracket", "!mytag"},
		{"!bracket.mytag", "!mytag"},
		{"!bracket.mytag.literal", "!mytag"},
		{"!mytag.bracket.other", "!mytag.other"},
		// Arguments on the surviving labels are preserved.
		{"!key(name).bracket", "!key(name)"},
		{"!bracket.key(name)", "!key(name)"},
		{"!a(x,y).literal.b(z)", "!a(x,y).b(z)"},
		// The case mergeop and patch care about.
		{"!logd-patch-root.bracket", "!logd-patch-root"},
	}
	for _, tt := range tests {
		if got := StripPresentation(tt.tag); got != tt.want {
			t.Errorf("StripPresentation(%q) = %q, want %q", tt.tag, got, tt.want)
		}
	}
}

// TestStripPresentationCoversLiteral pins the behavior change that came with the category:
// patching and raw matching stripped BracketTag but not LiteralTag, though both record how
// a node was written rather than what it is. mergeop's dataTag said so in its doc comment
// while dropping only one of them.
func TestStripPresentationCoversLiteral(t *testing.T) {
	for _, tag := range presentationTags {
		if got := StripPresentation(tag); got != "" {
			t.Errorf("StripPresentation(%q) = %q, want %q", tag, got, "")
		}
		if got := StripPresentation("!data." + tag[1:]); got != "!data" {
			t.Errorf("StripPresentation(%q) = %q, want %q", "!data."+tag[1:], got, "!data")
		}
	}
}
