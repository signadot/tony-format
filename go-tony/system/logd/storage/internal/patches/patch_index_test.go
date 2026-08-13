package patches

import (
	"reflect"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

func parsePaths(t *testing.T, paths ...string) map[string]*kpath.KPath {
	t.Helper()
	parsed := make(map[string]*kpath.KPath, len(paths))
	for _, path := range paths {
		if path == "" {
			parsed[path] = nil
			continue
		}
		kp, err := kpath.Parse(path)
		if err != nil {
			t.Fatalf("parse %q: %v", path, err)
		}
		parsed[path] = kp
	}
	return parsed
}

func TestMaximalPaths(t *testing.T) {
	tests := []struct {
		name      string
		paths     []string
		apply     []string
		dominator map[string]string
	}{{
		// "a-b" sorts between "a" and "a.b" as a STRING ('-' is below '.'), and is
		// under neither. maximalPaths walks in segment order, where it does not.
		name:      "field that sorts between a path and its child",
		paths:     []string{"a", "a-b", "a.b"},
		apply:     []string{"a", "a-b"},
		dominator: map[string]string{"a.b": "a"},
	}, {
		name:      "the document root dominates everything",
		paths:     []string{"", "a", "a.b", "z[3]"},
		apply:     []string{""},
		dominator: map[string]string{"a": "", "a.b": "", "z[3]": ""},
	}, {
		name:      "a descendant several levels down is dominated by the maximal path",
		paths:     []string{"a", "a.b.c.d", "a.b.e"},
		apply:     []string{"a"},
		dominator: map[string]string{"a.b.c.d": "a", "a.b.e": "a"},
	}, {
		name:      "sparse and dense elements are dominated by their container",
		paths:     []string{"items", "items{1}.name", "items[0]", "other(k)"},
		apply:     []string{"items", "other(k)"},
		dominator: map[string]string{"items{1}.name": "items", "items[0]": "items"},
	}, {
		name:      "siblings are all maximal",
		paths:     []string{"a.x", "a.y", "b"},
		apply:     []string{"a.x", "a.y", "b"},
		dominator: map[string]string{},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apply, dominator := maximalPaths(parsePaths(t, tt.paths...))
			if !reflect.DeepEqual(apply, tt.apply) {
				t.Errorf("apply paths = %v, want %v", apply, tt.apply)
			}
			if !reflect.DeepEqual(dominator, tt.dominator) {
				t.Errorf("dominator = %v, want %v", dominator, tt.dominator)
			}
		})
	}
}

// rooted marks a node as a patch root, which is what the walk collects and what
// makes its path an apply path (or a dominated one).
func rooted(node *ir.Node) *ir.Node { return node.WithTag(tx.PatchRootTag) }

func TestBuildPatchValueIndex_DominatedRootsFoldOncePerEntry(t *testing.T) {
	// Entry 0 writes at "a", making it the apply path. Entry 1 writes at "a.b" and
	// "a.c" — two roots, both dominated by "a" — and must contribute the subtree at
	// "a" exactly once, not once per dominated root.
	entry0 := ir.FromMap(map[string]*ir.Node{
		"a": rooted(ir.FromMap(map[string]*ir.Node{"b": ir.FromInt(1)})),
	})
	entry1 := ir.FromMap(map[string]*ir.Node{
		"a": ir.FromMap(map[string]*ir.Node{
			"b": rooted(ir.FromInt(2)),
			"c": rooted(ir.FromInt(3)),
		}),
	})

	index, err := buildPatchValueIndex([]*ir.Node{entry0, entry1})
	if err != nil {
		t.Fatalf("buildPatchValueIndex: %v", err)
	}
	if len(index) != 1 {
		t.Fatalf("expected one apply path, got %v", keysOf(index))
	}
	if got := len(index["a"]); got != 2 {
		t.Fatalf("expected 2 contributions at %q, got %d", "a", got)
	}
	// The second contribution is entry 1's subtree at "a", carrying both writes.
	sub := index["a"][1]
	if b := findField(sub, "b"); b == nil || b.Int64 == nil || *b.Int64 != 2 {
		t.Errorf("expected b=2 in the folded subtree, got %v", sub)
	}
	if c := findField(sub, "c"); c == nil || c.Int64 == nil || *c.Int64 != 3 {
		t.Errorf("expected c=3 in the folded subtree, got %v", sub)
	}
}

func TestBuildPatchValueIndex_CommitOrder(t *testing.T) {
	// Three entries writing the same path: the contributions must be in the order
	// the entries commit, since they are applied in sequence.
	var patches []*ir.Node
	for i := int64(1); i <= 3; i++ {
		patches = append(patches, ir.FromMap(map[string]*ir.Node{
			"a": rooted(ir.FromInt(i)),
		}))
	}
	index, err := buildPatchValueIndex(patches)
	if err != nil {
		t.Fatalf("buildPatchValueIndex: %v", err)
	}
	got := index["a"]
	if len(got) != 3 {
		t.Fatalf("expected 3 contributions, got %d", len(got))
	}
	for i, node := range got {
		if node.Int64 == nil || *node.Int64 != int64(i+1) {
			t.Fatalf("contribution %d = %v, want %d", i, node, i+1)
		}
	}
}

func TestBuildPatchValueIndex_SparseRootIsNotNavigated(t *testing.T) {
	// A root at a sparse-index path contributes the node the walk found, not one
	// re-derived by navigating: GetKPath does not resolve "items{1}" back to it.
	entry := ir.FromMap(map[string]*ir.Node{
		"items": ir.FromIntKeysMap(map[uint32]*ir.Node{
			1: rooted(ir.FromString("B")),
		}),
	})
	index, err := buildPatchValueIndex([]*ir.Node{entry})
	if err != nil {
		t.Fatalf("buildPatchValueIndex: %v", err)
	}
	nodes, ok := index["items{1}"]
	if !ok {
		t.Fatalf("expected an apply path at %q, got %v", "items{1}", keysOf(index))
	}
	if len(nodes) != 1 || nodes[0].String != "B" {
		t.Fatalf("expected the walked node, got %v", nodes)
	}
}

func keysOf(m map[string][]*ir.Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
