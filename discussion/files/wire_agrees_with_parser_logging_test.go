package stream

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

var (
	zzMu    sync.Mutex
	zzSeen  = map[string]int{}
	zzLines int
	digits  = regexp.MustCompile(`[0-9]+`)
	alnum   = regexp.MustCompile(`[A-Za-z0-9_]+`)
)

func zzSig(err error) string {
	s := err.Error()
	if i := strings.IndexAny(s, "`\""); i >= 0 {
		s = s[:i]
	}
	return digits.ReplaceAllString(s, "N")
}

func zzLog(sig, src, pw, dw string, perr, derr error) {
	zzMu.Lock()
	defer zzMu.Unlock()
	if zzSeen[sig] >= 3 || zzLines >= 5000 {
		return
	}
	zzSeen[sig]++
	zzLines++
	f, err := os.OpenFile(os.Getenv("ZZ_OUT"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "SIG %s\nSRC %q\nPARSE %q %v\nWIRE %q %v\n\n", sig, src, pw, perr, dw, derr)
}

func FuzzZZWireAgreesWithParser(f *testing.F) {
	for _, s := range []string{`{a b c}`, `{a, b}`, `{a b: 1}`, `{a !t b}`, `{1 2 3}`, `{a: {b c}}`, `[{a b}]`,
		`{a: !t}`, `{a: !t, b: 1}`, `[!t]`, `[1, !t]`, `[1,]`, `{a: 1,}`, "{a: !t\n}", "[!t # c\n, 1]",
		`{a: 1, b: [1, 2, {c: !t "x"}]}`, `[0x1f, -1.5, true, null, "s"]`, "{\"q k\": 'v', r: |\n  x\n}",
		"{a: # c\n 1}", "[# h\n 1, 2 # l\n]", `{a: !key(id) [{id: 1}]}`, `{<<: x, a: .[y]}`, `{a: "é\n"}`,
		"{a: >\n  x\n  y\n}", `[[], {}, [[]], {a: {}}]`, `{0: a, 5: b}`, `[-0, 9223372036854775807, 1e400]`} {
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
			for {
				ev, err := dec.ReadEvent()
				if err == io.EOF {
					break
				}
				if err != nil || (ev.Type != EventHeadComment && ev.Type != EventLineComment) {
					// more after the document: compare as "wire accepts trailing"
					if perr != nil {
						zzLog("TRAILING-ON-WIRE/"+zzSig(perr), src, pw, "", perr, nil)
					}
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
		switch {
		case perr != nil && derr != nil:
		case perr == nil && derr == nil && pw == dw:
		case perr != nil:
			zzLog("WIRE-ACCEPTS/"+zzSig(perr), src, pw, dw, perr, derr)
		case derr != nil:
			zzLog("WIRE-REFUSES/"+zzSig(derr), src, pw, dw, perr, derr)
		default:
			sk := alnum.ReplaceAllString(src, "a")
			if len(sk) > 24 {
				sk = sk[:24]
			}
			zzLog("DIFFER/"+sk, src, pw, dw, perr, derr)
		}
	})
}
