package dirbuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func buildDir(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The build section is found by NAME. Checking Fields[0] meant a build file whose
// first field was anything else configured nothing at all -- no sources, no
// patches, no error, and a build which quietly produced an empty output.
func TestBuildSectionIsFoundWhereverItIs(t *testing.T) {
	root := buildDir(t, map[string]string{
		"build.tony": "note: something\nbuild:\n  sources:\n  - dir: ./src\n",
		"src/a.yaml": "a: 1\n",
	})
	d, err := OpenDir(root, nil)
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	if len(d.Sources) != 1 {
		t.Fatalf("%d sources, want 1: the build section was not found", len(d.Sources))
	}
}

// And a file with no build section says so, rather than building nothing.
func TestABuildFileWithoutABuildSectionIsAnError(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"only a comment", "# nothing here\n"},
		{"no build key", "other: 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked: %v", r)
				}
			}()
			root := buildDir(t, map[string]string{"build.tony": tc.content})
			if _, err := OpenDir(root, nil); err == nil {
				t.Error("accepted a build file with no build section")
			} else if !strings.Contains(err.Error(), "build:") {
				t.Errorf("error does not say what is missing: %v", err)
			}
		})
	}
}

// A comment before the build section is ordinary, and used to be fatal: taking
// Values[0] of a comment wrapper read off the end when the file was only a
// comment, and the panic came out of OpenDir.
func TestACommentBeforeTheBuildSectionIsFine(t *testing.T) {
	root := buildDir(t, map[string]string{
		"build.tony": "# what this builds\nbuild:\n  sources:\n  - dir: ./src\n",
		"src/a.yaml": "a: 1\n",
	})
	if _, err := OpenDir(root, nil); err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
}

// The env a caller passes is merged whether or not the build file declares one.
// The merge sat under `if dir.Env != nil`, so it was dropped exactly for the
// files which had no env: section of their own.
func TestPassedEnvSurvivesABuildFileWithNoEnv(t *testing.T) {
	root := buildDir(t, map[string]string{
		"build.tony": "build:\n  sources:\n  - dir: ./src\n",
		"src/a.yaml": "a: 1\n",
	})
	d, err := OpenDir(root, map[string]*ir.Node{"passed": ir.FromString("in")})
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	got := d.Env["passed"]
	if got == nil || got.String != "in" {
		t.Errorf("env = %v, want passed=in", d.Env)
	}
}

// .buildignore.tony ignores files, and the patterns are the point of it: an exact
// name was caught by a map lookup, so only the GLOBS failed, and only for files.
func TestBuildIgnoreAppliesToFiles(t *testing.T) {
	for _, tc := range []struct{ name, pattern string }{
		{"an exact name", "- gen.skip.yaml\n"},
		{"a glob", "- \"*.skip.yaml\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := buildDir(t, map[string]string{
				"build.tony":            "build:\n  sources:\n  - dir: ./src\n",
				"src/keep.yaml":         "keep: 1\n",
				"src/gen.skip.yaml":     "skipped: 1\n",
				"src/.buildignore.tony": tc.pattern,
			})
			d, err := OpenDir(root, nil)
			if err != nil {
				t.Fatalf("OpenDir: %v", err)
			}
			docs, err := d.fetch()
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if len(docs) != 1 {
				t.Fatalf("fetched %d documents, want 1 -- the ignored file was read", len(docs))
			}
			if ir.Get(docs[0], "keep") == nil {
				t.Error("the wrong document survived")
			}
		})
	}
}

// A name is a name: no newline in it, and no panic for a document which has no
// name of its own.
func TestFileNamesAreNames(t *testing.T) {
	d := &Dir{Output: &DirOutput{}}

	if got := d.fileName(ir.FromInt(42)); strings.ContainsAny(got, "\n\r") {
		t.Errorf("file name %q holds a newline", got)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked naming a document: %v", r)
		}
	}()
	wrapped := &ir.Node{
		Type:   ir.CommentType,
		Lines:  []string{"# lead"},
		Values: []*ir.Node{ir.FromMap(map[string]*ir.Node{"kind": ir.FromString("ConfigMap")})},
	}
	if got := d.fileName(wrapped); got == "" {
		t.Error("a commented document got no name")
	}
}

// A failing exec source says what the command said.
func TestExecSourceReportsStderr(t *testing.T) {
	root := buildDir(t, map[string]string{
		"build.tony": "build:\n  sources:\n  - exec: \"sh -c\"\n",
	})
	d, err := OpenDir(root, nil)
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	src := DirSource{Exec: ptr("sh /nonexistent-script-for-this-test")}
	_, err = src.Fetch(d.Root, nil)
	if err == nil {
		t.Fatal("a failing command was not reported")
	}
	if !strings.Contains(err.Error(), "nonexistent-script-for-this-test") {
		t.Errorf("the error says nothing about what happened: %v", err)
	}
}
