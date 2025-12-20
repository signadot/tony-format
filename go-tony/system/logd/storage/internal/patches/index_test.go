package patches

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

func TestBuildPatchIndex(t *testing.T) {
	// Create a patch with tagged roots
	patch1 := ir.FromMap(map[string]*ir.Node{
		"users": ir.FromMap(map[string]*ir.Node{
			"alice": ir.FromString("data").WithTag(tx.PatchRootTag),
		}),
	})

	patch2 := ir.FromMap(map[string]*ir.Node{
		"users": ir.FromMap(map[string]*ir.Node{
			"bob": ir.FromString("data").WithTag(tx.PatchRootTag),
		}),
	})

	entries := []*dlog.Entry{
		{Commit: 1, Patch: patch1},
		{Commit: 2, Patch: patch2},
	}

	index := BuildPatchIndex(entries)

	// Check alice path
	alicePatches := index.Lookup("users.alice")
	if len(alicePatches) != 1 {
		t.Errorf("expected 1 patch for users.alice, got %d", len(alicePatches))
	}
	if len(alicePatches) > 0 && alicePatches[0].Commit != 1 {
		t.Errorf("expected commit 1 for users.alice, got %d", alicePatches[0].Commit)
	}

	// Check bob path
	bobPatches := index.Lookup("users.bob")
	if len(bobPatches) != 1 {
		t.Errorf("expected 1 patch for users.bob, got %d", len(bobPatches))
	}
	if len(bobPatches) > 0 && bobPatches[0].Commit != 2 {
		t.Errorf("expected commit 2 for users.bob, got %d", bobPatches[0].Commit)
	}

	// Check non-existent path
	if index.HasPatches("users.charlie") {
		t.Error("expected no patches for users.charlie")
	}
}

func TestBuildPatchIndex_MultiplePatchesSamePath(t *testing.T) {
	// Two commits affecting the same path - must be applied in order
	patch1 := ir.FromMap(map[string]*ir.Node{
		"config": ir.FromString("v1").WithTag(tx.PatchRootTag),
	})
	patch2 := ir.FromMap(map[string]*ir.Node{
		"config": ir.FromString("v2").WithTag(tx.PatchRootTag),
	})

	entries := []*dlog.Entry{
		{Commit: 1, Patch: patch1},
		{Commit: 2, Patch: patch2},
	}

	index := BuildPatchIndex(entries)

	configPatches := index.Lookup("config")
	if len(configPatches) != 2 {
		t.Errorf("expected 2 patches for config, got %d", len(configPatches))
	}
	if len(configPatches) >= 2 {
		if configPatches[0].Commit != 1 || configPatches[1].Commit != 2 {
			t.Errorf("expected commits [1,2], got [%d,%d]", configPatches[0].Commit, configPatches[1].Commit)
		}
	}
}

func TestBuildPatchIndex_ArrayPath(t *testing.T) {
	patch := ir.FromSlice([]*ir.Node{
		ir.FromString("first"),
		ir.FromString("patched").WithTag(tx.PatchRootTag),
	})

	entries := []*dlog.Entry{
		{Commit: 1, Patch: patch},
	}

	index := BuildPatchIndex(entries)

	if !index.HasPatches("[1]") {
		t.Error("expected patch at [1]")
	}
	if index.HasPatches("[0]") {
		t.Error("expected no patch at [0]")
	}
}

func TestBuildPatchIndex_NilPatch(t *testing.T) {
	entries := []*dlog.Entry{
		{Commit: 1, Patch: nil},
	}

	index := BuildPatchIndex(entries)

	if len(index.Paths()) != 0 {
		t.Error("expected no paths for nil patch")
	}
}

func TestWalkIRTree(t *testing.T) {
	node := ir.FromMap(map[string]*ir.Node{
		"a": ir.FromMap(map[string]*ir.Node{
			"b": ir.FromInt(1),
		}),
		"c": ir.FromSlice([]*ir.Node{
			ir.FromInt(2),
			ir.FromInt(3),
		}),
	})

	var paths []string
	walkIRTree(node, "", func(n *ir.Node, path string) {
		paths = append(paths, path)
	})

	// Should visit: "", "a", "a.b", "c", "c[0]", "c[1]"
	expected := map[string]bool{
		"":     true,
		"a":    true,
		"a.b":  true,
		"c":    true,
		"c[0]": true,
		"c[1]": true,
	}

	if len(paths) != len(expected) {
		t.Errorf("expected %d paths, got %d: %v", len(expected), len(paths), paths)
	}

	for _, p := range paths {
		if !expected[p] {
			t.Errorf("unexpected path: %q", p)
		}
	}
}
