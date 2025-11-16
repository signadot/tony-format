package token

import (
	"unicode/utf8"
)

func yamlPlain(d []byte, ts *tkState, off int, pd *PosDoc) (int, error) {
	//fmt.Printf("yamlPlain on `%s`\n", d)
	i := 0
	n := len(d)
	var lastR rune
	lastSz := 0
	lnIndent := ts.lnIndent
	if ts.kvSep {
		lnIndent += 2
	}
	lnIndent += 2 * ts.bElt
	inBrkts := ts.cb > 0 || ts.sb > 0
	// fmt.Printf("yaml plain: brkts=%t kvSep=%t lnIndent=%d\n", inBrkts, ts.kvSep, lnIndent)
	for i < n {
		// fmt.Printf("\tyaml plain: (last %q) `%s...`\n", string(lastR), d[i:min(i+10, n)])
		r, sz := utf8.DecodeRune(d[i:])
		i += sz
		//fmt.Printf("yplain %q %d/%d\n", string(r), i, n)
		switch r {
		case ' ', '\t':
			if lastR == ':' {
				i -= 2
				goto done
			}
		case '#':
			if lastR == ' ' || i == 0 {
				i -= (sz + lastSz)
				goto done
			}
		case '[', ']', '{', '}', ',':
			if inBrkts {
				i--
				goto done
			}
		case '?', '-':
			for j := i - 1; j >= 0; j-- {
				if d[j] == '\n' {
					i = max(0, j-1)
					if i <= lnIndent {
						goto done
					}
				}
				if d[j] == ' ' {
					continue
				}
				break
			}
			if i != 1 {
				continue
			}
			if len(d) == 1 {
				return 0, ErrYAMLPlain
			}
			switch d[1] {
			case ' ', '\t', '\r', '\n':
				return 0, ErrYAMLPlain
			}

		case '\n':
			if lastR == ':' {
				i -= 2
				goto done
			}
			pd.nl(i + off - 1)
			if inBrkts {
				goto done
			}
			if lnIndent == 0 {
				continue
			}
			j := i
			// fmt.Printf("\t\ti=%d j=%d off=%d\n", i, j, off)
			for j < n {
				if d[j] != ' ' {
					break
				}
				j++
			}

			// fmt.Printf("\t\ti=%d j=%d off=%d\n", i, j, off)

			if j-i < lnIndent-1 {
				i--
				goto done
			}
			// fmt.Printf("\t\t\ti=%d j=%d off=%d `%s`\n", i, j, off, d[i-1:j])
			i = j

		// "reserved" etc
		case '@', '`', '%', '!':
			if i == 1 {
				i--
				goto done
			}

		default:
			if printable(r) {
				switch r {
				case 0xfeff, '\r':
					i--
					goto done
				}
			} else {
				i--
				goto done
			}
		}
		lastR = r
		lastSz = sz
	}
done:
	if i <= 0 {
		return 0, ErrYAMLPlain
	}
	j := i - 1
	for j >= 0 {
		switch d[j] {
		case ' ', '\t', '\v', '\r':
			j--
		default:
			goto doneTrim

		}
	}
doneTrim:
	i = j + 1
	return i, nil
}

// either return a Literal or String,
// with preference for Literal when
// it fits.
func yamlPlainToken(d []byte, pos *Pos) *Token {
	folded := make([]byte, 0, len(d))
	//fmt.Printf("yplain token\n")
	lastNL := false
	mLastNL := false
	chomp := false
	for i, c := range d {
		//fmt.Printf("\t%d: %q\n", i, string(c))
		lastNL = i != 0 && d[i-1] == '\n'
		switch c {
		case '\n':
			if lastNL {
				folded = append(folded, c)
				mLastNL = true
			}
		case ' ', '\t', '\r', '\b':
			if lastNL && !mLastNL {
				chomp = true
			} else if !chomp {
				folded = append(folded, ' ')
			}
		default:
			mLastNL = false
			if chomp {
				folded = append(folded, ' ')
			}
			chomp = false
			folded = append(folded, c)
		}
	}
	n := len(folded) - 1
	for n >= 0 {
		switch folded[n] {
		case ' ', '\t', '\r', '\v':
		default:
			goto chompEnd
		}
		n--
	}
chompEnd:
	n++
	//
	return litOrString(folded[:n], pos)
}

func printable(r rune) bool {
	switch r {
	case '\t', '\r', '\n':
		return true
	}
	switch {
	case 0x20 <= r && r <= 0x7e:
		return true
	case 0xa0 <= r && r <= 0xd7ff:
		return true
	case 0xe000 <= r && r <= 0xfffd:
		return true
	case 0x010000 <= r && r <= 0x10FFFF:
		return true
	case r == 0x85:
		return true
	}
	return false
}
