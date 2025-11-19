package dirbuild

import (
	"os"
	"testing"
	"bytes"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/gomap"
)

func TestDirGoMap(t *testing.T) {
	dir := &Dir{
		Sources: []DirSource{
			{
				Dir: ptr("srcDir"),
			},
		},
		Patches: []DirPatch{
			{
				If: "zoo",
				Match: ir.Null().WithTag("!pass"),
				Patch: ir.FromSlice([]*ir.Node{
					ir.FromBool(true),
				}),
			},
		},
		DestDir: "destDir",
		Env: map[string]any{
			"fred": ir.FromMap(map[string]*ir.Node{
				"barney": ir.FromString("wilma"),
			},
			),
		},
	}
	_ = dir
	n, err := gomap.ToIR(dir)
	if err != nil {
		t.Error(err)
		return
	}
	altDir := &Dir{}
	if err := gomap.FromIR(n, altDir); err != nil {
		t.Error(err)
		return
	}
	back, err := gomap.ToIR(altDir)
	if err != nil {
		t.Error(err)
		return
	}
	buf1 := bytes.NewBuffer(nil)
	if err := encode.Encode(n, buf1); err != nil {
		t.Error(err)
		return
	}
	buf2 := bytes.NewBuffer(nil)
	if err := encode.Encode(back, buf2); err != nil {
		t.Error(err)
		return
	}
	if bytes.Equal(buf1.Bytes(), buf2.Bytes()) {
		return
	}
	encode.Encode(n, os.Stdout)
	encode.Encode(back, os.Stdout)
	t.Errorf("mismatch")
}

func ptr[T any](v T) *T { return &v }
