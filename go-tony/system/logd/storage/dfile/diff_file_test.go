package dfile_test

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/dfile"
)

func TestWriteDiffFile(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		p       string
		df      *dfile.DiffFile
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotErr := dfile.WriteDiffFile(tt.p, tt.df)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("WriteDiffFile() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("WriteDiffFile() succeeded unexpectedly")
			}
		})
	}
}
