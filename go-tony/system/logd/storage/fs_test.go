package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
)

func TestFS_FormatParseLogSegment(t *testing.T) {

	tests := []struct {
		name    string
		seg     *index.LogSegment
		pending bool
		want    string
	}{
		{
			name:    "point diff",
			seg:     index.PointLogSegment(100, 500, "foo/bar"),
			pending: false,
			want:    "foo/bar/c100-c500-0.diff",
		},
		{
			name:    "point pending",
			seg:     index.PointLogSegment(0, 500, "foo/bar"),
			pending: true,
			want:    "foo/bar/c500.pending",
		},
		{
			name: "compacted diff",
			seg: &index.LogSegment{
				StartCommit: 100, StartTx: 500,
				EndCommit: 116, EndTx: 516,
				RelPath: "foo",
			},
			pending: false,
			want:    "foo/c100.c500-c116.c516-0.diff",
		},
		{
			name:    "root path point",
			seg:     index.PointLogSegment(10, 20, ""),
			pending: false,
			want:    "b10-b20-0.diff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test Format
			got := paths.FormatLogSegment(tt.seg, 0, tt.pending)
			if got != tt.want {
				t.Errorf("FormatLogSegment() = %q, want %q", got, tt.want)
			}

			// Test Parse (round-trip)
			if !tt.pending { // Only test parsing for non-pending (pending has commit=0)
				parsed, _, err := paths.ParseLogSegment(got)
				if err != nil {
					t.Fatalf("ParseLogSegment() error = %v", err)
				}
				if diff := cmp.Diff(tt.seg, parsed); diff != "" {
					t.Errorf("ParseLogSegment() mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestFS_PathMapping(t *testing.T) {
	fs := &FS{Root: "/logd"}

	tests := []struct {
		virtual string
		fsPath  string
	}{
		{
			virtual: "/proc/processes",
			fsPath:  "/logd/paths/children/proc/children/processes",
		},
		{
			virtual: "/",
			fsPath:  "/logd/paths",
		},
		{
			virtual: "",
			fsPath:  "/logd/paths",
		},
		{
			virtual: "/foo",
			fsPath:  "/logd/paths/children/foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.virtual, func(t *testing.T) {
			// Test PathToFilesystem
			got := fs.PathToFilesystem(tt.virtual)
			if got != tt.fsPath {
				t.Errorf("PathToFilesystem(%q) = %q, want %q", tt.virtual, got, tt.fsPath)
			}

			// Test FilesystemToPath (round-trip)
			back := fs.FilesystemToPath(got)
			want := tt.virtual
			if want == "" {
				want = "/"
			}
			if back != want {
				t.Errorf("FilesystemToPath(%q) = %q, want %q", got, back, want)
			}
		})
	}
}

func TestFS_ListLogSegments(t *testing.T) {
	tmpDir := t.TempDir()
	fs := &FS{Root: tmpDir}

	// Create test structure
	testPath := "/test/path"
	fsPath := fs.PathToFilesystem(testPath)
	if err := os.MkdirAll(fsPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create some diff files
	createDiff := func(name string) {
		path := filepath.Join(fsPath, name)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	createDiff("c100-a500.diff")
	createDiff("c101-a501.diff")
	createDiff("c100.a500-c116.a516.diff") // compacted
	createDiff("a502.pending")             // should be included
	createDiff("readme.txt")               // should be ignored

	segments, err := fs.ListLogSegments(testPath)
	if err != nil {
		t.Fatalf("ListLogSegments() error = %v", err)
	}

	// Should have 4 segments (3 diffs + 1 pending)
	if len(segments) != 4 {
		t.Errorf("ListLogSegments() returned %d segments, want 4", len(segments))
	}

	// Verify sorting (should be by commit, then tx)
	if len(segments) >= 2 {
		if segments[0].StartCommit > segments[1].StartCommit {
			t.Errorf("segments not sorted by commit")
		}
	}
}

func TestFS_EnsurePathDir(t *testing.T) {
	tmpDir := t.TempDir()
	fs := &FS{Root: tmpDir}

	testPath := "/deep/nested/path"
	if err := fs.EnsurePathDir(testPath); err != nil {
		t.Fatalf("EnsurePathDir() error = %v", err)
	}

	// Verify directory exists
	fsPath := fs.PathToFilesystem(testPath)
	if _, err := os.Stat(fsPath); os.IsNotExist(err) {
		t.Errorf("EnsurePathDir() did not create directory %q", fsPath)
	}
}
