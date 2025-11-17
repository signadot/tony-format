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
