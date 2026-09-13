package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -w writes a file back in its own format: the one the flags name, else the one its
// extension names. A .json file came back in tony syntax under its own name
// (p478tacqh12krg32msn0 item 22).
func TestViewWriteKeepsTheFilesFormat(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, file, in, want string
		args                 []string
	}{
		{"json by extension", "d.json", `{"b":2,"a":1}`, "{\n  \"b\": 2,\n  \"a\": 1\n}\n", nil},
		{"yaml by extension", "d.yaml", "a:   1\nb: 2\n", "a: 1\nb: 2\n", nil},
		{"tony otherwise", "d.tony", "a:   1\n", "a: 1\n", nil},
		{"a flag names it", "d.txt", `{"a":1}`, "{\n  \"a\": 1\n}\n", []string{"-j"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.file)
			if err := os.WriteFile(path, []byte(tc.in), 0o644); err != nil {
				t.Fatal(err)
			}
			args := append(append([]string{}, tc.args...), "v", "-w", path)
			if code, out := runOIn(t, "", args...); code != 0 {
				t.Fatalf("exit %d: %s", code, out)
			}
			got, _ := os.ReadFile(path)
			if string(got) != tc.want {
				t.Errorf("file holds %q, want %q", got, tc.want)
			}
		})
	}

	// Flags naming two formats are refused with -w.
	path := filepath.Join(dir, "two.json")
	_ = os.WriteFile(path, []byte(`{"a":1}`), 0o644)
	code, _, errOut := runOBoth(t, "-I", "j", "-O", "y", "v", "-w", path)
	if code != 2 || !strings.Contains(errOut, "own format") {
		t.Errorf("-I j -O y -w: exit %d, stderr %q; want a refusal", code, errOut)
	}
	if got, _ := os.ReadFile(path); string(got) != `{"a":1}` {
		t.Errorf("the refused write changed the file: %q", got)
	}
}
