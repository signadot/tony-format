package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Documents from several inputs are each preceded by a comment naming the input.
// JSON has no comments, so in JSON nothing names it: the line made the output
// something `o -j` could not read back (p478tacqh12krg32msn0 item 21).
func TestJSONOutputCarriesNoProvenanceComment(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	for p, c := range map[string]string{a: `{"x": 1}`, b: `{"x": 2}`} {
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, out := runOIn(t, "", "-j", "get", ".x", a, b)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if strings.Contains(out, "# from") {
		t.Errorf("JSON output carries a comment:\n%s", out)
	}
	// And o reads its own output back.
	code, back := runOIn(t, out, "-j", "v")
	if code != 0 {
		t.Errorf("o -j does not read the output back: exit %d: %s\noutput was:\n%s", code, back, out)
	}

	// In tony the input is still named.
	code, out = runOIn(t, "", "get", ".x", a, b)
	if code != 0 || !strings.Contains(out, "# from "+b) {
		t.Errorf("tony output does not name the second input (exit %d):\n%s", code, out)
	}
}
