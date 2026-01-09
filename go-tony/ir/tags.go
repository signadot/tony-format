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

func TagCompose(tag string, args []string, oTag string) string {
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

func TagRemove(tag, what string) string {
	b := &strings.Builder{}
	for tag != "" {
		hd, args, rest := TagArgs(tag)
		tag = rest
		if hd == what {
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
			for _, arg := range args {
				if err := CheckTag("!" + arg); err != nil {
					return err
				}
			}
			if rest != "" {
				return CheckTag("!" + rest)
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
			if c == '[' || c == ']' {
				continue
			}
			return fmt.Errorf("invalid char: %c", c)
		}
	}
	if depth != 0 {
		return errors.New("imbalanced parentheses")
	}
	if tag != "" && head == "" {
		return errors.New("missing tag label")
	}
	return nil
}
