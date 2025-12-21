package tx

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

func TestTagPatchRoots(t *testing.T) {
	tests := []struct {
		name       string
		patches    []*PatcherData
		wantTags   []string // expected tags after TagPatchRoots
	}{
		{
			name:     "empty patches",
			patches:  nil,
			wantTags: nil,
		},
		{
			name: "single patch no existing tag",
			patches: []*PatcherData{
				testPatcherData("a.b", ir.FromString("value")),
			},
			wantTags: []string{PatchRootTag},
		},
		{
			name: "single patch with existing tag",
			patches: []*PatcherData{
				testPatcherData("a.b", ir.FromString("value").WithTag("!existing")),
			},
			wantTags: []string{PatchRootTag + ".existing"},
		},
		{
			name: "multiple patches",
			patches: []*PatcherData{
				testPatcherData("a", ir.FromInt(1)),
				testPatcherData("b", ir.FromInt(2)),
			},
			wantTags: []string{PatchRootTag, PatchRootTag},
		},
		{
			name: "nil data node",
			patches: []*PatcherData{
				{API: &api.Patch{Patch: api.Body{Path: "a", Data: nil}}},
			},
			wantTags: []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			TagPatchRoots(tt.patches)

			for i, pd := range tt.patches {
				var gotTag string
				if pd.API.Patch.Data != nil {
					gotTag = pd.API.Patch.Data.Tag
				}
				if i < len(tt.wantTags) && gotTag != tt.wantTags[i] {
					t.Errorf("patch %d: got tag %q, want %q", i, gotTag, tt.wantTags[i])
				}
			}
		})
	}
}

func TestHasPatchRootTag(t *testing.T) {
	tests := []struct {
		name string
		node *ir.Node
		want bool
	}{
		{
			name: "nil node",
			node: nil,
			want: false,
		},
		{
			name: "no tag",
			node: ir.FromInt(1),
			want: false,
		},
		{
			name: "has patch root tag",
			node: ir.FromInt(1).WithTag(PatchRootTag),
			want: true,
		},
		{
			name: "patch root tag composed with other",
			node: ir.FromInt(1).WithTag(PatchRootTag + ".other"),
			want: true,
		},
		{
			name: "different tag",
			node: ir.FromInt(1).WithTag("!other"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasPatchRootTag(tt.node); got != tt.want {
				t.Errorf("HasPatchRootTag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStripPatchRootTag(t *testing.T) {
	tests := []struct {
		name    string
		node    *ir.Node
		wantTag string
	}{
		{
			name:    "nil node",
			node:    nil,
			wantTag: "",
		},
		{
			name:    "no tag",
			node:    ir.FromInt(1),
			wantTag: "",
		},
		{
			name:    "only patch root tag",
			node:    ir.FromInt(1).WithTag(PatchRootTag),
			wantTag: "",
		},
		{
			name:    "patch root with other tag",
			node:    ir.FromInt(1).WithTag(PatchRootTag + ".other"),
			wantTag: "!other", // TagRemove preserves ! prefix on remaining tag
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			StripPatchRootTag(tt.node)
			var gotTag string
			if tt.node != nil {
				gotTag = tt.node.Tag
			}
			if gotTag != tt.wantTag {
				t.Errorf("after StripPatchRootTag: got tag %q, want %q", gotTag, tt.wantTag)
			}
		})
	}
}

// testPatcherData is a helper to create PatcherData for testing
func testPatcherData(path string, data *ir.Node) *PatcherData {
	return &PatcherData{
		ReceivedAt: time.Now(),
		API: &api.Patch{
			Patch: api.Body{
				Path: path,
				Data: data,
			},
		},
	}
}
