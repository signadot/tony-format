package mergeop

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every built-in operation has a summary, or `o patch -tags` lists it with nothing beside
// it. Coverage is the part that goes stale silently: an operation added to the registry
// works, and is listed, and says nothing about itself until someone notices.
// Only the built-in operations: a namespaced one belongs to a consumer, who describes it
// by implementing Summarized, and whose registration this package cannot audit.
func TestEveryOperationHasASummary(t *testing.T) {
	var missing []string
	for _, s := range Symbols() {
		if strings.Contains(s.String(), NamespaceSep) {
			continue
		}
		if strings.TrimSpace(Summary(s.String())) == "" {
			missing = append(missing, s.String())
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d of %d operations have no summary: %v", len(missing), len(Symbols()), missing)
	}
}

// And no summary belongs to an operation the registry does not hold, which is what a
// rename leaves behind.
func TestNoSummaryWithoutAnOperation(t *testing.T) {
	known := map[string]bool{}
	for _, s := range Symbols() {
		known[s.String()] = true
	}
	for name := range summaries {
		if !known[name] {
			t.Errorf("summary for %q, which the registry does not hold", name)
		}
	}
}

// The index page carries the same one-liners, and a reader who finds them differing has
// no way to tell which is current. They are written in two places because one is prose
// for the web and the other is what the tool prints; they are held together here.
//
// The comparison ignores markdown: the page writes `from:` where the tool writes from:.
func TestSummariesMatchTheIndexTable(t *testing.T) {
	src, err := os.ReadFile(indexPath)
	if err != nil {
		t.Skipf("no %s to check against: %v", indexPath, err)
	}
	row := regexp.MustCompile(`(?m)^\| ` + "`" + `!([^` + "`" + `]+)` + "`" + ` \| (.*?) \|\s*$`)
	seen := 0
	for _, m := range row.FindAllStringSubmatch(string(src), -1) {
		name, text := m[1], strings.ReplaceAll(m[2], "`", "")
		want := Summary(name)
		if want == "" {
			continue // an eval operation, or one this registry does not hold
		}
		seen++
		if text != want {
			t.Errorf("%s:\n  page says     %q\n  registry says %q", name, text, want)
		}
	}
	if seen != len(summaries) {
		t.Errorf("the page carries %d of the %d summaries", seen, len(summaries))
	}
}
