package ir

import "testing"

// A tag argument has no quoting, so what a caller hands over has to be writable as
// one.  TagCompose refuses rather than emitting a tag which says something else:
// `!key` over a field named "a,b" reads back as two arguments, and over "a)b" as a
// truncated one.  b6ad0qw0h12krhk5gdn0.
func TestTagArgOK(t *testing.T) {
	for _, arg := range []string{
		"id", "has.dot", "has space", `has"quote`, "has{brace}", "has[bracket]", "*",
		"üñïçø∂é", "-1", "1.5",
		// an argument may be a tag, which is why parentheses are allowed at all
		"array(int)", "bracket.key(meta.name)", "map(string,array(int))", "a(b(c))",
	} {
		if !TagArgOK(arg) {
			t.Errorf("%q is refused as a tag argument, and is writable", arg)
			continue
		}
		tag := TagCompose("!key", []string{arg}, "")
		head, args, rest := TagArgs(tag)
		if head != "!key" || rest != "" || len(args) != 1 || args[0] != arg {
			t.Errorf("%q -> %q -> head=%q args=%q rest=%q", arg, tag, head, args, rest)
		}
	}

	for _, arg := range []string{"", "a,b", ",", "a)b", ")", "a(b", "(", "a(b,c"} {
		if TagArgOK(arg) {
			t.Errorf("%q is accepted as a tag argument, and cannot be written as one", arg)
		}
	}
}

// The refusal is a panic, since a tag which quietly means something else is worse
// than a stopped program, and a caller whose argument comes from data is the one
// which has to check first.
func TestTagComposeRefusesWhatItCannotWrite(t *testing.T) {
	for _, arg := range []string{"", "a,b", "a)b", "a(b"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("TagCompose accepted %q", arg)
				}
			}()
			_ = TagCompose("!key", []string{arg}, "")
		}()
	}
}
