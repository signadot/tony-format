package stream

import (
	"bytes"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

func FuzzZZWireAgreesWithParser(f *testing.F) {
	for _, s := range []string{`{a b c}`, `{a, b}`, `{a b: 1}`, `{a !t b}`, `{1 2 3}`, `{a: {b c}}`, `[{a b}]`,
		`{a: !t}`, `{a: !t, b: 1}`, `[!t]`, `[1, !t]`, `[1,]`, `{a: 1,}`, "{a: !t\n}", "[!t # c\n, 1]",
		`{a: 1, b: [1, 2, {c: !t "x"}]}`, `[0x1f, -1.5, true, null, "s"]`, `{"q k": 'v', r: |
  x
}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if !utf8.ValidString(src) {
			return
		}
		trim := strings.TrimSpace(src)
		if !strings.HasPrefix(trim, "{") && !strings.HasPrefix(trim, "[") {
			return
		}
		var pw string
		n, perr := parse.Parse([]byte(src))
		if perr == nil && n != nil {
			var b bytes.Buffer
			if err := encode.Encode(n, &b, encode.EncodeWire(true)); err != nil {
				return
			}
			pw = b.String()
		}
		dec, _ := NewDecoder(strings.NewReader(src), WithBrackets())
		m, derr := ReadDocument(dec)
		if derr == nil {
			// a second document on the stream is the wire's business, not a disagreement
			for {
				ev, err := dec.ReadEvent()
				if err == io.EOF {
					break
				}
				if err != nil || (ev.Type != EventHeadComment && ev.Type != EventLineComment) {
					return
				}
			}
		}
		var dw string
		if derr == nil && m != nil {
			var b bytes.Buffer
			if err := encode.Encode(m, &b, encode.EncodeWire(true)); err != nil {
				return
			}
			dw = b.String()
		}
		if skip(src) {
			return
		}
		switch {
		case perr != nil && derr != nil:
		case perr == nil && derr == nil && pw == dw:
		case perr == nil && derr == nil && strings.Contains(pw, "!sparsearray") && sortedEq(pw, dw):
		default:
			t.Fatalf("%q\nparse:  %q %v\ndecode: %q %v", src, pw, perr, dw, derr)
		}
	})
}

// known classes, skipped to find the next
func skip(src string) bool {
	if regexp.MustCompile(`\d[eE.]`).MatchString(src) {
		return true
	}
	if regexp.MustCompile(`[^\s,\[{]\s+!\S*\s*[\]}]`).MatchString(src) {
		return true
	}
	if regexp.MustCompile(`(^|[{\[,:])\s*:`).MatchString(src) {
		return true
	}
	// mismatched closers: {] or [}
	depth := []byte{}
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch c {
		case '"', '\'':
			return false
		case ',', ':':
			if len(depth) == 0 {
				return true
			}
		case '{', '[':
			depth = append(depth, c)
		case '}', ']':
			if len(depth) == 0 {
				return true
			}
			o := depth[len(depth)-1]
			depth = depth[:len(depth)-1]
			if (o == '{') != (c == '}') {
				return true
			}
		}
	}
	return false
}

func sortedEq(a, b string) bool {
	x, y := []byte(a), []byte(b)
	slices.Sort(x)
	slices.Sort(y)
	return string(x) == string(y)
}
