package ir

import (
	"errors"
	"fmt"
	"strings"
)

func HeadTag(tag string) (string, string) {
	hd, args, rest := TagArgs(tag)
	if len(args) == 0 {
		return hd, rest
	}
	return hd + "(" + strings.Join(args, ",") + ")", rest
}

func TagArgs(tag string) (string, []string, string) {
	var (
		head, rest string
		args       []string
		n          = len(tag)
		c          byte
		depth      int
		open       int
		argStart   int
	)
	for i := 0; i < n; i++ {
		c = tag[i]
		//fmt.Printf("%d %c %d %d\n", i, c, depth, argStart)
		switch c {
		case '.':
			if depth != 0 {
				continue
			}
			if open != 0 {
				head = tag[:open]
			} else {
				head = tag[:i]
			}
			if i < n {
				rest = tag[i+1:]
			}
			return head, args, "!" + rest
		case '(':
			if depth == 0 {
				open = i
				argStart = i + 1
			}
			depth++
		case ')':
			depth--
			if depth != 0 {
				continue
			}
			if i != argStart && argStart != 0 {
				args = append(args, tag[argStart:i])
			}
			argStart = 0
		case ',':
			if depth != 1 {
				continue
			}
			if argStart != 0 {
				args = append(args, tag[argStart:i])
			}
			argStart = i + 1
		}
	}
	if rest != "" {
		rest = "!" + rest
	}
	if open != 0 {
		head = tag[:open]
	} else {
		head = tag
	}
	return head, args, rest
}

// TagArgOK reports whether arg can be written as a tag argument and read back as
// itself.
//
// A tag argument has no quoting, so the grammar's own characters are the limit: "("
// and ")" delimit an argument list and "," separates arguments.  An argument may
// still CONTAIN them, because an argument may be a tag -- !retag composes over
// `bracket.key(meta.name)` and !map takes `array(int)` -- as long as the
// parentheses balance and no comma sits outside them.  What cannot be written is a
// name holding an unbalanced parenthesis, a comma at the top level, or nothing at
// all.
//
// A caller which takes an argument from data -- a field name, say -- is the one
// which has to ask.  See TagCompose.
func TagArgOK(arg string) bool {
	if arg == "" {
		return false
	}
	depth := 0
	for i := 0; i < len(arg); i++ {
		switch arg[i] {
		case '(':
			depth++
		case ')':
			if depth--; depth < 0 {
				return false
			}
		case ',':
			if depth == 0 {
				return false
			}
		}
	}
	return depth == 0
}

// TagCompose builds a tag from a head, its arguments and the tag it composes over.
//
// Every argument must satisfy TagArgOK, and composing one which does not PANICS: a
// tag argument has no quoting, so `!key` over a field named "a,b" would be written
// `!key(a,b)` and read back as two arguments -- a tag which says something the
// caller did not. Checking what it hands over is the caller's part of this, and
// callers whose arguments come from data have to do it before calling; the panic is
// for the ones which do not, since a tag that quietly means something else is worse
// than a stopped program.
func TagCompose(tag string, args []string, oTag string) string {
	for _, arg := range args {
		if !TagArgOK(arg) {
			panic(fmt.Sprintf("ir.TagCompose(%q): argument %q cannot be written as a tag argument", tag, arg))
		}
	}
	headTag := tag
	if len(args) != 0 {
		headTag += "(" + strings.Join(args, ",") + ")"
	}
	if oTag != "" {
		return headTag + "." + oTag[1:]
	}
	return headTag
}

// TagHas: what should be ! prefixed
func TagHas(tag, what string) bool {
	for {
		if tag == "" {
			return false
		}
		hd, _, rest := TagArgs(tag)
		if hd == what {
			return true
		}
		tag = rest
	}
}

func TagGet(tag, what string) (string, []string) {
	if tag == "" {
		return "", nil
	}
	head, args, rest := TagArgs(tag)
	if head == what {
		return head, args
	}
	return TagGet(rest, what)
}

