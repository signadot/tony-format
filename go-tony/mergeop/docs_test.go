package mergeop

import (
	"os"
	"strings"
	"testing"
)

// docsPath is where the operator table lives, relative to this package.
const docsPath = "../../docs/matchpatch.md"

// tableOps reads the MergeOps table and answers what it says exists, and which
// column each row claims.
func tableOps(t *testing.T) map[string][2]bool {
	t.Helper()
	src, err := os.ReadFile(docsPath)
	if err != nil {
		t.Skipf("no %s to check against: %v", docsPath, err)
	}
	ops := map[string][2]bool{}
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 4 {
			continue
		}
		name := strings.TrimSpace(cols[1])
		if name == "" || name == "MergeOp" || strings.HasPrefix(name, "-") {
			continue
		}
		ops[name] = [2]bool{
			strings.TrimSpace(cols[2]) == "+",
			strings.TrimSpace(cols[3]) == "+",
		}
	}
	if len(ops) == 0 {
		t.Fatalf("no operator rows found in %s", docsPath)
	}
	return ops
}

// TestDocsTableMatchesTheRegistry: the table in matchpatch.md is what a person
// writes patterns from, so it has to be what the binary accepts.
//
// It had drifted both ways at once: thirteen registered operators were missing,
// and it documented "type", which nothing registers. A tag naming no operator is
// not an error anywhere -- SplitChild folds it into the node as data -- so a
// pattern written from that row constrained nothing and said so to nobody
// (issue cpccpt2rh12krjxafxn0).
//
// This is a test rather than a habit because the diff is mechanical: the binary
// prints the list with `o match -tags` and `o patch -tags`.
func TestDocsTableMatchesTheRegistry(t *testing.T) {
	documented := tableOps(t)

	registered := map[string][2]bool{}
	for _, s := range Symbols() {
		registered[s.String()] = [2]bool{s.IsMatch(), s.IsPatch()}
	}

	for name := range registered {
		if _, ok := documented[name]; !ok {
			t.Errorf("%s is registered and absent from the table in %s", name, docsPath)
		}
	}
	for name := range documented {
		if _, ok := registered[name]; !ok {
			t.Errorf("the table in %s documents %q, which no operator registers: a pattern "+
				"using it is read as data and constrains nothing", docsPath, name)
		}
	}
	for name, want := range registered {
		got, ok := documented[name]
		if !ok {
			continue // already reported
		}
		if got != want {
			t.Errorf("%s: the table says match=%v patch=%v, the registry says match=%v patch=%v",
				name, got[0], got[1], want[0], want[1])
		}
	}
}

// referencePath is the per-operation reference page. Unlike the table above it
// carries prose, so nothing can generate it from the registry -- a Symbol knows
// its name and whether it matches or patches, and no more.
const referencePath = "../../docs/generated/mergeop.md"

// TestReferenceCoversEveryOperation: the reference page documented 10 of the 33
// operations the library registers, and said so in a preamble rather than being
// completed. A page that names itself a reference and is missing two thirds of
// its subject is worse than a short one: a reader who finds !insert and !delete
// there reasonably concludes that !addtag and !comment do not exist.
//
// This does not check what each entry SAYS -- prose cannot be tested -- only
// that every registered operation has one, which is the part that goes stale
// silently every time an operation is added (3cdjz00jh12krns4g1n0 added
// !comment, and this page did not notice).
func TestReferenceCoversEveryOperation(t *testing.T) {
	src, err := os.ReadFile(referencePath)
	if err != nil {
		t.Skipf("no %s to check against: %v", referencePath, err)
	}
	text := string(src)

	var missing []string
	for _, s := range Symbols() {
		heading := "## `!" + s.String() + "`"
		if !strings.Contains(text, heading) {
			missing = append(missing, s.String())
		}
	}
	if len(missing) > 0 {
		t.Errorf("%s has no entry for %d of %d operations: %v",
			referencePath, len(missing), len(Symbols()), missing)
	}
}
