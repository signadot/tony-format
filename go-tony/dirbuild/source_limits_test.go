package dirbuild

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A url: or exec: source runs under a timeout the source sets (timeout:), 60s when
// it does not, and a run killed by it is reported as that -- the command, and how
// long it had. It was a hard-coded 10s reported as "signal: killed"
// (p478tacqh12krg32msn0 item 22).
func TestExecSourceTimeoutIsTheSourcesAndSaysSo(t *testing.T) {
	root := buildDir(t, map[string]string{
		"build.tony": "build:\n  sources:\n  - exec: \"sleep 5\"\n    timeout: 200ms\n",
	})
	d, err := OpenDir(root, nil)
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	started := time.Now()
	_, err = d.Run(nopWriterCloser{Writer: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("a command that outlives its timeout built without error")
	}
	if time.Since(started) > 3*time.Second {
		t.Errorf("the timeout of 200ms was not honoured: the run took %s", time.Since(started))
	}
	for _, want := range []string{"sleep 5", "200ms", "timeout"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}

	var s DirSource
	if d, err := s.fetchTimeout(); err != nil || d != DefaultFetchTimeout {
		t.Errorf("no timeout: -> %s, %v; want the default %s", d, err, DefaultFetchTimeout)
	}
	s.Timeout = "0s"
	if _, err := s.fetchTimeout(); err == nil {
		t.Error("a timeout of nothing was accepted")
	}
}

// A document's !filename names a file in output.destDir, not anywhere else: ../x and
// an absolute path wrote outside the directory (p478tacqh12krg32msn0 item 22).
func TestFilenameCannotLeaveTheDestination(t *testing.T) {
	for _, name := range []string{"../escaped", "/tmp/escaped", "sub/../../escaped"} {
		t.Run(name, func(t *testing.T) {
			root := buildDir(t, map[string]string{
				"build.tony": "build:\n  output: {destDir: ./out}\n  sources:\n  - dir: ./src\n",
				"src/a.tony": "!filename(" + name + ") {a: 1}\n",
			})
			if err := os.MkdirAll(filepath.Join(root, "out"), 0o755); err != nil {
				t.Fatal(err)
			}
			d, err := OpenDir(root, nil)
			if err != nil {
				t.Fatalf("OpenDir: %v", err)
			}
			_, err = d.Run(nopWriterCloser{Writer: &bytes.Buffer{}})
			if err == nil || !strings.Contains(err.Error(), "outside output.destDir") {
				t.Errorf("build with !filename(%s): err %v, want a refusal", name, err)
			}
			if _, statErr := os.Stat(filepath.Join(root, "escaped.tony")); statErr == nil {
				t.Errorf("a file was written outside the destination")
			}
		})
	}
	// A name within the directory is fine.
	root := buildDir(t, map[string]string{
		"build.tony": "build:\n  output: {destDir: ./out}\n  sources:\n  - dir: ./src\n",
		"src/a.tony": "!filename(kept) {a: 1}\n",
	})
	_ = os.MkdirAll(filepath.Join(root, "out"), 0o755)
	d, err := OpenDir(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Run(nopWriterCloser{Writer: &bytes.Buffer{}}); err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "out", "kept.tony")); err != nil {
		t.Errorf("the kept file was not written: %v", err)
	}
}
