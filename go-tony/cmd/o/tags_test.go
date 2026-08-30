package main

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

// -tags answers with a tony document, so the answer is data the rest of the tool can
// read. A bulleted list could not be piped anywhere, and said only the names.
func TestTagsAnswersWithADocument(t *testing.T) {
	for _, cmd := range []string{"patch", "match"} {
		t.Run(cmd, func(t *testing.T) {
			code, out, errOut := runOBoth(t, cmd, "-tags")
			if code != 0 {
				t.Fatalf("exited %d: %s", code, errOut)
			}
			doc, err := parse.Parse([]byte(out))
			if err != nil {
				t.Fatalf("the answer does not parse as tony: %v\n%s", err, out)
			}
			// Every operation the command accepts is a field, and each says what it
			// does rather than only that it exists.
			for _, s := range mergeop.Symbols() {
				if cmd == "patch" && !s.IsPatch() || cmd == "match" && !s.IsMatch() {
					continue
				}
				v, err := doc.GetKPath(s.String())
				if err != nil || v == nil {
					t.Errorf("%s is not in the answer", s)
					continue
				}
				if strings.TrimSpace(v.String) == "" {
					t.Errorf("%s has no summary in the answer", s)
				}
			}
		})
	}
}

// The answer honours the output options, because it goes through the same encoder as
// everything else. -j is the cheap proof; colour is the reason it matters at a terminal.
func TestTagsHonoursTheOutputFormat(t *testing.T) {
	code, out, _ := runOBoth(t, "patch", "-tags", "-j")
	if code != 0 {
		t.Fatalf("exited %d", code)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "{") || !strings.Contains(out, `"insert"`) {
		t.Errorf("-j did not answer with json:\n%s", out[:min(len(out), 200)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
