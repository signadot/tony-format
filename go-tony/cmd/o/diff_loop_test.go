package main

import (
	"strings"
	"testing"
)

// A looped command that fails, or prints nothing, is a missed iteration: said on stderr,
// counted against -loopLim, and the loop goes round with the last document kept. It was a
// nil document handed to the differ, and a panic, at the moment a watch on a flaky command
// was worth having (02sa60w3h12kry6zm5n0).
func TestDiffLoopMissesAFailedOrEmptyRun(t *testing.T) {
	for _, tc := range []struct{ name, cmd, missed string }{
		{"a command that prints nothing", "true", "wrote no document"},
		{"a command that fails", "false", "exited with an error"},
		{"a command that prints only a comment", "echo '# nothing yet'", "wrote no document"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runOBoth(t, "d", "-loop", tc.cmd, "-loopEvery", "10ms", "-loopLim", "2")
			if code != 0 {
				t.Fatalf("exit %d, stderr:\n%s", code, errOut)
			}
			if out != "" {
				t.Errorf("a missed iteration wrote a difference:\n%s", out)
			}
			if n := strings.Count(errOut, "missed"); n != 2 || !strings.Contains(errOut, tc.missed) {
				t.Errorf("stderr should say both iterations were missed and why:\n%s", errOut)
			}
		})
	}
}

// A missed iteration keeps the last document, so the next real one is diffed against
// what was last seen and not against nothing; and it is neither a match nor a mismatch
// for -loopUntil.
func TestDiffLoopKeepsTheLastDocumentAcrossAMiss(t *testing.T) {
	dir := t.TempDir()
	counter := dir + "/n"
	// Prints {n: 1}, then nothing, then {n: 2}: the second run is a miss.
	script := `f=` + counter + `; c=$(cat $f 2>/dev/null || echo 0); c=$((c+1)); echo $c > $f; ` +
		`case $c in 2) exit 0;; *) echo "{n: $((c > 2 ? 2 : c))}";; esac`
	code, out, errOut := runOBoth(t, "d", "-loop", script, "-loopEvery", "10ms", "-loopUntil", "{n: 2}", "-loopLim", "5")
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
	if strings.Count(errOut, "missed") != 1 {
		t.Errorf("one miss expected on stderr:\n%s", errOut)
	}
	// The first difference is the whole document arriving; the second is n going 1 to 2,
	// diffed against the run BEFORE the miss.
	if strings.Count(out, "difference found") != 2 || !strings.Contains(out, "n: !replace") && !strings.Contains(out, "n: 2") {
		t.Errorf("expected two differences, the second n: 1 -> 2 across the miss:\n%s", out)
	}
}