// presentationTags record how a value is written rather than what it is.  Two
// documents differing only in these hold the same data, so the operations that
// compare or combine data — patching, raw matching — drop them first, and the
// encoder consumes them instead of emitting them as tags.
//
// Keep this the single definition of the category.  It is consulted from
// several packages, and a tag that belongs here but is missing does not fail
// loudly: it surfaces as a spurious retag in a diff, or as "cannot encode tags
// in json" from the encoder.
var presentationTags = [...]string{BracketTag, LiteralTag, HexTag, OctTag, BinTag, ExpTag}

// IsPresentation reports whether a single tag label is a presentation tag.  The
// label is '!' prefixed and carries no arguments, which is what TagArgs yields
// for each label of a composed tag.
func IsPresentation(label string) bool {
	for _, p := range presentationTags {
		if label == p {
			return true
		}
	}
	return false
}

// StripPresentation removes every presentation label from a composed tag,
// leaving the labels that say what the value is.
func StripPresentation(tag string) string {
	return tagFilter(tag, IsPresentation)
}

func TagRemove(tag, what string) string {
	return tagFilter(tag, func(label string) bool { return label == what })
}

// tagFilter rebuilds tag without the labels drop selects, preserving each
// label's arguments and the '!' which only the first label of a composed tag
// carries.
func tagFilter(tag string, drop func(label string) bool) string {
	b := &strings.Builder{}
	for tag != "" {
		hd, args, rest := TagArgs(tag)
		tag = rest
		if drop(hd) {
			continue
		}
		if b.Len() != 0 {
			b.WriteByte('.')
			// Subsequent tags: strip ! prefix since only first tag has it
			if len(hd) > 0 && hd[0] == '!' {
				hd = hd[1:]
			}
		} else if len(hd) > 0 && hd[0] != '!' {
			// First remaining tag needs ! prefix
			b.WriteByte('!')
		}
		b.WriteString(hd)
		if len(args) == 0 {
			continue
		}
		b.WriteByte('(')
		for i, arg := range args {
			if i != 0 {
				b.WriteByte(',')
			}
			b.WriteString(arg)

		}
		b.WriteByte(')')
	}
	return b.String()
}

func CheckTag(tag string) error {
	var (
		head, rest string
		args       []string
		n          = len(tag)
		c          byte
		depth      int
		open       int
		argStart   int
	)
	for i := 0; i < n; i++ {
		c = tag[i]
		switch c {
		case '.':
			if depth != 0 {
				continue
			}
			if open != 0 {
				head = tag[:open]
			} else {
				head = tag[:i]
			}
			if i < n {
				rest = tag[i+1:]
			}
			// arguments and the labels after a '.' are written without the
			// '!', which only the whole tag's first label carries, so they
			// are checked as they stand.
			for _, arg := range args {
				if err := CheckTag(arg); err != nil {
					return err
				}
			}
			if rest != "" {
				return CheckTag(rest)
			}
			return nil
		case '(':
			if depth == 0 {
				open = i
				argStart = i + 1
			}
			depth++
		case ')':
			depth--
			if depth < 0 {
				return errors.New("mismatched parentheses")
			}
			if depth != 0 {
				continue
			}
			if i != argStart && argStart != 0 {
				args = append(args, tag[argStart:i])
			}
			argStart = 0
		case ',':
			if depth != 1 {
				continue
			}
			if argStart != 0 && argStart != i {
				args = append(args, tag[argStart:i])
			}
			argStart = i + 1
		default:
			if 'a' <= c && c <= 'z' {
				continue
			}
			if 'A' <= c && c <= 'Z' {
				continue
			}
			if '0' <= c && c <= '9' {
				continue
			}
			if c == '[' || c == ']' || c == '-' || c == '_' {
				continue
			}
			return fmt.Errorf("invalid char: %c", c)
		}
	}
	if depth != 0 {
		return errors.New("imbalanced parentheses")
	}
	if head == "" {
		// no '.' ended a label, so the whole tag is one, with its arguments
		if open != 0 {
			head = tag[:open]
		} else if tag != "" && tag[0] != '(' {
			head = tag
		}
		for _, arg := range args {
			if err := CheckTag(arg); err != nil {
				return err
			}
		}
	}
	if tag != "" && head == "" {
		return errors.New("missing tag label")
	}
	return nil
}
