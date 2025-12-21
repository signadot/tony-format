package ir

import "testing"

func TestTags(t *testing.T) {
	var (
		head, rest string
		args       []string
	)
	head, args, rest = TagArgs("!a(1,2).b")
	t.Logf("%q %v %q", head, args, rest)
	head, args, rest = TagArgs("!a(b(c).d(e,f(1))).b")
	t.Logf("%q %v %q", head, args, rest)
	head, args, rest = TagArgs("!embed(X)")
	t.Logf("%q %v %q", head, args, rest)
}

func TestTagRemove(t *testing.T) {
	tests := []struct {
		tag    string
		remove string
		want   string
	}{
		// Remove first tag, remaining gets ! prefix
		{"!a.b", "!a", "!b"},
		{"!a.b.c", "!a", "!b.c"},
		// Remove with args
		{"!a(x).b", "!a", "!b"},
		{"!a.b(y)", "!a", "!b(y)"},
		{"!a(x,y).b(z)", "!a", "!b(z)"},
		// Remove non-existent tag (no match)
		{"!a.b", "!c", "!a.b"},
		// Single tag removal
		{"!a", "!a", ""},
		// Real-world case: logd-patch-root.bracket
		{"!logd-patch-root.bracket", "!logd-patch-root", "!bracket"},
	}
	for _, tt := range tests {
		got := TagRemove(tt.tag, tt.remove)
		if got != tt.want {
			t.Errorf("TagRemove(%q, %q) = %q, want %q", tt.tag, tt.remove, got, tt.want)
		}
	}
}
